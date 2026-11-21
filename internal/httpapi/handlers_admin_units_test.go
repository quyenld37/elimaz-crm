package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/testkit"
)

const (
	adminUnitsReader = "doc-khach@elimaz.test" // customer.read
	adminUnitsNoPerm = "chi-kho@elimaz.test"   // không có customer.read
)

// listAdminUnits qua HTTP: parent rỗng → tỉnh; có parent → xã của tỉnh đó lọc theo q, tối đa 50; thiếu quyền → 403.
func TestListAdminUnitsAPI(t *testing.T) {
	staff := testkit.OpenStaff(t)
	keys := cfaccesstest.AccessKeyServer(t)
	if _, err := staff.Owner.Pool.Exec(t.Context(), `INSERT INTO customer.admin_units
		(code, level, parent_code, name, name_search, effective_from, source) VALUES
		('T01', 'province', NULL, 'Thành phố Thử Một', 'thanh pho thu mot', DATE '2025-07-01', 'test'),
		('T02', 'province', NULL, 'Tỉnh Thử Hai', 'tinh thu hai', DATE '2025-07-01', 'test'),
		('T02001', 'ward', 'T02', 'Phường Bến Hai', 'phuong ben hai', DATE '2025-07-01', 'test')`); err != nil {
		t.Fatal(err)
	}
	if _, err := staff.Owner.Pool.Exec(t.Context(), `INSERT INTO customer.admin_units
		(code, level, parent_code, name, name_search, effective_from, source)
		SELECT 'T01' || lpad(i::text, 3, '0'), 'ward', 'T01', 'Phường Bến số ' || i, 'phuong ben so ' || i,
			DATE '2025-07-01', 'test' FROM generate_series(1, 60) AS i`); err != nil {
		t.Fatal(err)
	}
	tenant := staff.Tenant(t, "A")
	staff.Grant(t, tenant, staff.User(t, tenant, adminUnitsReader, "active"), []string{"customer.read"}, "all", "")
	staff.Grant(t, tenant, staff.User(t, tenant, adminUnitsNoPerm, "active"), []string{"inventory.read"}, "all", "")
	srv := testServer(t, staff, keys, 0)
	get := func(path, email string) map[string]any {
		t.Helper()
		return mustOK(t, path, serveAs(srv, keys, http.MethodGet, path, email, "", ""), http.StatusOK)
	}

	expect(t, "thiếu customer.read", serveAs(srv, keys, http.MethodGet, "/api/v1/admin-units", adminUnitsNoPerm, "", ""),
		http.StatusForbidden, "forbidden")

	items := get("/api/v1/admin-units", adminUnitsReader)["items"].([]any) //nolint:forcetypeassert // test
	// Chỉ xét mã mẫu T… (dữ liệu thật, nếu đã migrate, thêm tỉnh mã số); mọi dòng là tỉnh, không có parentCode.
	var mine []map[string]any
	for _, it := range items {
		p := it.(map[string]any) //nolint:forcetypeassert // test
		if p["level"] != "province" {
			t.Fatalf("parent rỗng chỉ trả tỉnh: %v", p)
		}
		if _, ok := p["parentCode"]; ok {
			t.Fatalf("tỉnh không có parentCode: %v", p)
		}
		if code, _ := p["code"].(string); strings.HasPrefix(code, "T") {
			mine = append(mine, p)
		}
	}
	if len(mine) != 2 || mine[0]["code"] != "T01" || mine[0]["name"] != "Thành phố Thử Một" || mine[1]["code"] != "T02" {
		t.Fatalf("tỉnh mẫu: %v", mine)
	}

	items = get("/api/v1/admin-units?parent=T01&q=ben", adminUnitsReader)["items"].([]any) //nolint:forcetypeassert // test
	if len(items) != 50 {
		t.Fatalf("muốn đúng 50 xã (tối đa), có %d", len(items))
	}
	for _, it := range items {
		w := it.(map[string]any) //nolint:forcetypeassert // test
		if w["level"] != "ward" || w["parentCode"] != "T01" {
			t.Fatalf("xã sai tỉnh: %v", w)
		}
	}

	items = get("/api/v1/admin-units?parent=T01&q="+"B%E1%BA%BEN+s%E1%BB%91+7", adminUnitsReader)["items"].([]any) //nolint:forcetypeassert // test
	if len(items) != 1 || items[0].(map[string]any)["code"] != "T01007" {                                          //nolint:forcetypeassert // test
		t.Fatalf("q có dấu %q: %v", "BẾN số 7", items)
	}
	items = get("/api/v1/admin-units?parent=T99", adminUnitsReader)["items"].([]any) //nolint:forcetypeassert // test
	if len(items) != 0 {
		t.Fatalf("tỉnh lạ phải rỗng: %v", items)
	}
	expect(t, "q quá dài", serveAs(srv, keys, http.MethodGet,
		fmt.Sprintf("/api/v1/admin-units?parent=T01&q=%0101d", 0), adminUnitsReader, "", ""), http.StatusBadRequest, "bad_request")
}
