package inventory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
)

func (e *invEnv) ledgerOf(doc uuid.UUID) int {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE doc_id = $1`, doc)
}

// ReleaseInTransitByItems: giữ hàng in_transit → released, in_transit −, on_hand không đổi (hàng hoàn về kho); gọi lần
// hai hay dòng còn held → ErrStateConflict, không đổi gì.
func TestReleaseInTransitByItems(t *testing.T) {
	e := newInvEnv(t)
	v := e.stock(5)
	l1, l2 := e.line(v, 2), e.line(v, 1)
	ids, err := e.reserve(sysActor(), l1, l2)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.startTransit(ids[:1], sysActor()); err != nil {
		t.Fatal(err)
	}
	release := func(items ...uuid.UUID) error {
		return e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
			return w.ReleaseInTransitByItems(ctx, tx, items, inventory.Actor{Type: inventory.ActorCarrier})
		})
	}
	if err := release(l1.OrderItemID); err != nil {
		t.Fatal(err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 5, Held: 1, InTransit: 0, Available: 4})
	if s := e.reservationState(ids[0]); s != inventory.StateReleased {
		t.Errorf("giữ hàng 1: %s", s)
	}
	if err := release(l1.OrderItemID); !errors.Is(err, inventory.ErrStateConflict) {
		t.Errorf("lần hai: %v", err)
	}
	if err := release(l2.OrderItemID); !errors.Is(err, inventory.ErrStateConflict) {
		t.Errorf("dòng held: %v", err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 5, Held: 1, InTransit: 0, Available: 4})
}

// ReceiveCustomerReturn: on_hand + q; Damaged thêm damaged + q (có thể bán không đổi); mỗi biến thể một stock.changed,
// sổ doc_type return, doc_id = mã phiếu; đầu vào sai → ErrInvalidInput, không ghi gì.
func TestReceiveCustomerReturn(t *testing.T) {
	e := newInvEnv(t)
	good, bad := e.stock(2), e.variants(1)[0] // bad: chưa có dòng số dư
	doc := uuid.New()
	err := e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		return w.ReceiveCustomerReturn(ctx, tx, doc, []inventory.ReturnLine{
			{VariantID: bad, WarehouseID: e.warehouse, Qty: 1, Damaged: true},
			{VariantID: good, WarehouseID: e.warehouse, Qty: 2},
		}, sysActor())
	})
	if err != nil {
		t.Fatal(err)
	}
	want(t, e.balance(good), inventory.Balance{OnHand: 4, Available: 4})
	want(t, e.balance(bad), inventory.Balance{OnHand: 1, Damaged: 1, Available: 0})
	if n := e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE doc_id = $1 AND doc_type = 'return'`, doc); n != 3 {
		t.Errorf("dòng sổ return = %d, mong 3", n)
	}
	// Mutation testing (Task 403): mỗi dòng số dư đổi (kể cả dòng có hàng hỏng) có một stock.changed.
	for _, v := range []uuid.UUID{good, bad} {
		if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'stock.changed' AND aggregate_id = $1`,
			v.String()+":"+e.warehouse.String()); n == 0 {
			t.Errorf("biến thể %s: không có stock.changed", v)
		}
	}
	for name, lines := range map[string][]inventory.ReturnLine{
		"rỗng":        nil,
		"số 0":        {{VariantID: good, WarehouseID: e.warehouse, Qty: 0}},
		"thiếu kho":   {{VariantID: good, Qty: 1}},
		"biến thể lạ": {{VariantID: uuid.New(), WarehouseID: e.warehouse, Qty: 1}},
	} {
		d := uuid.New()
		err := e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
			return w.ReceiveCustomerReturn(ctx, tx, d, lines, sysActor())
		})
		if !errors.Is(err, inventory.ErrInvalidInput) || e.ledgerOf(d) != 0 {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		return w.ReceiveCustomerReturn(ctx, tx, uuid.Nil, []inventory.ReturnLine{
			{VariantID: good, WarehouseID: e.warehouse, Qty: 1},
		}, sysActor())
	}); !errors.Is(err, inventory.ErrInvalidInput) {
		t.Errorf("thiếu mã phiếu: %v", err)
	}
}

// MoveToDamaged: damaged + q khi có thể bán ≥ q (sổ doc_type damage); không đủ → ErrInsufficient, không ghi gì.
func TestMoveToDamaged(t *testing.T) {
	e := newInvEnv(t)
	v := e.stock(3)
	if _, err := e.reserve(sysActor(), e.line(v, 2)); err != nil {
		t.Fatal(err)
	}
	move := func(doc uuid.UUID, q int32) error {
		return e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
			return w.MoveToDamaged(ctx, tx, doc, v, e.warehouse, q, sysActor())
		})
	}
	d1, d2 := uuid.New(), uuid.New()
	if err := move(d1, 1); err != nil {
		t.Fatal(err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 3, Held: 2, Damaged: 1, Available: 0})
	if n := e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE doc_id = $1 AND doc_type = 'damage'`, d1); n != 1 {
		t.Errorf("sổ damage = %d", n)
	}
	if err := move(d2, 1); !errors.Is(err, inventory.ErrInsufficient) || e.ledgerOf(d2) != 0 {
		t.Errorf("không đủ: %v", err)
	}
	if err := move(uuid.New(), 0); !errors.Is(err, inventory.ErrInvalidInput) {
		t.Errorf("số 0: %v", err)
	} // Mutation testing (Task 403): thiếu từng mã một → ErrInvalidInput.
	for name, ids := range map[string][3]uuid.UUID{
		"thiếu mã phiếu": {uuid.Nil, v, e.warehouse},
		"thiếu biến thể": {uuid.New(), uuid.Nil, e.warehouse},
		"thiếu kho":      {uuid.New(), v, uuid.Nil},
	} {
		if err := e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
			return w.MoveToDamaged(ctx, tx, ids[0], ids[1], ids[2], 1, sysActor())
		}); !errors.Is(err, inventory.ErrInvalidInput) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
