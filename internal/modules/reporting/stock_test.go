package reporting_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/reporting"
)

// stockEnv là snapEnv (kho W, áo A-S) cộng kho W2 và bộ chạy tại chỗ trên crm_app.
type stockEnv struct {
	*snapEnv
	wh2 uuid.UUID
	rep *reporting.Interactive
}

func newStockEnv(t *testing.T) stockEnv {
	t.Helper()
	e := newSnapEnv(t, nil)
	return stockEnv{snapEnv: e, wh2: e.seed.Warehouse("Kho W2"), rep: reporting.NewInteractive(e.s.App, reporting.InteractiveOptions{})}
}

// invUser là người xem báo cáo tồn: không kho nào → phạm vi all; có kho → chỉ các kho đó.
func (e stockEnv) invUser(whs ...uuid.UUID) identity.AuthUser {
	u := identity.AuthUser{TenantID: e.tenant, UserID: uuid.New()}
	if len(whs) == 0 {
		u.Grants = []identity.Grant{{Permission: "report.inventory.read", Scope: identity.Scope{Type: identity.ScopeAll}}}
	}
	for _, w := range whs {
		u.Grants = append(u.Grants, identity.Grant{Permission: "report.inventory.read", Scope: identity.Scope{Type: identity.ScopeWarehouse, ID: w.String()}})
	}
	return u
}

// stock chạy báo cáo tồn và trả kết quả hay lỗi.
func (e stockEnv) stock(t *testing.T, u identity.AuthUser, p reporting.StockParams) (reporting.StockReport, error) {
	t.Helper()
	res, err := e.rep.Run(t.Context(), u, reporting.KeyStock, &p)
	if err != nil {
		return reporting.StockReport{}, err //nolint:wrapcheck // test
	}
	r, ok := res.(reporting.StockReport)
	if !ok {
		t.Fatalf("kết quả kiểu %T, mong reporting.StockReport", res)
	}
	if r.Status != "ready" || r.Rows == nil {
		t.Errorf("status %q, rows nil = %v", r.Status, r.Rows == nil)
	}
	for _, row := range r.Rows {
		if row.Available != row.OnHand-row.Damaged-row.Held-row.InTransit {
			t.Errorf("dòng %s: available %d ≠ onHand − damaged − held − inTransit", row.SKU, row.Available)
		}
	}
	return r, nil
}

func (e stockEnv) mustStock(t *testing.T, u identity.AuthUser, p reporting.StockParams) reporting.StockReport {
	t.Helper()
	r, err := e.stock(t, u, p)
	if err != nil {
		t.Fatalf("báo cáo tồn %+v: %v", p, err)
	}
	return r
}

// planLedger là sổ của Task 233 (kế hoạch M6-10) trên áo A-S ở kho W, rồi chụp 15, 16, 17 (Now = 18/11 08:00).
func (e stockEnv) planLedger(t *testing.T) {
	t.Helper()
	e.snapEnv.planLedger()
	if got := e.run0(t); !slices.Equal(got, []string{"2026-11-15", "2026-11-16", "2026-11-17"}) {
		t.Fatalf("ngày chụp %v", got)
	}
}

// run0 chụp tồn với Now = 18/11 08:00 giờ VN.
func (e stockEnv) run0(t *testing.T) []string {
	t.Helper()
	return e.run(t, snapHandler(vnAt(18, 8, 0, 0), nil)).Days
}

// ownerExec chạy một câu ghi bằng chủ bảng trong ngữ cảnh tenant (RLS áp cả cho chủ bảng).
func (e stockEnv) ownerExec(t *testing.T, q string, args ...any) {
	t.Helper()
	e.s.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), q, args...)
		return err //nolint:wrapcheck // test
	})
}

// stockAt là tham số báo cáo tồn ngày date với phần lọc, trang pg.
func stockAt(date string, pg reporting.StockPage) reporting.StockParams {
	return reporting.StockParams{Date: date, StockPage: pg}
}

// onlyRow đòi báo cáo có đúng một dòng và trả nó.
func onlyRow(t *testing.T, r reporting.StockReport) reporting.StockRow {
	t.Helper()
	if len(r.Rows) != 1 {
		t.Fatalf("số dòng %d: %+v", len(r.Rows), r.Rows)
	}
	return r.Rows[0]
}

func checkStockRow(t *testing.T, name string, row reporting.StockRow, onHand, held, inTransit, damaged int64) {
	t.Helper()
	if row.OnHand != onHand || row.Held != held || row.InTransit != inTransit || row.Damaged != damaged {
		t.Errorf("%s: {%d, %d, %d, %d}, mong {%d, %d, %d, %d}", name, row.OnHand, row.Held, row.InTransit, row.Damaged,
			onHand, held, inTransit, damaged)
	}
}

// Hôm nay đọc số dư (live), ngày đã qua đọc bản chụp; tên kho, SKU, tên mẫu, màu, size đúng; FirstSnapshotDay là ngày
// chụp đầu; ngày chưa chụp, trước bản chụp đầu → ErrNoSnapshot; ngày mai, ngày sai dạng → ErrBadDate.
func TestStock_LiveAndSnapshot(t *testing.T) {
	t.Parallel()
	e := newStockEnv(t)
	u := e.invUser()

	r, err := e.stock(t, u, reporting.StockParams{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != reporting.SourceLive || r.FirstSnapshotDay != nil || len(r.Rows) != 0 || r.Next != nil {
		t.Errorf("tenant chưa có tồn: %+v", r)
	}

	e.planLedger(t)
	today := reporting.Today(time.Now())
	for _, date := range []string{"", today} {
		r = e.mustStock(t, u, reporting.StockParams{Date: date})
		row := onlyRow(t, r)
		checkStockRow(t, "hôm nay", row, 18, 0, 0, 0)
		if r.Source != reporting.SourceLive || r.Date != today || r.FirstSnapshotDay == nil || *r.FirstSnapshotDay != "2026-11-15" {
			t.Errorf("date %q: nguồn %q, ngày %q, ngày chụp đầu %v", date, r.Source, r.Date, r.FirstSnapshotDay)
		}
		if row.Available != 18 || row.SKU != "AO-A-S" || row.ProductName != "Áo A" || row.Color != "Mặc định" ||
			row.Size != "AO-A-S" || row.WarehouseID != e.wh || row.WarehouseName != "Kho W" || row.VariantID != e.variant {
			t.Errorf("dòng hôm nay %+v", row)
		}
	}

	r = e.mustStock(t, u, reporting.StockParams{Date: "2026-11-16"})
	row := onlyRow(t, r)
	checkStockRow(t, "16/11", row, 20, 0, 2, 0)
	if r.Source != reporting.SourceSnapshot || r.Date != "2026-11-16" || row.Available != 18 || row.WarehouseName != "Kho W" {
		t.Errorf("16/11: nguồn %q, ngày %q, dòng %+v", r.Source, r.Date, row)
	}
	checkStockRow(t, "15/11", onlyRow(t, e.mustStock(t, u, reporting.StockParams{Date: "2026-11-15"})), 20, 0, 0, 0)
	checkStockRow(t, "17/11", onlyRow(t, e.mustStock(t, u, reporting.StockParams{Date: "2026-11-17"})), 18, 0, 0, 0)

	for _, c := range []struct {
		date string
		want error
	}{
		{"2026-11-10", reporting.ErrNoSnapshot}, // trước bản chụp đầu
		{"2026-11-20", reporting.ErrNoSnapshot}, // ngày đã qua chưa chụp
		{reporting.Today(time.Now().Add(24 * time.Hour)), reporting.ErrBadDate},
		{"2026-02-30", reporting.ErrBadDate},
		{"16/11/2026", reporting.ErrBadDate},
	} {
		if _, err := e.stock(t, u, reporting.StockParams{Date: c.date}); !errors.Is(err, c.want) {
			t.Errorf("date %q: %v, mong %v", c.date, err, c.want)
		}
	}
}

// Ngày đã qua đọc bản chụp, không tính lại từ sổ: dòng sổ chèn muộn (chủ bảng) có mốc trước mốc 16/11 không đổi số của
// ngày 16.
func TestStock_SnapshotNotRecomputed(t *testing.T) {
	t.Parallel()
	e := newStockEnv(t)
	e.planLedger(t)
	e.ownerExec(t, `INSERT INTO inventory.stock_ledger (tenant_id, variant_id, warehouse_id, bucket, delta, doc_type,
		doc_id, step, actor_type, created_at) VALUES ($1, $2, $3, 'on_hand', 5, 'import', $4, 'late', 'system', $5)`,
		e.tenant, e.variant, e.wh, uuid.New(), vnAt(16, 12, 0, 0))
	checkStockRow(t, "16/11 sau dòng sổ muộn", onlyRow(t, e.mustStock(t, e.invUser(), reporting.StockParams{Date: "2026-11-16"})), 20, 0, 2, 0)
}

// Người chỉ có kho W1 không thấy dòng của W2 (cả hôm nay lẫn bản chụp); lọc warehouseId = W2 → ErrWarehouseForbidden;
// lọc kho trong phạm vi chỉ ra kho đó; người phạm vi all lọc W2 chỉ thấy W2; dòng mọi ngăn bằng 0 bị ẩn.
func TestStock_WarehouseScope(t *testing.T) {
	t.Parallel()
	e := newStockEnv(t)
	e.seed.Ledger(e.variant, e.wh, "on_hand", 3, "import", vnAt(15, 10, 0, 0))
	e.seed.Ledger(e.variant, e.wh2, "on_hand", 4, "import", vnAt(15, 11, 0, 0))
	e.seed.Ledger(e.variant, e.wh2, "damaged", 1, "damage", vnAt(15, 12, 0, 0))
	zero := e.seed.Variant(e.seed.Own(), "Áo A", "AO-A-M")
	e.seed.Ledger(zero, e.wh, "on_hand", 2, "import", vnAt(15, 10, 0, 0))
	e.seed.Ledger(zero, e.wh, "on_hand", -2, "export", vnAt(15, 11, 0, 0))
	e.run0(t)

	w1 := e.invUser(e.wh)
	for _, date := range []string{"", "2026-11-15"} {
		row := onlyRow(t, e.mustStock(t, w1, reporting.StockParams{Date: date}))
		if row.WarehouseID != e.wh || row.OnHand != 3 {
			t.Errorf("W1 ngày %q: %+v", date, row)
		}
		if _, err := e.stock(t, w1, stockAt(date, reporting.StockPage{WarehouseID: &e.wh2})); !errors.Is(err, reporting.ErrWarehouseForbidden) {
			t.Errorf("W1 lọc W2 ngày %q: %v", date, err)
		}
		row = onlyRow(t, e.mustStock(t, w1, stockAt(date, reporting.StockPage{WarehouseID: &e.wh})))
		if row.WarehouseID != e.wh {
			t.Errorf("W1 lọc W1: %+v", row)
		}
		all := e.mustStock(t, e.invUser(), reporting.StockParams{Date: date})
		if len(all.Rows) != 2 {
			t.Errorf("phạm vi all ngày %q: %d dòng (dòng mọi ngăn 0 phải ẩn): %+v", date, len(all.Rows), all.Rows)
		}
		row = onlyRow(t, e.mustStock(t, e.invUser(), stockAt(date, reporting.StockPage{WarehouseID: &e.wh2})))
		checkStockRow(t, "all lọc W2", row, 4, 0, 0, 1)
		if row.Available != 3 {
			t.Errorf("W2 available %d, mong 3", row.Available)
		}
	}
}

// Tham số sai → ErrBadParams: limit ngoài 1..500, con trỏ hỏng, tiền tố dài quá 64.
func TestStock_BadParams(t *testing.T) {
	t.Parallel()
	for _, pg := range []reporting.StockPage{
		{Limit: 501}, {Limit: -1}, {Cursor: "!!!"}, {Cursor: "AAAA"}, {SKUPrefix: strings.Repeat("A", 65)},
	} {
		p := stockAt("", pg)
		if err := p.Validate(); !errors.Is(err, reporting.ErrBadParams) {
			t.Errorf("%+v: %v", p, err)
		}
	}
	c := reporting.StockCursor{SKU: "AO-1", VariantID: uuid.New(), WarehouseID: uuid.New()}
	got, err := reporting.ParseStockCursor(c.String())
	if err != nil || got != c {
		t.Errorf("con trỏ đi về: %+v, %v", got, err)
	}
}

// bulkVariants chèn n biến thể (mã AO-0000…, QU-0000… xen kẽ) của người bán own, mỗi biến thể on_hand 1 ở kho W, cùng
// một dòng sổ nhập 15/11 (số dư = tổng sổ), bằng chủ bảng trong một câu mỗi bảng; ANALYZE để kế hoạch như dữ liệu thật.
func (e stockEnv) bulkVariants(t *testing.T, n int) {
	t.Helper()
	ctx := t.Context()
	own := e.seed.Own()
	at := vnAt(15, 9, 0, 0)
	for i, c := range []struct {
		q    string
		args []any
	}{
		{`INSERT INTO catalog.products (tenant_id, seller_id, code, name, name_search, status)
			VALUES ($1, $2, 'BULK', 'Mẫu nhiều', 'mau nhieu', 'active')`, []any{e.tenant, own}},
		{`INSERT INTO catalog.variants (tenant_id, seller_id, product_id, sku, barcode, color, size, price)
			SELECT $1, $2, p.id, (CASE WHEN g % 2 = 0 THEN 'AO-' ELSE 'QU-' END) || lpad(g::text, 4, '0'), 'BK' || lpad(g::text, 6, '0'), 'Đen', 'S' || g, 100000
			FROM catalog.products p, generate_series(1, $3::int) g WHERE p.tenant_id = $1 AND p.code = 'BULK'`, []any{e.tenant, own, n}},
		{`INSERT INTO inventory.stock_balances (tenant_id, variant_id, warehouse_id, seller_id, on_hand)
			SELECT $1, v.id, $3, $2, 1 FROM catalog.variants v WHERE v.tenant_id = $1 AND v.barcode LIKE 'BK%'`, []any{e.tenant, own, e.wh}},
		{`INSERT INTO inventory.stock_ledger (tenant_id, variant_id, warehouse_id, bucket, delta, doc_type, doc_id, step, actor_type, created_at)
			SELECT $1, v.id, $2, 'on_hand', 1, 'import', gen_random_uuid(), 'bulk', 'system', $3
			FROM catalog.variants v WHERE v.tenant_id = $1 AND v.barcode LIKE 'BK%'`, []any{e.tenant, e.wh, at}},
		{`ANALYZE catalog.variants, inventory.stock_balances, inventory.stock_ledger`, nil},
	} {
		if err := e.s.TryExec(ctx, e.tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, c.q, c.args...)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatalf("dựng %d biến thể, câu %d: %v", n, i, err)
		}
	}
}

// 1.200 biến thể, limit 500 → ba trang 500, 500, 200; không trùng; tăng dần theo (SKU, biến thể, kho); trang cuối không
// có con trỏ. Tiền tố "ao-" (bỏ dấu cách, không phân biệt hoa thường) chỉ ra SKU bắt đầu bằng AO-, đủ 600, qua trang.
func TestStock_Pagination(t *testing.T) {
	t.Parallel()
	e := newStockEnv(t)
	e.bulkVariants(t, 1200)
	e.run0(t)
	u := e.invUser()
	for _, date := range []string{"", "2026-11-15"} {
		var all []reporting.StockRow
		var sizes []int
		p := stockAt(date, reporting.StockPage{Limit: 500})
		for range 5 {
			r := e.mustStock(t, u, p)
			all = append(all, r.Rows...)
			sizes = append(sizes, len(r.Rows))
			if r.Next == nil {
				break
			}
			p.Cursor = r.Next.String()
		}
		if !slices.Equal(sizes, []int{500, 500, 200}) {
			t.Errorf("ngày %q: cỡ trang %v", date, sizes)
		}
		checkSortedUnique(t, date, all)

		var ao []reporting.StockRow
		p = stockAt(date, reporting.StockPage{Limit: 500, SKUPrefix: "  ao-  "})
		for range 5 {
			r := e.mustStock(t, u, p)
			ao = append(ao, r.Rows...)
			if r.Next == nil {
				break
			}
			p.Cursor = r.Next.String()
		}
		if len(ao) != 600 {
			t.Errorf("ngày %q: tiền tố AO- ra %d dòng, mong 600", date, len(ao))
		}
		for _, row := range ao {
			if !strings.HasPrefix(row.SKU, "AO-") {
				t.Errorf("ngày %q: tiền tố AO- ra %s", date, row.SKU)
			}
		}
		checkSortedUnique(t, date+" AO-", ao)
	}
}

// checkSortedUnique: các dòng tăng ngặt theo (SKU theo byte, biến thể, kho).
func checkSortedUnique(t *testing.T, name string, rows []reporting.StockRow) {
	t.Helper()
	key := func(r reporting.StockRow) string {
		return fmt.Sprintf("%s\x00%s\x00%s", r.SKU, r.VariantID, r.WarehouseID)
	}
	for i := 1; i < len(rows); i++ {
		if key(rows[i-1]) >= key(rows[i]) {
			t.Fatalf("%s: dòng %d (%s) không sau dòng %d (%s)", name, i, rows[i].SKU, i-1, rows[i-1].SKU)
		}
	}
}

// Người không có kho nào trong phạm vi (chỉ có quyền khác) không thấy dòng nào.
func TestStock_NoInventoryScope(t *testing.T) {
	t.Parallel()
	e := newStockEnv(t)
	e.seed.Ledger(e.variant, e.wh, "on_hand", 3, "import", vnAt(15, 10, 0, 0))
	u := identity.AuthUser{TenantID: e.tenant, UserID: uuid.New()}
	r := e.mustStock(t, u, reporting.StockParams{})
	if len(r.Rows) != 0 {
		t.Errorf("không phạm vi: %+v", r.Rows)
	}
}
