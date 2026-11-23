package httpapi

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/platform/xlsx"
)

const catalogImporter = "nhap@elimaz.test" // tenant A, có catalog.import

// createCatalogImport → 202 validating, một lệnh catalog.import.validate, gửi lại cùng khóa → cùng bản nhập;
// getCatalogImport trả {status, summary, errors}; tệp sai mục đích → 409 file_not_ready; tệp của tenant B → 404;
// chỉ có catalog.manage → 403 ở cả ba operation; getCatalogImportTemplate → tệp .xlsx đọc lại được.
func TestCatalogAPI_Imports(t *testing.T) {
	t.Parallel()
	e := newCatalogAPIEnv(t)
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, catalogImporter, "active"), []string{"catalog.import"}, "all", "")
	var fileID, image, foreign uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		fileID = filestest.Uploaded(t, tx, e.tenant, "catalog_import")
		image = filestest.Uploaded(t, tx, e.tenant, "product_image")
		return nil
	})
	b := e.staff.Tenant(t, "B")
	e.staff.Exec(t, b, func(tx pgx.Tx) error {
		foreign = filestest.Uploaded(t, tx, b, "catalog_import")
		return nil
	})

	body := `{"fileId":"` + fileID.String() + `"}`
	rec := e.do(http.MethodPost, "/api/v1/catalog/imports", catalogImporter, "i1", body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("createCatalogImport: %d %s", rec.Code, rec.Body)
	}
	created := decodeJSON(t, rec)
	if created["status"] != "validating" || created["fileId"] != fileID.String() {
		t.Fatalf("bản nhập: %v", created)
	}
	id, _ := created["id"].(string)
	if again := e.do(http.MethodPost, "/api/v1/catalog/imports", catalogImporter, "i1", body); again.Code != http.StatusAccepted ||
		decodeJSON(t, again)["id"] != id {
		t.Fatalf("gửi lại cùng khóa: %d %s", again.Code, again.Body)
	}
	if n := e.staff.Count(t, "SELECT count(*) FROM platform.jobs WHERE kind = 'catalog.import.validate' AND dedupe_key = $1", id); n != 1 {
		t.Fatalf("%d lệnh kiểm", n)
	}
	got := e.do(http.MethodGet, "/api/v1/catalog/imports/"+id, catalogImporter, "", "")
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"errors":[]`) ||
		!strings.Contains(got.Body.String(), `"summary":{"models":0,"create":0,"update":0,"unchanged":0}`) {
		t.Fatalf("getCatalogImport: %d %s", got.Code, got.Body)
	}
	for name, c := range map[string]struct {
		id   uuid.UUID
		code int
		err  string
	}{
		"mục đích ảnh": {image, http.StatusConflict, `"file_not_ready"`},
		"tenant B":     {foreign, http.StatusNotFound, `"not_found"`},
	} {
		rec := e.do(http.MethodPost, "/api/v1/catalog/imports", catalogImporter, "x-"+c.id.String(), `{"fileId":"`+c.id.String()+`"}`)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.err) {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	if rec := e.do(http.MethodGet, "/api/v1/catalog/imports/"+uuid.NewString(), catalogImporter, "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("bản nhập không có: %d", rec.Code)
	}
	for _, r := range []struct{ method, path, key, body string }{
		{http.MethodPost, "/api/v1/catalog/imports", "m1", body},
		{http.MethodGet, "/api/v1/catalog/imports/" + id, "", ""},
		{http.MethodGet, "/api/v1/catalog/imports/template", "", ""},
	} {
		if rec := e.do(r.method, r.path, catalogManager, r.key, r.body); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s chỉ có catalog.manage: %d", r.method, r.path, rec.Code)
		}
	}
	tpl := e.do(http.MethodGet, "/api/v1/catalog/imports/template", catalogImporter, "", "")
	if tpl.Code != http.StatusOK || !strings.Contains(tpl.Header().Get("Content-Disposition"), ".xlsx") ||
		tpl.Header().Get("Content-Type") != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Fatalf("mẫu: %d %v", tpl.Code, tpl.Header())
	}
	b2 := tpl.Body.Bytes()
	if rows, _, err := xlsx.Read(bytes.NewReader(b2), int64(len(b2)), xlsx.Limits{}, []string{"Mã mẫu*", "SKU*", "Giá bán*"}); err != nil || len(rows) != 0 {
		t.Fatalf("đọc lại mẫu: %v, %d dòng", err, len(rows))
	}
}

// readyImport chèn thẳng một bản nhập ready (plan rỗng) của người bán mặc định tenant A; trả mã và version.
func (e *catalogAPIEnv) readyImport(status string) (uuid.UUID, int64) {
	e.t.Helper()
	var (
		id      uuid.UUID
		version int64
	)
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		file := filestest.Uploaded(e.t, tx, e.tenant, "catalog_import")
		plan := any(`{"products":[]}`)
		if status != "ready" {
			plan = nil
		}
		return tx.QueryRow(e.t.Context(), `INSERT INTO catalog.imports (tenant_id, file_id, file_sha256, status, seller_id,
			created_by, plan)
			SELECT $1, $2::uuid, sha256(convert_to($2::uuid::text, 'UTF8')), $3, s.id, gen_random_uuid(), $4::jsonb
			FROM catalog.sellers s WHERE s.tenant_id = $1 AND s.code = 'ELIMAZ' RETURNING id, version`,
			e.tenant, file, status, plan).Scan(&id, &version)
	})
	return id, version
}

// applyCatalogImport: 50 yêu cầu song song cùng Idempotency-Key → 1 việc catalog.import.apply, mọi 202 cùng bản nhập
// applying; version cũ → 409 version_conflict; bản invalid → 409 import_not_ready; bản nhập lạ → 404; chỉ có
// catalog.manage → 403.
func TestCatalogAPI_ImportApply(t *testing.T) {
	t.Parallel()
	e := newCatalogAPIEnv(t)
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, catalogImporter, "active"), []string{"catalog.import"}, "all", "")
	id, version := e.readyImport("ready")
	path := "/api/v1/catalog/imports/" + id.String() + "/apply"
	body := fmt.Sprintf(`{"version":%d}`, version)
	if rec := e.do(http.MethodPost, path, catalogManager, "m-1", body); rec.Code != http.StatusForbidden {
		t.Fatalf("chỉ có catalog.manage: %d", rec.Code)
	}
	recs := make([]*httptest.ResponseRecorder, 50)
	var wg sync.WaitGroup
	for i := range recs {
		wg.Go(func() { recs[i] = e.do(http.MethodPost, path, catalogImporter, "apply-50", body) })
	}
	wg.Wait()
	accepted := 0
	for _, rec := range recs {
		switch {
		case rec.Code == http.StatusAccepted:
			accepted++
			if m := decodeJSON(t, rec); m["status"] != "applying" || m["id"] != id.String() {
				t.Errorf("202: %v", m)
			}
		case rec.Code == http.StatusConflict && errCode(t, rec) == "request_in_progress":
		default:
			t.Errorf("chỉ được 202 hoặc 409 request_in_progress: %d %s", rec.Code, rec.Body)
		}
	}
	if accepted == 0 {
		t.Fatal("không có 202 nào")
	}
	if n := e.staff.Count(t, "SELECT count(*) FROM platform.jobs WHERE kind = 'catalog.import.apply' AND dedupe_key = $1", id.String()); n != 1 {
		t.Fatalf("%d việc áp", n)
	}
	other, otherVersion := e.readyImport("ready")
	invalid, invalidVersion := e.readyImport("invalid")
	for name, c := range map[string]struct {
		id      uuid.UUID
		version int64
		code    int
		err     string
	}{
		"version cũ":    {other, otherVersion - 1, http.StatusConflict, "version_conflict"},
		"bản invalid":   {invalid, invalidVersion, http.StatusConflict, "import_not_ready"},
		"đang applying": {id, version + 1, http.StatusConflict, "import_not_ready"},
		"không có":      {uuid.New(), 1, http.StatusNotFound, "not_found"},
	} {
		rec := e.do(http.MethodPost, "/api/v1/catalog/imports/"+c.id.String()+"/apply", catalogImporter, "k-"+c.id.String()+fmt.Sprint(c.version),
			fmt.Sprintf(`{"version":%d}`, c.version))
		if rec.Code != c.code || errCode(t, rec) != c.err {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
}
