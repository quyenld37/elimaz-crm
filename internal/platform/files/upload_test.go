package files_test

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/testkit"
)

const mib = 1 << 20

// TP03 (phần kích thước): giới hạn theo mục đích, trước khi có dòng nào.
func TestCreateUpload_TooLarge(t *testing.T) {
	e := newEnv(t)
	store := testkit.MemFileStore(t)
	cases := []struct {
		name string
		in   files.UploadInput
		want error
	}{
		{"ảnh 25 MiB", imageInput(e.a, 25*mib), files.ErrTooLarge},
		{"svg", files.UploadInput{TenantID: e.a, Purpose: "product_image", FileName: "a.svg", ContentType: "image/svg+xml", SizeBytes: 100}, files.ErrTypeNotAllowed},
		{"excel 6 MiB", files.UploadInput{
			TenantID: e.a, Purpose: "catalog_import", FileName: "a.xlsx",
			ContentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", SizeBytes: 6 * mib,
		}, files.ErrTooLarge},
		{"mục đích chưa đăng ký", files.UploadInput{TenantID: e.a, Purpose: "khong_co", FileName: "a.jpg", ContentType: "image/jpeg", SizeBytes: 100}, files.ErrUnknownPurpose},
		{"kích thước 0", imageInput(e.a, 0), files.ErrInvalidFile},
		{"tên quá 255 ký tự", files.UploadInput{TenantID: e.a, Purpose: "product_image", FileName: strings.Repeat("a", 256), ContentType: "image/jpeg", SizeBytes: 100}, files.ErrInvalidFile},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, _, err := e.create(store, c.in); !errors.Is(err, c.want) {
				t.Fatalf("muốn %v, nhận %v", c.want, err)
			}
		})
	}
	var n int
	e.must(e.a, func(tx pgx.Tx) error { return tx.QueryRow(t.Context(), "SELECT count(*) FROM platform.files").Scan(&n) })
	if n != 0 {
		t.Fatalf("bị từ chối mà vẫn có %d dòng", n)
	} // Mutation testing (Task 403): tên đúng 255 ký tự (chữ có dấu, nhiều byte) được nhận.
	if _, _, err := e.create(store, files.UploadInput{
		TenantID: e.a, Purpose: "product_image", FileName: strings.Repeat("ả", 251) + ".jpg", ContentType: "image/jpeg", SizeBytes: 100,
	}); err != nil {
		t.Fatalf("tên đúng 255 ký tự: %v", err)
	}
}

// Mục đích không cho nhân viên tải lên (Upload = false) → ErrUploadNotAllowed. Mục đích thử, sổ riêng của test.
func TestCreateUpload_PurposeWithoutUpload(t *testing.T) {
	e := newEnv(t)
	files.RegisterPurpose(files.Purpose{
		Key: "test_server_only", Bucket: files.BucketPrivate,
		AllowedTypes: []string{"application/pdf"}, MaxBytes: mib,
	})
	in := files.UploadInput{TenantID: e.a, Purpose: "test_server_only", FileName: "a.pdf", ContentType: "application/pdf", SizeBytes: 100}
	if _, _, err := e.create(testkit.MemFileStore(t), in); !errors.Is(err, files.ErrUploadNotAllowed) {
		t.Fatalf("muốn ErrUploadNotAllowed, nhận %v", err)
	}
}

// Tải lên đúng bằng putUrl rồi complete → uploaded, một lệnh file.process; khóa không chứa tên tệp.
func TestUpload_PutThenComplete(t *testing.T) {
	for name, store := range stores(t) {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			body := randomBytes(t, 1000)
			f, put := e.mustCreate(store, imageInput(e.a, 1000))
			checkNewUpload(t, e, f, put)
			if code := putVia(t, put, body); code != http.StatusOK {
				t.Fatalf("PUT: %d", code)
			}
			done, err := e.complete(store, e.a, f.ID)
			if err != nil || done.Status != files.StatusUploaded {
				t.Fatalf("sau complete phải uploaded, nhận %+v %v", done, err)
			}
			if n := e.jobs("file.process", f.ID); n != 1 {
				t.Fatalf("phải đúng 1 lệnh file.process, có %d", n)
			}
			if got := readAll(t, store, files.BucketTmp, f.Key); !bytes.Equal(got, body) {
				t.Fatal("nội dung trên kho khác nội dung đã tải")
			}
			checkResend(t, e, store, f, put, body)
		})
	}
}

func checkNewUpload(t *testing.T, e *env, f files.File, put files.PresignedPut) {
	t.Helper()
	if f.Status != files.StatusAwaitingUpload || f.Bucket != files.BucketTmp || f.Key != files.TmpKey(e.a, f.ID) {
		t.Fatalf("dòng mới sai: %+v", f)
	}
	now := time.Now()
	if put.Headers["Content-Type"] != "image/jpeg" || put.ExpiresAt.Before(now.Add(14*time.Minute)) || put.ExpiresAt.After(now.Add(16*time.Minute)) {
		t.Fatalf("đường dẫn ký sẵn sai: %+v", put)
	}
}

// Gửi lại complete: giữ nguyên, không thêm lệnh. PUT lần hai lên cùng đường dẫn: không ghi đè (If-None-Match: *).
func checkResend(t *testing.T, e *env, store files.FileStore, f files.File, put files.PresignedPut, body []byte) {
	t.Helper()
	again, err := e.complete(store, e.a, f.ID)
	if err != nil || again.Status != files.StatusUploaded || e.jobs("file.process", f.ID) != 1 {
		t.Fatalf("complete lần hai: %+v %v, %d lệnh", again, err, e.jobs("file.process", f.ID))
	}
	if code := putVia(t, put, randomBytes(t, len(body))); code == http.StatusOK {
		t.Fatal("PUT lần hai lên khóa đã có phải bị từ chối")
	}
	if got := readAll(t, store, files.BucketTmp, f.Key); !bytes.Equal(got, body) {
		t.Fatal("PUT lần hai đã ghi đè nội dung")
	}
}

// 50 lần complete cùng lúc → vẫn đúng 1 lệnh, mọi lần trả uploaded.
func TestUpload_ConcurrentComplete(t *testing.T) {
	e := newEnv(t)
	store := testkit.MemFileStore(t)
	f, put := e.mustCreate(store, imageInput(e.a, 500))
	if code := putVia(t, put, randomBytes(t, 500)); code != http.StatusOK {
		t.Fatalf("PUT: %d", code)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for range 50 {
		wg.Go(func() {
			got, err := e.complete(store, e.a, f.ID)
			if err == nil && got.Status != files.StatusUploaded {
				err = errors.New("trạng thái " + got.Status)
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := e.jobs("file.process", f.ID); n != 1 {
		t.Fatalf("50 lần complete phải ra đúng 1 lệnh, có %d", n)
	}
}

// complete trước khi tải → ErrUploadMissing (không đổi gì); kho từ chối tải thiếu byte; tải khác kích thước bằng
// đường khác → rejected size_mismatch, không lệnh.
func TestUpload_MissingAndSizeMismatch(t *testing.T) {
	for name, store := range stores(t) {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			f, put := e.mustCreate(store, imageInput(e.a, 1000))
			if _, err := e.complete(store, e.a, f.ID); !errors.Is(err, files.ErrUploadMissing) {
				t.Fatalf("complete trước khi tải: muốn ErrUploadMissing, nhận %v", err)
			}
			if code := putVia(t, put, randomBytes(t, 900)); code == http.StatusOK {
				t.Fatal("900 byte vào chữ ký 1.000 byte phải bị kho từ chối")
			}
			if _, err := store.Head(t.Context(), files.BucketTmp, f.Key); !errors.Is(err, files.ErrObjectNotFound) {
				t.Fatalf("bị từ chối mà vẫn có đối tượng: %v", err)
			}
			if err := store.Put(t.Context(), files.BucketTmp, f.Key, bytes.NewReader(randomBytes(t, 900)), 900, "image/jpeg"); err != nil {
				t.Fatal(err)
			}
			got, err := e.complete(store, e.a, f.ID)
			if err != nil || got.Status != files.StatusRejected || got.RejectReason != "size_mismatch" {
				t.Fatalf("muốn rejected size_mismatch, nhận %+v %v", got, err)
			}
			if n := e.jobs("file.process", f.ID); n != 0 {
				t.Fatalf("tệp bị từ chối không có lệnh, có %d", n)
			}
		})
	}
}

// PQ08: tenant B không complete được tệp của A.
func TestUpload_OtherTenantNotFound(t *testing.T) {
	e := newEnv(t)
	store := testkit.MemFileStore(t)
	f, put := e.mustCreate(store, imageInput(e.a, 10))
	if code := putVia(t, put, randomBytes(t, 10)); code != http.StatusOK {
		t.Fatalf("PUT: %d", code)
	}
	if _, err := e.complete(store, e.b, f.ID); !errors.Is(err, files.ErrNotFound) {
		t.Fatalf("tenant B complete tệp của A: muốn ErrNotFound, nhận %v", err)
	}
	e.must(e.b, func(tx pgx.Tx) error {
		_, err := files.Status(t.Context(), tx, f.ID)
		if !errors.Is(err, files.ErrNotFound) {
			t.Errorf("tenant B Status tệp của A: muốn ErrNotFound, nhận %v", err)
		}
		return nil
	})
	if e.jobs("file.process", f.ID) != 0 {
		t.Fatal("tenant B đã xếp lệnh cho tệp của A")
	}
}

// Khóa trên kho không bao giờ chứa tên tệp của người dùng (ca cố định và quy luật rapid).
func TestUpload_KeyHasNoUserFileName(t *testing.T) {
	e := newEnv(t)
	store := testkit.MemFileStore(t)
	for _, name := range []string{"../../etc/passwd.jpg", "<script>.png", "a/b\\c.jpg", "ảnh áo dài.jpg"} {
		f, put := e.mustCreate(store, files.UploadInput{TenantID: e.a, Purpose: "product_image", FileName: name, ContentType: "image/png", SizeBytes: 10})
		if f.Key != files.TmpKey(e.a, f.ID) || f.OriginalName != name {
			t.Fatalf("tên %q: khóa %q, tên gốc %q", name, f.Key, f.OriginalName)
		}
		if strings.Contains(put.URL, "passwd") || strings.Contains(put.URL, "script") {
			t.Fatalf("đường dẫn ký sẵn chứa tên tệp: %s", put.URL)
		}
	}
	rapid.Check(t, func(rt *rapid.T) {
		name := rapid.StringMatching(`[^\x00]{1,60}`).Draw(rt, "name")
		f, _, err := e.create(store, files.UploadInput{TenantID: e.a, Purpose: "product_image", FileName: name, ContentType: "image/webp", SizeBytes: 10})
		if err != nil {
			rt.Fatalf("CreateUpload(%q): %v", name, err)
		}
		if f.Key != files.TmpKey(e.a, f.ID) {
			rt.Fatalf("khóa %q không phải TmpKey", f.Key)
		}
	})
}

// Mutation testing (Task 403): created_by là người tạo; không có người (uuid.Nil) → NULL.
func TestCreateUpload_CreatedBy(t *testing.T) {
	e := newEnv(t)
	store := testkit.MemFileStore(t)
	in := files.UploadInput{TenantID: e.a, Purpose: "product_image", FileName: "a.jpg", ContentType: "image/jpeg", SizeBytes: 10}
	createdBy := func(actor uuid.UUID) *uuid.UUID {
		var by *uuid.UUID
		e.must(e.a, func(tx pgx.Tx) error {
			f, _, err := files.CreateUpload(t.Context(), tx, store, in, actor)
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			return tx.QueryRow(t.Context(), "SELECT created_by FROM platform.files WHERE id = $1", f.ID).Scan(&by)
		})
		return by
	}
	if by := createdBy(e.actor); by == nil || *by != e.actor {
		t.Errorf("created_by = %v, mong %s", by, e.actor)
	}
	if by := createdBy(uuid.Nil); by != nil {
		t.Errorf("không có người tạo: created_by = %v, mong NULL", by)
	}
}
