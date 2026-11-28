package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Xuất danh sách khách qua HTTP (PQ06, Task 238 = M6-15): customer.export phạm vi page → 403 (quyền toàn shop); phạm
// vi all → 202 report customers, format xlsx; chữ tự do {"q":"Nguyễn"} → 400 bad_params; xong → GET một lần, liệt kê
// thấy lần chạy; …/file của người tạo → 200 và một dòng audit customer.export_downloaded; người khác → 404; người
// không có customer.export → 403.
func TestCustomerExport_HTTP(t *testing.T) {
	t.Parallel()
	e := newFileHTTPEnv(t)
	page := e.staff.User(t, e.tenant, "page@elimaz.test", "active")
	e.staff.Grant(t, e.tenant, page, []string{"customer.export"}, "page", uuid.NewString())
	for _, email := range []string{"ql2@elimaz.test", "nv@elimaz.test"} {
		e.users[email] = e.staff.User(t, e.tenant, email, "active")
		e.staff.Grant(t, e.tenant, e.users[email], []string{"customer.export"}, "all", "")
	}
	const path = "/api/v1/reports/customers/exports"
	if rec := e.do(http.MethodPost, path, "page@elimaz.test", uuid.NewString(), `{"params":{}}`); rec.Code != http.StatusForbidden {
		t.Errorf("phạm vi page: %d %s", rec.Code, rec.Body.String())
	}
	rec := e.do(http.MethodPost, path, "ql2@elimaz.test", uuid.NewString(), `{"params":{"q":"Nguyễn"}}`)
	if code := errCode(t, rec); rec.Code != http.StatusBadRequest || code != "bad_params" {
		t.Errorf("chữ tự do: %d %s", rec.Code, rec.Body.String())
	}
	rec = e.do(http.MethodPost, path, "ql2@elimaz.test", uuid.NewString(), `{"params":{"lifecycle":"lead","minOrders":0}}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("tạo: %d %s", rec.Code, rec.Body.String())
	}
	var run struct {
		ID, Family, Report, Format, Status string
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if run.Family != "customers" || run.Report != "customers" || run.Format != "xlsx" || run.Status != "queued" {
		t.Errorf("lần chạy %+v", run)
	}
	e.drain()
	rec = e.do(http.MethodGet, path+"/"+run.ID, "ql2@elimaz.test", "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ready"`) {
		t.Errorf("GET: %d %s", rec.Code, rec.Body.String())
	}
	rec = e.do(http.MethodGet, path, "ql2@elimaz.test", "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), run.ID) {
		t.Errorf("liệt kê: %d %s", rec.Code, rec.Body.String())
	}
	if rec := e.do(http.MethodGet, path, "nv@elimaz.test", "", ""); strings.Contains(rec.Body.String(), run.ID) {
		t.Errorf("người khác liệt kê thấy lần chạy: %s", rec.Body.String())
	}
	rec = e.do(http.MethodGet, path+"/"+run.ID+"/file", "ql2@elimaz.test", "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"url"`) {
		t.Errorf("file: %d %s", rec.Code, rec.Body.String())
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.export_downloaded'
		AND entity_id = $1 AND actor_id = $2`, run.ID, e.users["ql2@elimaz.test"].String()); n != 1 {
		t.Errorf("audit tải = %d", n)
	}
	expectCustomerExportHidden(t, e, path+"/"+run.ID)
}

// expectCustomerExportHidden: người khác (có customer.export) → 404; người không có customer.export → 403.
func expectCustomerExportHidden(t *testing.T, e fileHTTPEnv, runPath string) {
	t.Helper()
	for _, p := range []string{runPath, runPath + "/file"} {
		if rec := e.do(http.MethodGet, p, "nv@elimaz.test", "", ""); rec.Code != http.StatusNotFound {
			t.Errorf("người khác %s: %d", p, rec.Code)
		}
		if rec := e.do(http.MethodGet, p, "chu@elimaz.test", "", ""); rec.Code != http.StatusForbidden {
			t.Errorf("không customer.export %s: %d", p, rec.Code)
		}
	}
}
