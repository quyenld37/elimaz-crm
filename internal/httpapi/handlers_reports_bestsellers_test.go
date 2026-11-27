package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/reportseed"
)

// GET /api/v1/reports/sales/best-sellers trên kịch bản chín đơn (Task 232): mặc định sku, placed, 50 dòng; mức product
// không có variantId, sku, color, size; limit 2 → 2 dòng, totalRows 3; limit 501, level lạ → 400 bad_params (ogen theo
// hợp đồng); ngày sai → 400 bad_date; người chỉ có report.inventory.read → 403.
func TestReportsBestSellers(t *testing.T) {
	t.Parallel()
	staff, keys := testkit.OpenStaff(t), cfaccesstest.AccessKeyServer(t)
	tenant := staff.Tenant(t, "A")
	sc := reportseed.NineOrders(reportseed.New(t, staff.Owner, tenant), "2026-11-10")
	staff.Grant(t, tenant, staff.User(t, tenant, "banhang@elimaz.test", "active"), []string{"report.sales.read"}, "all", "")
	staff.Grant(t, tenant, staff.User(t, tenant, "kho@elimaz.test", "active"), []string{"report.inventory.read"}, "all", "")
	srv := testServer(t, staff, keys, 0)
	path := bestSellersPath
	r := getBestSellers(t, srv, keys, "mặc định", "")
	if r.Status != "ready" || r.Level != "sku" || r.Basis != "placed" || r.TotalRows != 4 || len(r.Rows) != 4 ||
		r.Period["from"] != "2026-11-10" || r.Period["to"] != "2026-11-10" {
		t.Fatalf("mặc định: %+v", r)
	}
	top := r.Rows[0]
	if top.VariantID == nil || *top.VariantID != sc.AMedium.String() || top.SKU == nil || *top.SKU != "AOA-M" ||
		top.Name != "Áo A" || top.Color == nil || top.Size == nil || top.Quantity != 4 || top.Orders != 2 ||
		top.GoodsAmount != 600_000 || top.ReturnedQuantity != 1 {
		t.Errorf("dòng đầu: %+v", top)
	}
	if c := r.Rows[3]; c.SKU == nil || *c.SKU != "VAYC" || c.Quantity != 2 || c.GoodsAmount != 360_000 {
		t.Errorf("váy C: %+v", c)
	}

	r = getBestSellers(t, srv, keys, "product limit 2", "&level=product&limit=2")
	if r.Level != "product" || r.TotalRows != 3 || len(r.Rows) != 2 || r.Rows[0].Name != "Áo A" || r.Rows[0].Quantity != 7 ||
		r.Rows[0].Orders != 4 || r.Rows[0].GoodsAmount != 1_050_000 || r.Rows[1].Name != "Quần B" {
		t.Fatalf("product limit 2: %+v", r)
	}
	for _, x := range r.Rows {
		if x.VariantID != nil || x.SKU != nil || x.Color != nil || x.Size != nil {
			t.Errorf("mức mẫu có trường của biến thể: %+v", x)
		}
	}

	r = getBestSellers(t, srv, keys, "product delivered", "&level=product&basis=delivered")
	if r.Basis != "delivered" || len(r.Rows) != 1 || r.Rows[0].Quantity != 5 || r.Rows[0].GoodsAmount != 750_000 {
		t.Fatalf("product delivered: %+v", r)
	}

	for _, c := range []struct {
		name, query, email string
		status             int
		code               string
	}{
		{"limit 501", "&limit=501", "banhang@elimaz.test", http.StatusBadRequest, "bad_params"},
		{"limit 0", "&limit=0", "banhang@elimaz.test", http.StatusBadRequest, "bad_params"},
		{"level lạ", "&level=color", "banhang@elimaz.test", http.StatusBadRequest, "bad_params"},
		{"basis completed", "&basis=completed", "banhang@elimaz.test", http.StatusBadRequest, "bad_params"},
		{"chỉ có quyền tồn", "", "kho@elimaz.test", http.StatusForbidden, "forbidden"},
	} {
		expect(t, c.name, serveAs(srv, keys, http.MethodGet, path+c.query, c.email, "", ""), c.status, c.code)
	}
	expect(t, "from sau to", serveAs(srv, keys, http.MethodGet, "/api/v1/reports/sales/best-sellers?from=2026-11-11&to=2026-11-10",
		"banhang@elimaz.test", "", ""), http.StatusBadRequest, "bad_date")
}

// bestSellerRowBody, bestSellersBody: thân JSON của GET bán chạy mà test đọc.
type bestSellerRowBody struct {
	ProductID        string  `json:"productId"`
	VariantID        *string `json:"variantId"`
	SKU              *string `json:"sku"`
	Name             string  `json:"name"`
	Color            *string `json:"color"`
	Size             *string `json:"size"`
	Quantity         int64   `json:"quantity"`
	Orders           int64   `json:"orders"`
	GoodsAmount      int64   `json:"goodsAmount"`
	ReturnedQuantity int64   `json:"returnedQuantity"`
}

type bestSellersBody struct {
	Status    string              `json:"status"`
	Period    map[string]string   `json:"period"`
	Level     string              `json:"level"`
	Basis     string              `json:"basis"`
	Rows      []bestSellerRowBody `json:"rows"`
	TotalRows int64               `json:"totalRows"`
}

// getBestSellers gọi GET bán chạy kỳ 10/11/2026 (thêm query) bằng banhang@elimaz.test, đòi 200.
func getBestSellers(t *testing.T, srv http.Handler, keys *cfaccesstest.AccessKeys, name, query string) bestSellersBody {
	t.Helper()
	rec := serveAs(srv, keys, http.MethodGet, bestSellersPath+query, "banhang@elimaz.test", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: mã %d (%s)", name, rec.Code, rec.Body.String())
	}
	var r bestSellersBody
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

const bestSellersPath = "/api/v1/reports/sales/best-sellers?from=2026-11-10&to=2026-11-10"
