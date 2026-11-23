package inventory_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/testkit"
)

// KH04: available = on_hand − damaged − held − in_transit, ở Availability và ở view.
func TestAvailability_Formula(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(10)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE inventory.stock_balances SET damaged = 1, held = 2, in_transit = 3
			WHERE variant_id = $1`, v)
		return err //nolint:wrapcheck // test
	})
	var got []inventory.Balance
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		got, err = inventory.Availability(t.Context(), tx, e.warehouse, []uuid.UUID{v})
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].VariantID != v || got[0].WarehouseID != e.warehouse {
		t.Fatalf("Availability trả sai: %+v", got)
	}
	want(t, got[0], inventory.Balance{OnHand: 10, Damaged: 1, Held: 2, InTransit: 3, Available: 4})
	want(t, e.balance(v), inventory.Balance{OnHand: 10, Damaged: 1, Held: 2, InTransit: 3, Available: 4})
}

// Biến thể chưa có dòng số dư: Availability trả số 0 (Q-T28-4), theo variant_id tăng dần, bỏ trùng.
func TestAvailability_MissingRowIsZero(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	vs := e.variants(2)
	e.seed(vs[0], 3)
	var got []inventory.Balance
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		got, err = inventory.Availability(t.Context(), tx, e.warehouse, []uuid.UUID{vs[1], vs[0], vs[1]})
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("mong 2 dòng, có %+v", got)
	}
	for _, b := range got {
		exp := int32(0)
		if b.VariantID == vs[0] {
			exp = 3
		}
		if b.OnHand != exp || b.Available != exp || b.WarehouseID != e.warehouse {
			t.Errorf("số dư sai: %+v", b)
		}
	}
	if !uuidLess(got[0].VariantID, got[1].VariantID) {
		t.Errorf("không theo variant_id tăng dần: %+v", got)
	}
}

func TestReserve_HoldsAndRecords(t *testing.T) {
	t.Parallel()
	eachSeller(t, testReserveHoldsAndRecords)
}

func testReserveHoldsAndRecords(t *testing.T, e *invEnv) {
	v := e.stock(5)
	staff := inventory.Actor{Type: "staff", ID: uuid.New()}
	ids, err := e.reserve(staff, e.line(v, 2))
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("mong 1 mã giữ hàng, có %v", ids)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 5, Held: 2, Available: 3})
	if s := e.reservationState(ids[0]); s != "held" {
		t.Errorf("trạng thái %q, mong held", s)
	}
	if n := e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE doc_type = 'order' AND doc_id = $1
		AND bucket = 'held' AND delta = 2 AND step = 'reserve' AND actor_type = 'staff' AND actor_id = $2`, ids[0], staff.ID); n != 1 {
		t.Errorf("mong 1 dòng sổ giữ hàng của nhân viên, có %d", n)
	}
	// Sự kiện stock.changed: aggregate balance:<variant>:<warehouse>, Seq = version (seed 1 lần + reserve 1 lần → 3).
	agg := v.String() + ":" + e.warehouse.String()
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'stock.changed' AND aggregate_type = 'balance' AND aggregate_id = $1
		AND seq = 3 AND payload = jsonb_build_object('variantIds', jsonb_build_array($2::text), 'warehouseId', $3::text,
		'sellerId', $4::text)`, agg, v.String(), e.warehouse.String(), e.seller.String()); n != 1 {
		t.Errorf("mong 1 sự kiện stock.changed seq 3 đúng payload, có %d", n)
	}
	sig := e.bus.Signals()
	if len(sig) != 1 || sig[0].Topic != "inventory.balance" || sig[0].Requires != "inventory.read" ||
		sig[0].Scope == nil || *sig[0].Scope != (identity.Scope{Type: "warehouse", ID: e.warehouse.String()}) ||
		sig[0].Entity != (realtime.Entity{Type: "balance", ID: v.String() + ":" + e.warehouse.String()}) ||
		sig[0].TenantID != e.tenant {
		t.Errorf("báo hiệu sai: %+v", sig)
	}
}

// raceReserve chạy n goroutine, mỗi cái một transaction giữ qty 1 của v; trả số lần được và các lỗi thiếu hàng.
// Lỗi khác làm test đỏ.
func raceReserve(t *testing.T, e *invEnv, v uuid.UUID, n int) (ok int, short []*inventory.InsufficientError) {
	t.Helper()
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() { _, errs[i] = e.reserve(sysActor(), e.line(v, 1)) })
	}
	wg.Wait()
	for _, err := range errs {
		var ie *inventory.InsufficientError
		switch {
		case err == nil:
			ok++
		case errors.As(err, &ie) && errors.Is(err, inventory.ErrInsufficient):
			short = append(short, ie)
		default:
			t.Errorf("lỗi lạ: %v", err)
		}
	}
	return ok, short
}

// Chiếc cuối: hai transaction song song giữ qty 1 trên available 1 → đúng một được.
func TestReserve_LastItem(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(1)
	ok, short := raceReserve(t, e, v, 2)
	if ok != 1 || len(short) != 1 {
		t.Fatalf("mong 1 được 1 thiếu, có %d được %d thiếu", ok, len(short))
	}
	if ie := short[0]; ie.Requested != 1 || ie.Available != 0 || ie.VariantID != v || ie.WarehouseID != e.warehouse {
		t.Errorf("InsufficientError sai: %+v", ie)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 1, Held: 1, Available: 0})
	if n := e.count("SELECT count(*) FROM inventory.stock_ledger WHERE variant_id = $1 AND bucket = 'held'", v); n != 1 {
		t.Errorf("mong 1 dòng sổ giữ hàng, có %d", n)
	}
}

// 50 goroutine qty 1 trên available 10 → đúng 10 được.
func TestReserve_FiftyOnTen(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(10)
	if ok, short := raceReserve(t, e, v, 50); ok != 10 || len(short) != 40 {
		t.Fatalf("mong 10 được 40 thiếu, có %d được %d thiếu", ok, len(short))
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 10, Held: 10, Available: 0})
	if n := e.count("SELECT count(*) FROM inventory.stock_reservations WHERE variant_id = $1 AND state = 'held'", v); n != 10 {
		t.Errorf("mong 10 dòng giữ hàng, có %d", n)
	}
}

// Reserve 2 dòng, dòng B thiếu → lỗi, transaction lùi toàn bộ.
func TestReserve_PartialShortageRollsBack(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	a, b := e.stock(5), e.stock(1)
	_, err := e.reserve(sysActor(), e.line(a, 2), e.line(b, 2))
	var ie *inventory.InsufficientError
	if !errors.As(err, &ie) || ie.VariantID != b || ie.Requested != 2 || ie.Available != 1 {
		t.Fatalf("mong InsufficientError của B (2/1), có %v", err)
	}
	want(t, e.balance(a), inventory.Balance{OnHand: 5, Available: 5})
	if n := e.count("SELECT count(*) FROM inventory.stock_reservations"); n != 0 {
		t.Errorf("mong 0 dòng giữ hàng, có %d", n)
	}
	if n := e.count("SELECT count(*) FROM inventory.stock_ledger WHERE bucket = 'held'"); n != 0 {
		t.Errorf("mong 0 dòng sổ giữ hàng, có %d", n)
	}
}

// Biến thể chưa có dòng số dư → thiếu, available 0; số lượng ≤ 0, actor lạ → ErrInvalidInput.
func TestReserve_NoRowAndInvalidInput(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.variants(1)[0]
	_, err := e.reserve(sysActor(), e.line(v, 1))
	var ie *inventory.InsufficientError
	if !errors.As(err, &ie) || ie.Available != 0 {
		t.Fatalf("mong InsufficientError available 0, có %v", err)
	}
	s := e.stock(3)
	for name, tc := range map[string]struct {
		a inventory.Actor
		l inventory.ReserveLine
	}{
		"qty 0":          {sysActor(), e.line(s, 0)},
		"qty âm":         {sysActor(), e.line(s, -1)},
		"actor lạ":       {inventory.Actor{Type: "robot"}, e.line(s, 1)},
		"thiếu dòng đơn": {sysActor(), inventory.ReserveLine{VariantID: s, WarehouseID: e.warehouse, Qty: 1}},
	} {
		if _, err := e.reserve(tc.a, tc.l); !errors.Is(err, inventory.ErrInvalidInput) {
			t.Errorf("%s: mong ErrInvalidInput, có %v", name, err)
		}
	}
	want(t, e.balance(s), inventory.Balance{OnHand: 3, Available: 3})
}

// Tenant B không thấy số dư của A; view ngoài ngữ cảnh tenant → 0 dòng.
func TestTenantIsolation(t *testing.T) {
	t.Parallel()
	staff := testkit.OpenStaff(t)
	a := newInvEnvOn(t, staff, "A")
	b := newInvEnvOn(t, staff, "B")
	v := a.stock(4)
	if _, err := a.reserve(sysActor(), a.line(v, 1)); err != nil {
		t.Fatal(err)
	}
	if n := b.count("SELECT count(*) FROM inventory.stock_available_v"); n != 0 {
		t.Errorf("tenant B thấy %d dòng số dư của A", n)
	}
	var got []inventory.Balance
	if err := b.app(func(tx pgx.Tx) error {
		var err error
		got, err = inventory.Availability(t.Context(), tx, a.warehouse, []uuid.UUID{v})
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].OnHand != 0 {
		t.Errorf("tenant B đọc được số dư của A: %+v", got)
	}
	// B không giữ được hàng của A (không thấy dòng số dư).
	if _, err := b.reserve(sysActor(), inventory.ReserveLine{OrderItemID: uuid.New(), VariantID: v, WarehouseID: a.warehouse, Qty: 1}); !errors.Is(err, inventory.ErrInsufficient) {
		t.Errorf("tenant B giữ hàng của A: %v", err)
	}
	for _, view := range []string{"inventory.stock_available_v", "inventory.warehouses_v", "inventory.stock_ledger_v"} {
		var n int
		if err := staff.App.Pool.QueryRow(context.Background(), "SELECT count(*) FROM "+view).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s ngoài ngữ cảnh tenant trả %d dòng", view, n)
		}
	}
}
