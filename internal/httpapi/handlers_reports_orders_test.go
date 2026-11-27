package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/reportseed"
)

const reportsDay = "from=2026-11-17&to=2026-11-17"

// reportsHTTPEnv là máy chủ thử có kịch bản chín đơn, người toàn shop chu@ và người chỉ có Page P1 p1@.
type reportsHTTPEnv struct {
	t    *testing.T
	srv  http.Handler
	keys *cfaccesstest.AccessKeys
	sc   reportseed.Scenario
}

func newReportsHTTPEnv(t *testing.T) reportsHTTPEnv {
	t.Helper()
	staff, keys := testkit.OpenStaff(t), cfaccesstest.AccessKeyServer(t)
	tenant := staff.Tenant(t, "A")
	sc := reportseed.NineOrders(reportseed.New(t, staff.Owner, tenant), "2026-11-17")
	staff.Grant(t, tenant, staff.User(t, tenant, "chu@elimaz.test", "active"), []string{"report.sales.read"}, "all", "")
	staff.Grant(t, tenant, staff.User(t, tenant, "p1@elimaz.test", "active"), []string{"report.sales.read"}, "page", sc.P1.String())
	return reportsHTTPEnv{t: t, srv: NewServer(testServerDeps(t, staff, keys, 0)), keys: keys, sc: sc}
}

// get gọi GET path bằng email, đòi 200 và trả thân JSON.
func (e reportsHTTPEnv) get(email, path string) map[string]any {
	e.t.Helper()
	rec := serveAs(e.srv, e.keys, http.MethodGet, path, email, "", "")
	if rec.Code != http.StatusOK {
		e.t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		e.t.Fatal(err)
	}
	return body
}

// Báo cáo đơn, danh sách chi tiết và lựa chọn bộ lọc qua HTTP (kế hoạch M6, Task 230) trên kịch bản chín đơn: hình
// dạng hợp đồng, tiền là số nguyên JSON, dòng danh sách chi tiết chỉ có khóa của hợp đồng, phân trang bằng nextCursor;
// mặc định limit 50.
func TestReportsOrders_HTTP(t *testing.T) {
	t.Parallel()
	e := newReportsHTTPEnv(t)
	get, sc, day := e.get, e.sc, reportsDay

	rep := get("chu@elimaz.test", "/api/v1/reports/sales/orders?"+day+"&groupBy=care_by")
	totals, _ := rep["totals"].(map[string]any)
	if rep["status"] != "ready" || rep["basis"] != "placed" || rep["groupBy"] != "care_by" || totals["orders"] != 8.0 ||
		totals["goodsAmount"] != 1_910_000.0 || totals["successRateBp"] != 6666.0 || totals["key"] != "total" {
		t.Fatalf("báo cáo đơn: %v", rep)
	}
	if p, _ := rep["period"].(map[string]any); p["from"] != "2026-11-17" || p["to"] != "2026-11-17" {
		t.Fatalf("kỳ: %v", rep["period"])
	}
	if rows, _ := rep["rows"].([]any); len(rows) != 3 {
		t.Fatalf("dòng: %v", rep["rows"])
	}
	done := get("chu@elimaz.test", "/api/v1/reports/sales/orders?"+day+"&basis=completed&groupBy=status")
	if tt, _ := done["totals"].(map[string]any); tt["orders"] != 3.0 || tt["placed"] != nil || tt["active"] != nil || tt["cancelled"] != nil {
		t.Fatalf("ngày hoàn tất: %v", done)
	}

	// Danh sách chi tiết: hai trang, khóa của một dòng đúng hợp đồng.
	q := "/api/v1/reports/sales/orders/drilldown?" + day + "&metric=placed&cellDimension=care_by&cellKey=" + sc.An.String() + "&limit=2"
	p1 := get("chu@elimaz.test", q)
	items, _ := p1["items"].([]any)
	next, _ := p1["nextCursor"].(string)
	if p1["status"] != "ready" || p1["total"] != 4.0 || len(items) != 2 || next == "" {
		t.Fatalf("trang 1: %v", p1)
	}
	expectDrillKeys(t, items)
	p2 := get("chu@elimaz.test", q+"&cursor="+url.QueryEscape(next))
	if items2, _ := p2["items"].([]any); p2["total"] != 4.0 || len(items2) != 2 || p2["nextCursor"] != nil {
		t.Fatalf("trang 2: %v", p2)
	}

	// Không gửi limit: mặc định 50, một trang đủ 8 đơn.
	if all := get("chu@elimaz.test", "/api/v1/reports/sales/orders/drilldown?"+day+"&metric=placed"); all["total"] != 8.0 || all["nextCursor"] != nil {
		t.Fatalf("danh sách cả bảng: %v", all)
	}
}

// Lựa chọn bộ lọc theo phạm vi, báo cáo của người chỉ có Page P1; tham số lạ → 400 bad_params, ngày sai → bad_date,
// lọc Page ngoài phạm vi → 403 forbidden (kế hoạch M6, Task 230).
func TestReportsOrders_HTTPScopeAndErrors(t *testing.T) {
	t.Parallel()
	e := newReportsHTTPEnv(t)
	get, sc, day, srv, keys := e.get, e.sc, reportsDay, e.srv, e.keys
	f := get("chu@elimaz.test", "/api/v1/reports/sales/filters")
	if pages, _ := f["pages"].([]any); len(pages) != 2 {
		t.Fatalf("bộ lọc toàn shop: %v", f)
	}
	fp := get("p1@elimaz.test", "/api/v1/reports/sales/filters")
	if pages, _ := fp["pages"].([]any); len(pages) != 1 || pages[0].(map[string]any)["id"] != sc.P1.String() {
		t.Fatalf("bộ lọc của người P1: %v", fp)
	}
	if tt, _ := get("p1@elimaz.test", "/api/v1/reports/sales/orders?"+day)["totals"].(map[string]any); tt["orders"] != 4.0 {
		t.Fatalf("báo cáo của người P1: %v", tt)
	}

	for _, c := range []struct {
		name, email, path string
		status            int
		code              string
	}{
		{"chiều nhóm lạ", "chu@elimaz.test", "/api/v1/reports/sales/orders?" + day + "&groupBy=foo", http.StatusBadRequest, "bad_params"},
		{"cơ sở lạ", "chu@elimaz.test", "/api/v1/reports/sales/orders?" + day + "&basis=x", http.StatusBadRequest, "bad_params"},
		{"thiếu metric", "chu@elimaz.test", "/api/v1/reports/sales/orders/drilldown?" + day, http.StatusBadRequest, "bad_params"},
		{"con trỏ hỏng", "chu@elimaz.test", "/api/v1/reports/sales/orders/drilldown?" + day + "&metric=placed&cursor=%25%25", http.StatusBadRequest, "bad_params"},
		{"khóa ô Page không phải uuid", "chu@elimaz.test", "/api/v1/reports/sales/orders/drilldown?" + day + "&metric=placed&cellDimension=page&cellKey=abc", http.StatusBadRequest, "bad_params"},
		{"ô nhãn lạ", "chu@elimaz.test", "/api/v1/reports/sales/orders/drilldown?" + day + "&metric=placed&cellDimension=status&cellKey=x", http.StatusBadRequest, "bad_params"},
		{"limit quá 200", "chu@elimaz.test", "/api/v1/reports/sales/orders/drilldown?" + day + "&metric=placed&limit=201", http.StatusBadRequest, "bad_params"},
		{"ngày sai", "chu@elimaz.test", "/api/v1/reports/sales/orders?from=2026-11-31&to=2026-12-01", http.StatusBadRequest, "bad_date"},
		{"Page ngoài phạm vi", "p1@elimaz.test", "/api/v1/reports/sales/orders?" + day + "&pageId=" + sc.P2.String(), http.StatusForbidden, "forbidden"},
		{"Page ngoài phạm vi, danh sách", "p1@elimaz.test", "/api/v1/reports/sales/orders/drilldown?" + day + "&metric=placed&pageId=" + sc.P2.String(), http.StatusForbidden, "forbidden"},
	} {
		expect(t, c.name, serveAs(srv, keys, http.MethodGet, c.path, c.email, "", ""), c.status, c.code)
	}
}

// expectDrillKeys: dòng danh sách chi tiết chỉ có khóa của hợp đồng (không SĐT, địa chỉ, tên khách) và đủ khóa bắt buộc.
func expectDrillKeys(t *testing.T, items []any) {
	t.Helper()
	allowed := []string{
		"orderId", "code", "customerCode", "placedAt", "completedAt", "statusLabel", "channel", "sourceName",
		"careByName", "carrierKey", "goodsAmount", "shippingFeeCharged",
	}
	for _, it := range items {
		m, _ := it.(map[string]any)
		for k := range m {
			if !slices.Contains(allowed, k) {
				t.Errorf("dòng danh sách chi tiết có khóa ngoài hợp đồng %q", k)
			}
		}
		for _, k := range []string{"orderId", "code", "statusLabel", "channel", "goodsAmount", "shippingFeeCharged", "placedAt"} {
			if _, ok := m[k]; !ok {
				t.Errorf("dòng thiếu %q: %v", k, m)
			}
		}
	}
}
