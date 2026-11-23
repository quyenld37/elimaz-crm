package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5"
)

const (
	listReaderW1    = "xem-sp-w1@elimaz.test"  // catalog.read mọi nơi, inventory.read chỉ ở kho w1
	listOtherB      = "xem-sp-b@elimaz.test"   // tenant B, catalog.read
	listCatalogOnly = "chi-xem-sp@elimaz.test" // catalog.read, không có quyền kho
)

// Danh sách mẫu, tìm biến thể qua HTTP: quyền, cột "Có thể bán" theo kho có phạm vi, con trỏ hỏng, tenant khác.
func TestListsAPI(t *testing.T) {
	t.Parallel()
	e := newInvAPIEnv(t)
	e.variant(e.w1, 7)
	e.variant(e.w1, 3)
	e.listUsers(true, "catalog.read")

	expect(t, "không có catalog.read", e.do(http.MethodGet, "/api/v1/catalog/products", invManager, "", ""),
		http.StatusForbidden, "forbidden")

	lst := mustOK(t, "listProducts", e.do(http.MethodGet, "/api/v1/catalog/products?q=quan+kho", listReaderW1, "", ""), http.StatusOK)
	items := lst["items"].([]any) //nolint:forcetypeassert // test
	if len(items) != 1 {
		t.Fatalf("muốn 1 mẫu QK01: %v", lst)
	}
	it := items[0].(map[string]any) //nolint:forcetypeassert // test
	if it["code"] != "QK01" || it["variantCount"] != float64(2) || it["minPrice"] != float64(100000) {
		t.Fatalf("mẫu sai: %v", it)
	}
	if _, ok := it["available"]; ok {
		t.Fatalf("không gửi kho thì không có available: %v", it)
	}
	if _, ok := lst["nextCursor"]; ok {
		t.Fatalf("một trang thì không có nextCursor: %v", lst)
	}

	expect(t, "kho khi không có inventory.read", e.do(http.MethodGet, "/api/v1/catalog/products?warehouseId="+e.w1.String(),
		listCatalogOnly, "", ""), http.StatusForbidden, "forbidden")
	expect(t, "kho w2 bằng quyền kho w1", e.do(http.MethodGet, "/api/v1/catalog/products?warehouseId="+e.w2.String(),
		listReaderW1, "", ""), http.StatusForbidden, "forbidden")
	lst = mustOK(t, "listProducts kho w1", e.do(http.MethodGet, "/api/v1/catalog/products?warehouseId="+e.w1.String(),
		listReaderW1, "", ""), http.StatusOK)
	it = lst["items"].([]any)[0].(map[string]any) //nolint:forcetypeassert // test
	if it["available"] != float64(10) {
		t.Fatalf("available ở w1 = 7 + 3: %v", it)
	}

	// Trang 1 dòng: có nextCursor; gửi lại ra trang sau rỗng.
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO catalog.products (tenant_id, seller_id, status, code, name, name_search)
			VALUES ($1, $2, 'draft', 'QK02', 'Quần kho hai', 'quan kho hai')`, e.tenant, e.seller)
		return err //nolint:wrapcheck // test
	})
	lst = mustOK(t, "trang 1", e.do(http.MethodGet, "/api/v1/catalog/products?limit=1&status=draft&status=active",
		listReaderW1, "", ""), http.StatusOK)
	cur, _ := lst["nextCursor"].(string)
	if len(lst["items"].([]any)) != 1 || cur == "" { //nolint:forcetypeassert // test
		t.Fatalf("trang 1: %v", lst)
	}
	lst = mustOK(t, "trang 2", e.do(http.MethodGet, "/api/v1/catalog/products?limit=1&cursor="+url.QueryEscape(cur),
		listReaderW1, "", ""), http.StatusOK)
	if items := lst["items"].([]any); len(items) != 1 || items[0].(map[string]any)["code"] != "QK01" { //nolint:forcetypeassert // test
		t.Fatalf("trang 2: %v", lst)
	}
	expect(t, "con trỏ hỏng", e.do(http.MethodGet, "/api/v1/catalog/products?cursor=xyz", listReaderW1, "", ""),
		http.StatusBadRequest, "invalid_input")
	expect(t, "q toàn khoảng trắng", e.do(http.MethodGet, "/api/v1/catalog/variants/search?q=+++", listReaderW1, "", ""),
		http.StatusBadRequest, "invalid_input")

	hits := mustOKArray(t, "searchVariants", e.do(http.MethodGet, "/api/v1/catalog/variants/search?q=qk01-", listReaderW1, "", ""))
	// Mẫu QK01 và biến thể đều active → bán được; xếp theo SKU.
	if len(hits) != 2 || hits[0].(map[string]any)["sku"] != "QK01-1" || hits[0].(map[string]any)["sellable"] != true { //nolint:forcetypeassert // test
		t.Fatalf("tìm SKU: %v", hits)
	}
	if got := mustOKArray(t, "tenant B", e.do(http.MethodGet, "/api/v1/catalog/variants/search?q=qk01", listOtherB, "", "")); len(got) != 0 {
		t.Fatalf("tenant B thấy biến thể của A: %v", got)
	}
	lst = mustOK(t, "tenant B list", e.do(http.MethodGet, "/api/v1/catalog/products", listOtherB, "", ""), http.StatusOK)
	if len(lst["items"].([]any)) != 0 { //nolint:forcetypeassert // test
		t.Fatalf("tenant B thấy mẫu của A: %v", lst)
	}
}

// listUsers tạo người đọc của test danh sách: listReaderW1 (inventory.read chỉ ở kho w1, thêm catalog.read mọi nơi khi
// readerCatalog), listCatalogOnly (catalog.read), listOtherB ở tenant B2 với quyền otherB mọi nơi.
func (e *invAPIEnv) listUsers(readerCatalog bool, otherB string) {
	t := e.t
	reader := e.staff.User(t, e.tenant, listReaderW1, "active")
	if readerCatalog {
		e.staff.Grant(t, e.tenant, reader, []string{"catalog.read"}, "all", "")
	}
	e.staff.Grant(t, e.tenant, reader, []string{"inventory.read"}, "warehouse", e.w1.String())
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, listCatalogOnly, "active"), []string{"catalog.read"}, "all", "")
	b := e.staff.Tenant(t, "B2")
	seedSellerWarehouse(t, e.staff, b)
	e.staff.Grant(t, b, e.staff.User(t, b, listOtherB, "active"), []string{otherB}, "all", "")
}

// mustOKArray đòi 200 và thân là mảng JSON.
func mustOKArray(t *testing.T, name string, rec *httptest.ResponseRecorder) []any {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", name, rec.Code, rec.Body.String())
	}
	var out []any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s: đọc JSON: %v (%s)", name, err, rec.Body.String())
	}
	return out
}

// Danh sách tồn qua HTTP (Task 334): phạm vi kho, warehouseId bắt buộc, lọc "Thiếu so với phần giữ" sau ca KH07
// (on_hand 2, held 4: đếm thiếu dưới phần giữ, đơn giữ nguyên), con trỏ hỏng, tenant khác.
func TestListBalancesAPI(t *testing.T) {
	t.Parallel()
	e := newInvAPIEnv(t)
	short := e.variant(e.w1, 5)
	e.variant(e.w1, 3)
	e.variant(e.w2, 9)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE inventory.stock_balances SET on_hand = 2, held = 4
			WHERE variant_id = $1 AND warehouse_id = $2`, short, e.w1)
		return err //nolint:wrapcheck // test
	})
	e.listUsers(false, "inventory.read")

	base := "/api/v1/inventory/balances?warehouseId="
	expect(t, "không có inventory.read", e.do(http.MethodGet, base+e.w1.String(), listCatalogOnly, "", ""),
		http.StatusForbidden, "forbidden")
	expect(t, "kho w2 bằng quyền kho w1", e.do(http.MethodGet, base+e.w2.String(), listReaderW1, "", ""),
		http.StatusForbidden, "forbidden")
	expect(t, "thiếu warehouseId", e.do(http.MethodGet, "/api/v1/inventory/balances", listReaderW1, "", ""),
		http.StatusBadRequest, "bad_request")
	expect(t, "con trỏ hỏng", e.do(http.MethodGet, base+e.w1.String()+"&cursor=xyz", listReaderW1, "", ""),
		http.StatusBadRequest, "invalid_input")

	lst := mustOK(t, "listBalances w1", e.do(http.MethodGet, base+e.w1.String(), listReaderW1, "", ""), http.StatusOK)
	if items := lst["items"].([]any); len(items) != 2 { //nolint:forcetypeassert // test
		t.Fatalf("w1 có 2 biến thể: %v", lst)
	}
	lst = mustOK(t, "onlyShort", e.do(http.MethodGet, base+e.w1.String()+"&onlyShort=true", listReaderW1, "", ""), http.StatusOK)
	items := lst["items"].([]any) //nolint:forcetypeassert // test
	if len(items) != 1 {
		t.Fatalf("sau KH07 muốn đúng 1 dòng thiếu: %v", lst)
	}
	it := items[0].(map[string]any) //nolint:forcetypeassert // test
	if it["variantId"] != short.String() || it["onHand"] != float64(2) || it["held"] != float64(4) ||
		it["available"] != float64(-2) || it["sku"] != "QK01-1" || it["productName"] != "Quần kho" ||
		it["productStatus"] != "active" || it["variantStatus"] != "active" {
		t.Fatalf("dòng thiếu sai: %v", it)
	}

	// Trang 1 dòng: có nextCursor; trang sau là dòng còn lại, không còn trang.
	lst = mustOK(t, "trang 1", e.do(http.MethodGet, base+e.w1.String()+"&limit=1", listReaderW1, "", ""), http.StatusOK)
	cur, _ := lst["nextCursor"].(string)
	if cur == "" {
		t.Fatalf("trang 1 thiếu nextCursor: %v", lst)
	}
	lst = mustOK(t, "trang 2", e.do(http.MethodGet, base+e.w1.String()+"&limit=1&cursor="+url.QueryEscape(cur),
		listReaderW1, "", ""), http.StatusOK)
	if _, more := lst["nextCursor"]; more || len(lst["items"].([]any)) != 1 { //nolint:forcetypeassert // test
		t.Fatalf("trang 2: %v", lst)
	}

	lst = mustOK(t, "tenant B", e.do(http.MethodGet, base+e.w1.String(), listOtherB, "", ""), http.StatusOK)
	if len(lst["items"].([]any)) != 0 { //nolint:forcetypeassert // test
		t.Fatalf("tenant B thấy số dư của A: %v", lst)
	}
}
