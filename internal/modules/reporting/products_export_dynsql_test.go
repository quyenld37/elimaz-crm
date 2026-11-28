package reporting

import (
	"context"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Câu danh sách sản phẩm trên PostgreSQL thật: đọc theo trang 2 dòng (keyset mã mẫu, SKU theo thứ tự byte) ra đúng tập
// và thứ tự của một lần đọc cả bảng; mẫu đã xóa và mẫu của người bán khác không có; phạm vi kho rỗng → Có thể bán 0.
func TestProductRows_Keyset(t *testing.T) {
	t.Parallel()
	e := newSetEnv(t)
	own := e.seed.Own()
	for _, x := range []struct{ product, sku string }{
		{"Mẫu B", "B-2"}, {"Mẫu B", "B-1"}, {"Mẫu A", "A-1"}, {"Mẫu C", "C-1"}, {"Mẫu C", "C-3"}, {"Mẫu C", "C-2"},
	} {
		e.seed.Variant(own, x.product, x.sku)
	}
	e.seed.Variant(e.seed.Seller("KHAC", "partner"), "Khác", "K-1")
	gone := e.seed.Variant(own, "Đã xóa", "X-1")
	e.st.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE catalog.products SET deleted_at = now()
			WHERE id = (SELECT product_id FROM catalog.variants WHERE id = $1)`, gone)
		return err //nolint:wrapcheck // test
	})
	s := DataScope{TenantID: e.tenant, SellerID: own, AllWarehouses: false}
	all, paged := readProducts(t, e, s, 1000), readProducts(t, e, s, 2)
	key := func(r productRow) string { return r.ProductCode + "/" + r.SKU }
	if len(all) != 6 || !slices.EqualFunc(all, paged, func(a, b productRow) bool { return key(a) == key(b) }) {
		t.Fatalf("một lần %d dòng, theo trang %d dòng", len(all), len(paged))
	}
	keys := make([]string, 0, len(all))
	for _, r := range all {
		keys = append(keys, key(r))
		if r.Available != 0 || r.SKU == "K-1" || r.SKU == "X-1" {
			t.Errorf("dòng lạ %+v", r)
		}
	}
	if !slices.IsSorted(keys) {
		t.Errorf("thứ tự = %v", keys)
	}
}

// readProducts đọc cả danh sách sản phẩm theo trang limit dòng (keyset) trong một transaction báo cáo.
func readProducts(t *testing.T, e setEnv, s DataScope, limit int) []productRow {
	t.Helper()
	var out []productRow
	err := inReportTx(t, e.st.App, s, func(ctx context.Context, tx pgx.Tx) error {
		out = nil
		var after *productKey
		for range 100 {
			rows, err := productRows(ctx, tx, s, &ProductsExportParams{}, after, limit)
			if err != nil {
				return err
			}
			out = append(out, rows...)
			if len(rows) < limit {
				return nil
			}
			last := rows[len(rows)-1]
			after = &productKey{code: last.ProductCode, sku: last.SKU}
		}
		t.Fatal("không dừng")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
