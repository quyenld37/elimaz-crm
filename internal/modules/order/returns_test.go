package order_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/queries/orderview"
)

// deliveredOrder tạo đơn confirmed các dòng lines (phí 30.000) rồi gắn vận đơn mới và báo giao xong → delivered.
func (e *orderEnv) deliveredOrder(lines ...order.LineInput) order.Order {
	e.t.Helper()
	o := e.mustCreateM2(e.m2Input(30000, lines...)).Order
	s := uuid.New()
	if err := e.attachBy(o.ID, s); err != nil {
		e.t.Fatal(err)
	}
	e.mustOnEvent(o.ID, s, order.StepDelivered, true)
	o = e.get(o.ID)
	if o.Status != order.StatusDelivered {
		e.t.Fatalf("đơn chưa delivered: %s", o.Status)
	}
	return o
}

// orderD là đơn D của kế hoạch (Task M2-22): dòng A ×1 net 250.000, dòng B ×1 net 180.000, đã giao. Trả đơn, A, B.
func (e *orderEnv) orderD() (order.Order, uuid.UUID, uuid.UUID) {
	e.t.Helper()
	a, b := e.variant(250000, 5), e.variant(180000, 5)
	o := e.deliveredOrder(ln(a, 1, 0), ln(b, 1, 0))
	if o.Items[0].NetAmount != 250000 || o.Items[1].NetAmount != 180000 {
		e.t.Fatalf("đơn D: %+v", o.Items)
	}
	return o, a, b
}

// itemOf trả dòng đơn của biến thể v.
func itemOf(t *testing.T, o order.Order, v uuid.UUID) order.Item {
	t.Helper()
	for _, it := range o.Items {
		if it.VariantID == v {
			return it
		}
	}
	t.Fatalf("đơn %s không có biến thể %s", o.Code, v)
	return order.Item{}
}

func custReturn(lines ...order.ReturnLineInput) order.CreateReturnInput {
	return order.CreateReturnInput{Kind: order.ReturnCustomer, Lines: lines, Reason: "khách không vừa"}
}

func rl(item uuid.UUID, q int) order.ReturnLineInput {
	return order.ReturnLineInput{OrderItemID: item, Quantity: q}
}

func (e *orderEnv) createReturn(o uuid.UUID, in order.CreateReturnInput) (order.Return, error) {
	var r order.Return
	err := e.app(func(tx pgx.Tx) error {
		var err error
		r, err = e.writer().CreateReturn(e.t.Context(), tx, e.seller, o, in, e.actor, e.now())
		return err //nolint:wrapcheck // test
	})
	return r, err
}

func (e *orderEnv) mustReturn(o uuid.UUID, in order.CreateReturnInput) order.Return {
	e.t.Helper()
	r, err := e.createReturn(o, in)
	if err != nil {
		e.t.Fatalf("tạo phiếu trả: %v", err)
	}
	return r
}

func (e *orderEnv) receive(r order.Return, lines ...order.ReceiveLine) (order.Return, error) {
	var out order.Return
	err := e.app(func(tx pgx.Tx) error {
		var err error
		out, err = e.writer().ReceiveReturn(e.t.Context(), tx, e.seller, r.ID, r.Version, lines, e.actor, e.now())
		return err //nolint:wrapcheck // test
	})
	return out, err
}

func (e *orderEnv) mustReceive(r order.Return, lines ...order.ReceiveLine) order.Return {
	e.t.Helper()
	out, err := e.receive(r, lines...)
	if err != nil {
		e.t.Fatalf("nhận phiếu %s: %v", r.Code, err)
	}
	return out
}

// receiveAll nhận mọi dòng của r với cùng tình trạng cond.
func receiveAll(r order.Return, cond string) []order.ReceiveLine {
	out := make([]order.ReceiveLine, 0, len(r.Items))
	for _, it := range r.Items {
		out = append(out, order.ReceiveLine{OrderItemID: it.OrderItemID, Condition: cond})
	}
	return out
}

func (e *orderEnv) returnStep(fn func(tx pgx.Tx, w order.Writer) (order.Return, error)) (order.Return, error) {
	var out order.Return
	err := e.app(func(tx pgx.Tx) error {
		var err error
		out, err = fn(tx, e.writer())
		return err
	})
	return out, err
}

func (e *orderEnv) closeReturn(r order.Return) (order.Return, error) {
	return e.returnStep(func(tx pgx.Tx, w order.Writer) (order.Return, error) {
		return w.CloseReturn(e.t.Context(), tx, e.seller, r.ID, r.Version, e.actor, e.now())
	})
}

func (e *orderEnv) cancelReturn(r order.Return) (order.Return, error) {
	return e.returnStep(func(tx pgx.Tx, w order.Writer) (order.Return, error) {
		return w.CancelReturn(e.t.Context(), tx, e.seller, r.ID, r.Version, e.actor, e.now())
	})
}

func (e *orderEnv) getReturn(id uuid.UUID) order.Return {
	e.t.Helper()
	r, err := e.returnStep(func(tx pgx.Tx, _ order.Writer) (order.Return, error) {
		return order.GetReturn(e.t.Context(), tx, e.seller, id)
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

// returnEvent đọc payload sự kiện topic (aggregate order) của đơn o; không có → nil.
func (e *orderEnv) returnEvent(o uuid.UUID, topic string) map[string]any {
	e.t.Helper()
	var raw []byte
	err := e.staff.TryExec(e.t.Context(), e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT payload FROM platform.domain_events WHERE aggregate_type = 'order'
			AND aggregate_id = $1 AND topic = $2 ORDER BY seq DESC LIMIT 1`, o.String(), topic).Scan(&raw)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		e.t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		e.t.Fatal(err)
	}
	return m
}

// TD05/AT13, TD07/WAT28: trả A ×1 → requested, refundAmount 250.000, mã TH; phiếu hai trả A ×1 khi phiếu một còn mở →
// ErrReturnQtyExceeds; trả quá số mua, dòng lạ, trùng dòng → lỗi; đơn chưa giao → ErrReturnNotAllowed; người bán khác,
// đơn không có → ErrNotFound.
func TestCreateReturn_TD05PerLine(t *testing.T) {
	e := newM2Env(t)
	o, a, b := e.orderD()
	ia, ib := itemOf(t, o, a), itemOf(t, o, b)
	r := e.mustReturn(o.ID, custReturn(rl(ia.ID, 1)))
	if r.Status != order.ReturnRequested || r.Kind != order.ReturnCustomer || r.RefundAmount != 250000 ||
		len(r.Items) != 1 || r.Items[0].RefundAmount != 250000 || r.Items[0].Quantity != 1 || r.OrderID != o.ID ||
		len(r.Code) < 9 || r.Code[:2] != "TH" || r.Version != 1 || r.Reason != "khách không vừa" {
		t.Fatalf("phiếu: %+v", r)
	}
	if got := e.getReturn(r.ID); got.Code != r.Code || got.RefundAmount != 250000 || len(got.Items) != 1 {
		t.Errorf("GetReturn: %+v", got)
	}
	if _, err := e.createReturn(o.ID, custReturn(rl(ia.ID, 1))); !errors.Is(err, order.ErrReturnQtyExceeds) {
		t.Errorf("phiếu hai cùng A: %v", err)
	}
	for name, in := range map[string]order.CreateReturnInput{
		"B ×2":                   custReturn(rl(ib.ID, 2)),
		"dòng lạ":                custReturn(rl(uuid.New(), 1)),
		"trùng B":                custReturn(rl(ib.ID, 1), rl(ib.ID, 1)),
		"số 0":                   custReturn(rl(ib.ID, 0)),
		"không dòng":             custReturn(),
		"loại lạ":                {Kind: order.ReturnCarrier, Lines: []order.ReturnLineInput{rl(ib.ID, 1)}},
		"đổi thiếu đơn thay thế": {Kind: order.ReturnExchange, Lines: []order.ReturnLineInput{rl(ib.ID, 1)}},
	} {
		_, err := e.createReturn(o.ID, in)
		want := order.ErrInvalidInput
		if name == "B ×2" {
			want = order.ErrReturnQtyExceeds
		}
		if !errors.Is(err, want) {
			t.Errorf("%s: mong %v, có %v", name, want, err)
		}
	}
	conf, _, _ := e.confirmedDH01()
	if _, err := e.createReturn(conf.ID, custReturn(rl(conf.Items[0].ID, 1))); !errors.Is(err, order.ErrReturnNotAllowed) {
		t.Errorf("đơn chưa giao: %v", err)
	}
	if _, err := e.createReturn(uuid.New(), custReturn(rl(ia.ID, 1))); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("đơn không có: %v", err)
	}
	other := *e
	other.seller = e.partner
	if _, err := other.createReturn(o.ID, custReturn(rl(ib.ID, 1))); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("người bán khác: %v", err)
	}
	if n := e.count(`SELECT count(*) FROM ord.returns WHERE order_id = $1`, o.ID); n != 1 {
		t.Errorf("số phiếu của D = %d, mong 1", n)
	}
}

// Làm tròn (WAT31): D2 dòng C ×2 net 500.001 → trả C ×1 = 250.000, lần sau C ×1 = 250.001, tổng 500.001.
func TestCreateReturn_RoundingLastUnit(t *testing.T) {
	e := newM2Env(t)
	c := e.variant(250001, 5)
	o := e.deliveredOrder(ln(c, 2, 1))
	ic := itemOf(t, o, c)
	if ic.NetAmount != 500001 {
		t.Fatalf("net C = %d", ic.NetAmount)
	}
	r1 := e.mustReturn(o.ID, custReturn(rl(ic.ID, 1)))
	r2 := e.mustReturn(o.ID, custReturn(rl(ic.ID, 1)))
	if r1.RefundAmount != 250000 || r2.RefundAmount != 250001 {
		t.Fatalf("tiền hoàn %d + %d, mong 250000 + 250001", r1.RefundAmount, r2.RefundAmount)
	}
}

// TD08/WAT29: phiếu requested → tồn không đổi, chưa có order.partially_returned; nhận A sellable → on_hand +1, phiếu
// restocked, sự kiện order.partially_returned có refundAmount 250.000, đơn vẫn delivered (version +1). Nhận lần hai,
// version cũ → lỗi. Trả nốt B và nhận → order.returned.
func TestReceiveReturn_TD08Restock(t *testing.T) {
	e := newM2Env(t)
	o, a, b := e.orderD()
	before := e.balance(a)
	r := e.mustReturn(o.ID, custReturn(rl(itemOf(t, o, a).ID, 1)))
	if got := e.balance(a); got != before {
		t.Errorf("tồn đổi khi mới tạo phiếu: %+v → %+v", before, got)
	}
	if e.returnEvent(o.ID, order.TopicOrderPartiallyReturned) != nil {
		t.Error("có order.partially_returned khi phiếu còn requested")
	}
	stale := r
	stale.Version++
	if _, err := e.receive(stale, receiveAll(r, order.ConditionSellable)...); !errors.Is(err, order.ErrVersionConflict) {
		t.Errorf("version cũ: %v", err)
	}
	if _, err := e.receive(r, order.ReceiveLine{OrderItemID: itemOf(t, o, b).ID, Condition: order.ConditionSellable}); !errors.Is(err, order.ErrInvalidInput) {
		t.Errorf("nhận dòng không có trong phiếu: %v", err)
	}
	got := e.mustReceive(r, receiveAll(r, order.ConditionSellable)...)
	if got.Status != order.ReturnRestocked || got.RefundAmount != 250000 || got.ReceivedAt == nil ||
		!got.ReceivedAt.Equal(e.now()) || got.Version != r.Version+1 || got.Items[0].Condition != order.ConditionSellable {
		t.Fatalf("phiếu sau nhận: %+v", got)
	}
	if bal := e.balance(a); bal.OnHand != before.OnHand+1 || bal.Damaged != 0 || bal.Available != before.Available+1 {
		t.Errorf("tồn A sau nhận: %+v (trước %+v)", bal, before)
	}
	d := e.get(o.ID)
	if d.Status != order.StatusDelivered || d.Version != o.Version+1 {
		t.Errorf("đơn D sau nhận: %s v%d (trước v%d)", d.Status, d.Version, o.Version)
	}
	ev := e.returnEvent(o.ID, order.TopicOrderPartiallyReturned)
	if ev == nil || ev["refundAmount"] != float64(250000) || ev["returnId"] != r.ID.String() ||
		ev["kind"] != order.ReturnCustomer || ev["status"] != order.StatusDelivered || ev["version"] != float64(d.Version) {
		t.Errorf("order.partially_returned: %v", ev)
	}
	if _, err := e.receive(got, receiveAll(r, order.ConditionSellable)...); !errors.Is(err, order.ErrReturnNotAllowed) {
		t.Errorf("nhận lần hai: %v", err)
	}
	r2 := e.mustReturn(o.ID, custReturn(rl(itemOf(t, o, b).ID, 1)))
	e.mustReceive(r2, receiveAll(r2, order.ConditionSellable)...)
	if ev := e.returnEvent(o.ID, order.TopicOrderReturned); ev == nil || ev["refundAmount"] != float64(180000) ||
		ev["returnId"] != r2.ID.String() {
		t.Errorf("order.returned khi trả đủ: %v", ev)
	}
	if g := e.get(o.ID); g.Status != order.StatusDelivered {
		t.Errorf("đơn gốc lùi trạng thái: %s", g.Status)
	}
}

// Hàng hỏng: nhận damaged → on_hand +1, damaged +1, Available không đổi; missing → tồn không đổi, tiền hoàn dòng 0.
func TestReceiveReturn_DamagedAndMissing(t *testing.T) {
	e := newM2Env(t)
	o, a, b := e.orderD()
	ba, bb := e.balance(a), e.balance(b)
	r := e.mustReturn(o.ID, custReturn(rl(itemOf(t, o, a).ID, 1), rl(itemOf(t, o, b).ID, 1)))
	got := e.mustReceive(r,
		order.ReceiveLine{OrderItemID: itemOf(t, o, a).ID, Condition: order.ConditionDamaged},
		order.ReceiveLine{OrderItemID: itemOf(t, o, b).ID, Condition: order.ConditionMissing})
	if na := e.balance(a); na.OnHand != ba.OnHand+1 || na.Damaged != ba.Damaged+1 || na.Available != ba.Available {
		t.Errorf("A hỏng: %+v (trước %+v)", na, ba)
	}
	if nb := e.balance(b); nb != bb {
		t.Errorf("B thiếu mà tồn đổi: %+v (trước %+v)", nb, bb)
	}
	if got.RefundAmount != 250000 {
		t.Errorf("tiền hoàn phiếu = %d, mong 250000 (B thiếu không hoàn)", got.RefundAmount)
	}
	for _, it := range got.Items {
		want := int64(250000)
		if it.Condition == order.ConditionMissing {
			want = 0
		}
		if it.RefundAmount != want {
			t.Errorf("dòng %+v: tiền hoàn mong %d", it, want)
		}
	}
	if ev := e.returnEvent(o.ID, order.TopicOrderReturned); ev == nil || ev["refundAmount"] != float64(250000) {
		t.Errorf("order.returned: %v", ev)
	}
}

// TD06/AT14: đổi A size M lấy A size L → phiếu exchange + đơn thay thế orderType exchange giữ A-L ×1, cùng khách, người
// bán, ReplacedReturnID; đơn D không đổi dòng, tổng, trạng thái. Hủy phiếu đổi → đơn thay thế cancelled, A-L nhả.
func TestCreateReturn_TD06Exchange(t *testing.T) {
	e := newM2Env(t)
	o, a, _ := e.orderD()
	al := e.variant(250000, 3)
	repl := e.m2Input(0, ln(al, 1, 0))
	repl.ShipAddress = nil
	r := e.mustReturn(o.ID, order.CreateReturnInput{
		Kind: order.ReturnExchange, Lines: []order.ReturnLineInput{rl(itemOf(t, o, a).ID, 1)}, Replacement: &repl,
	})
	if r.Kind != order.ReturnExchange || r.Status != order.ReturnRequested || r.ReplacementOrderID == nil {
		t.Fatalf("phiếu đổi: %+v", r)
	}
	n := e.get(*r.ReplacementOrderID)
	if n.OrderType != order.OrderTypeExchange || n.ReplacedReturnID == nil || *n.ReplacedReturnID != r.ID ||
		n.CustomerID != o.CustomerID || n.SellerID != o.SellerID || len(n.Items) != 1 || n.Items[0].VariantID != al ||
		n.Items[0].Qty != 1 || n.Status != order.StatusConfirmed || n.CouponDiscount != 0 ||
		n.ShipAddress.WardCode != o.ShipAddress.WardCode {
		t.Fatalf("đơn thay thế: %+v", n)
	}
	if b := e.balance(al); b.Held != 1 || b.Available != 2 {
		t.Errorf("A-L sau đổi: %+v", b)
	}
	d := e.get(o.ID)
	if d.Status != o.Status || d.GrandTotal != o.GrandTotal || len(d.Items) != len(o.Items) || d.Version != o.Version {
		t.Errorf("đơn gốc bị sửa: %+v", d)
	}
	coupon := e.m2Input(0, ln(al, 1, 0))
	coupon.CouponCode = "SALE10"
	if _, err := e.createReturn(o.ID, order.CreateReturnInput{
		Kind: order.ReturnExchange, Lines: []order.ReturnLineInput{rl(itemOf(t, o, a).ID, 1)}, Replacement: &coupon,
	}); !errors.Is(err, order.ErrInvalidInput) {
		t.Errorf("đơn thay thế có mã giảm giá: %v", err)
	}
	c, err := e.cancelReturn(r)
	if err != nil || c.Status != order.ReturnCancelled || c.Version != r.Version+1 {
		t.Fatalf("hủy phiếu đổi: %+v, %v", c, err)
	}
	if g := e.get(*r.ReplacementOrderID); g.Status != order.StatusCancelled {
		t.Errorf("đơn thay thế sau hủy phiếu: %s", g.Status)
	}
	if b := e.balance(al); b.Held != 0 || b.Available != 3 {
		t.Errorf("A-L sau hủy phiếu: %+v", b)
	}
	// Phiếu đã hủy không tính vào số đã trả.
	e.mustReturn(o.ID, custReturn(rl(itemOf(t, o, a).ID, 1)))
}

// Hủy phiếu đổi khi đơn thay thế đã gửi hãng → ErrReturnNotAllowed, không đổi gì.
func TestCancelReturn_ExchangeShipped(t *testing.T) {
	e := newM2Env(t)
	o, a, _ := e.orderD()
	repl := e.m2Input(0, ln(e.variant(250000, 3), 1, 0))
	r := e.mustReturn(o.ID, order.CreateReturnInput{
		Kind: order.ReturnExchange, Lines: []order.ReturnLineInput{rl(itemOf(t, o, a).ID, 1)}, Replacement: &repl,
	})
	if err := e.attachBy(*r.ReplacementOrderID, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.cancelReturn(r); !errors.Is(err, order.ErrReturnNotAllowed) {
		t.Errorf("hủy phiếu đổi đã gửi hãng: %v", err)
	}
	if g := e.getReturn(r.ID); g.Status != order.ReturnRequested {
		t.Errorf("phiếu: %s", g.Status)
	}
}

// Đóng, hủy: requested → cancelled (số đã trả mở lại); restocked → closed; đóng phiếu requested, hủy phiếu restocked →
// ErrReturnNotAllowed; version cũ → ErrVersionConflict; phiếu không có → ErrNotFound.
func TestCloseCancelReturn(t *testing.T) {
	e := newM2Env(t)
	o, a, _ := e.orderD()
	ia := itemOf(t, o, a).ID
	r := e.mustReturn(o.ID, custReturn(rl(ia, 1)))
	if _, err := e.closeReturn(r); !errors.Is(err, order.ErrReturnNotAllowed) {
		t.Errorf("đóng phiếu requested: %v", err)
	}
	stale := r
	stale.Version = 9
	if _, err := e.cancelReturn(stale); !errors.Is(err, order.ErrVersionConflict) {
		t.Errorf("hủy version cũ: %v", err)
	}
	c, err := e.cancelReturn(r)
	if err != nil || c.Status != order.ReturnCancelled {
		t.Fatalf("hủy: %+v %v", c, err)
	}
	r2 := e.mustReturn(o.ID, custReturn(rl(ia, 1)))
	r2 = e.mustReceive(r2, receiveAll(r2, order.ConditionSellable)...)
	if _, err := e.cancelReturn(r2); !errors.Is(err, order.ErrReturnNotAllowed) {
		t.Errorf("hủy phiếu restocked: %v", err)
	}
	cl, err := e.closeReturn(r2)
	if err != nil || cl.Status != order.ReturnClosed || cl.Version != r2.Version+1 {
		t.Fatalf("đóng: %+v %v", cl, err)
	}
	missing := r2
	missing.ID = uuid.New()
	if _, err := e.closeReturn(missing); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("phiếu không có: %v", err)
	}
}

// pickedUpDH01 tạo đơn DH01 confirmed (V1 ×2, V2 ×1), gắn vận đơn mới S1 và báo đã lấy hàng; trả đơn, V1, V2, S1.
func (e *orderEnv) pickedUpDH01() (order.Order, uuid.UUID, uuid.UUID, uuid.UUID) {
	e.t.Helper()
	o, v1, v2 := e.confirmedDH01()
	s1 := uuid.New()
	if err := e.attachBy(o.ID, s1); err != nil {
		e.t.Fatal(err)
	}
	e.mustOnEvent(o.ID, s1, order.StepPickedUp, true)
	return o, v1, v2, s1
}

// returning gọi OnShipmentReturning(o, s) với tác nhân hãng.
func (e *orderEnv) returning(o, s uuid.UUID) (applied bool, err error) {
	carrier := order.Actor{TenantID: e.tenant, Type: inventory.ActorCarrier}
	err = e.app(func(tx pgx.Tx) error {
		var callErr error
		applied, callErr = e.writer().OnShipmentReturning(e.t.Context(), tx, e.seller, o, s, carrier, e.now())
		return callErr //nolint:wrapcheck // test
	})
	return applied, err
}

// openCarrierReturn: vận đơn s của đơn o bắt đầu hoàn, trả phiếu carrier_return của nó.
func (e *orderEnv) openCarrierReturn(o, s uuid.UUID) order.Return {
	e.t.Helper()
	if _, err := e.returning(o, s); err != nil {
		e.t.Fatal(err)
	}
	r, err := e.carrierReturnFor(s)
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

func (e *orderEnv) carrierReturnFor(s uuid.UUID) (order.Return, error) {
	return e.returnStep(func(tx pgx.Tx, _ order.Writer) (order.Return, error) {
		return order.CarrierReturnFor(e.t.Context(), tx, e.seller, s)
	})
}

// TD02 (phía đơn): đơn có vận đơn S1 đã lấy hàng; OnShipmentReturning(S1) → phiếu carrier_return requested đủ mọi
// dòng, đơn confirmed, Available không đổi; lần hai, vận đơn cũ → false; CarrierReturnFor(S1) trả phiếu đó, vận đơn
// lạ → ErrNotFound.
func TestCarrierReturn_TD02Requested(t *testing.T) {
	e := newM2Env(t)
	o, v1, v2, s1 := e.pickedUpDH01()
	b1, b2 := e.balance(v1), e.balance(v2)
	if applied, err := e.returning(o.ID, s1); err != nil || !applied {
		t.Fatalf("lần đầu: %v %v", applied, err)
	}
	for name, s := range map[string]uuid.UUID{"lần hai": s1, "vận đơn cũ": uuid.New()} {
		if applied, err := e.returning(o.ID, s); err != nil || applied {
			t.Errorf("%s: %v %v", name, applied, err)
		}
	}
	r, err := e.carrierReturnFor(s1)
	if err != nil || r.Kind != order.ReturnCarrier || r.Status != order.ReturnRequested || r.ShipmentID == nil ||
		*r.ShipmentID != s1 || len(r.Items) != 2 || r.RefundAmount != 0 {
		t.Fatalf("phiếu hoàn: %+v %v", r, err)
	}
	if n := e.count(`SELECT count(*) FROM ord.returns WHERE order_id = $1`, o.ID); n != 1 {
		t.Errorf("số phiếu hoàn = %d", n)
	}
	if g := e.get(o.ID); g.Status != order.StatusConfirmed {
		t.Errorf("đơn khi bắt đầu hoàn: %s", g.Status)
	}
	if e.balance(v1) != b1 || e.balance(v2) != b2 {
		t.Error("tồn đổi khi mới bắt đầu hoàn")
	}
	if _, err := e.carrierReturnFor(uuid.New()); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("CarrierReturnFor vận đơn lạ: %v", err)
	}
}

// TD02 (phía đơn): kho nhận kiện hoàn (V1 sellable, V2 damaged) → in_transit về 0, V2 damaged +1, đơn returned, nhãn
// returned, sự kiện order.returned, mọi giữ hàng released.
func TestCarrierReturn_TD02Receive(t *testing.T) {
	e := newM2Env(t)
	o, v1, v2, s1 := e.pickedUpDH01()
	b1, b2 := e.balance(v1), e.balance(v2)
	r := e.openCarrierReturn(o.ID, s1)
	got := e.mustReceive(r,
		order.ReceiveLine{OrderItemID: itemOf(t, o, v1).ID, Condition: order.ConditionSellable},
		order.ReceiveLine{OrderItemID: itemOf(t, o, v2).ID, Condition: order.ConditionDamaged})
	if got.Status != order.ReturnRestocked {
		t.Errorf("phiếu sau nhận: %s", got.Status)
	}
	if n1 := e.balance(v1); n1.InTransit != 0 || n1.OnHand != b1.OnHand || n1.Available != b1.Available+2 {
		t.Errorf("V1 sau nhận: %+v (trước %+v)", n1, b1)
	}
	if n2 := e.balance(v2); n2.InTransit != 0 || n2.Damaged != 1 || n2.OnHand != b2.OnHand || n2.Available != 0 {
		t.Errorf("V2 sau nhận: %+v (trước %+v)", n2, b2)
	}
	g := e.get(o.ID)
	label := orderview.Label(orderview.Axes{Status: g.Status, PendingReason: g.PendingReason, PackingStatus: g.PackingStatus})
	if g.Status != order.StatusReturned || label != "returned" {
		t.Errorf("đơn sau nhận hoàn: %s, nhãn %s", g.Status, label)
	}
	if e.events(o.ID, order.TopicOrderReturned, g.Version) != 1 {
		t.Error("thiếu order.returned")
	}
	if n := e.heldOf(o.ID, inventory.StateReleased); n != 3 { // V1 ×2 + V2 ×1
		t.Errorf("giữ hàng released = %d", n)
	}
}

// Phiếu hoàn của hãng: dòng missing không nhả giữ hàng in_transit (Quyết định 2); dòng sellable nhả.
func TestCarrierReturn_MissingStaysInTransit(t *testing.T) {
	e := newM2Env(t)
	o, v1, v2, s1 := e.pickedUpDH01()
	r := e.openCarrierReturn(o.ID, s1)
	e.mustReceive(r,
		order.ReceiveLine{OrderItemID: itemOf(t, o, v1).ID, Condition: order.ConditionSellable},
		order.ReceiveLine{OrderItemID: itemOf(t, o, v2).ID, Condition: order.ConditionMissing})
	if b := e.balance(v2); b.InTransit != 1 {
		t.Errorf("V2 thiếu: %+v (mong in_transit 1)", b)
	}
	if b := e.balance(v1); b.InTransit != 0 {
		t.Errorf("V1: %+v", b)
	}
	if g := e.get(o.ID); g.Status != order.StatusReturned {
		t.Errorf("đơn: %s", g.Status)
	}
}

// listReturns: mới nhất trước, lọc trạng thái, loại; trang sau theo con trỏ; người bán khác không thấy.
func TestListReturns(t *testing.T) {
	e := newM2Env(t)
	o, a, b := e.orderD()
	r1 := e.mustReturn(o.ID, custReturn(rl(itemOf(t, o, a).ID, 1)))
	r2 := e.mustReturn(o.ID, custReturn(rl(itemOf(t, o, b).ID, 1)))
	list := func(seller uuid.UUID, f order.ReturnFilter) ([]order.Return, *order.ReturnCursor) {
		t.Helper()
		var rs []order.Return
		var next *order.ReturnCursor
		if err := e.app(func(tx pgx.Tx) error {
			var err error
			rs, next, err = order.ListReturns(e.t.Context(), tx, seller, f)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
		return rs, next
	}
	rs, next := list(e.seller, order.ReturnFilter{Limit: 1})
	if len(rs) != 1 || rs[0].ID != r2.ID || next == nil || len(rs[0].Items) != 1 {
		t.Fatalf("trang 1: %+v %v", rs, next)
	}
	parsed, err := order.ParseReturnCursor(next.String())
	if err != nil {
		t.Fatal(err)
	}
	if rs, next = list(e.seller, order.ReturnFilter{Limit: 1, After: &parsed}); len(rs) != 1 || rs[0].ID != r1.ID || next != nil {
		t.Errorf("trang 2: %+v %v", rs, next)
	}
	if _, err := order.ParseReturnCursor("!!"); !errors.Is(err, order.ErrInvalidInput) {
		t.Errorf("con trỏ hỏng: %v", err)
	}
	if rs, _ = list(e.seller, order.ReturnFilter{Status: order.ReturnCancelled}); len(rs) != 0 {
		t.Errorf("lọc cancelled: %+v", rs)
	}
	if rs, _ = list(e.seller, order.ReturnFilter{Kind: order.ReturnCustomer}); len(rs) != 2 {
		t.Errorf("lọc customer_return: %d", len(rs))
	}
	if rs, _ = list(e.partner, order.ReturnFilter{}); len(rs) != 0 {
		t.Errorf("người bán khác thấy %d phiếu", len(rs))
	}
}
