package httpapi

import (
	"fmt"
	"net/http"
	"testing"
)

// Đồng bộ địa giới qua HTTP (kế hoạch M3, Task M3-18): hãng giả của tools/fakecarrier trả 2 tỉnh, 3 phường (mã trùng
// mã CRM); danh mục thử của CRM có thêm "Phường Bến Nghé" mà hãng không có. POST …/locations/sync (manage) → 202
// jobId; chạy worker → GET /carrier-locations/unmapped?carrier=fake chỉ còn phường đó; quyền read đọc được danh sách
// nhưng không đồng bộ được (403); thiếu carrier → 400; cursor hỏng → 400; tài khoản lạ → 404.
func TestCarrierLocationsAPI(t *testing.T) {
	t.Parallel()
	e := newCAEnv(t)
	if _, err := e.staff.Owner.Pool.Exec(t.Context(), `INSERT INTO customer.admin_units
		(code, level, parent_code, name, name_search, effective_from, source)
		SELECT c, l, p, n, lower(n), DATE '2025-07-01', 'test' FROM (VALUES
		('01', 'province', NULL, 'Thành phố Hà Nội'), ('79', 'province', NULL, 'Thành phố Hồ Chí Minh'),
		('00004', 'ward', '01', 'Phường Ba Đình'), ('00070', 'ward', '01', 'Phường Hoàn Kiếm'),
		('26734', 'ward', '79', 'Phường Sài Gòn'), ('26740', 'ward', '79', 'Phường Bến Nghé')) AS v (c, l, p, n)`); err != nil {
		t.Fatal(err)
	}
	id := e.activeAccount()
	if code, _, raw := e.send(http.MethodPost, "/api/v1/carrier-accounts/"+id+"/locations/sync", caReader, ""); code != http.StatusForbidden {
		t.Fatalf("quyền read đồng bộ: %d %s", code, raw)
	}
	if code, b, raw := e.send(http.MethodPost, "/api/v1/carrier-accounts/00000000-0000-7000-8000-000000000001/locations/sync",
		caManager, ""); code != http.StatusNotFound || b["error"] != "not_found" {
		t.Fatalf("tài khoản lạ: %d %s", code, raw)
	}
	code, job, raw := e.send(http.MethodPost, "/api/v1/carrier-accounts/"+id+"/locations/sync", caManager, "")
	if code != http.StatusAccepted || job["jobId"] == nil {
		t.Fatalf("đồng bộ: %d %s", code, raw)
	}
	e.run()
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = 'command.carrier.locations' AND status = 'done'`); n != 1 {
		t.Fatalf("lệnh đồng bộ done = %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'fulfillment.carrier_account.locations_sync'`); n != 1 {
		t.Errorf("audit đồng bộ = %d dòng, muốn 1", n)
	}
	e.checkUnmapped()
}

// activeAccount tạo tài khoản fake mode api có khóa, kiểm cho active, trả mã.
func (e *caEnv) activeAccount() string {
	e.t.Helper()
	code, acc, raw := e.send(http.MethodPost, "/api/v1/carrier-accounts", caManager, e.createBody(`,"credentials":{"token":"`+caToken+`"}`))
	if code != http.StatusCreated {
		e.t.Fatalf("tạo: %d %s", code, raw)
	}
	id := fmt.Sprint(acc["id"])
	if code, _, _ := e.send(http.MethodPost, "/api/v1/carrier-accounts/"+id+"/check", caManager, ""); code != http.StatusAccepted {
		e.t.Fatalf("kiểm: %d", code)
	}
	e.run()
	return id
}

// checkUnmapped: sau đồng bộ chỉ còn "Phường Bến Nghé" chưa khớp với fake; hãng chưa đồng bộ (vtp) có mọi mã, phân
// trang theo cursor; tham số sai → 400; không quyền đọc → 403.
func (e *caEnv) checkUnmapped() {
	t := e.t
	t.Helper()
	code, page, raw := e.send(http.MethodGet, "/api/v1/carrier-locations/unmapped?carrier=fake", caReader, "")
	items, _ := page["items"].([]any)
	if code != http.StatusOK || len(items) != 1 || page["nextCursor"] != nil {
		t.Fatalf("chưa khớp: %d %s", code, raw)
	}
	if it, _ := items[0].(map[string]any); it["level"] != "ward" || it["ourCode"] != "26740" || it["name"] != "Phường Bến Nghé" ||
		it["parentCode"] != "79" {
		t.Fatalf("dòng chưa khớp: %v", it)
	}
	// Hãng chưa đồng bộ: mọi mã chưa khớp, trang 2 dòng có nextCursor, trang sau tiếp đúng chỗ.
	code, page, raw = e.send(http.MethodGet, "/api/v1/carrier-locations/unmapped?carrier=vtp&limit=2", caReader, "")
	items, _ = page["items"].([]any)
	if code != http.StatusOK || len(items) != 2 || page["nextCursor"] != "province:79" {
		t.Fatalf("trang 1 của hãng chưa đồng bộ: %d %s", code, raw)
	}
	code, page, raw = e.send(http.MethodGet, "/api/v1/carrier-locations/unmapped?carrier=vtp&level=ward&cursor=province:79", caReader, "")
	items, _ = page["items"].([]any)
	if code != http.StatusOK || len(items) != 4 {
		t.Fatalf("trang phường sau cursor: %d %s", code, raw)
	}
	for _, q := range []string{"", "?carrier=fake&cursor=bad", "?carrier=fake&level=district"} {
		if code, _, raw := e.send(http.MethodGet, "/api/v1/carrier-locations/unmapped"+q, caReader, ""); code != http.StatusBadRequest {
			t.Errorf("GET unmapped%s: %d %s, muốn 400", q, code, raw)
		}
	}
	if code, _, _ := e.send(http.MethodGet, "/api/v1/carrier-locations/unmapped?carrier=fake", caNobody, ""); code != http.StatusForbidden {
		t.Errorf("không quyền đọc: %d, muốn 403", code)
	}
}
