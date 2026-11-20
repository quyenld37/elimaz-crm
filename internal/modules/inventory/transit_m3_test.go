package inventory_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
)

// Ba hàm tồn của vận chuyển (kế hoạch M3, Task M3-05): ConsumePartial (TD09), ReverseConsumption (TD10), RecordLoss
// (TD04). Dữ liệu chung: vA on_hand 5, vB on_hand 10; đơn D1 có dòng vA ×1, vB ×2, đã giữ rồi đi giao
// (StartTransitByItems của M1).

type d1 struct {
	vA, vB uuid.UUID
	lA, lB inventory.ReserveLine
	ids    []uuid.UUID // mã giữ hàng của lA, lB
}

func (e *invEnv) d1InTransit() d1 {
	e.t.Helper()
	d := d1{vA: e.stock(5), vB: e.stock(10)}
	d.lA, d.lB = e.line(d.vA, 1), e.line(d.vB, 2)
	ids, err := e.reserve(sysActor(), d.lA, d.lB)
	if err != nil {
		e.t.Fatal(err)
	}
	d.ids = ids
	if err := e.byItems(func(ctx context.Context, tx pgx.Tx, w inventory.Writer, items []uuid.UUID) error {
		return w.StartTransitByItems(ctx, tx, items, sysActor())
	}, d.lA.OrderItemID, d.lB.OrderItemID); err != nil {
		e.t.Fatal(err)
	}
	return d
}

func (e *invEnv) byItems(fn func(ctx context.Context, tx pgx.Tx, w inventory.Writer, items []uuid.UUID) error,
	items ...uuid.UUID,
) error {
	return e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error { return fn(ctx, tx, w, items) })
}

func (e *invEnv) consumeByItems(items ...uuid.UUID) error {
	return e.byItems(func(ctx context.Context, tx pgx.Tx, w inventory.Writer, items []uuid.UUID) error {
		return w.ConsumeByItems(ctx, tx, items, sysActor())
	}, items...)
}

func (e *invEnv) consumePartial(lines ...inventory.PartialLine) error {
	return e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		return w.ConsumePartial(ctx, tx, lines, sysActor())
	})
}

func (e *invEnv) reverse(ref uuid.UUID, reason string, items ...uuid.UUID) error {
	return e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		return w.ReverseConsumption(ctx, tx, items, ref, reason, inventory.Actor{Type: inventory.ActorCarrier})
	})
}

func (e *invEnv) recordLoss(ref uuid.UUID, reason string, lines ...inventory.LossLine) error {
	return e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		return w.RecordLoss(ctx, tx, lines, ref, reason, staffActor())
	})
}

// partialThenReverse: dòng đơn item (đang đi giao 2) giao một phần 1, giao nốt (ConsumeByItems), rồi đảo với ref mới.
func (e *invEnv) partialThenReverse(item uuid.UUID) {
	e.t.Helper()
	if err := e.consumePartial(inventory.PartialLine{OrderItemID: item, Qty: 1}); err != nil {
		e.t.Fatal(err)
	}
	if err := e.consumeByItems(item); err != nil {
		e.t.Fatal(err)
	}
	if err := e.reverse(uuid.New(), "carrier_corrected", item); err != nil {
		e.t.Fatal(err)
	}
}

func (e *invEnv) ledgerRows(docType string, doc uuid.UUID, step string) int {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE doc_type = $1 AND doc_id = $2 AND step = $3`,
		docType, doc, step)
}

// TD09: giao một phần vB ×1 khi đang đi giao 2 → vB on_hand 9, in_transit 1, giữ hàng còn 1 in_transit, sổ bước
// consume_partial (một dòng mỗi ngăn); ConsumeByItems của M1 cho phần còn lại → on_hand 8, in_transit 0, consumed.
// Qty bằng số đang giữ → như ConsumeByItems (bước consume).
func TestConsumePartial(t *testing.T) {
	e := newInvEnv(t)
	d := e.d1InTransit()
	if err := e.consumePartial(inventory.PartialLine{OrderItemID: d.lB.OrderItemID, Qty: 1}); err != nil {
		t.Fatal(err)
	}
	want(t, e.balance(d.vB), inventory.Balance{OnHand: 9, InTransit: 1, Available: 8})
	if s, q := e.reservationState(d.ids[1]), e.reservationQty(d.ids[1]); s != inventory.StateInTransit || q != 1 {
		t.Errorf("giữ hàng vB: %s ×%d, mong in_transit ×1", s, q)
	}
	for _, b := range []string{"in_transit", "on_hand"} {
		if n := e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE doc_type = 'order' AND doc_id = $1
			AND step = 'consume_partial' AND bucket = $2 AND delta = -1`, d.ids[1], b); n != 1 {
			t.Errorf("sổ consume_partial ngăn %s: %d dòng, mong 1", b, n)
		}
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'stock.changed' AND aggregate_id = $1`,
		d.vB.String()+":"+e.warehouse.String()); n < 3 {
		t.Errorf("stock.changed của vB: %d, mong ≥ 3 (giữ, đi giao, giao một phần)", n)
	}
	want(t, e.balance(d.vA), inventory.Balance{OnHand: 5, InTransit: 1, Available: 4}) // dòng khác không đổi
	if err := e.consumeByItems(d.lB.OrderItemID); err != nil {
		t.Fatal(err)
	}
	want(t, e.balance(d.vB), inventory.Balance{OnHand: 8, Available: 8})
	if s := e.reservationState(d.ids[1]); s != inventory.StateConsumed {
		t.Errorf("giữ hàng vB: %s", s)
	}
	// Qty = quantity: như ConsumeByItems.
	if err := e.consumePartial(inventory.PartialLine{OrderItemID: d.lA.OrderItemID, Qty: 1}); err != nil {
		t.Fatal(err)
	}
	want(t, e.balance(d.vA), inventory.Balance{OnHand: 4, Available: 4})
	if s := e.reservationState(d.ids[0]); s != inventory.StateConsumed {
		t.Errorf("giữ hàng vA: %s", s)
	}
	if n := e.ledgerRows("order", d.ids[0], "consume"); n != 2 {
		t.Errorf("sổ consume vA: %d dòng, mong 2", n)
	}
	if n := e.ledgerRows("order", d.ids[0], "consume_partial"); n != 0 {
		t.Errorf("sổ consume_partial vA: %d dòng, mong 0", n)
	}
}

// Một lệnh nhiều dòng: dòng giao đủ và dòng giao một phần cùng lúc, truyền theo thứ tự ngược với thứ tự khóa.
func TestConsumePartial_Mixed(t *testing.T) {
	e := newInvEnv(t)
	d := e.d1InTransit()
	if err := e.consumePartial(
		inventory.PartialLine{OrderItemID: d.lB.OrderItemID, Qty: 1},
		inventory.PartialLine{OrderItemID: d.lA.OrderItemID, Qty: 1},
	); err != nil {
		t.Fatal(err)
	}
	want(t, e.balance(d.vA), inventory.Balance{OnHand: 4, Available: 4})
	want(t, e.balance(d.vB), inventory.Balance{OnHand: 9, InTransit: 1, Available: 8})
}

// Không đủ, gọi lặp, sai trạng thái → ErrStateConflict; đầu vào sai → ErrInvalidInput; cả lệnh không đổi gì.
func TestConsumePartial_Rejects(t *testing.T) {
	e := newInvEnv(t)
	d := e.d1InTransit()
	held := e.line(d.vA, 1) // dòng đơn chỉ held
	if _, err := e.reserve(sysActor(), held); err != nil {
		t.Fatal(err)
	}
	beforeA, beforeB := e.snapshot(d.vA), e.snapshot(d.vB)
	pl := func(item uuid.UUID, q int32) inventory.PartialLine {
		return inventory.PartialLine{OrderItemID: item, Qty: q}
	}
	for name, c := range map[string]struct {
		lines []inventory.PartialLine
		want  error
	}{
		"nhiều hơn số giữ":      {[]inventory.PartialLine{pl(d.lB.OrderItemID, 3)}, inventory.ErrStateConflict},
		"một dòng hỏng cả lệnh": {[]inventory.PartialLine{pl(d.lA.OrderItemID, 1), pl(d.lB.OrderItemID, 3)}, inventory.ErrStateConflict},
		"dòng đơn còn held":     {[]inventory.PartialLine{pl(held.OrderItemID, 1)}, inventory.ErrStateConflict},
		"dòng đơn không có":     {[]inventory.PartialLine{pl(uuid.New(), 1)}, inventory.ErrStateConflict},
		"số lượng 0":            {[]inventory.PartialLine{pl(d.lB.OrderItemID, 0)}, inventory.ErrInvalidInput},
		"số lượng âm":           {[]inventory.PartialLine{pl(d.lB.OrderItemID, -1)}, inventory.ErrInvalidInput},
		"thiếu mã dòng đơn":     {[]inventory.PartialLine{pl(uuid.Nil, 1)}, inventory.ErrInvalidInput},
		"trùng dòng đơn":        {[]inventory.PartialLine{pl(d.lB.OrderItemID, 1), pl(d.lB.OrderItemID, 1)}, inventory.ErrInvalidInput},
		"không dòng":            {nil, inventory.ErrInvalidInput},
	} {
		if err := e.consumePartial(c.lines...); !errors.Is(err, c.want) {
			t.Errorf("%s: mong %v, có %v", name, c.want, err)
		}
	}
	if err := e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		return w.ConsumePartial(ctx, tx, []inventory.PartialLine{pl(d.lB.OrderItemID, 1)}, inventory.Actor{Type: "robot"})
	}); !errors.Is(err, inventory.ErrInvalidInput) {
		t.Errorf("tác nhân lạ: %v", err)
	}
	if !e.snapshot(d.vA).equal(beforeA) || !e.snapshot(d.vB).equal(beforeB) {
		t.Fatal("lệnh bị từ chối đã đổi số dư, sổ hay giữ hàng")
	}
	// Giao một phần, giao nốt, đảo, rồi giao đủ lần hai → ErrStateConflict (không để lỗi trùng sổ lọt ra), không đổi gì.
	e.partialThenReverse(d.lB.OrderItemID)
	before := e.snapshot(d.vB)
	if err := e.consumePartial(pl(d.lB.OrderItemID, 1)); !errors.Is(err, inventory.ErrStateConflict) {
		t.Errorf("giao đủ lần hai sau đảo: mong ErrStateConflict, có %v", err)
	}
	if !e.snapshot(d.vB).equal(before) {
		t.Error("lệnh bị từ chối đã đổi số dư")
	}
}

// Giao một phần lần hai trên cùng dòng giữ hàng (còn đủ số) → ErrStateConflict, không đổi gì.
func TestConsumePartial_SecondPartialRejected(t *testing.T) {
	e := newInvEnv(t)
	v := e.stock(10)
	l := e.line(v, 3)
	ids, err := e.reserve(sysActor(), l)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.startTransit(ids, sysActor()); err != nil {
		t.Fatal(err)
	}
	if err := e.consumePartial(inventory.PartialLine{OrderItemID: l.OrderItemID, Qty: 1}); err != nil {
		t.Fatal(err)
	}
	before := e.snapshot(v)
	if err := e.consumePartial(inventory.PartialLine{OrderItemID: l.OrderItemID, Qty: 1}); !errors.Is(err,
		inventory.ErrStateConflict) {
		t.Fatalf("lần hai: mong ErrStateConflict, có %v", err)
	}
	if !e.snapshot(v).equal(before) {
		t.Fatal("lần hai đã đổi số dư")
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 9, InTransit: 2, Available: 7})
}

// TD10: hãng sửa "đã giao" → đảo: consumed → in_transit, on_hand +q, in_transit +q, sổ adjust/ref bước
// reverse_consume; cùng ref lần hai → không đổi gì; thiếu lý do → ErrReasonRequired.
func TestReverseConsumption(t *testing.T) {
	e := newInvEnv(t)
	d := e.d1InTransit()
	if err := e.consumeByItems(d.lA.OrderItemID, d.lB.OrderItemID); err != nil {
		t.Fatal(err)
	}
	want(t, e.balance(d.vA), inventory.Balance{OnHand: 4, Available: 4})
	r1 := uuid.New()
	if err := e.reverse(r1, "", d.lA.OrderItemID, d.lB.OrderItemID); !errors.Is(err, inventory.ErrReasonRequired) {
		t.Errorf("thiếu lý do: mong ErrReasonRequired, có %v", err)
	}
	if err := e.reverse(r1, "Hãng sửa", d.lA.OrderItemID); !errors.Is(err, inventory.ErrInvalidInput) {
		t.Errorf("lý do sai mẫu: mong ErrInvalidInput, có %v", err)
	}
	if err := e.reverse(uuid.Nil, "carrier_corrected", d.lA.OrderItemID); !errors.Is(err, inventory.ErrInvalidInput) {
		t.Errorf("thiếu ref: mong ErrInvalidInput, có %v", err)
	}
	if err := e.reverse(r1, "carrier_corrected"); !errors.Is(err, inventory.ErrInvalidInput) {
		t.Errorf("không dòng đơn: mong ErrInvalidInput, có %v", err)
	}
	want(t, e.balance(d.vA), inventory.Balance{OnHand: 4, Available: 4})
	if err := e.reverse(r1, "carrier_corrected", d.lA.OrderItemID, d.lB.OrderItemID); err != nil {
		t.Fatal(err)
	}
	want(t, e.balance(d.vA), inventory.Balance{OnHand: 5, InTransit: 1, Available: 4})
	want(t, e.balance(d.vB), inventory.Balance{OnHand: 10, InTransit: 2, Available: 8})
	for _, id := range d.ids {
		if s := e.reservationState(id); s != inventory.StateInTransit {
			t.Errorf("giữ hàng %s: %s, mong in_transit", id, s)
		}
	}
	if n := e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE doc_type = 'adjust' AND doc_id = $1
		AND step = 'reverse_consume' AND actor_type = 'carrier' AND delta > 0`, r1); n != 4 {
		t.Errorf("sổ reverse_consume: %d dòng, mong 4 (2 biến thể × 2 ngăn)", n)
	}
	beforeA, beforeB := e.snapshot(d.vA), e.snapshot(d.vB)
	if err := e.reverse(r1, "carrier_corrected", d.lA.OrderItemID, d.lB.OrderItemID); err != nil {
		t.Fatalf("cùng ref lần hai: %v", err)
	}
	if !e.snapshot(d.vA).equal(beforeA) || !e.snapshot(d.vB).equal(beforeB) {
		t.Fatal("cùng ref lần hai đã đổi số dư hay sổ")
	}
	// Ref mới trên dòng không còn consumed → ErrStateConflict.
	if err := e.reverse(uuid.New(), "carrier_corrected", d.lA.OrderItemID); !errors.Is(err, inventory.ErrStateConflict) {
		t.Errorf("dòng in_transit: mong ErrStateConflict, có %v", err)
	}
	// Sau đảo đi đường hoàn (M2): ReleaseInTransitByItems.
	if err := e.byItems(func(ctx context.Context, tx pgx.Tx, w inventory.Writer, items []uuid.UUID) error {
		return w.ReleaseInTransitByItems(ctx, tx, items, sysActor())
	}, d.lA.OrderItemID); err != nil {
		t.Fatal(err)
	}
	want(t, e.balance(d.vA), inventory.Balance{OnHand: 5, Available: 5})
}

// Hai dòng đơn cùng biến thể trong một lệnh đảo: sổ gộp theo (biến thể, kho), không trùng khóa unique.
func TestReverseConsumption_SameVariantTwoItems(t *testing.T) {
	e := newInvEnv(t)
	v := e.stock(6)
	l1, l2 := e.line(v, 1), e.line(v, 2)
	ids, err := e.reserve(sysActor(), l1, l2)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.startTransit(ids, sysActor()); err != nil {
		t.Fatal(err)
	}
	if err := e.consume(ids, sysActor()); err != nil {
		t.Fatal(err)
	}
	ref := uuid.New()
	if err := e.reverse(ref, "carrier_corrected", l1.OrderItemID, l2.OrderItemID); err != nil {
		t.Fatal(err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 6, InTransit: 3, Available: 3})
	if n := e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE doc_id = $1 AND delta = 3`, ref); n != 2 {
		t.Errorf("sổ gộp: %d dòng delta 3, mong 2", n)
	}
	if msg := e.checkInvariants(v); msg != "" {
		t.Error(msg)
	}
}

// Đảo sau khi đã giao một phần: chỉ trả lại phần giao nốt (quantity hiện tại của giữ hàng, Q-T96-6).
func TestReverseConsumption_AfterPartial(t *testing.T) {
	e := newInvEnv(t)
	d := e.d1InTransit()
	e.partialThenReverse(d.lB.OrderItemID)
	want(t, e.balance(d.vB), inventory.Balance{OnHand: 9, InTransit: 1, Available: 8})
	if msg := e.checkInvariants(d.vB); msg != "" {
		t.Error(msg)
	}
}

// TD04: hàng thất lạc sau khi hãng hoàn (ReleaseInTransitByItems của M2) → on_hand −1, có thể bán −1, sổ adjust/ref
// bước loss; cùng ref lần hai → không đổi gì; thiếu hàng có thể bán, chưa có số dư → ErrStateConflict.
func TestRecordLoss(t *testing.T) {
	e := newInvEnv(t)
	d := e.d1InTransit()
	if err := e.byItems(func(ctx context.Context, tx pgx.Tx, w inventory.Writer, items []uuid.UUID) error {
		return w.ReleaseInTransitByItems(ctx, tx, items, sysActor())
	}, d.lB.OrderItemID); err != nil {
		t.Fatal(err)
	}
	want(t, e.balance(d.vB), inventory.Balance{OnHand: 10, Available: 10})
	p1 := uuid.New()
	loss := func(v uuid.UUID, q int32) inventory.LossLine {
		return inventory.LossLine{VariantID: v, WarehouseID: e.warehouse, Qty: q}
	}
	if err := e.recordLoss(p1, "", loss(d.vB, 1)); !errors.Is(err, inventory.ErrReasonRequired) {
		t.Errorf("thiếu lý do: mong ErrReasonRequired, có %v", err)
	}
	if err := e.recordLoss(p1, "missing_items", loss(d.vB, 1)); err != nil {
		t.Fatal(err)
	}
	want(t, e.balance(d.vB), inventory.Balance{OnHand: 9, Available: 9})
	if n := e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE doc_type = 'adjust' AND doc_id = $1 AND step = 'loss'
		AND bucket = 'on_hand' AND delta = -1 AND actor_type = 'staff'`, p1); n != 1 {
		t.Errorf("sổ loss: %d dòng, mong 1", n)
	}
	before := e.snapshot(d.vB)
	if err := e.recordLoss(p1, "missing_items", loss(d.vB, 1)); err != nil {
		t.Fatalf("cùng ref lần hai: %v", err)
	}
	if !e.snapshot(d.vB).equal(before) {
		t.Fatal("cùng ref lần hai đã đổi số dư hay sổ")
	}
	// Có thể bán vA = 4 (1 đang đi giao): mất 5 → thiếu; một dòng thiếu thì cả lệnh không đổi gì.
	beforeA := e.snapshot(d.vA)
	noBalance := e.variants(1)[0]
	for name, c := range map[string]struct {
		lines []inventory.LossLine
		want  error
	}{
		"thiếu hàng có thể bán": {[]inventory.LossLine{loss(d.vA, 5)}, inventory.ErrStateConflict},
		"dòng sau thiếu":        {[]inventory.LossLine{loss(d.vB, 1), loss(d.vA, 3), loss(d.vA, 2)}, inventory.ErrStateConflict},
		"chưa có số dư":         {[]inventory.LossLine{loss(noBalance, 1)}, inventory.ErrStateConflict},
		"số lượng 0":            {[]inventory.LossLine{loss(d.vB, 0)}, inventory.ErrInvalidInput},
		"thiếu biến thể":        {[]inventory.LossLine{loss(uuid.Nil, 1)}, inventory.ErrInvalidInput},
		"không dòng":            {nil, inventory.ErrInvalidInput},
	} {
		if err := e.recordLoss(uuid.New(), "missing_items", c.lines...); !errors.Is(err, c.want) {
			t.Errorf("%s: mong %v, có %v", name, c.want, err)
		}
	}
	if err := e.recordLoss(uuid.Nil, "missing_items", loss(d.vB, 1)); !errors.Is(err, inventory.ErrInvalidInput) {
		t.Errorf("thiếu ref: %v", err)
	}
	if !e.snapshot(d.vA).equal(beforeA) || !e.snapshot(d.vB).equal(before) {
		t.Fatal("lệnh bị từ chối đã đổi số dư")
	}
	// Dòng cùng biến thể được cộng gộp: một dòng sổ −2.
	p2 := uuid.New()
	if err := e.recordLoss(p2, "missing_items", loss(d.vB, 1), loss(d.vB, 1)); err != nil {
		t.Fatal(err)
	}
	want(t, e.balance(d.vB), inventory.Balance{OnHand: 7, Available: 7})
	if n := e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE doc_id = $1 AND delta = -2`, p2); n != 1 {
		t.Errorf("sổ gộp: %d", n)
	}
}

// Song song: 20 goroutine trên 10 đơn chung vA, vB (mỗi đơn vA ×1, vB ×2, đã held); với mỗi đơn, một goroutine
// StartTransitByItems (thứ tự B, A) rồi ConsumePartial vB ×1, một goroutine StartTransitByItems (thứ tự A, B) rồi
// ConsumeByItems — mở thẳng transaction (không RetryTx). Sai trạng thái do bên kia đi trước là kết quả hợp lệ; mọi
// lỗi khác (40P01) → hỏng. Sau đó: pg_stat_database.deadlocks không tăng, số dư khớp sổ và giữ hàng.
func TestTransitM3_NoDeadlock(t *testing.T) {
	e := newInvEnv(t)
	vA, vB := e.stock(100), e.stock(100)
	type order struct{ a, b inventory.ReserveLine }
	orders := make([]order, 10)
	for i := range orders {
		orders[i] = order{e.line(vA, 1), e.line(vB, 2)}
		if _, err := e.reserve(sysActor(), orders[i].a, orders[i].b); err != nil {
			t.Fatal(err)
		}
	}
	before := e.deadlocks()
	startThen := func(first, second uuid.UUID, then func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error) func() error {
		return func() error {
			return e.rawTx(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
				err := w.StartTransitByItems(ctx, tx, []uuid.UUID{first, second}, sysActor())
				if err != nil && !errors.Is(err, inventory.ErrStateConflict) {
					return err //nolint:wrapcheck // test
				}
				return then(ctx, tx, w)
			})
		}
	}
	errs := make([]error, 2*len(orders))
	var wg sync.WaitGroup
	for i, o := range orders {
		partial := startThen(o.b.OrderItemID, o.a.OrderItemID, func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
			return w.ConsumePartial(ctx, tx, []inventory.PartialLine{{OrderItemID: o.b.OrderItemID, Qty: 1}}, sysActor())
		})
		full := startThen(o.a.OrderItemID, o.b.OrderItemID, func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
			return w.ConsumeByItems(ctx, tx, []uuid.UUID{o.b.OrderItemID, o.a.OrderItemID}, sysActor())
		})
		wg.Go(func() { errs[2*i] = partial() })
		wg.Go(func() { errs[2*i+1] = full() })
	}
	wg.Wait()
	ok := 0
	for i, err := range errs {
		if err == nil {
			ok++
		} else if !errors.Is(err, inventory.ErrStateConflict) {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
	if ok == 0 {
		t.Error("không lệnh nào thành công")
	}
	for _, v := range []uuid.UUID{vA, vB} {
		if msg := e.checkInvariants(v) + e.checkOnceOut(v, nil); msg != "" {
			t.Errorf("biến thể %s: %s", v, msg)
		}
	}
	e.noNewDeadlocks(before)
}
