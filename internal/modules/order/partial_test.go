package order_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
)

func (e *orderEnv) deliverPartial(o, s uuid.UUID, lines ...order.DeliveredLine) (bool, error) {
	return e.asCarrier(func(tx pgx.Tx, w order.Writer, a order.Actor) (bool, error) {
		return w.MarkDeliveredPartial(e.t.Context(), tx, e.seller, o, s, lines, a, e.now())
	})
}

// TD09: vB in_transit 2, giao [vA×1, vB×1] → đơn delivered, vA on_hand 4, vB on_hand 9, in_transit 1; order.delivered
// (seq = version) có partial: true; lịch sử confirmed → delivered; gọi lại → false, tồn không đổi.
func TestMarkDeliveredPartial(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	o, va, vb := e.pickedUpD1()
	balanceIs(t, e.balance(vb), 10, 0, 2)
	lines := []order.DeliveredLine{{OrderItemID: o.Items[0].ID, Qty: 1}, {OrderItemID: o.Items[1].ID, Qty: 1}}
	if applied, err := e.deliverPartial(o.ID, shipS1(), lines...); err != nil || !applied {
		t.Fatalf("giao một phần: %v, %v", applied, err)
	}
	got := e.get(o.ID)
	if got.Status != order.StatusDelivered || got.Version != o.Version+1 {
		t.Fatalf("sau giao một phần: %s, version %d (trước %d)", got.Status, got.Version, o.Version)
	}
	balanceIs(t, e.balance(va), 4, 0, 0)
	balanceIs(t, e.balance(vb), 9, 0, 1)
	if n := e.events(o.ID, order.TopicOrderDelivered, got.Version); n != 1 {
		t.Errorf("order.delivered seq %d: %d", got.Version, n)
	}
	if p := e.returnEvent(o.ID, order.TopicOrderDelivered); p["partial"] != true || p["status"] != order.StatusDelivered {
		t.Errorf("payload order.delivered: %v; muốn partial true", p)
	}
	if n := e.moves(o.ID, order.StatusConfirmed, order.StatusDelivered, inventory.ActorCarrier); n != 1 {
		t.Errorf("lịch sử confirmed → delivered: %d", n)
	}
	if applied, err := e.deliverPartial(o.ID, shipS1(), lines...); err != nil || applied {
		t.Errorf("gọi lại: %v, %v; muốn false, nil", applied, err)
	}
	e.mustOnEvent(o.ID, shipS1(), order.StepDelivered, false)
	balanceIs(t, e.balance(vb), 9, 0, 1)
}

// Giao đủ qua OnShipmentEvent (M2): payload order.delivered không có khóa partial (omitempty, payload M2 giữ nguyên).
func TestOnShipmentEvent_DeliveredNoPartialKey(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	o, _, _ := e.deliveredD1()
	if p := e.returnEvent(o.ID, order.TopicOrderDelivered); p == nil {
		t.Fatal("không có order.delivered")
	} else if _, ok := p["partial"]; ok {
		t.Errorf("order.delivered của M2 có khóa partial: %v", p)
	}
}

// Hãng giao khi chưa báo lấy hàng: dòng held → in_transit trước, rồi giao một phần.
func TestMarkDeliveredPartial_FromHeld(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	o, va, vb := e.attachedD1()
	if applied, err := e.deliverPartial(o.ID, shipS1(), order.DeliveredLine{OrderItemID: o.Items[1].ID, Qty: 1}); err != nil ||
		!applied {
		t.Fatalf("giao một phần từ held: %v, %v", applied, err)
	}
	balanceIs(t, e.balance(va), 5, 0, 1)
	balanceIs(t, e.balance(vb), 9, 0, 1)
	if g := e.get(o.ID); g.Status != order.StatusDelivered {
		t.Errorf("trạng thái: %s", g.Status)
	}
}

// Lỗi: không dòng, dòng của đơn khác → ErrInvalidInput; số lớn hơn số đang giữ → lỗi của inventory; vận đơn khác hay
// đơn chưa gắn → false. Mọi lần lỗi không đổi gì.
func TestMarkDeliveredPartial_Rejects(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	o, _, vb := e.pickedUpD1()
	other, _, _ := e.d1()
	if _, err := e.deliverPartial(o.ID, shipS1()); !errors.Is(err, order.ErrInvalidInput) {
		t.Errorf("không dòng: %v; muốn ErrInvalidInput", err)
	}
	foreign := order.DeliveredLine{OrderItemID: other.Items[0].ID, Qty: 1}
	if _, err := e.deliverPartial(o.ID, shipS1(), foreign); !errors.Is(err, order.ErrInvalidInput) {
		t.Errorf("dòng của đơn khác: %v; muốn ErrInvalidInput", err)
	}
	if _, err := e.deliverPartial(o.ID, shipS1(), order.DeliveredLine{OrderItemID: o.Items[1].ID, Qty: 3}); !errors.Is(err,
		inventory.ErrStateConflict) {
		t.Errorf("giao 3 khi giữ 2: %v; muốn inventory.ErrStateConflict", err)
	}
	if applied, err := e.deliverPartial(o.ID, shipS2(), order.DeliveredLine{OrderItemID: o.Items[1].ID, Qty: 1}); err != nil ||
		applied {
		t.Errorf("vận đơn khác: %v, %v; muốn false, nil", applied, err)
	}
	if applied, err := e.deliverPartial(other.ID, shipS1(), order.DeliveredLine{OrderItemID: other.Items[0].ID, Qty: 1}); err != nil ||
		applied {
		t.Errorf("đơn chưa gắn: %v, %v; muốn false, nil", applied, err)
	}
	if g := e.get(o.ID); g.Status != order.StatusConfirmed || g.Version != o.Version {
		t.Errorf("lần bị từ chối đổi đơn: %s, %d", g.Status, g.Version)
	}
	balanceIs(t, e.balance(vb), 10, 0, 2)
}

// Đơn cancel_requested mà hãng giao một phần (giao trước khi kịp hủy): về confirmed (carrier_rejected_cancel) rồi
// delivered, như OnShipmentEvent(delivered) của M2.
func TestMarkDeliveredPartial_CancelRequested(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	o, _, _ := e.pickedUpD1()
	if _, err := e.cancel(o); err != nil {
		t.Fatal(err)
	}
	if g := e.get(o.ID); g.Status != order.StatusCancelRequested {
		t.Fatalf("muốn cancel_requested: %s", g.Status)
	}
	if applied, err := e.deliverPartial(o.ID, shipS1(), order.DeliveredLine{OrderItemID: o.Items[0].ID, Qty: 1}); err != nil ||
		!applied {
		t.Fatalf("giao một phần khi chờ hủy: %v, %v", applied, err)
	}
	if g := e.get(o.ID); g.Status != order.StatusDelivered || g.CancelReason != nil {
		t.Errorf("sau giao: %s, lý do hủy %v", g.Status, g.CancelReason)
	}
	if n := e.moves(o.ID, order.StatusCancelRequested, order.StatusConfirmed, inventory.ActorCarrier); n != 1 {
		t.Errorf("lịch sử cancel_requested → confirmed: %d", n)
	}
}
