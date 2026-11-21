package order_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
)

// Phiếu partial_delivery cho phần chưa giao (kế hoạch M3, Task M3-26; TD09). Đơn D1: vA ×1 (on_hand 5), vB ×2
// (on_hand 10), mỗi cái 210.000; vận đơn S1 đã lấy hàng, khách nhận vA ×1, vB ×1.

func (e *orderEnv) createPartialReturn(o, s uuid.UUID, lines ...order.ReturnLineInput) (order.Return, error) {
	return e.returnStep(func(tx pgx.Tx, w order.Writer) (order.Return, error) {
		return w.CreatePartialReturn(e.t.Context(), tx, e.seller, o, s, lines, e.actor, e.now())
	})
}

// partialD1 là D1 đã giao một phần (vA 1, vB 1) và có phiếu partial_delivery 1 vB requested.
func (e *orderEnv) partialD1() (o order.Order, va, vb uuid.UUID, r order.Return) {
	e.t.Helper()
	o, va, vb = e.pickedUpD1()
	ia, ib := itemOf(e.t, o, va).ID, itemOf(e.t, o, vb).ID
	if applied, err := e.deliverPartial(o.ID, shipS1(), order.DeliveredLine{OrderItemID: ia, Qty: 1},
		order.DeliveredLine{OrderItemID: ib, Qty: 1}); err != nil || !applied {
		e.t.Fatalf("giao một phần: %v, %v", applied, err)
	}
	r, err := e.createPartialReturn(o.ID, shipS1(), rl(ib, 1))
	if err != nil {
		e.t.Fatalf("tạo phiếu partial_delivery: %v", err)
	}
	return e.get(o.ID), va, vb, r
}

// TD09 phía đơn: phiếu partial_delivery requested, vận đơn S1, tiền hoàn 0, mã TH…, một dòng vB ×1; đơn không đổi
// (delivered, cùng version); tồn không đổi (vB on_hand 9, in_transit 1).
func TestCreatePartialReturn(t *testing.T) {
	e := newM2Env(t)
	o, _, vb, r := e.partialD1()
	if r.Kind != order.ReturnPartialDelivery || r.Status != order.ReturnRequested || r.RefundAmount != 0 ||
		r.ShipmentID == nil || *r.ShipmentID != shipS1() || len(r.Code) < 9 || r.Code[:2] != "TH" {
		t.Fatalf("phiếu: %+v", r)
	}
	if len(r.Items) != 1 || r.Items[0].OrderItemID != itemOf(t, o, vb).ID || r.Items[0].Quantity != 1 ||
		r.Items[0].RefundAmount != 0 {
		t.Fatalf("dòng phiếu: %+v", r.Items)
	}
	if g := e.get(o.ID); g.Status != order.StatusDelivered || g.Version != o.Version {
		t.Errorf("đơn sau tạo phiếu: %s, %d (trước %d)", g.Status, g.Version, o.Version)
	}
	balanceIs(t, e.balance(vb), 9, 0, 1)
}

// Từ chối: phiếu partial_delivery thứ hai của cùng vận đơn, đơn chưa giao → ErrReturnNotAllowed; vận đơn không phải
// vận đơn hiện hành của đơn → ErrInvalidTransition; không dòng, số ≤ 0, dòng của đơn khác, dòng trùng →
// ErrInvalidInput; số vượt số mua − phiếu chưa hủy → ErrReturnQtyExceeds. Mọi lần từ chối không thêm phiếu.
func TestCreatePartialReturn_Rejects(t *testing.T) {
	e := newM2Env(t)
	o, va, _, _ := e.partialD1()
	ia := itemOf(t, o, va).ID
	o2, _, vb2 := e.deliveredD1Again()
	ib2 := itemOf(t, o2, vb2).ID
	confirmed, _, _ := e.d1()
	cases := []struct {
		name            string
		order, shipment uuid.UUID
		lines           []order.ReturnLineInput
		want            error
	}{
		{"phiếu thứ hai", o.ID, shipS1(), []order.ReturnLineInput{rl(ia, 1)}, order.ErrReturnNotAllowed},
		{
			"đơn chưa giao", confirmed.ID, shipS1(),
			[]order.ReturnLineInput{rl(confirmed.Items[0].ID, 1)},
			order.ErrReturnNotAllowed,
		},
		{"vận đơn không hiện hành", o2.ID, shipS1(), []order.ReturnLineInput{rl(ib2, 1)}, order.ErrInvalidTransition},
		{"không dòng", o2.ID, shipS2(), nil, order.ErrInvalidInput},
		{"số 0", o2.ID, shipS2(), []order.ReturnLineInput{rl(ib2, 0)}, order.ErrInvalidInput},
		{"dòng đơn khác", o2.ID, shipS2(), []order.ReturnLineInput{rl(ia, 1)}, order.ErrInvalidInput},
		{"dòng trùng", o2.ID, shipS2(), []order.ReturnLineInput{rl(ib2, 1), rl(ib2, 1)}, order.ErrInvalidInput},
		{"vượt số mua", o2.ID, shipS2(), []order.ReturnLineInput{rl(ib2, 3)}, order.ErrReturnQtyExceeds},
	}
	for _, c := range cases {
		if _, err := e.createPartialReturn(c.order, c.shipment, c.lines...); !errors.Is(err, c.want) {
			t.Errorf("%s: %v; muốn %v", c.name, err, c.want)
		}
	}
	if n := e.count(`SELECT count(*) FROM ord.returns WHERE order_id = ANY($1)`, []uuid.UUID{o.ID, o2.ID, confirmed.ID}); n != 1 {
		t.Fatalf("phiếu sau từ chối: %d, muốn 1", n)
	}
}

// deliveredD1Again tạo một D1 khác gắn S2, hãng đã lấy hàng rồi giao đủ (đơn delivered, vận đơn hiện hành S2).
func (e *orderEnv) deliveredD1Again() (order.Order, uuid.UUID, uuid.UUID) {
	e.t.Helper()
	o, va, vb := e.d1()
	if err := e.attachBy(o.ID, shipS2()); err != nil {
		e.t.Fatal(err)
	}
	e.mustOnEvent(o.ID, shipS2(), order.StepPickedUp, true)
	e.mustOnEvent(o.ID, shipS2(), order.StepDelivered, true)
	return e.get(o.ID), va, vb
}

// Kho nhận phiếu partial_delivery: vB 1 tốt → in_transit 0, on_hand 9 (có thể bán tăng 1); phiếu restocked; đơn vẫn
// delivered, version + 1; order.partially_returned (seq = version) mang kind partial_delivery, refundAmount 0,
// shipmentId S1, missingQty 0; không ghi thất lạc.
func TestReceivePartialReturn_Sellable(t *testing.T) {
	e := newM2Env(t)
	o, _, vb, r := e.partialD1()
	before := e.balance(vb)
	got := e.mustReceiveQty(r, qty(itemOf(t, o, vb).ID, 1, 0, 0))
	if got.Status != order.ReturnRestocked || got.RefundAmount != 0 {
		t.Fatalf("phiếu sau nhận: %s, %d", got.Status, got.RefundAmount)
	}
	balanceIs(t, e.balance(vb), 9, 0, 0)
	if avail := e.balance(vb).Available; avail != before.Available+1 {
		t.Errorf("có thể bán %d, muốn %d", avail, before.Available+1)
	}
	g := e.get(o.ID)
	if g.Status != order.StatusDelivered || g.Version != o.Version+1 {
		t.Fatalf("đơn sau nhận: %s, %d (trước %d)", g.Status, g.Version, o.Version)
	}
	if n := e.events(o.ID, order.TopicOrderPartiallyReturned, g.Version); n != 1 {
		t.Errorf("order.partially_returned seq %d: %d", g.Version, n)
	}
	p := e.lastPayload(o.ID, order.TopicOrderPartiallyReturned)
	raw := e.returnEvent(o.ID, order.TopicOrderPartiallyReturned)
	if p.Kind != order.ReturnPartialDelivery || p.ReturnID != r.ID || p.ShipmentID == nil || *p.ShipmentID != shipS1() ||
		p.MissingQty == nil || *p.MissingQty != 0 || raw["refundAmount"] != float64(0) || p.Status != order.StatusDelivered {
		t.Fatalf("payload: %+v %v", p, raw)
	}
	if n := e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE step = 'loss' AND doc_id = $1`,
		r.ID); n != 0 {
		t.Errorf("thất lạc: %d", n)
	}
	if n := e.events(o.ID, order.TopicOrderReturned, g.Version); n != 0 {
		t.Errorf("order.returned: %d (phiếu giao một phần không phát order.returned)", n)
	}
}

// Hỏng → ngăn hỏng; thiếu → thất lạc (on_hand −1), missingQty 1.
func TestReceivePartialReturn_DamagedMissing(t *testing.T) {
	for _, c := range []struct {
		name            string
		line            func(uuid.UUID) order.ReceiveQtyLine
		onHand, damaged int32
		missing         int32
	}{
		{"hỏng", func(i uuid.UUID) order.ReceiveQtyLine { return qty(i, 0, 1, 0) }, 9, 1, 0},
		{"thiếu", func(i uuid.UUID) order.ReceiveQtyLine { return qty(i, 0, 0, 1) }, 8, 0, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newM2Env(t)
			o, _, vb, r := e.partialD1()
			e.mustReceiveQty(r, c.line(itemOf(t, o, vb).ID))
			b := e.balance(vb)
			balanceIs(t, b, c.onHand, 0, 0)
			if b.Damaged != c.damaged {
				t.Errorf("damaged %d, muốn %d", b.Damaged, c.damaged)
			}
			if p := e.lastPayload(o.ID, order.TopicOrderPartiallyReturned); p.MissingQty == nil || *p.MissingQty != c.missing {
				t.Errorf("missingQty: %v, muốn %d", p.MissingQty, c.missing)
			}
			if n := e.count(`SELECT coalesce(sum(-delta), 0)::int FROM inventory.stock_ledger WHERE step = 'loss'
				AND doc_type = 'adjust' AND doc_id = $1`, r.ID); n != int(c.missing) {
				t.Errorf("thất lạc ghi sổ = %d, muốn %d", n, c.missing)
			}
		})
	}
}

// Phiếu sai số, sai version như mọi phiếu: lệch tổng → ErrQtyMismatch, version cũ → ErrVersionConflict; hủy phiếu giao
// một phần → ErrReturnNotAllowed (phiếu vẫn requested); không đổi gì.
func TestReceivePartialReturn_Guards(t *testing.T) {
	e := newM2Env(t)
	o, _, vb, r := e.partialD1()
	ib := itemOf(t, o, vb).ID
	if _, err := e.receiveQty(r, qty(ib, 2, 0, 0)); !errors.Is(err, order.ErrQtyMismatch) {
		t.Errorf("nhận 2 khi phiếu 1: %v", err)
	}
	stale := r
	stale.Version++
	if _, err := e.receiveQty(stale, qty(ib, 1, 0, 0)); !errors.Is(err, order.ErrVersionConflict) {
		t.Errorf("version khác: %v", err)
	}
	if _, err := e.cancelReturn(r); !errors.Is(err, order.ErrReturnNotAllowed) {
		t.Errorf("hủy phiếu giao một phần: %v; muốn ErrReturnNotAllowed", err)
	}
	if g := e.getReturn(r.ID); g.Status != order.ReturnRequested {
		t.Errorf("phiếu sau lần hủy bị chặn: %s", g.Status)
	}
	balanceIs(t, e.balance(vb), 9, 0, 1)
}

// Khách trả sau phần đã nhận (vB 1 đã giao, 1 vB đi phiếu partial_delivery): hoàn floor(420.000 × 1 / 2) = 210.000,
// không phải cả dòng 420.000; trả 2 vB → ErrReturnQtyExceeds (chỉ còn 1 cái khách giữ).
func TestCreateReturn_AfterPartialDeliveryRefund(t *testing.T) {
	e := newM2Env(t)
	o, _, vb, _ := e.partialD1()
	ib := itemOf(t, o, vb).ID
	if _, err := e.createReturn(o.ID, custReturn(rl(ib, 2))); !errors.Is(err, order.ErrReturnQtyExceeds) {
		t.Errorf("trả 2 vB: %v; muốn ErrReturnQtyExceeds", err)
	}
	r := e.mustReturn(o.ID, custReturn(rl(ib, 1)))
	if r.RefundAmount != 210000 {
		t.Fatalf("tiền hoàn %d, muốn 210000", r.RefundAmount)
	}
}
