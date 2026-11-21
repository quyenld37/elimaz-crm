package files_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
)

// testExportPurpose: tệp xuất thử ghi từ máy chủ (như tệp xuất M6), chỉ có trong sổ của test và dòng thử của bảng.
func testExportPurpose() files.Purpose {
	return files.Purpose{Key: "test_export", Bucket: files.BucketPrivate, AllowedTypes: []string{"text/csv"}, MaxBytes: mib}
}

// addPurposeRow chèn dòng file_purposes bằng chủ bảng (mốc sau làm bằng một dòng INSERT trong migration).
func (e *env) addPurposeRow(key, bucket, mode string) error {
	_, err := e.staff.Owner.Pool.Exec(e.t.Context(),
		"INSERT INTO platform.file_purposes (key, bucket, mode) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING", key, bucket, mode)
	return err //nolint:wrapcheck // test
}

func (e *env) register(tenant uuid.UUID, f files.NewFile) (uuid.UUID, error) {
	var id uuid.UUID
	err := e.inTenant(tenant, func(tx pgx.Tx) error {
		var err error
		id, err = files.Register(e.t.Context(), tx, f)
		return err //nolint:wrapcheck // test
	})
	return id, err
}

func (e *env) downloadURL(store files.FileStore, tenant, id uuid.UUID, ttl time.Duration) (string, error) {
	var url string
	err := e.inTenant(tenant, func(tx pgx.Tx) error {
		var err error
		url, err = files.DownloadURL(e.t.Context(), tx, store, id, ttl, "bao-cao.csv")
		return err //nolint:wrapcheck // test
	})
	return url, err
}

func TestRegister_ServerWrittenFile(t *testing.T) {
	for name, store := range stores(t) {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			files.RegisterPurpose(testExportPurpose())
			if err := e.addPurposeRow("test_export", "private", "standard"); err != nil {
				t.Fatal(err)
			}
			body := randomBytes(t, 10*1024)
			id, key := e.putAndRegister(store, body, nil)
			e.checkReRegisterAndNoOverwrite(store, id, key, body)
			e.checkDownload(store, id, body)
			past := time.Now().Add(-time.Minute)
			oldID, _ := e.putAndRegister(store, body, &past)
			if _, err := e.downloadURL(store, e.a, oldID, time.Minute); !errors.Is(err, files.ErrExpired) {
				t.Fatalf("tệp đã hết hạn: muốn ErrExpired, nhận %v", err)
			}
			e.checkMarkDeleted(store, id, key)
		})
	}
}

// putAndRegister: Put 10 KB vào priv/<A>/<id> (ngoài transaction) rồi Register → ready.
func (e *env) putAndRegister(store files.FileStore, body []byte, expires *time.Time) (uuid.UUID, string) {
	e.t.Helper()
	sum := sha256.Sum256(body)
	id := uuid.Must(uuid.NewV7())
	key := files.ServerKey(e.a, id)
	if err := store.Put(e.t.Context(), files.BucketPrivate, key, bytes.NewReader(body), int64(len(body)), "text/csv"); err != nil {
		e.t.Fatal(err)
	}
	got, err := e.register(e.a, files.NewFile{
		ID: id, Purpose: "test_export", Key: key, SHA256: sum[:], Size: int64(len(body)),
		ContentType: "text/csv", FileName: "bao-cao.csv", CreatedBy: &e.actor, ExpiresAt: expires,
	})
	if err != nil || got != id {
		e.t.Fatalf("Register: %v %v", got, err)
	}
	e.must(e.a, func(tx pgx.Tx) error {
		st, err := files.Status(e.t.Context(), tx, id)
		if err != nil || st.Status != files.StatusReady || st.Purpose != "test_export" || st.Deleted {
			e.t.Errorf("Status: %+v %v", st, err)
		}
		return nil
	})
	return id, key
}

// Register lần hai cùng khóa (id mới) → id cũ; Put lên khóa đã có → ErrObjectExists, nội dung không đổi.
func (e *env) checkReRegisterAndNoOverwrite(store files.FileStore, id uuid.UUID, key string, body []byte) {
	e.t.Helper()
	again, err := e.register(e.a, files.NewFile{
		ID: uuid.Must(uuid.NewV7()), Purpose: "test_export", Key: key,
		Size: int64(len(body)), ContentType: "text/csv", FileName: "bao-cao.csv",
	})
	if err != nil || again != id {
		e.t.Fatalf("Register lần hai cùng khóa: %v %v, muốn %v", again, err, id)
	}
	err = store.Put(e.t.Context(), files.BucketPrivate, key, bytes.NewReader(randomBytes(e.t, 10)), 10, "text/csv")
	if !errors.Is(err, files.ErrObjectExists) {
		e.t.Fatalf("Put lên khóa đã có: muốn ErrObjectExists, nhận %v", err)
	}
	if !bytes.Equal(readAll(e.t, store, files.BucketPrivate, key), body) {
		e.t.Fatal("Put lên khóa đã có đã đổi nội dung")
	}
}

// DownloadURL đọc được đúng byte; tenant B → ErrNotFound.
func (e *env) checkDownload(store files.FileStore, id uuid.UUID, body []byte) {
	e.t.Helper()
	url, err := e.downloadURL(store, e.a, id, time.Minute)
	if err != nil {
		e.t.Fatal(err)
	}
	if code, got := get(e.t, url); code != http.StatusOK || !bytes.Equal(got, body) {
		e.t.Fatalf("đọc đường dẫn tải: %d, %d byte", code, len(got))
	}
	if _, err := e.downloadURL(store, e.b, id, time.Minute); !errors.Is(err, files.ErrNotFound) {
		e.t.Fatalf("tenant B DownloadURL tệp của A: muốn ErrNotFound, nhận %v", err)
	}
}

// MarkDeleted → DownloadURL ErrNotFound, Status báo Deleted, kho vẫn giữ đối tượng.
func (e *env) checkMarkDeleted(store files.FileStore, id uuid.UUID, key string) {
	e.t.Helper()
	ctx := e.t.Context()
	e.must(e.a, func(tx pgx.Tx) error { return files.MarkDeleted(ctx, tx, id, e.actor) })
	if _, err := e.downloadURL(store, e.a, id, time.Minute); !errors.Is(err, files.ErrNotFound) {
		e.t.Fatalf("tệp đã xóa: muốn ErrNotFound, nhận %v", err)
	}
	e.must(e.a, func(tx pgx.Tx) error {
		st, err := files.Status(ctx, tx, id)
		if err != nil || !st.Deleted {
			e.t.Errorf("Status sau MarkDeleted: %+v %v", st, err)
		}
		return nil
	})
	if _, err := store.Head(ctx, files.BucketPrivate, key); err != nil {
		e.t.Fatalf("MarkDeleted không được xóa trên kho: %v", err)
	}
}

// Register kiểm mục đích, giới hạn và khóa trước khi chèn.
func TestRegister_Refusals(t *testing.T) {
	e := newEnv(t)
	files.RegisterPurpose(testExportPurpose())
	files.RegisterPurpose(testPrivateImagePurpose())
	for _, row := range [][3]string{{"test_export", "private", "standard"}, {"test_private_image", "private", "private"}} {
		if err := e.addPurposeRow(row[0], row[1], row[2]); err != nil {
			t.Fatal(err)
		}
	}
	id := uuid.Must(uuid.NewV7())
	ok := files.NewFile{ID: id, Purpose: "test_export", Key: files.ServerKey(e.a, id), Size: 10, ContentType: "text/csv", FileName: "a.csv"}
	cases := []struct {
		name string
		edit func(*files.NewFile)
		want error
	}{
		{"mục đích chưa đăng ký", func(f *files.NewFile) { f.Purpose = "khong_co" }, files.ErrUnknownPurpose},
		{"mục đích chế độ private", func(f *files.NewFile) { f.Purpose = "test_private_image"; f.ContentType = "image/jpeg" }, files.ErrPurposeMode},
		// Ảnh chế độ standard cũng phải qua imager (ADR-45): Register không được tạo ảnh ready chưa xử lý.
		{"mục đích ảnh chế độ standard", func(f *files.NewFile) { f.Purpose = "product_image"; f.ContentType = "image/jpeg" }, files.ErrPurposeMode},
		{"loại không cho phép", func(f *files.NewFile) { f.ContentType = "text/html" }, files.ErrTypeNotAllowed},
		{"quá lớn", func(f *files.NewFile) { f.Size = 2 * mib }, files.ErrTooLarge},
		{"khóa của tenant khác", func(f *files.NewFile) { f.Key = files.ServerKey(e.b, id) }, files.ErrInvalidFile},
		{"khóa không có tiền tố", func(f *files.NewFile) { f.Key = "orig/abc" }, files.ErrInvalidFile},
		{"mã băm sai độ dài", func(f *files.NewFile) { f.SHA256 = []byte{1, 2} }, files.ErrInvalidFile},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := ok
			c.edit(&f)
			if _, err := e.register(e.a, f); !errors.Is(err, c.want) {
				t.Fatalf("muốn %v, nhận %v", c.want, err)
			}
		})
	}
	if _, err := e.register(e.a, ok); err != nil {
		t.Fatalf("bản đúng phải được: %v", err)
	}
}

// Trạng thái chưa sẵn sàng hay bị từ chối không ký đường dẫn tải (R10).
func TestDownloadURL_NotReadyAndRejected(t *testing.T) {
	e := newEnv(t)
	store := filestest.MemFileStore(t)
	f, put := e.mustCreate(store, imageInput(e.a, 10))
	if _, err := e.downloadURL(store, e.a, f.ID, time.Minute); !errors.Is(err, files.ErrNotReady) {
		t.Fatalf("awaiting_upload: muốn ErrNotReady, nhận %v", err)
	}
	if code := putVia(t, put, randomBytes(t, 10)); code != http.StatusOK {
		t.Fatalf("PUT: %d", code)
	}
	if _, err := e.complete(store, e.a, f.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.downloadURL(store, e.a, f.ID, time.Minute); !errors.Is(err, files.ErrNotReady) {
		t.Fatalf("uploaded: muốn ErrNotReady, nhận %v", err)
	}
	g, _ := e.mustCreate(store, imageInput(e.a, 10))
	if err := store.Put(t.Context(), files.BucketTmp, g.Key, bytes.NewReader(randomBytes(t, 5)), 5, "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.complete(store, e.a, g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.downloadURL(store, e.a, g.ID, time.Minute); !errors.Is(err, files.ErrRejected) {
		t.Fatalf("rejected: muốn ErrRejected, nhận %v", err)
	}
	if _, err := e.downloadURL(store, e.a, uuid.Must(uuid.NewV7()), time.Minute); !errors.Is(err, files.ErrNotFound) {
		t.Fatalf("mã lạ: muốn ErrNotFound, nhận %v", err)
	}
}
