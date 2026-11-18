package httpapi

import (
	"bytes"
	"net/http"
	"strings"
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
