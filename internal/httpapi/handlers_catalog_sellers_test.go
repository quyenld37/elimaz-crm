package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/catalogtest"
	"elimaz.com.vn/crm/internal/testkit/shoptest"
)

// TestCatalogAPI_PartnerSellerIs404 (KH14, ADR-29): API chỉ làm việc với người bán mặc định; mẫu, biến thể của người
// bán PARTNER1 cùng shop → 404 ở mọi đường đọc và sửa, không đổi dữ liệu, không có trong danh sách. Mẫu cùng mã AO01
// của ELIMAZ vẫn tạo được.
func TestCatalogAPI_PartnerSellerIs404(t *testing.T) {
	e := newCatalogAPIEnv(t)
	_, partner := shoptest.SeedSellers(t, e.staff.Owner, e.tenant)
	var pid, vid uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		pid = catalogtest.Product(t, tx, e.tenant, partner, "AO01")
		vid = catalogtest.Variant(t, tx, e.tenant, partner, pid, 1, 100000)
		return nil
	})
	p := "/api/v1/catalog/products/" + pid.String()
	for _, c := range []struct{ name, method, path, body string }{
		{"getProduct", http.MethodGet, p, ""},
		{"updateProduct", http.MethodPatch, p, `{"version":1,"name":"X"}`},
		{"addVariant", http.MethodPost, p + "/variants", `{"productVersion":1,"sku":"AO01-X","color":"Đỏ","size":"X","price":1}`},
		{"updateVariant", http.MethodPatch, "/api/v1/catalog/variants/" + vid.String(), `{"version":1,"price":1}`},
		{"getClassification", http.MethodGet, p + "/classification", ""},
		{"setProductClassification", http.MethodPut, p + "/classification", `{"version":1,"attributeValueIds":[],"tagIds":[]}`},
		{"reorderProductImages", http.MethodPut, p + "/images/order", `{"productVersion":1,"imageIds":["` + uuid.NewString() + `"]}`},
		{"deleteProductImage", http.MethodDelete, p + "/images/" + uuid.NewString(), ""},
	} {
		key := ""
		if c.method != http.MethodGet {
			key = "k-" + c.name
		}
		expect(t, c.name+" mẫu người bán khác", e.do(c.method, c.path, catalogManager, key, c.body), http.StatusNotFound, "not_found")
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM catalog.products WHERE id = $1 AND version = 1
		AND name = 'Áo thử'`, pid); n != 1 {
		t.Errorf("mẫu của người bán khác bị đổi")
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM catalog.variants WHERE seller_id = $1`, partner); n != 1 {
		t.Errorf("người bán khác có %d biến thể, mong 1 (không thêm qua API)", n)
	}
	own := e.do(http.MethodPost, "/api/v1/catalog/products", catalogManager, "c-ao01",
		`{"code":"AO01","name":"Áo","variants":[{"sku":"SKU-1","color":"Đỏ","size":"S1","price":1}]}`)
	if own.Code != http.StatusCreated {
		t.Fatalf("ELIMAZ tạo AO01, SKU-1 trùng mã của người bán khác: %d %s", own.Code, own.Body.String())
	}
	list := e.do(http.MethodGet, "/api/v1/catalog/products", catalogViewer, "", "")
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), pid.String()) ||
		len(decodeJSON(t, list)["items"].([]any)) != 1 { //nolint:forcetypeassert // test
		t.Errorf("listProducts phải chỉ có mẫu của ELIMAZ: %d %s", list.Code, list.Body.String())
	}
}

// TestCatalogAPI_RetrySameKeyAfterConflict (ghi chú reviewer Task 27): yêu cầu bị từ chối 409 không lưu theo khóa
// chống trùng; gửi lại cùng khóa với version đúng được xử lý như yêu cầu mới.
func TestCatalogAPI_RetrySameKeyAfterConflict(t *testing.T) {
	e := newCatalogAPIEnv(t)
	id := e.createDam01()["id"].(string) //nolint:forcetypeassert // test
	path := "/api/v1/catalog/products/" + id
	expect(t, "version sai", e.do(http.MethodPatch, path, catalogManager, "k409", `{"version":7,"name":"Tên mới"}`),
		http.StatusConflict, "version_conflict")
	rec := e.do(http.MethodPatch, path, catalogManager, "k409", `{"version":1,"name":"Tên mới"}`)
	if rec.Code != http.StatusOK || decodeJSON(t, rec)["name"] != "Tên mới" || decodeJSON(t, rec)["version"] != float64(2) {
		t.Fatalf("gửi lại cùng khóa sau 409: %d %s", rec.Code, rec.Body.String())
	}
}
