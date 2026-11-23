package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/files/filestest"
)

const filesManager = "m@files.test" // tenant A, có platform.files.manage

// imageWithoutV1 tạo tệp ready mục đích purpose của tenant rồi xóa biến thể v1 của nó (ảnh "chưa có bản v1").
func (e *filesAPIEnv) imageWithoutV1(tenant uuid.UUID, purpose string) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, tenant, func(tx pgx.Tx) error {
		id = filestest.Image(e.t, tx, tenant, purpose)
		_, err := tx.Exec(e.t.Context(), "DELETE FROM platform.file_variants WHERE file_id = $1", id)
		return err //nolint:wrapcheck // test
	})
	return id
}

// regenerateImages (TP07): người có platform.files.manage xếp một lệnh cho mỗi tệp ảnh ready của tenant mình thiếu
// bản đó (không tính tệp đã đủ, tệp không phải ảnh, tệp của tenant khác), có audit; gọi lại → 0; không quyền → 403;
// bộ kích thước chưa có → 422 unknown_preset; presetVersion 0 → 400.
func TestFilesAPI_RegenerateImages(t *testing.T) {
	t.Parallel()
	e := newFilesAPIEnv(t)
	e.staff.Grant(t, e.a, e.staff.User(t, e.a, filesManager, "active"), []string{"platform.files.manage"}, "all", "")
	missing := e.imageWithoutV1(e.a, "product_image")
	e.imageWithoutV1(e.a, "catalog_import")
	e.staff.Exec(t, e.a, func(tx pgx.Tx) error {
		filestest.Image(t, tx, e.a, "product_image") // đã đủ v1
		return nil
	})
	b := e.staff.Tenant(t, "B")
	e.imageWithoutV1(b, "product_image")

	const path = "/api/v1/platform/files/regenerate"
	rec := e.do(http.MethodPost, path, filesManager, `{"presetVersion":1}`)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"enqueued":1}` {
		t.Fatalf("regenerate: %d %s", rec.Code, rec.Body)
	}
	if n := e.staff.Count(t, "SELECT count(*) FROM platform.jobs WHERE kind = 'file.regenerate' AND dedupe_key = $1",
		missing.String()+":v1"); n != 1 {
		t.Fatalf("tệp thiếu v1 có %d lệnh", n)
	}
	if n := e.staff.Count(t, "SELECT count(*) FROM platform.jobs WHERE kind = 'file.regenerate'"); n != 1 {
		t.Fatalf("có %d lệnh file.regenerate, muốn 1", n)
	}
	if n := e.staff.CountIn(t, e.a, "SELECT count(*) FROM identity.audit_logs WHERE action = 'platform.files.regenerate'"); n != 1 {
		t.Fatalf("có %d dòng audit", n)
	}
	if rec := e.do(http.MethodPost, path, filesManager, `{"presetVersion":1}`); rec.Code != http.StatusOK ||
		strings.TrimSpace(rec.Body.String()) != `{"enqueued":0}` {
		t.Fatalf("gọi lại: %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(http.MethodPost, path, filesA, `{"presetVersion":1}`); rec.Code != http.StatusForbidden {
		t.Fatalf("chỉ có files.upload: muốn 403, nhận %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, path, filesManager, `{"presetVersion":99}`); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), `"unknown_preset"`) {
		t.Fatalf("bộ kích thước lạ: %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(http.MethodPost, path, filesManager, `{"presetVersion":0}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("presetVersion 0: muốn 400, nhận %d", rec.Code)
	}
}
