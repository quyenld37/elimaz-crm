package files_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/testkit"
)

// imageWithOriginal tạo ảnh product_image ready của tenant A và ghi ảnh gốc orig/<sha> lên kho fs; trả mã tệp, nội dung.
func (e *env) imageWithOriginal(fs files.FileStore) (uuid.UUID, string) {
	e.t.Helper()
	var (
		id  uuid.UUID
		sha string
	)
	e.must(e.a, func(tx pgx.Tx) error {
		id = filestest.Image(e.t, tx, e.a, "product_image")
		return tx.QueryRow(e.t.Context(), "SELECT encode(content_sha256, 'hex') FROM platform.files WHERE id = $1", id).Scan(&sha)
	})
	body := "anh-goc-" + sha
	if err := fs.Put(e.t.Context(), files.BucketOriginals, files.OriginalKey(sha), strings.NewReader(body),
		int64(len(body)), "image/jpeg"); err != nil {
		e.t.Fatal(err)
	}
	return id, body
}

func (e *env) originalURL(fs files.FileStore, tenant, id uuid.UUID, ttl time.Duration, name string) (files.SignedURL, error) {
	var out files.SignedURL
	err := e.inTenant(tenant, func(tx pgx.Tx) error {
		var err error
		out, err = files.OriginalURL(e.t.Context(), tx, fs, id, ttl, name)
		return err //nolint:wrapcheck // test
	})
	return out, err
}

// getWithHeader đọc url bằng GET; trả mã HTTP, thân và header.
func getWithHeader(t *testing.T, url string) (int, string, http.Header) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	status, h, body := testkit.Do(t, req)
	return status, string(body), h
}

// TP05 trên kho thật (SeaweedFS): đường dẫn ảnh gốc đọc được, tải về dạng attachment với tên <tên>.jpg, hạn 2 s →
// sau 3 s kho trả 403.
func TestOriginalURL_ExpiresOnStore(t *testing.T) {
	e := newEnv(t)
	fs := testkit.S3FileStore(t)
	id, body := e.imageWithOriginal(fs)
	before := time.Now()
	su, err := e.originalURL(fs, e.a, id, 2*time.Second, "DAM01-2")
	if err != nil {
		t.Fatalf("OriginalURL: %v", err)
	}
	if su.ExpiresAt.Before(before.Add(2*time.Second-time.Second)) || su.ExpiresAt.After(time.Now().Add(2*time.Second)) {
		t.Errorf("ExpiresAt sai: %v (ký lúc %v)", su.ExpiresAt, before)
	}
	code, got, h := getWithHeader(t, su.URL)
	if code != http.StatusOK || got != body {
		t.Fatalf("đọc ảnh gốc trong hạn: %d %q", code, got)
	}
	if cd := h.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, "DAM01-2.jpg") {
		t.Errorf("Content-Disposition sai: %q", cd)
	}
	time.Sleep(3 * time.Second)
	if code, _ := get(t, su.URL); code != http.StatusForbidden {
		t.Errorf("đọc sau khi hết hạn: muốn 403, nhận %d", code)
	}
}

// Chỉ ảnh standard đang ready có ảnh gốc: tenant khác, đã xóa, mất → ErrNotFound; chưa xử lý xong → ErrNotReady;
// ảnh riêng tư (R10) và tệp không phải ảnh → ErrNotFound (không bao giờ ký tới bản thô hay bản khác ảnh gốc).
func TestOriginalURL_Refusals(t *testing.T) {
	e := newEnv(t)
	fs := testkit.MemFileStore(t)
	id, _ := e.imageWithOriginal(fs)
	if _, err := e.originalURL(fs, e.b, id, time.Minute, "x"); !errors.Is(err, files.ErrNotFound) {
		t.Errorf("tenant B: muốn ErrNotFound, nhận %v", err)
	}
	files.RegisterPurpose(testPrivateImagePurpose())
	if err := e.addPurposeRow("test_private_image", "private", "private"); err != nil {
		t.Fatal(err)
	}
	var uploaded, private, notImage, lost, deleted uuid.UUID
	e.must(e.a, func(tx pgx.Tx) error {
		uploaded = filestest.Uploaded(t, tx, e.a, "product_image")
		private = filestest.Image(t, tx, e.a, "test_private_image")
		notImage = filestest.Image(t, tx, e.a, "catalog_import")
		lost = filestest.Image(t, tx, e.a, "product_image")
		filestest.MarkLost(t, tx, lost)
		deleted = filestest.Image(t, tx, e.a, "product_image")
		return files.MarkDeleted(t.Context(), tx, deleted, e.actor)
	})
	for _, c := range []struct {
		name string
		id   uuid.UUID
		want error
	}{
		{"đang xử lý", uploaded, files.ErrNotReady},
		{"riêng tư", private, files.ErrNotFound},
		{"không phải ảnh", notImage, files.ErrNotFound},
		{"mất", lost, files.ErrNotFound},
		{"đã xóa", deleted, files.ErrNotFound},
	} {
		if _, err := e.originalURL(fs, e.a, c.id, time.Minute, "x"); !errors.Is(err, c.want) {
			t.Errorf("%s: muốn %v, nhận %v", c.name, c.want, err)
		}
	}
}

// Mutation testing (Task 403): hạn đường dẫn ảnh gốc cho nhân viên đúng 5 phút (TP05), không thành 0.
func TestOriginalTTL(t *testing.T) {
	if files.OriginalTTL != 5*time.Minute {
		t.Fatalf("OriginalTTL = %v, mong 5m", files.OriginalTTL)
	}
}
