package queries_test

import (
	"slices"
	"testing"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/queries"
	"elimaz.com.vn/crm/internal/testkit"
)

// Kế hoạch của các truy vấn danh sách, tìm bằng crm_app trong ngữ cảnh tenant trên 10.000 mẫu, 50.000 biến thể: không
// Seq Scan (kể cả câu bên trong hàm SECURITY DEFINER, đọc bằng auto_explain).
func TestPlansHaveNoSeqScan(t *testing.T) {
	f := newFixture(t)
	dress, tag := f.catDress, f.tagNew
	first := queries.ProductFilter{SellerID: f.sellerA, Limit: 50}
	cases := []struct {
		name string
		sql  string
		args []any
	}{
		{"tìm tên qua hàm", "SELECT * FROM catalog.search_variants($1, $2, $3, 20)", []any{f.sellerA, "dam lua mau 12", "dam lua mau 12"}},
		{"tìm SKU qua hàm", "SELECT * FROM catalog.search_variants($1, $2, $3, 20)", []any{f.sellerA, "dam01-", "DAM01-"}},
		{"tìm barcode qua hàm", "SELECT * FROM catalog.search_variants($1, $2, $3, 20)", []any{f.sellerA, f.barcodeOfP00042S, f.barcodeOfP00042S}},
	}
	for _, c := range []struct {
		name string
		pf   queries.ProductFilter
	}{
		{"danh sách mẫu trang đầu", first},
		{"danh sách có kho", withWarehouse(first, f)},
		{"danh sách lọc đủ", queries.ProductFilter{
			SellerID: f.sellerA, Limit: 50, Status: []string{"active"}, CategoryID: &dress, TagID: &tag, Q: "dam lua",
			WarehouseID: f.warehouseA,
		}},
	} {
		sql, args, err := queries.ProductsSQL(c.pf, []uuid.UUID{f.catDress, f.catLong})
		if err != nil {
			t.Fatal(err)
		}
		cases = append(cases, struct {
			name string
			sql  string
			args []any
		}{c.name, sql, args})
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nodes, err := testkit.ExplainAsApp(t.Context(), f.app, f.tenantA, c.sql, c.args...)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(nodes, func(n string) bool { return n == "Index Scan" || n == "Bitmap Index Scan" || n == "Index Only Scan" }) {
				t.Fatalf("kế hoạch không dùng index: %v", nodes)
			}
			testkit.RequireNoSeqScan(t, nodes)
		})
	}
}

func withWarehouse(pf queries.ProductFilter, f *fixture) queries.ProductFilter {
	pf.WarehouseID = f.warehouseA
	return pf
}
