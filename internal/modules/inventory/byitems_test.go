package inventory_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/inventory"
)

// snapshot là số dư, dòng sổ (bỏ mã, doc, thời gian) và trạng thái giữ hàng của một biến thể, để so hai đường.
type snapshot struct {
	bal    inventory.Balance
	ledger []string
	states []string
}

func (e *invEnv) snapshot(v uuid.UUID) snapshot {
	e.t.Helper()
	s := snapshot{bal: e.balance(v)}
	s.bal.VariantID, s.bal.WarehouseID = uuid.Nil, uuid.Nil
	if err := e.app(func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), `SELECT bucket || ':' || delta || ':' || doc_type || ':' || step || ':' || actor_type
			FROM inventory.stock_ledger WHERE variant_id = $1 ORDER BY 1`, v)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		if s.ledger, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return err //nolint:wrapcheck // test
		}
		rows, err = tx.Query(e.t.Context(), "SELECT state FROM inventory.stock_reservations WHERE variant_id = $1 ORDER BY 1", v)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		s.states, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return s
}

func (s snapshot) equal(o snapshot) bool {
	if s.bal != o.bal || len(s.ledger) != len(o.ledger) || len(s.states) != len(o.states) {
		return false
	}
	for i := range s.ledger {
		if s.ledger[i] != o.ledger[i] {
			return false
		}
	}
	for i := range s.states {
		if s.states[i] != o.states[i] {
			return false
		}
	}
	return true
}

// TestByItems_SameAsCore: ba hàm theo order_item_id cho cùng số dư, sổ, trạng thái như bản theo mã giữ hàng; gọi
// lại lần hai → ErrStateConflict.
func TestByItems_SameAsCore(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	type path struct {
		release, transit, consume func(ctx context.Context, tx pgx.Tx, w inventory.Writer, items, ids []uuid.UUID) error
	}
	core := path{
		release: func(ctx context.Context, tx pgx.Tx, w inventory.Writer, _, ids []uuid.UUID) error {
			return w.Release(ctx, tx, ids, "order_cancelled", sysActor())
		},
		transit: func(ctx context.Context, tx pgx.Tx, w inventory.Writer, _, ids []uuid.UUID) error {
			return w.StartTransit(ctx, tx, ids, sysActor())
		},
		consume: func(ctx context.Context, tx pgx.Tx, w inventory.Writer, _, ids []uuid.UUID) error {
			return w.Consume(ctx, tx, ids, sysActor())
		},
	}
	byItems := path{
		release: func(ctx context.Context, tx pgx.Tx, w inventory.Writer, items, _ []uuid.UUID) error {
			return w.ReleaseHeldByItems(ctx, tx, items, "order_cancelled", sysActor())
		},
		transit: func(ctx context.Context, tx pgx.Tx, w inventory.Writer, items, _ []uuid.UUID) error {
			return w.StartTransitByItems(ctx, tx, items, sysActor())
		},
		consume: func(ctx context.Context, tx pgx.Tx, w inventory.Writer, items, _ []uuid.UUID) error {
			return w.ConsumeByItems(ctx, tx, items, sysActor())
		},
	}
	// run: biến thể mới on_hand 10; hai dòng đơn (2 và 3 cái); dòng 1 nhả; dòng 2 đi giao rồi giao xong.
	run := func(p path) snapshot {
		v := e.stock(10)
		l1, l2 := e.line(v, 2), e.line(v, 3)
		ids, err := e.reserve(sysActor(), l1, l2)
		if err != nil {
			t.Fatal(err)
		}
		step := func(name string, f func(ctx context.Context, tx pgx.Tx, w inventory.Writer, items, ids []uuid.UUID) error,
			items, rids []uuid.UUID,
		) {
			t.Helper()
			call := func() error {
				return e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error { return f(ctx, tx, w, items, rids) })
			}
			if err := call(); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if err := call(); !errors.Is(err, inventory.ErrStateConflict) {
				t.Fatalf("%s lần hai: mong ErrStateConflict, có %v", name, err)
			}
		}
		step("release", p.release, []uuid.UUID{l1.OrderItemID}, ids[:1])
		step("transit", p.transit, []uuid.UUID{l2.OrderItemID}, ids[1:])
		step("consume", p.consume, []uuid.UUID{l2.OrderItemID}, ids[1:])
		return e.snapshot(v)
	}
	a, b := run(core), run(byItems)
	if !a.equal(b) {
		t.Fatalf("hai đường khác nhau:\nlõi    %+v\ntheo dòng %+v", a, b)
	}
	if a.bal.OnHand != 7 || a.bal.Held != 0 || a.bal.InTransit != 0 || a.bal.Available != 7 {
		t.Errorf("số dư cuối sai: %+v", a.bal)
	}
	// Dòng đơn không có giữ hàng → ErrStateConflict.
	if err := e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		return w.ConsumeByItems(ctx, tx, []uuid.UUID{uuid.New()}, sysActor())
	}); !errors.Is(err, inventory.ErrStateConflict) {
		t.Errorf("dòng đơn lạ: mong ErrStateConflict, có %v", err)
	}
}

// TestReservationStatesByItems: đếm dòng giữ hàng của các dòng đơn theo trạng thái; dòng đơn lạ, rỗng → map rỗng.
func TestReservationStatesByItems(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(10)
	l1, l2, l3 := e.line(v, 1), e.line(v, 2), e.line(v, 3)
	ids, err := e.reserve(sysActor(), l1, l2, l3)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		return w.StartTransit(ctx, tx, ids[1:2], sysActor())
	}); err != nil {
		t.Fatal(err)
	}
	states := func(items ...uuid.UUID) map[string]int {
		t.Helper()
		var m map[string]int
		if err := e.app(func(tx pgx.Tx) error {
			var err error
			m, err = inventory.ReservationStatesByItems(t.Context(), tx, items)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
		return m
	}
	got := states(l1.OrderItemID, l2.OrderItemID, uuid.New())
	if len(got) != 2 || got[inventory.StateHeld] != 1 || got[inventory.StateInTransit] != 1 {
		t.Errorf("trạng thái của l1, l2: %v", got)
	}
	if got := states(l3.OrderItemID); len(got) != 1 || got[inventory.StateHeld] != 1 {
		t.Errorf("trạng thái của l3: %v", got)
	}
	if got := states(); len(got) != 0 {
		t.Errorf("không dòng đơn: %v", got)
	}
}

// resize gọi ResizeHeld cho dòng đơn item trong một transaction crm_app.
func (e *invEnv) resize(item uuid.UUID, q int32, step string) error {
	return e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		return w.ResizeHeld(ctx, tx, item, q, step, sysActor())
	})
}

func (e *invEnv) reservationQty(id uuid.UUID) int {
	e.t.Helper()
	return e.count(`SELECT quantity FROM inventory.stock_reservations WHERE id = $1`, id)
}

// TestResizeHeld (kế hoạch M2, Task M2-20; V1 tồn 5, dòng đơn giữ 2 → có thể bán 3): đổi thành 6 → *InsufficientError
// (thiếu 4, có thể bán 3), số dư, sổ, giữ hàng không đổi; thành 5 → held 5, có thể bán 0, một dòng sổ held +3 bước
// edit_v2, một stock.changed; thành 1 → held 1, có thể bán 4, sổ −4; cùng số → không ghi gì; bước sai mẫu hay trùng
// bước lõi, số ≤ 0, tác nhân lạ → ErrInvalidInput; dòng đơn không có giữ held (lạ, đã nhả) → ErrStateConflict.
func TestResizeHeld(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(5)
	l := e.line(v, 2)
	ids, err := e.reserve(sysActor(), l)
	if err != nil {
		t.Fatal(err)
	}
	before := e.snapshot(v)
	err = e.resize(l.OrderItemID, 6, "edit_v2")
	ie, ok := errors.AsType[*inventory.InsufficientError](err)
	if !ok || !errors.Is(err, inventory.ErrInsufficient) || ie.VariantID != v || ie.Requested != 4 || ie.Available != 3 {
		t.Fatalf("đổi thành 6: %v (%+v), mong InsufficientError thiếu 4 có 3", err, ie)
	}
	if after := e.snapshot(v); !after.equal(before) || e.reservationQty(ids[0]) != 2 {
		t.Fatalf("thiếu hàng mà vẫn đổi: %+v → %+v", before, after)
	}
	events := func() int {
		return e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'stock.changed' AND aggregate_id = $1`,
			v.String()+":"+e.warehouse.String())
	}
	ev := events()
	if err := e.resize(l.OrderItemID, 5, "edit_v2"); err != nil {
		t.Fatalf("đổi thành 5: %v", err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 5, Held: 5, Available: 0})
	if e.reservationQty(ids[0]) != 5 || e.reservationState(ids[0]) != inventory.StateHeld {
		t.Errorf("giữ hàng sau đổi thành 5: %d %s", e.reservationQty(ids[0]), e.reservationState(ids[0]))
	}
	if n := e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE doc_type = 'order' AND doc_id = $1
		AND bucket = 'held' AND delta = 3 AND step = 'edit_v2'`, ids[0]); n != 1 {
		t.Errorf("dòng sổ held +3 edit_v2: %d", n)
	}
	if got := events(); got != ev+1 {
		t.Errorf("stock.changed: %d → %d, mong +1", ev, got)
	}
	if err := e.resize(l.OrderItemID, 1, "edit_v3"); err != nil {
		t.Fatalf("đổi thành 1: %v", err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 5, Held: 1, Available: 4})
	if n := e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE doc_id = $1 AND delta = -4 AND step = 'edit_v3'`,
		ids[0]); n != 1 {
		t.Errorf("dòng sổ held −4 edit_v3: %d", n)
	}
	same := e.snapshot(v)
	if err := e.resize(l.OrderItemID, 1, "edit_v4"); err != nil {
		t.Fatalf("cùng số: %v", err)
	}
	if after := e.snapshot(v); !after.equal(same) {
		t.Errorf("cùng số mà vẫn ghi: %+v → %+v", same, after)
	}
	for name, call := range map[string]func() error{
		"số 0":          func() error { return e.resize(l.OrderItemID, 0, "edit_v5") },
		"bước có dấu :": func() error { return e.resize(l.OrderItemID, 2, "edit:v5") },
		"bước lõi":      func() error { return e.resize(l.OrderItemID, 2, "reserve") },
		"tác nhân lạ": func() error {
			return e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
				return w.ResizeHeld(ctx, tx, l.OrderItemID, 2, "edit_v5", inventory.Actor{Type: "robot"})
			})
		},
	} {
		if err := call(); !errors.Is(err, inventory.ErrInvalidInput) {
			t.Errorf("%s: %v, mong ErrInvalidInput", name, err)
		}
	}
	if err := e.resize(uuid.New(), 2, "edit_v5"); !errors.Is(err, inventory.ErrStateConflict) {
		t.Errorf("dòng đơn lạ: %v, mong ErrStateConflict", err)
	}
	if err := e.release(ids, "order_cancelled"); err != nil {
		t.Fatal(err)
	}
	if err := e.resize(l.OrderItemID, 2, "edit_v5"); !errors.Is(err, inventory.ErrStateConflict) {
		t.Errorf("đã nhả: %v, mong ErrStateConflict", err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 5, Held: 0, Available: 5})
}

// TestResizeHeldInvariants (rapid, bộ bất biến của M1 Task 28 với ResizeHeld): 1–3 biến thể, 1–4 goroutine chạy xen
// kẽ giữ, đổi số lượng giữ, nhả, đi giao, nhập kho → mọi cột ≥ 0; sổ = số dư từng ngăn; held = tổng giữ held;
// available ≥ 0 (checkInvariants của property_test.go).
func TestResizeHeldInvariants(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	var stepSeq atomic.Int64
	rapid.Check(t, func(rt *rapid.T) {
		vs := e.variants(rapid.IntRange(1, 3).Draw(rt, "variants"))
		for _, v := range vs {
			e.seed(v, rapid.Int32Range(1, 6).Draw(rt, "stock"))
		}
		e.checkConcurrentOps(rt, vs, 5, func(ops []propOp) error { return e.runResizeOps(vs, ops, &stepSeq) })
	})
}

// runResizeOps: 0 giữ, 1 đổi số lượng giữ (bước edit_v<số tăng dần>), 2 nhả, 3 đi giao, 4 nhập kho; thiếu hàng và sai
// trạng thái là kết quả hợp lệ.
func (e *invEnv) runResizeOps(vs []uuid.UUID, ops []propOp, stepSeq *atomic.Int64) error {
	var items, rids []uuid.UUID
	for _, op := range ops {
		v := vs[op.variant]
		var err error
		switch {
		case op.kind == 0:
			l := e.line(v, op.qty)
			var ids []uuid.UUID
			if ids, err = e.reserve(sysActor(), l); err == nil {
				items, rids = append(items, l.OrderItemID), append(rids, ids...)
			}
		case op.kind == 4:
			err = e.app(func(tx pgx.Tx) error { return inventory.ImportForTest(e.t.Context(), tx, v, e.warehouse, op.qty) })
		case len(items) == 0:
			continue
		case op.kind == 1:
			err = e.resize(items[op.target%len(items)], op.qty, fmt.Sprintf("edit_v%d", stepSeq.Add(1)))
		case op.kind == 2:
			err = e.release([]uuid.UUID{rids[op.target%len(rids)]}, "prop")
		default:
			err = e.startTransit([]uuid.UUID{rids[op.target%len(rids)]}, sysActor())
		}
		if err != nil && !errors.Is(err, inventory.ErrInsufficient) && !errors.Is(err, inventory.ErrStateConflict) {
			return err
		}
	}
	return nil
}
