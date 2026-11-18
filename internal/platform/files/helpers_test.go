package files_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// env: database thử (crm_app và chủ bảng), hai tenant A, B, một nhân viên của A.
type env struct {
	t     *testing.T
	staff *testkit.Staff
	a, b  uuid.UUID
	actor uuid.UUID
}

func newEnv(t *testing.T) *env {
	t.Helper()
	s := testkit.OpenStaff(t)
	e := &env{t: t, staff: s, a: s.Tenant(t, "A"), b: s.Tenant(t, "B")}
	e.actor = s.User(t, e.a, "nv@a.test", "active")
	return e
}

// inTenant chạy fn bằng crm_app trong ngữ cảnh tenant (như tiến trình api).
func (e *env) inTenant(tenant uuid.UUID, fn func(pgx.Tx) error) error {
	return tenancy.WithTenant(e.t.Context(), e.staff.App, tenant, fn, db.TxAPI) //nolint:wrapcheck // test
}

func (e *env) must(tenant uuid.UUID, fn func(pgx.Tx) error) {
	e.t.Helper()
	if err := e.inTenant(tenant, fn); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) create(store files.FileStore, in files.UploadInput) (files.File, files.PresignedPut, error) {
	var (
		f   files.File
		put files.PresignedPut
	)
	err := e.inTenant(in.TenantID, func(tx pgx.Tx) error {
		var err error
		f, put, err = files.CreateUpload(e.t.Context(), tx, store, in, e.actor)
		return err //nolint:wrapcheck // test
	})
	return f, put, err
}

func (e *env) mustCreate(store files.FileStore, in files.UploadInput) (files.File, files.PresignedPut) {
	e.t.Helper()
	f, put, err := e.create(store, in)
	if err != nil {
		e.t.Fatalf("CreateUpload: %v", err)
	}
	return f, put
}

func (e *env) complete(store files.FileStore, tenant, id uuid.UUID) (files.File, error) {
	return files.CompleteUpload(e.t.Context(), e.staff.App, store, queue.NewQueue(), tenant, id) //nolint:wrapcheck // test
}

func (e *env) jobs(kind string, id uuid.UUID) int {
	e.t.Helper()
	return e.staff.Count(e.t, "SELECT count(*) FROM platform.jobs WHERE kind = $1 AND dedupe_key = $2", kind, id.String())
}

func imageInput(tenant uuid.UUID, size int64) files.UploadInput {
	return files.UploadInput{TenantID: tenant, Purpose: "product_image", FileName: "ao-dai.jpg", ContentType: "image/jpeg", SizeBytes: size}
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	_, _ = rand.Read(b) // crypto/rand.Read không trả lỗi (Go ≥ 1.24)
	return b
}

// putVia gửi body lên đường dẫn ký sẵn như trình duyệt (đặt đúng các header được ký); trả mã HTTP.
func putVia(t *testing.T, p files.PresignedPut, body []byte) int {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, p.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range p.Headers {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("PUT đường dẫn ký sẵn: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// get đọc url bằng GET; trả mã HTTP và thân.
func get(t *testing.T, url string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	status, _, body := testkit.Do(t, req)
	return status, body
}

func readAll(t *testing.T, s files.FileStore, b files.Bucket, key string) []byte {
	t.Helper()
	rc, err := s.Open(context.WithoutCancel(t.Context()), b, key, 1<<30)
	if err != nil {
		t.Fatalf("Open %s/%s: %v", b, key, err)
	}
	defer func() { _ = rc.Close() }()
	out, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// stores: hai bản hiện thực thật của FileStore, mọi ca của đường tải lên chạy trên cả hai.
func stores(t *testing.T) map[string]files.FileStore {
	t.Helper()
	return map[string]files.FileStore{"mem": testkit.MemFileStore(t), "s3": testkit.S3FileStore(t)}
}
