package queries_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/queries"
)

// testBalances: danh sách tồn (Task 334) trên database của TestProducts (fixture_test.go: 50.000 biến thể có số dư ở
// kho A; mẫu P00003 có 5 biến thể on_hand 10, held 12 → có thể bán −2; mọi biến thể khác on_hand 10, held 1 → 9).
// Dùng chung database để gói không dựng hai lần 50.000 biến thể.
func testBalances(t *testing.T, f *fixture) {
	t.Run("Columns", func(t *testing.T) { testBalanceColumns(t, f) })
	t.Run("OnlyShort", func(t *testing.T) { testBalanceOnlyShort(t, f) })
	t.Run("Search", func(t *testing.T) { testBalanceSearch(t, f) })
	t.Run("PagesAll", func(t *testing.T) { testBalancePagesAll(t, f) })
	t.Run("OtherWarehouseAndTenant", func(t *testing.T) { testBalanceOtherWarehouseAndTenant(t, f) })
	t.Run("Invalid", func(t *testing.T) { testBalanceInvalid(t, f) })
}

func balances(t *testing.T, f *fixture, tenant uuid.UUID, bf queries.BalanceFilter) ([]queries.BalanceRow, *queries.BalanceCursor) {
	t.Helper()
	var rows []queries.BalanceRow
	var next *queries.BalanceCursor
	f.inTenant(t, tenant, func(tx pgx.Tx) error {
		var err error
		rows, next, err = queries.ListBalances(t.Context(), tx, bf)
		return err //nolint:wrapcheck // test
	})
	return rows, next
}

func (f *fixture) balanceFilter() queries.BalanceFilter {
	return queries.BalanceFilter{SellerID: f.sellerA, WarehouseID: f.warehouseA, Limit: 50}
}

// Đủ cột của một dòng; thứ tự biến thể mới tạo trước (variant_id giảm).
func testBalanceColumns(t *testing.T, f *fixture) {
	bf := f.balanceFilter()
	bf.Q = "P00042-S"
	rows, next := balances(t, f, f.tenantA, bf)
	if len(rows) != 1 || next != nil {
		t.Fatalf("muốn đúng 1 dòng P00042-S, nhận %d (next %v)", len(rows), next)
	}
	r := rows[0]
	if r.SKU != "P00042-S" || r.Barcode != f.barcodeOfP00042S || r.ProductCode != "P00042" || r.Color != "Đen" ||
		r.Size != "S" || r.ProductName != "Quần lụa mẫu 42" {
		t.Fatalf("cột mô tả sai: %+v", r)
	}
	// P00042: g % 4 = 2 → draft; biến thể active.
	if r.ProductStatus != "draft" || r.VariantStatus != "active" {
		t.Fatalf("trạng thái sai: %+v", r)
	}
	if r.OnHand != 10 || r.Held != 1 || r.InTransit != 0 || r.Damaged != 0 || r.Available != 9 {
		t.Fatalf("số sai: %+v", r)
	}
	rows, _ = balances(t, f, f.tenantA, f.balanceFilter())
	for i := 1; i < len(rows); i++ {
		if rows[i-1].VariantID.String() <= rows[i].VariantID.String() {
			t.Fatalf("không theo thứ tự variant_id giảm ở dòng %d", i)
		}
	}
}

// OnlyShort: đúng 5 biến thể của P00003 (có thể bán −2), không dòng nào khác.
func testBalanceOnlyShort(t *testing.T, f *fixture) {
	bf := f.balanceFilter()
	bf.OnlyShort = true
	rows, next := balances(t, f, f.tenantA, bf)
	if len(rows) != 5 || next != nil {
		t.Fatalf("muốn 5 dòng thiếu, nhận %d (next %v)", len(rows), next)
	}
	for _, r := range rows {
		if r.ProductCode != "P00003" || r.Available != -2 {
			t.Fatalf("dòng không thiếu: %+v", r)
		}
	}
	bf.Q = "P00003-M"
	if rows, _ := balances(t, f, f.tenantA, bf); len(rows) != 1 || rows[0].SKU != "P00003-M" {
		t.Fatalf("thiếu + tìm: %+v", rows)
	}
	bf.Q = "P00004"
	if rows, _ := balances(t, f, f.tenantA, bf); len(rows) != 0 {
		t.Fatalf("P00004 không thiếu: %+v", rows)
	}
}

// Tìm theo barcode, mã mẫu, tên không dấu (tối đa 100 biến thể khớp).
func testBalanceSearch(t *testing.T, f *fixture) {
	bf := f.balanceFilter()
	bf.Q = f.barcodeOfP00042S
	if rows, _ := balances(t, f, f.tenantA, bf); len(rows) != 1 || rows[0].SKU != "P00042-S" {
		t.Fatalf("tìm barcode: %+v", rows)
	}
	bf.Q = "dam01"
	rows, _ := balances(t, f, f.tenantA, bf)
	if len(rows) != 4 { // DAM01-S/M/L, DAM011-S
		t.Fatalf("tìm mã mẫu DAM01: %d dòng", len(rows))
	}
	bf.Q, bf.Limit = "dam lua", 100
	rows, next := balances(t, f, f.tenantA, bf)
	if len(rows) != 100 || next != nil {
		t.Fatalf("tên khớp nhiều: muốn 100 dòng một trang (giới hạn khớp), nhận %d (next %v)", len(rows), next)
	}
}

// Đi hết các trang 50 dòng: đủ mọi biến thể có số dư ở kho A (gồm DAM01, DAM011), không trùng.
func testBalancePagesAll(t *testing.T, f *fixture) {
	var want int
	f.inTenant(t, f.tenantA, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), "SELECT count(*) FROM inventory.stock_available_v WHERE warehouse_id = $1",
			f.warehouseA).Scan(&want)
	})
	bf := f.balanceFilter()
	bf.Limit = 100
	seen := map[uuid.UUID]bool{}
	for page := 0; ; page++ {
		if page > want/100+2 {
			t.Fatal("trang không dừng")
		}
		rows, next := balances(t, f, f.tenantA, bf)
		for _, r := range rows {
			if seen[r.VariantID] {
				t.Fatalf("trùng %s", r.SKU)
			}
			seen[r.VariantID] = true
		}
		if next == nil {
			break
		}
		bf.Cursor = next
	}
	if len(seen) != want {
		t.Fatalf("đi hết trang: %d dòng, muốn %d", len(seen), want)
	}
}

// Kho khác, người bán khác, tenant khác: 0 dòng.
func testBalanceOtherWarehouseAndTenant(t *testing.T, f *fixture) {
	bf := f.balanceFilter()
	bf.WarehouseID = uuid.Must(uuid.NewV7())
	if rows, _ := balances(t, f, f.tenantA, bf); len(rows) != 0 {
		t.Fatalf("kho không có: %d dòng", len(rows))
	}
	bf = f.balanceFilter()
	bf.SellerID = f.sellerB
	if rows, _ := balances(t, f, f.tenantA, bf); len(rows) != 0 {
		t.Fatalf("người bán khác: %d dòng", len(rows))
	}
	if rows, _ := balances(t, f, f.tenantB, f.balanceFilter()); len(rows) != 0 {
		t.Fatalf("tenant B thấy số dư của A: %d dòng", len(rows))
	}
}

func testBalanceInvalid(t *testing.T, f *fixture) {
	for name, bf := range map[string]queries.BalanceFilter{
		"limit 0":   {SellerID: f.sellerA, WarehouseID: f.warehouseA},
		"limit 101": {SellerID: f.sellerA, WarehouseID: f.warehouseA, Limit: 101},
		"không kho": {SellerID: f.sellerA, Limit: 10},
		"q trắng":   {SellerID: f.sellerA, WarehouseID: f.warehouseA, Limit: 10, Q: "   "},
	} {
		f.inTenant(t, f.tenantA, func(tx pgx.Tx) error {
			_, _, err := queries.ListBalances(t.Context(), tx, bf)
			if !errors.Is(err, queries.ErrInvalidInput) {
				t.Errorf("%s: muốn ErrInvalidInput, nhận %v", name, err)
			}
			return nil
		})
	}
}

func testBalanceCursorRoundTrip(t *testing.T) {
	c := queries.BalanceCursor{VariantID: uuid.Must(uuid.NewV7())}
	got, err := queries.ParseBalanceCursor(c.String())
	if err != nil || got != c {
		t.Fatalf("con trỏ đi về sai: %v %v", got, err)
	}
	for _, s := range []string{"", "xyz", "!!", "bm90LXV1aWQ"} {
		if _, err := queries.ParseBalanceCursor(s); !errors.Is(err, queries.ErrInvalidInput) {
			t.Fatalf("%q: muốn ErrInvalidInput, nhận %v", s, err)
		}
	}
}
