package reporting_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/reporting"
)

// movements chạy xuất nhập tồn kỳ from – to và trả kết quả hay lỗi; mọi dòng phải giữ đẳng thức cuối kỳ.
func (e stockEnv) movements(t *testing.T, u identity.AuthUser, p reporting.MovementsParams) (reporting.MovementsReport, error) {
	t.Helper()
	res, err := e.rep.Run(t.Context(), u, reporting.KeyStockMovements, &p)
	if err != nil {
		return reporting.MovementsReport{}, err //nolint:wrapcheck // test
	}
	r, ok := res.(reporting.MovementsReport)
	if !ok {
		t.Fatalf("kết quả kiểu %T, mong reporting.MovementsReport", res)
	}
	if r.Status != "ready" || r.Rows == nil || r.Period.FromDay != p.From || r.Period.ToDay != p.To {
		t.Errorf("status %q, kỳ %s – %s, rows nil = %v", r.Status, r.Period.FromDay, r.Period.ToDay, r.Rows == nil)
	}
	for _, row := range r.Rows {
		if row.Closing != row.Opening+row.Imported+row.ReturnedIn-row.Sold-row.ExportedOther+row.Adjusted {
			t.Errorf("dòng %s: cuối kỳ %d sai đẳng thức: %+v", row.SKU, row.Closing, row)
		}
	}
	return r, nil
}

func (e stockEnv) mustMovements(t *testing.T, u identity.AuthUser, p reporting.MovementsParams) reporting.MovementsReport {
	t.Helper()
	r, err := e.movements(t, u, p)
	if err != nil {
		t.Fatalf("xuất nhập tồn %+v: %v", p, err)
	}
	return r
}

func period(from, to string) reporting.MovementsParams {
	return reporting.MovementsParams{PeriodParams: reporting.PeriodParams{From: from, To: to}}
}

// movementsBySKU đổi các dòng sang map theo SKU (mỗi SKU một kho trong các test này).
func movementsBySKU(t *testing.T, r reporting.MovementsReport) map[string]reporting.MovementRow {
	t.Helper()
	m := map[string]reporting.MovementRow{}
	for _, row := range r.Rows {
		if _, dup := m[row.SKU]; dup {
			t.Fatalf("SKU %s có hai dòng", row.SKU)
		}
		m[row.SKU] = row
	}
	return m
}

// Kịch bản của kế hoạch (áo A-S, kho W): 15/11 nhập +20; 16/11 bán −2, nhập trả +1, kiểm kho −1 → kỳ 16/11: đầu 20,
// nhập 0, nhập trả 1, bán 2, điều chỉnh −1, cuối 18 = on_hand bản chụp 16/11; kỳ tới hôm nay: cuối = on_hand hiện tại.
// Áo B (đủ bảy doc_type): 15/11 nhập +10; 16/11 xuất −1, xuất hàng lỗi on_hand −1 / damaged +1, điều chỉnh +2 → xuất
// khác 2, điều chỉnh 2, lỗi cuối kỳ 1. Dòng ngăn held (kể cả flash_hold) không vào cột nào. Biến thể chỉ có sổ trước
// kỳ và cuối kỳ 0 bị ẩn.
func TestMovements_PlanScenario(t *testing.T) {
	t.Parallel()
	e := newStockEnv(t)
	a, b := e.variant, e.seed.Variant(e.seed.Own(), "Áo B", "AO-B-M")
	gone := e.seed.Variant(e.seed.Own(), "Áo C", "AO-C-L")
	e.seed.Ledger(a, e.wh, "on_hand", 20, "import", vnAt(15, 10, 0, 0))
	e.seed.Ledger(b, e.wh, "on_hand", 10, "import", vnAt(15, 10, 0, 0))
	e.seed.Ledger(gone, e.wh, "on_hand", 1, "import", vnAt(15, 10, 0, 0))
	e.seed.Ledger(gone, e.wh, "on_hand", -1, "export", vnAt(15, 11, 0, 0))
	e.seed.Ledger(a, e.wh, "held", 2, "order", vnAt(16, 8, 0, 0))
	e.seed.Ledger(a, e.wh, "held", -2, "order", vnAt(16, 9, 0, 0))
	e.seed.Ledger(a, e.wh, "on_hand", -2, "order", vnAt(16, 9, 0, 0))
	e.seed.Ledger(a, e.wh, "on_hand", 1, "return", vnAt(16, 10, 0, 0))
	e.seed.Ledger(a, e.wh, "on_hand", -1, "stocktake", vnAt(16, 23, 59, 59))
	e.seed.Ledger(b, e.wh, "on_hand", -1, "export", vnAt(16, 0, 0, 0))
	e.seed.Ledger(b, e.wh, "on_hand", -1, "damage", vnAt(16, 11, 0, 0))
	e.seed.Ledger(b, e.wh, "damaged", 1, "damage", vnAt(16, 11, 0, 0))
	e.seed.Ledger(b, e.wh, "on_hand", 2, "adjust", vnAt(16, 12, 0, 0))
	e.seed.Ledger(b, e.wh, "held", 1, "flash_hold", vnAt(16, 13, 0, 0))
	if got := e.run(t, snapHandler(vnAt(17, 8, 0, 0), nil)).Days; !slices.Equal(got, []string{"2026-11-15", "2026-11-16"}) {
		t.Fatalf("ngày chụp %v", got)
	}
	u := e.invUser()

	m := movementsBySKU(t, e.mustMovements(t, u, period("2026-11-16", "2026-11-16")))
	if len(m) != 2 {
		t.Fatalf("kỳ 16/11: %d dòng (biến thể cuối kỳ 0 phải ẩn): %+v", len(m), m)
	}
	ra, rb := m["AO-A-S"], m["AO-B-M"]
	want := reporting.MovementRow{
		VariantID: a, SKU: "AO-A-S", ProductName: "Áo A", Color: "Mặc định", Size: "AO-A-S",
		WarehouseID: e.wh, Opening: 20, ReturnedIn: 1, Sold: 2, Adjusted: -1, Closing: 18,
	}
	if ra != want {
		t.Errorf("áo A kỳ 16/11: %+v, mong %+v", ra, want)
	}
	if snap := e.snapshot(t, "2026-11-16")[snapKey{e.wh, a}]; int64(snap.onHand) != ra.Closing {
		t.Errorf("cuối kỳ %d ≠ on_hand bản chụp 16/11 %d", ra.Closing, snap.onHand)
	}
	if rb.Opening != 10 || rb.Imported != 0 || rb.ExportedOther != 2 || rb.Adjusted != 2 || rb.Closing != 10 || rb.DamagedClosing != 1 || rb.Sold != 0 {
		t.Errorf("áo B kỳ 16/11: %+v", rb)
	}

	m = movementsBySKU(t, e.mustMovements(t, u, period("2026-11-15", "2026-11-15")))
	if r := m["AO-A-S"]; r.Opening != 0 || r.Imported != 20 || r.Closing != 20 {
		t.Errorf("áo A kỳ 15/11: %+v", r)
	}
	if r := m["AO-C-L"]; r.Imported != 1 || r.ExportedOther != 1 || r.Closing != 0 {
		t.Errorf("áo C kỳ 15/11 (có phát sinh dù cuối kỳ 0): %+v", r)
	}

	today := reporting.Today(time.Now())
	m = movementsBySKU(t, e.mustMovements(t, u, period("2026-11-15", today)))
	if r := m["AO-A-S"]; r.Opening != 0 || r.Imported != 20 || r.Closing != 18 || r.Sold != 2 {
		t.Errorf("áo A kỳ tới hôm nay: %+v", r)
	}
	if r := m["AO-B-M"]; r.Closing != 10 || r.DamagedClosing != 1 || r.Imported != 10 {
		t.Errorf("áo B kỳ tới hôm nay: %+v", r)
	}
}

// Phạm vi kho, tham số: người chỉ có W1 không thấy W2, lọc W2 → ErrWarehouseForbidden; kỳ sai → ErrBadDate,
// ErrFromAfterTo; tiền tố SKU lọc đúng; doc_type lạ trong ngăn on_hand → ErrUnknownDocType, không lặng lẽ bỏ.
func TestMovements_ScopeAndErrors(t *testing.T) {
	t.Parallel()
	e := newStockEnv(t)
	q := e.seed.Variant(e.seed.Own(), "Quần Q", "QU-1")
	e.seed.Ledger(e.variant, e.wh, "on_hand", 3, "import", vnAt(15, 10, 0, 0))
	e.seed.Ledger(e.variant, e.wh2, "on_hand", 4, "import", vnAt(15, 10, 0, 0))
	e.seed.Ledger(q, e.wh, "on_hand", 5, "import", vnAt(15, 10, 0, 0))

	w1 := e.invUser(e.wh)
	r := e.mustMovements(t, w1, period("2026-11-15", "2026-11-15"))
	if len(r.Rows) != 2 || r.Next != nil {
		t.Fatalf("W1: %+v", r.Rows)
	}
	for _, row := range r.Rows {
		if row.WarehouseID != e.wh {
			t.Errorf("W1 thấy kho %s", row.WarehouseID)
		}
	}
	p := period("2026-11-15", "2026-11-15")
	p.WarehouseID = &e.wh2
	if _, err := e.movements(t, w1, p); !errors.Is(err, reporting.ErrWarehouseForbidden) {
		t.Errorf("W1 lọc W2: %v", err)
	}
	p.SKUPrefix = "qu"
	p.WarehouseID = nil
	r = e.mustMovements(t, e.invUser(), p)
	if len(r.Rows) != 1 || r.Rows[0].SKU != "QU-1" || r.Rows[0].Imported != 5 {
		t.Errorf("tiền tố QU: %+v", r.Rows)
	}
	p = period("2026-11-15", "2026-11-15")
	p.Limit = 1
	r = e.mustMovements(t, e.invUser(), p)
	if len(r.Rows) != 1 || r.Next == nil {
		t.Fatalf("trang 1 cỡ 1: %+v, next %v", r.Rows, r.Next)
	}
	p.Cursor = r.Next.String()
	r2 := e.mustMovements(t, e.invUser(), p)
	if len(r2.Rows) != 1 || r2.Rows[0] == r.Rows[0] {
		t.Errorf("trang 2: %+v", r2.Rows)
	}

	for _, c := range []struct {
		p    reporting.MovementsParams
		want error
	}{
		{period("2026-11-16", "2026-11-15"), reporting.ErrFromAfterTo},
		{period("2026-11-31", "2026-12-01"), reporting.ErrBadDate},
		{period("", ""), reporting.ErrBadDate},
		{reporting.MovementsParams{PeriodParams: reporting.PeriodParams{From: "2026-11-15", To: "2026-11-15"}, StockPage: reporting.StockPage{Limit: 501}}, reporting.ErrBadParams},
	} {
		if _, err := e.movements(t, e.invUser(), c.p); !errors.Is(err, c.want) {
			t.Errorf("%+v: %v, mong %v", c.p, err, c.want)
		}
	}

	e.ownerExec(t, `INSERT INTO inventory.stock_ledger (tenant_id, variant_id, warehouse_id, bucket, delta, doc_type,
		doc_id, step, actor_type, created_at) VALUES ($1, $2, $3, 'on_hand', 1, 'flash_hold', $4, 'x', 'system', $5)`,
		e.tenant, q, e.wh, uuid.New(), vnAt(15, 12, 0, 0))
	if _, err := e.movements(t, e.invUser(), period("2026-11-15", "2026-11-15")); !errors.Is(err, reporting.ErrUnknownDocType) {
		t.Errorf("doc_type lạ ở ngăn on_hand: %v", err)
	}
}
