package order_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
)

// Móc nối vận đơn khi đơn cancel_requested (Task 431, B-01, B-02 của review độc lập M2; 05 mục 2; kế hoạch M3 VC12).

// requestedDH01 tạo đơn DH01 có vận đơn S1 rồi yêu cầu hủy (cancel_requested, lý do customer_changed_mind); trả đơn
// sau yêu cầu, S1 và hai biến thể (V1 ×2 on_hand 5, V2 ×1 on_hand 1).
func (e *orderEnv) requestedDH01() (order.Order, uuid.UUID, uuid.UUID, uuid.UUID) {
	e.t.Helper()
	o, s1 := e.attachedDH01()
	req := e.mustCancel(o)
	if req.Status != order.StatusCancelRequested {
		e.t.Fatalf("yêu cầu hủy: %s", req.Status)
	}
	return req, s1, o.Items[0].VariantID, o.Items[1].VariantID
}

// requestedAfterPickup tạo đơn DH01 đã lấy hàng (pickedUpDH01) rồi yêu cầu hủy (nhân viên hủy đơn đang giao →
// cancel_requested, hàng in_transit); trả đơn sau yêu cầu, S1 và V1.
func (e *orderEnv) requestedAfterPickup() (order.Order, uuid.UUID, uuid.UUID) {
	e.t.Helper()
	o, v1, _, s1 := e.pickedUpDH01()
	req := e.mustCancel(e.get(o.ID))
	if req.Status != order.StatusCancelRequested {
		e.t.Fatalf("yêu cầu hủy đơn đang giao: %s", req.Status)
	}
	return req, s1, v1
}

// moves đếm dòng lịch sử from → to của đơn o với người làm actor (qua history của confirm_test.go).
func (e *orderEnv) moves(o uuid.UUID, from, to, actor string) int {
	e.t.Helper()
	return e.history(o, "from_status = $2 AND to_status = $3 AND actor_type = $4", from, to, actor)
}

// TD03 + VC12 (thứ tự gọi của M3: EffPickedUp rồi EffCancelRejected): đơn cancel_requested, hãng lấy hàng →
// applied, held → in_transit, đơn GIỮ cancel_requested (version không đổi, chưa có lời hãng về lệnh hủy); lấy hàng
// lần hai → false; hãng từ chối hủy → confirmed, vẫn in_transit, held 0; giao xong → delivered, on_hand −.
func TestOnShipmentEvent_PickedUpWhileCancelRequested(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	req, s1, v1, v2 := e.requestedDH01()
	e.mustOnEvent(req.ID, s1, order.StepPickedUp, true)
	got := e.get(req.ID)
	if got.Status != order.StatusCancelRequested || got.Version != req.Version || got.CurrentShipmentID == nil ||
		*got.CurrentShipmentID != s1 || got.CancelReason == nil {
		t.Fatalf("sau lấy hàng: %s, version %d (trước %d), vận đơn %v, lý do %v", got.Status, got.Version, req.Version,
			got.CurrentShipmentID, got.CancelReason)
	}
	balanceIs(t, e.balance(v1), 5, 0, 2)
	balanceIs(t, e.balance(v2), 1, 0, 1)
	e.mustOnEvent(req.ID, s1, order.StepPickedUp, false)
	if g := e.get(req.ID); g.Version != req.Version || g.Status != order.StatusCancelRequested {
		t.Errorf("lấy hàng lần hai đổi đơn: %s, %d", g.Status, g.Version)
	}
	if applied, err := e.carrierCancel(req.ID, s1, false); err != nil || !applied {
		t.Fatalf("hãng từ chối hủy sau lấy hàng: %v, %v", applied, err)
	}
	rej := e.get(req.ID)
	if rej.Status != order.StatusConfirmed || rej.CancelReason != nil || rej.Version != req.Version+1 {
		t.Errorf("sau từ chối: %s, lý do %v, version %d", rej.Status, rej.CancelReason, rej.Version)
	}
	if e.heldOf(req.ID, inventory.StateHeld) != 0 || e.heldOf(req.ID, inventory.StateInTransit) != 3 {
		t.Errorf("giữ hàng sau từ chối: held %d, in_transit %d", e.heldOf(req.ID, inventory.StateHeld),
			e.heldOf(req.ID, inventory.StateInTransit))
	}
	e.mustOnEvent(req.ID, s1, order.StepDelivered, true)
	if g := e.get(req.ID); g.Status != order.StatusDelivered {
		t.Errorf("sau giao: %s", g.Status)
	}
	balanceIs(t, e.balance(v1), 3, 0, 0)
	balanceIs(t, e.balance(v2), 0, 0, 0)
}

// Đơn cancel_requested đã lấy hàng mà hãng lại báo xác nhận hủy (mâu thuẫn) → ErrInvalidTransition, không nhả, đơn
// giữ cancel_requested (M3 mở anomaly cancel_conflict).
func TestOnShipmentEvent_PickedUpWhileCancelRequestedThenCarrierConfirms(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	req, s1, v1, _ := e.requestedDH01()
	e.mustOnEvent(req.ID, s1, order.StepPickedUp, true)
	if _, err := e.carrierCancel(req.ID, s1, true); !errors.Is(err, order.ErrInvalidTransition) {
		t.Errorf("hãng xác nhận hủy khi hàng đã lấy: %v; muốn ErrInvalidTransition", err)
	}
	if g := e.get(req.ID); g.Status != order.StatusCancelRequested || g.Version != req.Version {
		t.Errorf("đơn bị đổi: %s, %d", g.Status, g.Version)
	}
	balanceIs(t, e.balance(v1), 5, 0, 2)
}

// Hãng giao luôn khi đơn còn cancel_requested (chưa báo lấy hàng): hàng đã tới khách nên việc hủy hết nghĩa →
// cancel_requested → confirmed (lý do carrier_rejected_cancel, bỏ lý do hủy, order.confirmed seq v+1) → delivered
// (order.delivered seq v+2), held → in_transit → consumed, một transaction; gọi lại → false.
func TestOnShipmentEvent_DeliveredWhileCancelRequested(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	req, s1, v1, v2 := e.requestedDH01()
	e.mustOnEvent(req.ID, s1, order.StepDelivered, true)
	got := e.get(req.ID)
	if got.Status != order.StatusDelivered || got.CancelReason != nil || got.CancelNote != nil ||
		got.Version != req.Version+2 {
		t.Fatalf("sau giao: %s, lý do %v, version %d (trước %d)", got.Status, got.CancelReason, got.Version,
			req.Version)
	}
	balanceIs(t, e.balance(v1), 3, 0, 0)
	balanceIs(t, e.balance(v2), 0, 0, 0)
	if n := e.moves(req.ID, "cancel_requested", "confirmed", inventory.ActorCarrier); n != 1 {
		t.Errorf("lịch sử cancel_requested → confirmed của hãng: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM ord.order_status_history WHERE order_id = $1
		AND to_status = 'confirmed' AND reason = 'carrier_rejected_cancel'`, req.ID); n != 1 {
		t.Errorf("lý do carrier_rejected_cancel ở lịch sử: %d", n)
	}
	if n := e.moves(req.ID, "confirmed", "delivered", inventory.ActorCarrier); n != 1 {
		t.Errorf("lịch sử confirmed → delivered của hãng: %d", n)
	}
	if n := e.events(req.ID, "order.confirmed", req.Version+1); n != 1 {
		t.Errorf("order.confirmed seq %d: %d", req.Version+1, n)
	}
	if n := e.events(req.ID, order.TopicOrderDelivered, req.Version+2); n != 1 {
		t.Errorf("order.delivered seq %d: %d", req.Version+2, n)
	}
	e.mustOnEvent(req.ID, s1, order.StepDelivered, false)
	balanceIs(t, e.balance(v1), 3, 0, 0)
}

// Nhân viên yêu cầu hủy SAU khi hãng đã lấy hàng (đơn cancel_requested, hàng in_transit): tin lấy hàng lặp lại →
// false, đơn giữ cancel_requested; giao xong → delivered, trừ tồn đúng một lần.
func TestOnShipmentEvent_CancelRequestedAfterPickup(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	req, s1, v1 := e.requestedAfterPickup()
	o := req
	e.mustOnEvent(o.ID, s1, order.StepPickedUp, false)
	if g := e.get(o.ID); g.Status != order.StatusCancelRequested || g.Version != req.Version {
		t.Errorf("tin lấy hàng lặp lại đổi đơn: %s, %d", g.Status, g.Version)
	}
	e.mustOnEvent(o.ID, s1, order.StepDelivered, true)
	if g := e.get(o.ID); g.Status != order.StatusDelivered || g.Version != req.Version+2 {
		t.Errorf("sau giao: %s, %d", g.Status, g.Version)
	}
	balanceIs(t, e.balance(v1), 3, 0, 0)
}

// B-02: DetachShipment trên đơn cancel_requested (lệnh tạo vận đơn chết, hãng chắc chắn không có kiện) → như hãng xác
// nhận hủy: cancelled, gỡ vận đơn, nhả held, giữ lý do, order.cancelled, lịch sử của người gọi; gỡ lần hai và
// OnShipmentCancelConfirmed gọi sau → false, không đổi gì.
func TestDetachShipment_CancelRequestedCancelsOrder(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	req, s1, v1, v2 := e.requestedDH01()
	applied, err := e.detachBy(e.seller, req.ID, s1)
	if err != nil || !applied {
		t.Fatalf("gỡ vận đơn của đơn cancel_requested: %v, %v", applied, err)
	}
	got := e.get(req.ID)
	if got.Status != order.StatusCancelled || got.CurrentShipmentID != nil || got.CancelledAt == nil ||
		got.CancelReason == nil || *got.CancelReason != order.CancelCustomerChangedMind || got.Version != req.Version+1 {
		t.Fatalf("sau gỡ: %+v", got)
	}
	balanceIs(t, e.balance(v1), 5, 0, 0)
	balanceIs(t, e.balance(v2), 1, 0, 0)
	if n := e.events(req.ID, "order.cancelled", got.Version); n != 1 {
		t.Errorf("order.cancelled seq %d: %d", got.Version, n)
	}
	if n := e.moves(req.ID, "cancel_requested", "cancelled", inventory.ActorStaff); n != 1 {
		t.Errorf("lịch sử cancel_requested → cancelled: %d", n)
	}
	if applied, err := e.detachBy(e.seller, req.ID, s1); err != nil || applied {
		t.Errorf("gỡ lần hai: %v, %v", applied, err)
	}
	if applied, err := e.carrierCancel(req.ID, s1, true); err != nil || applied {
		t.Errorf("hãng xác nhận hủy sau khi đã gỡ: %v, %v", applied, err)
	}
	if g := e.get(req.ID); g.Version != got.Version {
		t.Errorf("gọi lại đổi đơn: %d → %d", got.Version, g.Version)
	}
}

// Thứ tự ngược (M3 gọi OnShipmentCancelConfirmed trước rồi DetachShipment) → cùng kết quả, DetachShipment gọi sau →
// false.
func TestDetachShipment_AfterCarrierConfirmsCancel(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	req, s1, _, _ := e.requestedDH01()
	if applied, err := e.carrierCancel(req.ID, s1, true); err != nil || !applied {
		t.Fatalf("hãng xác nhận hủy: %v, %v", applied, err)
	}
	fin := e.get(req.ID)
	if applied, err := e.detachBy(e.seller, req.ID, s1); err != nil || applied || e.get(req.ID).Version != fin.Version {
		t.Errorf("gỡ sau khi đã hủy: %v, %v", applied, err)
	}
}

// DetachShipment trên đơn cancel_requested mà hàng đã in_transit → ErrInvalidTransition, không đổi gì (phải đi
// đường hoàn); trên đơn delivered còn vận đơn hiện hành → ErrInvalidTransition, không đổi gì.
func TestDetachShipment_RefusedStates(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	req, s1, v1 := e.requestedAfterPickup()
	o := req
	if _, err := e.detachBy(e.seller, o.ID, s1); !errors.Is(err, order.ErrInvalidTransition) {
		t.Errorf("gỡ vận đơn khi hàng đã rời kho: %v; muốn ErrInvalidTransition", err)
	}
	if g := e.get(o.ID); g.Status != order.StatusCancelRequested || g.CurrentShipmentID == nil ||
		g.Version != req.Version {
		t.Errorf("đơn bị đổi: %+v", g)
	}
	balanceIs(t, e.balance(v1), 5, 0, 2)

	d, s2 := e.attachedDH01()
	e.mustOnEvent(d.ID, s2, order.StepDelivered, true)
	del := e.get(d.ID)
	if del.CurrentShipmentID == nil {
		t.Fatal("đơn delivered không còn vận đơn hiện hành")
	}
	if _, err := e.detachBy(e.seller, d.ID, s2); !errors.Is(err, order.ErrInvalidTransition) {
		t.Errorf("gỡ vận đơn của đơn delivered: %v; muốn ErrInvalidTransition", err)
	}
	if g := e.get(d.ID); g.Version != del.Version || g.CurrentShipmentID == nil {
		t.Errorf("đơn delivered bị đổi: %+v", g)
	}
}
