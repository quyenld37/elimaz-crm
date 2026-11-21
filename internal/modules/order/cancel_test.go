package order_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/queries/orderview"
)

// cancelAt hủy đơn id ở version với lý do reason, ghi chú note (người làm e.actor, giờ e.now()).
func (e *orderEnv) cancelAt(id uuid.UUID, version int64, reason, note string) (order.Order, error) {
	var o order.Order
	err := e.app(func(tx pgx.Tx) error {
		var err error
		o, err = e.writer().Cancel(e.t.Context(), tx, e.seller, id, version, reason, note, e.actor, e.now())
		return err //nolint:wrapcheck // test
	})
	return o, err
}

// cancel hủy đơn o ở version hiện tại của nó, lý do customer_changed_mind.
func (e *orderEnv) cancel(o order.Order) (order.Order, error) {
	return e.cancelAt(o.ID, o.Version, order.CancelCustomerChangedMind, "")
}

func (e *orderEnv) mustCancel(o order.Order) order.Order {
	e.t.Helper()
	got, err := e.cancel(o)
	if err != nil {
		e.t.Fatalf("hủy đơn %s: %v", o.Code, err)
	}
	return got
}

// carrierCancel gọi OnShipmentCancelConfirmed (confirmed = true) hay OnShipmentCancelRejected của vận đơn s lên đơn o,
// người làm là hãng.
func (e *orderEnv) carrierCancel(o, s uuid.UUID, confirmed bool) (bool, error) {
	var applied bool
	err := e.app(func(tx pgx.Tx) error {
		var err error
		carrier := order.Actor{TenantID: e.tenant, Type: inventory.ActorCarrier}
		if confirmed {
			applied, err = e.writer().OnShipmentCancelConfirmed(e.t.Context(), tx, e.seller, o, s, carrier, e.now())
		} else {
			applied, err = e.writer().OnShipmentCancelRejected(e.t.Context(), tx, e.seller, o, s, carrier, e.now())
		}
		return err //nolint:wrapcheck // test
	})
	return applied, err
}

// usedCount là used_count của mã cid.
func (e *orderEnv) usedCount(cid uuid.UUID) int {
	e.t.Helper()
	return e.count(`SELECT used_count FROM promotion.coupons WHERE id = $1`, cid)
}

// redemption là trạng thái lượt mã của đơn o ("" khi không có).
func (e *orderEnv) redemption(o uuid.UUID) string {
	e.t.Helper()
	var s string
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		err := tx.QueryRow(e.t.Context(), `SELECT coalesce(max(status), '') FROM promotion.coupon_redemptions
			WHERE order_id = $1`, o).Scan(&s)
		return err //nolint:wrapcheck // test
	})
	return s
}

// heldOf là tổng số lượng giữ hàng ở trạng thái state của các dòng đơn o.
func (e *orderEnv) heldOf(o uuid.UUID, state string) int {
	e.t.Helper()
	return e.count(`SELECT coalesce(sum(r.quantity), 0)::int FROM inventory.stock_reservations r
		JOIN ord.order_items oi ON oi.id = r.order_item_id WHERE oi.order_id = $1 AND r.state = $2`, o, state)
}

// sale10Order tạo mã SALE10 và đơn DH01 dùng mã đó (V1, V2 tồn 10); trả đơn, mã của mã và đầu vào đã gửi.
func (e *orderEnv) sale10Order() (order.Order, uuid.UUID, order.CreateInput) {
	e.t.Helper()
	cid := e.coupon(sale10("SALE10", 0))
	in := e.dh01(e.variant(250000, 10), e.variant(180000, 10), "SALE10")
	return e.mustCreateM2(in).Order, cid, in
}

// attachedDH01 tạo đơn DH01 không mã (confirmedDH01) và gắn vận đơn mới S1; trả đơn (đọc lại) và S1.
func (e *orderEnv) attachedDH01() (order.Order, uuid.UUID) {
	e.t.Helper()
	o, _, _ := e.confirmedDH01()
	s1 := uuid.New()
	if err := e.attachBy(o.ID, s1); err != nil {
		e.t.Fatal(err)
	}
	return e.get(o.ID), s1
}

// TD01: đơn confirmed V2 ×1 (tồn 1) → hủy → cancelled, Available(V2) = 1 ngay, lý do, ghi chú, giờ hủy, lịch sử và
// sự kiện order.cancelled (seq = version); đơn khác V2 ×1 tạo ngay sau → được.
func TestCancel_TD01ReleasesStock(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	o, _, v2 := e.confirmedDH01()
	if b := e.balance(v2); b.Available != 0 {
		t.Fatalf("trước hủy Available(V2) = %d", b.Available)
	}
	got, err := e.cancelAt(o.ID, o.Version, order.CancelOutOfStock, "khách báo hết tiền")
	if err != nil {
		t.Fatalf("hủy: %v", err)
	}
	if got.Status != order.StatusCancelled || got.Version != o.Version+1 || got.CancelReason == nil ||
		*got.CancelReason != order.CancelOutOfStock || got.CancelNote == nil || *got.CancelNote != "khách báo hết tiền" ||
		got.CancelledAt == nil || !got.CancelledAt.Equal(e.now()) || got.CurrentShipmentID != nil {
		t.Fatalf("đơn sau hủy: %+v", got)
	}
	if b := e.balance(v2); b.Available != 1 || b.Held != 0 {
		t.Errorf("sau hủy V2: %+v, mong Available 1, held 0", b)
	}
	if n := e.heldOf(o.ID, inventory.StateHeld); n != 0 {
		t.Errorf("đơn còn giữ %d", n)
	}
	if n := e.count(`SELECT count(*) FROM ord.order_status_history WHERE order_id = $1 AND from_status = 'confirmed'
		AND to_status = 'cancelled' AND reason = 'out_of_stock' AND actor_id = $2`, o.ID, e.actor.UserID); n != 1 {
		t.Errorf("lịch sử confirmed → cancelled: %d", n)
	}
	if n := e.events(o.ID, "order.cancelled", got.Version); n != 1 {
		t.Errorf("order.cancelled seq %d: %d", got.Version, n)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE aggregate_id = $1 AND payload::text LIKE '%hết tiền%'`,
		o.ID.String()); n != 0 {
		t.Errorf("ghi chú hủy lọt vào sự kiện")
	}
	if _, err := e.createM2(e.m2Input(0, ln(v2, 1, 0))); err != nil {
		t.Errorf("đơn mới V2 ×1 sau hủy: %v", err)
	}
}

// Hủy đơn draft, new (kể cả chờ duyệt giảm giá) → cancelled, nhả hàng; draft bỏ hạn nháp.
func TestCancel_DraftAndNew(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	v := e.variant(100000, 5)
	for _, intent := range []string{order.IntentDraft, order.IntentNew} {
		in := e.m2Input(0, ln(v, 2, 0))
		in.Intent = intent
		o := e.mustCreateM2(in).Order
		got := e.mustCancel(o)
		if got.Status != order.StatusCancelled || got.DraftExpiresAt != nil || got.PendingReason != "" {
			t.Errorf("%s sau hủy: %+v", intent, got)
		}
		if n := e.heldOf(o.ID, inventory.StateHeld); n != 0 {
			t.Errorf("%s: còn giữ %d", intent, n)
		}
	}
	a := e.actor
	a.CanApproveDiscount = false
	pending := e.createAs(e.dh16(e.variant(1000000, 5), order.IntentConfirmed, 100001), a)
	if pending.PendingReason != order.PendingDiscountApproval {
		t.Fatalf("đơn chờ duyệt: %+v", pending)
	}
	if got := e.mustCancel(pending); got.Status != order.StatusCancelled || got.PendingReason != "" {
		t.Errorf("hủy đơn chờ duyệt: %+v", got)
	}
	if b := e.balance(v); b.Held != 0 || b.Available != 5 {
		t.Errorf("V sau hai lần hủy: %+v", b)
	}
}

// KM03: đơn dùng SALE10 (used_count 1) → hủy → used_count 0, lượt released; mã tắt release_on_cancel → used_count giữ
// 1, lượt vẫn active.
func TestCancel_KM03ReleasesCoupon(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	o, cid, _ := e.sale10Order()
	if e.usedCount(cid) != 1 {
		t.Fatalf("used_count trước hủy = %d", e.usedCount(cid))
	}
	e.mustCancel(o)
	if n := e.usedCount(cid); n != 0 {
		t.Errorf("used_count sau hủy = %d, mong 0", n)
	}
	if s := e.redemption(o.ID); s != "released" {
		t.Errorf("lượt của đơn: %q, mong released", s)
	}
	if n := e.count(`SELECT count(*) FROM promotion.coupon_redemptions WHERE order_id = $1
		AND release_reason = 'order_cancelled'`, o.ID); n != 1 {
		t.Errorf("lý do trả lượt: %d dòng order_cancelled", n)
	}

	keep := e.coupon(sale10("GIULUOT", 0))
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE promotion.coupons SET release_on_cancel = false WHERE id = $1`, keep)
		return err //nolint:wrapcheck // test
	})
	o2 := e.mustCreateM2(e.dh01(e.variant(250000, 10), e.variant(180000, 10), "GIULUOT")).Order
	e.mustCancel(o2)
	if n := e.usedCount(keep); n != 1 {
		t.Errorf("mã tắt release_on_cancel: used_count = %d, mong 1", n)
	}
	if s := e.redemption(o2.ID); s != "active" {
		t.Errorf("mã tắt release_on_cancel: lượt %q, mong active", s)
	}
}

// TD03 phía đơn: đơn có vận đơn S1 → hủy → cancel_requested (nhãn cancel_requested), giữ hàng, lượt mã không đổi, sự
// kiện order.cancel_requested; OnShipmentCancelConfirmed(S1) → cancelled, gỡ vận đơn, nhả hàng, trả lượt, giữ lý do.
func TestCancel_TD03RequestThenCarrierConfirms(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	o, cid, _ := e.sale10Order()
	s1 := uuid.New()
	if err := e.attachBy(o.ID, s1); err != nil {
		t.Fatal(err)
	}
	o = e.get(o.ID)
	got, err := e.cancelAt(o.ID, o.Version, order.CancelUnreachable, "gọi ba lần")
	if err != nil {
		t.Fatalf("hủy đơn đã gửi hãng: %v", err)
	}
	label := orderview.Label(orderview.Axes{
		Status: got.Status, PendingReason: got.PendingReason,
		PackingStatus: got.PackingStatus,
	})
	if got.Status != order.StatusCancelRequested || label != "cancel_requested" || got.CurrentShipmentID == nil ||
		*got.CurrentShipmentID != s1 || got.CancelledAt != nil || got.CancelReason == nil ||
		*got.CancelReason != order.CancelUnreachable {
		t.Fatalf("sau yêu cầu hủy: %+v (nhãn %s)", got, label)
	}
	if n := e.heldOf(o.ID, inventory.StateHeld); n != 3 {
		t.Errorf("giữ hàng sau yêu cầu hủy: %d, mong 3", n)
	}
	if e.usedCount(cid) != 1 || e.redemption(o.ID) != "active" {
		t.Errorf("lượt mã đổi khi mới yêu cầu hủy")
	}
	if n := e.events(o.ID, "order.cancel_requested", got.Version); n != 1 {
		t.Errorf("order.cancel_requested seq %d: %d", got.Version, n)
	}
	if applied, err := e.carrierCancel(o.ID, s1, true); err != nil || !applied {
		t.Fatalf("hãng xác nhận hủy: %v, %v", applied, err)
	}
	fin := e.get(o.ID)
	if fin.Status != order.StatusCancelled || fin.CurrentShipmentID != nil || fin.CancelledAt == nil ||
		fin.CancelReason == nil || *fin.CancelReason != order.CancelUnreachable || fin.Version != got.Version+1 {
		t.Errorf("sau hãng xác nhận: %+v", fin)
	}
	if e.heldOf(o.ID, inventory.StateHeld) != 0 || e.usedCount(cid) != 0 || e.redemption(o.ID) != "released" {
		t.Errorf("hãng xác nhận hủy mà chưa nhả hàng hay trả lượt")
	}
	if n := e.count(`SELECT count(*) FROM ord.order_status_history WHERE order_id = $1
		AND from_status = 'cancel_requested' AND to_status = 'cancelled' AND actor_type = 'carrier'`, o.ID); n != 1 {
		t.Errorf("lịch sử cancel_requested → cancelled của hãng: %d", n)
	}
	if n := e.events(o.ID, "order.cancelled", fin.Version); n != 1 {
		t.Errorf("order.cancelled seq %d: %d", fin.Version, n)
	}
	// Gọi lại (hãng báo hai lần): vận đơn không còn hiện hành → false, không đổi gì.
	if applied, err := e.carrierCancel(o.ID, s1, true); err != nil || applied {
		t.Errorf("hãng báo lần hai: %v, %v", applied, err)
	}
}

// TD03 trường hợp khác: hãng từ chối hủy → confirmed, vẫn vận đơn S1, giữ hàng, bỏ lý do hủy.
func TestCancel_CarrierRejects(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	o, s1 := e.attachedDH01()
	req := e.mustCancel(o)
	if applied, err := e.carrierCancel(o.ID, s1, false); err != nil || !applied {
		t.Fatalf("hãng từ chối hủy: %v, %v", applied, err)
	}
	got := e.get(o.ID)
	if got.Status != order.StatusConfirmed || got.CurrentShipmentID == nil || *got.CurrentShipmentID != s1 ||
		got.CancelReason != nil || got.CancelNote != nil || got.Version != req.Version+1 {
		t.Errorf("sau hãng từ chối: %+v", got)
	}
	if n := e.heldOf(o.ID, inventory.StateHeld); n != 3 {
		t.Errorf("giữ hàng sau từ chối: %d", n)
	}
	if n := e.events(o.ID, "order.confirmed", got.Version); n != 1 {
		t.Errorf("order.confirmed seq %d: %d", got.Version, n)
	}
	if applied, err := e.carrierCancel(o.ID, s1, false); err != nil || applied {
		t.Errorf("từ chối lần hai: %v, %v; mong false", applied, err)
	}
}

// Vận đơn cũ: hiện hành là S1, hãng báo về S0 → applied false, không đổi gì (cả xác nhận lẫn từ chối).
func TestCancel_StaleShipment(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	o, _ := e.attachedDH01()
	req := e.mustCancel(o)
	for _, confirmed := range []bool{true, false} {
		if applied, err := e.carrierCancel(o.ID, uuid.New(), confirmed); err != nil || applied {
			t.Errorf("vận đơn cũ (xác nhận=%v): %v, %v", confirmed, applied, err)
		}
	}
	if got := e.get(o.ID); got.Status != order.StatusCancelRequested || got.Version != req.Version {
		t.Errorf("vận đơn cũ đổi đơn: %+v", got)
	}
}

// Hàng đã in_transit mà hãng báo hủy thành công → ErrInvalidTransition, không nhả, đơn vẫn cancel_requested.
func TestCancel_InTransitCannotBeCancelled(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	o, v1, _ := e.confirmedDH01()
	s1 := uuid.New()
	if err := e.attachBy(o.ID, s1); err != nil {
		t.Fatal(err)
	}
	e.mustOnEvent(o.ID, s1, order.StepPickedUp, true)
	req := e.mustCancel(e.get(o.ID))
	if req.Status != order.StatusCancelRequested {
		t.Fatalf("hủy đơn đang giao: %+v", req)
	}
	before := e.balance(v1)
	if _, err := e.carrierCancel(o.ID, s1, true); !errors.Is(err, order.ErrInvalidTransition) {
		t.Errorf("hãng báo hủy khi hàng đã rời kho: %v; mong ErrInvalidTransition", err)
	}
	if got := e.get(o.ID); got.Status != order.StatusCancelRequested || got.CurrentShipmentID == nil {
		t.Errorf("đơn bị đổi: %+v", got)
	}
	if after := e.balance(v1); after != before || e.heldOf(o.ID, inventory.StateInTransit) != 3 {
		t.Errorf("tồn đổi: %+v → %+v", before, after)
	}
}

// Đơn confirmed có vận đơn mà không yêu cầu hủy (chỉ hủy vận đơn để gửi lại): hãng xác nhận hủy → như DetachShipment:
// vẫn confirmed, gỡ vận đơn, giữ hàng.
func TestCancel_CarrierConfirmsOnConfirmedOrderDetaches(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	o, s1 := e.attachedDH01()
	if applied, err := e.carrierCancel(o.ID, s1, true); err != nil || !applied {
		t.Fatalf("hủy vận đơn của đơn confirmed: %v, %v", applied, err)
	}
	got := e.get(o.ID)
	if got.Status != order.StatusConfirmed || got.CurrentShipmentID != nil || e.heldOf(o.ID, inventory.StateHeld) != 3 {
		t.Errorf("sau hủy vận đơn: %+v", got)
	}
	if n := e.events(o.ID, order.TopicOrderUpdated, got.Version); n != 1 {
		t.Errorf("order.updated seq %d: %d", got.Version, n)
	}
}

// Từ chối: đơn delivered, đơn đã hủy, đơn đang yêu cầu hủy → ErrInvalidTransition; version cũ → ErrVersionConflict;
// lý do ngoài Q18, ghi chú quá 500 ký tự → ErrInvalidInput; người bán khác, đơn không có → ErrNotFound. Không đổi gì.
func TestCancel_Refusals(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	delivered, _, _ := e.confirmedDH01()
	s := uuid.New()
	if err := e.attachBy(delivered.ID, s); err != nil {
		t.Fatal(err)
	}
	e.mustOnEvent(delivered.ID, s, order.StepDelivered, true)
	d := e.get(delivered.ID)
	if _, err := e.cancel(d); !errors.Is(err, order.ErrInvalidTransition) {
		t.Errorf("hủy đơn delivered: %v; mong ErrInvalidTransition", err)
	}
	if got := e.get(d.ID); got.Status != order.StatusDelivered || got.Version != d.Version {
		t.Errorf("đơn delivered bị đổi: %+v", got)
	}

	o, _, _ := e.confirmedDH01()
	if _, err := e.cancelAt(o.ID, o.Version-1, order.CancelOther, ""); !errors.Is(err, order.ErrVersionConflict) {
		t.Errorf("version cũ: %v; mong ErrVersionConflict", err)
	}
	for _, c := range []struct{ reason, note string }{
		{"khong_co", ""}, {"", ""}, {order.CancelOther, strings.Repeat("ă", 501)},
	} {
		if _, err := e.cancelAt(o.ID, o.Version, c.reason, c.note); !errors.Is(err, order.ErrInvalidInput) {
			t.Errorf("lý do %q, ghi chú %d ký tự: %v; mong ErrInvalidInput", c.reason, len(c.note), err)
		}
	}
	if _, err := e.cancelAt(uuid.New(), 1, order.CancelOther, ""); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("đơn không có: %v", err)
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := e.writer().Cancel(t.Context(), tx, e.partner, o.ID, o.Version, order.CancelOther, "", e.actor, e.now())
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("người bán khác: %v; mong ErrNotFound", err)
	}
	if got := e.get(o.ID); got.Status != order.StatusConfirmed || got.Version != o.Version {
		t.Errorf("đơn bị đổi sau các lần từ chối: %+v", got)
	}

	cancelled := e.mustCancel(o)
	if _, err := e.cancel(cancelled); !errors.Is(err, order.ErrInvalidTransition) {
		t.Errorf("hủy lần hai: %v; mong ErrInvalidTransition", err)
	}
	o2, _ := e.attachedDH01()
	req := e.mustCancel(o2)
	if _, err := e.cancel(req); !errors.Is(err, order.ErrInvalidTransition) {
		t.Errorf("hủy đơn đang yêu cầu hủy: %v; mong ErrInvalidTransition", err)
	}
}

// Việc để lại của Task 74: gửi lại cùng khóa nghiệp vụ, cùng mã sau khi đơn gốc đã hủy và lượt mã đã trả → trả đơn
// cũ (đã hủy), không dùng mã lần hai; khác mã → ErrDuplicateOrder.
func TestCancel_ReplayAfterCancel(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	first, cid, in := e.sale10Order()
	e.coupon(sale10("SALE20", 0))
	e.mustCancel(first)
	in.CouponCode = "sale10"
	res, err := e.createM2(in)
	if err != nil || !res.Replayed || res.Order.ID != first.ID || res.Order.Status != order.StatusCancelled {
		t.Fatalf("gửi lại sau hủy: %+v, %v", res, err)
	}
	if n := e.usedCount(cid); n != 0 {
		t.Errorf("used_count = %d, mong 0", n)
	}
	in.CouponCode = "SALE20"
	if _, err := e.createM2(in); !errors.Is(err, order.ErrDuplicateOrder) {
		t.Errorf("gửi lại khác mã sau hủy: %v; mong ErrDuplicateOrder", err)
	}
	plain := e.m2Input(0, ln(first.Items[0].VariantID, 1, 0))
	o := e.mustCreateM2(plain).Order
	e.mustCancel(o)
	if res, err := e.createM2(plain); err != nil || !res.Replayed {
		t.Errorf("đơn không mã gửi lại sau hủy: %+v, %v", res, err)
	}
}
