package order_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
)

// Nhận phiếu trả theo số lượng tốt, hỏng, thiếu (kế hoạch M3, Task M3-23). Đơn DH01: V1 ×2 (on_hand 5), V2 ×1
// (on_hand 1), vận đơn S1 đã lấy hàng (V1 in_transit 2, V2 in_transit 1).

func (e *orderEnv) receiveQty(r order.Return, lines ...order.ReceiveQtyLine) (order.Return, error) {
	return e.returnStep(func(tx pgx.Tx, w order.Writer) (order.Return, error) {
		return w.ReceiveReturnQty(e.t.Context(), tx, e.seller, r.ID, r.Version, lines, e.actor, e.now())
	})
}

func (e *orderEnv) mustReceiveQty(r order.Return, lines ...order.ReceiveQtyLine) order.Return {
	e.t.Helper()
	out, err := e.receiveQty(r, lines...)
	if err != nil {
		e.t.Fatalf("nhận phiếu %s theo số lượng: %v", r.Code, err)
	}
	return out
}

func qty(item uuid.UUID, s, d, m int32) order.ReceiveQtyLine {
	return order.ReceiveQtyLine{OrderItemID: item, Sellable: s, Damaged: d, Missing: m}
}

// lineQty đọc condition và ba số của dòng phiếu returnID ứng với dòng đơn item.
func (e *orderEnv) lineQty(returnID, item uuid.UUID) (cond string, s, d, m int) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT condition, sellable_qty, damaged_qty, missing_qty FROM ord.return_items
			WHERE return_id = $1 AND order_item_id = $2`, returnID, item).Scan(&cond, &s, &d, &m)
	})
	return cond, s, d, m
}

// returnedPayload là phần payload order.returned / order.partially_returned test đọc.
type returnedPayload struct {
	CustomerID uuid.UUID  `json:"customerId"`
	ReturnID   uuid.UUID  `json:"returnId"`
	Kind       string     `json:"kind"`
	ShipmentID *uuid.UUID `json:"shipmentId"`
	MissingQty *int32     `json:"missingQty"`
	Status     string     `json:"status"`
}

// lastPayload đọc payload của sự kiện topic mới nhất của đơn o.
func (e *orderEnv) lastPayload(o uuid.UUID, topic string) returnedPayload {
	e.t.Helper()
	var raw []byte
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT payload FROM platform.domain_events WHERE aggregate_type = 'order'
			AND aggregate_id = $1 AND topic = $2 ORDER BY seq DESC LIMIT 1`, o.String(), topic).Scan(&raw)
	})
	var p returnedPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		e.t.Fatal(err)
	}
	return p
}

// carrierCase là đơn DH01 có phiếu hoàn của hãng đang requested, kèm tồn trước khi nhận.
type carrierCase struct {
	o          order.Order
	v1, v2, s1 uuid.UUID
	i1, i2     uuid.UUID
	b1, b2     inventory.Balance
	r          order.Return
}

func (e *orderEnv) carrierCase(t *testing.T) carrierCase {
	t.Helper()
	var c carrierCase
	c.o, c.v1, c.v2, c.s1 = e.pickedUpDH01()
	c.b1, c.b2 = e.balance(c.v1), e.balance(c.v2)
	c.r = e.openCarrierReturn(c.o.ID, c.s1)
	c.i1, c.i2 = itemOf(t, c.o, c.v1).ID, itemOf(t, c.o, c.v2).ID
	return c
}

// TD04 (ca a, phía đơn): V1 S1 D1 M0, V2 S1 → in_transit về 0, V1 on_hand giữ, damaged 1; dòng V1 sellable_qty 1,
// damaged_qty 1, condition mixed; đơn returned; order.returned mang kind carrier_return, shipmentId S1, missingQty 0,
// returnId; không ghi thất lạc.
func TestReceiveReturnQty_CarrierSplit(t *testing.T) {
	e := newM2Env(t)
	c := e.carrierCase(t)
	o, v1, v2, s1, i1, i2, b1, b2, r := c.o, c.v1, c.v2, c.s1, c.i1, c.i2, c.b1, c.b2, c.r
	got := e.mustReceiveQty(r, qty(i1, 1, 1, 0), qty(i2, 1, 0, 0))
	if got.Status != order.ReturnRestocked {
		t.Fatalf("phiếu: %s", got.Status)
	}
	if n := e.balance(v1); n.InTransit != 0 || n.OnHand != b1.OnHand || n.Damaged != 1 || n.Available != b1.Available+1 {
		t.Errorf("V1: %+v (trước %+v)", n, b1)
	}
	if n := e.balance(v2); n.InTransit != 0 || n.OnHand != b2.OnHand || n.Available != b2.Available+1 {
		t.Errorf("V2: %+v (trước %+v)", n, b2)
	}
	if c, s, d, m := e.lineQty(r.ID, i1); c != order.ConditionMixed || s != 1 || d != 1 || m != 0 {
		t.Errorf("dòng V1: %s %d/%d/%d", c, s, d, m)
	}
	if c, s, d, m := e.lineQty(r.ID, i2); c != order.ConditionSellable || s != 1 || d != 0 || m != 0 {
		t.Errorf("dòng V2: %s %d/%d/%d", c, s, d, m)
	}
	for _, it := range got.Items {
		if it.OrderItemID == i1 && it.Condition != order.ConditionMixed {
			t.Errorf("Return.Items V1 condition = %q", it.Condition)
		}
	}
	g := e.get(o.ID)
	if g.Status != order.StatusReturned || e.events(o.ID, order.TopicOrderReturned, g.Version) != 1 {
		t.Fatalf("đơn: %s, thiếu order.returned", g.Status)
	}
	p := e.lastPayload(o.ID, order.TopicOrderReturned)
	if p.Kind != order.ReturnCarrier || p.ReturnID != r.ID || p.ShipmentID == nil || *p.ShipmentID != s1 ||
		p.MissingQty == nil || *p.MissingQty != 0 || p.CustomerID != o.CustomerID || p.Status != order.StatusReturned {
		t.Errorf("payload order.returned: %+v", p)
	}
	if n := e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE step = 'loss'`); n != 0 {
		t.Errorf("dòng sổ thất lạc = %d", n)
	}
}

// TD04 (ca b, phía đơn): V1 S0 D1 M1, V2 S0 D0 M1 → mọi in_transit về 0; V1 on_hand −1 (bước loss), damaged 1; V2
// on_hand −1; sổ thất lạc doc_id = mã phiếu; missingQty = 2.
func TestReceiveReturnQty_CarrierMissingRecordsLoss(t *testing.T) {
	e := newM2Env(t)
	c := e.carrierCase(t)
	o, v1, v2, i1, i2, b1, b2, r := c.o, c.v1, c.v2, c.i1, c.i2, c.b1, c.b2, c.r
	e.mustReceiveQty(r, qty(i1, 0, 1, 1), qty(i2, 0, 0, 1))
	if n := e.balance(v1); n.InTransit != 0 || n.OnHand != b1.OnHand-1 || n.Damaged != 1 || n.Available != b1.Available {
		t.Errorf("V1: %+v (trước %+v)", n, b1)
	}
	if n := e.balance(v2); n.InTransit != 0 || n.OnHand != b2.OnHand-1 || n.Available != b2.Available {
		t.Errorf("V2: %+v (trước %+v)", n, b2)
	}
	if n := e.count(`SELECT coalesce(sum(-delta), 0)::int FROM inventory.stock_ledger WHERE step = 'loss'
		AND doc_type = 'adjust' AND doc_id = $1`, r.ID); n != 2 {
		t.Errorf("thất lạc ghi sổ = %d, muốn 2", n)
	}
	if c, _, d, m := e.lineQty(r.ID, i1); c != order.ConditionMixed || d != 1 || m != 1 {
		t.Errorf("dòng V1: %s d=%d m=%d", c, d, m)
	}
	if c, _, _, m := e.lineQty(r.ID, i2); c != order.ConditionMissing || m != 1 {
		t.Errorf("dòng V2: %s m=%d", c, m)
	}
	if p := e.lastPayload(o.ID, order.TopicOrderReturned); p.MissingQty == nil || *p.MissingQty != 2 {
		t.Errorf("missingQty: %+v", p)
	}
}

// Kiểm số lượng: thiếu dòng, dòng lạ, dòng trùng, S+D+M lệch → ErrQtyMismatch; số âm → ErrInvalidInput; phiếu vẫn
// requested, tồn không đổi.
func TestReceiveReturnQty_Mismatch(t *testing.T) {
	e := newM2Env(t)
	o, v1, v2, s1 := e.pickedUpDH01()
	r := e.openCarrierReturn(o.ID, s1)
	i1, i2 := itemOf(t, o, v1).ID, itemOf(t, o, v2).ID
	b1 := e.balance(v1)
	for name, c := range map[string]struct {
		lines []order.ReceiveQtyLine
		want  error
	}{
		"thiếu dòng":    {[]order.ReceiveQtyLine{qty(i1, 2, 0, 0)}, order.ErrQtyMismatch},
		"dòng lạ":       {[]order.ReceiveQtyLine{qty(i1, 2, 0, 0), qty(uuid.New(), 1, 0, 0)}, order.ErrQtyMismatch},
		"dòng trùng":    {[]order.ReceiveQtyLine{qty(i1, 2, 0, 0), qty(i1, 2, 0, 0)}, order.ErrQtyMismatch},
		"tổng thiếu":    {[]order.ReceiveQtyLine{qty(i1, 1, 0, 0), qty(i2, 1, 0, 0)}, order.ErrQtyMismatch},
		"tổng dư":       {[]order.ReceiveQtyLine{qty(i1, 2, 1, 0), qty(i2, 1, 0, 0)}, order.ErrQtyMismatch},
		"số âm":         {[]order.ReceiveQtyLine{qty(i1, 3, -1, 0), qty(i2, 1, 0, 0)}, order.ErrInvalidInput},
		"không có dòng": {nil, order.ErrQtyMismatch},
	} {
		if _, err := e.receiveQty(r, c.lines...); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, muốn %v", name, err, c.want)
		}
	}
	if g, err := e.carrierReturnFor(s1); err != nil || g.Status != order.ReturnRequested {
		t.Errorf("phiếu sau các lần sai: %+v %v", g, err)
	}
	if e.balance(v1) != b1 {
		t.Error("tồn đổi sau các lần sai")
	}
}

// Q-T91-8: phiếu carrier_return mà vận đơn của phiếu không còn là vận đơn hiện hành của đơn → ErrInvalidTransition,
// không đổi gì.
func TestReceiveReturnQty_NotCurrentShipment(t *testing.T) {
	e := newM2Env(t)
	o, v1, v2, s1 := e.pickedUpDH01()
	r := e.openCarrierReturn(o.ID, s1)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE ord.orders SET current_shipment_id = NULL WHERE id = $1`, o.ID)
		return err //nolint:wrapcheck // test
	})
	_, err := e.receiveQty(r, qty(itemOf(t, o, v1).ID, 2, 0, 0), qty(itemOf(t, o, v2).ID, 1, 0, 0))
	if !errors.Is(err, order.ErrInvalidTransition) {
		t.Fatalf("nhận phiếu của vận đơn cũ: %v", err)
	}
	if n := e.balance(v1); n.InTransit != 2 {
		t.Errorf("V1 sau lỗi: %+v", n)
	}
}

// Phiếu khách trả theo số lượng: dòng 2 cái nhận S1 D1 → on_hand +2, damaged +1, tiền hoàn đủ, condition mixed;
// order.returned mang kind customer_return, missingQty 0. (Dòng thiếu một phần: returns_qty_customer_test.go, M3-24.)
func TestReceiveReturnQty_Customer(t *testing.T) {
	e := newM2Env(t)
	a := e.variant(250000, 5)
	o := e.deliveredOrder(ln(a, 2, 0))
	i := o.Items[0].ID
	r := e.mustReturn(o.ID, custReturn(rl(i, 2)))
	b := e.balance(a)
	got := e.mustReceiveQty(r, qty(i, 1, 1, 0))
	if got.RefundAmount != r.RefundAmount || got.Status != order.ReturnRestocked {
		t.Errorf("phiếu: %+v", got)
	}
	if n := e.balance(a); n.OnHand != b.OnHand+2 || n.Damaged != b.Damaged+1 || n.Available != b.Available+1 {
		t.Errorf("tồn: %+v (trước %+v)", n, b)
	}
	if c, s, d, _ := e.lineQty(r.ID, i); c != order.ConditionMixed || s != 1 || d != 1 {
		t.Errorf("dòng: %s %d/%d", c, s, d)
	}
	if p := e.lastPayload(o.ID, order.TopicOrderReturned); p.Kind != order.ReturnCustomer || p.ReturnID != r.ID ||
		p.MissingQty == nil || *p.MissingQty != 0 || p.ShipmentID != nil {
		t.Errorf("payload: %+v", p)
	}
}

// Thân M2 (tình trạng cả dòng) giờ ghi cả ba số: sellable → sellable_qty = số dòng; missing → missing_qty.
func TestReceiveReturn_WritesQtyColumns(t *testing.T) {
	e := newM2Env(t)
	o, a, b := e.orderD()
	ia, ib := itemOf(t, o, a).ID, itemOf(t, o, b).ID
	r := e.mustReturn(o.ID, custReturn(rl(ia, 1), rl(ib, 1)))
	e.mustReceive(r, order.ReceiveLine{OrderItemID: ia, Condition: order.ConditionSellable},
		order.ReceiveLine{OrderItemID: ib, Condition: order.ConditionMissing})
	if c, s, d, m := e.lineQty(r.ID, ia); c != order.ConditionSellable || s != 1 || d != 0 || m != 0 {
		t.Errorf("dòng a: %s %d/%d/%d", c, s, d, m)
	}
	if c, s, d, m := e.lineQty(r.ID, ib); c != order.ConditionMissing || s != 0 || d != 0 || m != 1 {
		t.Errorf("dòng b: %s %d/%d/%d", c, s, d, m)
	}
}
