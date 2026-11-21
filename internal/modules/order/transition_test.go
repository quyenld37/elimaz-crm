package order_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
)

// transition chạy order.ApplyTransition trên đơn o trong một transaction crm_app.
func (e *orderEnv) transition(o *order.Order, to, pending string, at time.Time) error {
	return e.app(func(tx pgx.Tx) error {
		return order.ApplyTransition(e.t.Context(), tx, e.writer(), o, to, pending, "", e.actor, at)
	})
}

// draftOrder tạo một đơn nháp một dòng (biến thể mới, giá 1000, tồn 5).
func (e *orderEnv) draftOrder() order.Order {
	e.t.Helper()
	in := e.m2Input(0, ln(e.variant(1000, 5), 1, 0))
	in.Intent = order.IntentDraft
	return e.mustCreateM2(in).Order
}

// applyTransition: draft → confirmed đổi trạng thái, version + 1, bỏ hạn nháp, ghi confirmed_at, một dòng lịch sử
// (from draft), sự kiện order.confirmed với seq = version mới.
func TestApplyTransition_DraftToConfirmed(t *testing.T) {
	e := newM2Env(t)
	o := e.draftOrder()
	at := e.now().Add(time.Hour)
	if err := e.transition(&o, order.StatusConfirmed, "", at); err != nil {
		t.Fatalf("draft → confirmed: %v", err)
	}
	if o.Status != order.StatusConfirmed || o.Version != 2 {
		t.Errorf("đơn sau chuyển: %+v", o)
	}
	got := e.get(o.ID)
	if got.Status != order.StatusConfirmed || got.Version != 2 || got.DraftExpiresAt != nil || got.ConfirmedAt == nil ||
		!got.ConfirmedAt.Equal(at) || !got.UpdatedAt.Equal(at) {
		t.Errorf("đọc lại: %+v", got)
	}
	if n := e.count(`SELECT count(*) FROM ord.order_status_history WHERE order_id = $1 AND from_status = 'draft'
		AND to_status = 'confirmed' AND actor_id = $2 AND occurred_at = $3`, o.ID, e.actor.UserID, at); n != 1 {
		t.Errorf("lịch sử draft → confirmed: %d dòng", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE aggregate_id = $1 AND topic = 'order.confirmed'
		AND seq = 2`, o.ID.String()); n != 1 {
		t.Errorf("sự kiện order.confirmed seq 2: %d", n)
	}
}

// applyTransition từ chối: version cũ → ErrVersionConflict; cặp không có trong sơ đồ → ErrInvalidTransition; nháp
// thiếu địa chỉ → ErrAddressIncomplete. Không đổi gì.
func TestApplyTransition_Refusals(t *testing.T) {
	e := newM2Env(t)
	v := e.variant(1000, 5)
	in := e.m2Input(0, ln(v, 1, 0))
	in.Intent, in.ShipAddress = order.IntentDraft, nil
	o := e.mustCreateM2(in).Order

	stale := o
	stale.Version = 0
	if err := e.transition(&stale, order.StatusCancelled, "", e.now()); !errors.Is(err, order.ErrVersionConflict) {
		t.Errorf("version cũ: %v, mong ErrVersionConflict", err)
	}
	cp := o
	if err := e.transition(&cp, order.StatusNew, "", e.now()); !errors.Is(err, order.ErrInvalidTransition) {
		t.Errorf("draft → new: %v, mong ErrInvalidTransition", err)
	}
	cp = o
	if err := e.transition(&cp, order.StatusConfirmed, "", e.now()); !errors.Is(err, order.ErrAddressIncomplete) {
		t.Errorf("nháp thiếu địa chỉ → confirmed: %v, mong ErrAddressIncomplete", err)
	}
	if got := e.get(o.ID); got.Status != order.StatusDraft || got.Version != 1 {
		t.Errorf("đơn bị đổi: %+v", got)
	}
	if n := e.count(`SELECT count(*) FROM ord.order_status_history WHERE order_id = $1`, o.ID); n != 1 {
		t.Errorf("%d dòng lịch sử, mong 1", n)
	}
	cp = o
	if err := e.transition(&cp, order.StatusCancelled, "", e.now()); err != nil || cp.Version != 2 {
		t.Errorf("draft → cancelled: %v, version %d", err, cp.Version)
	}
	if got := e.get(o.ID); got.Status != order.StatusCancelled || got.DraftExpiresAt != nil || got.CancelledAt == nil {
		t.Errorf("sau hủy: %+v", got)
	}
}

// Mutation testing (Task 403): đổi trạng thái xong phát báo hiệu order cho đơn đó (sau sự kiện).
func TestApplyTransition_Signals(t *testing.T) {
	e := newM2Env(t)
	o := e.draftOrder()
	bus := &realtimetest.RecordingBus{}
	w := e.writer()
	w.Realtime = bus
	if err := e.app(func(tx pgx.Tx) error {
		return order.ApplyTransition(t.Context(), tx, w, &o, order.StatusConfirmed, "", "", e.actor, e.now())
	}); err != nil {
		t.Fatal(err)
	}
	sig := bus.Signals()
	if len(sig) != 1 || sig[0].Entity.ID != o.ID.String() || sig[0].TenantID != e.actor.TenantID {
		t.Fatalf("báo hiệu: %+v", sig)
	}
}
