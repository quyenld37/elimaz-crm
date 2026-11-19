package order_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/testkit"
)

// Helper riêng của Task M2-19 (không sửa orderEnv dùng chung).

// change chạy fn với Writer mới trong một transaction crm_app của e.
func (e *orderEnv) change(fn func(tx pgx.Tx, w order.Writer) (order.Order, error)) (order.Order, error) {
	var o order.Order
	err := e.app(func(tx pgx.Tx) error {
		var err error
		o, err = fn(tx, e.writer())
		return err
	})
	return o, err
}

func (e *orderEnv) confirm(id uuid.UUID, version int64, a order.Actor, at time.Time) (order.Order, error) {
	return e.change(func(tx pgx.Tx, w order.Writer) (order.Order, error) {
		return w.Confirm(e.t.Context(), tx, e.seller, id, version, a, at)
	})
}

func (e *orderEnv) approve(id uuid.UUID, version int64, a order.Actor, at time.Time) (order.Order, error) {
	return e.change(func(tx pgx.Tx, w order.Writer) (order.Order, error) {
		return w.ApproveDiscount(e.t.Context(), tx, e.seller, id, version, a, at)
	})
}

func (e *orderEnv) setPending(id uuid.UUID, version int64, reason string, at time.Time) (order.Order, error) {
	return e.change(func(tx pgx.Tx, w order.Writer) (order.Order, error) {
		return w.SetPending(e.t.Context(), tx, e.seller, id, version, reason, e.actor, at)
	})
}

// manager là người có quyền order.discount.approve (lớp HTTP tính từ grant).
func (e *orderEnv) manager() order.Actor {
	e.t.Helper()
	return order.Actor{
		TenantID: e.tenant, UserID: e.staff.User(e.t, e.tenant, "quanly-m219@elimaz.test", "active"),
		CanApproveDiscount: true,
	}
}

// createIntent tạo đơn ý định intent, địa chỉ addr (nil: không địa chỉ), giảm cấp đơn discount trên một biến thể
// 1.000.000 đồng (giảm 100.001 vượt ngưỡng mặc định 10%: đơn của nhân viên chờ duyệt).
func (e *orderEnv) createIntent(intent string, addr *order.ShipAddress, discount int64) order.Order {
	e.t.Helper()
	v := e.variant(1000000, 5)
	in := e.m2Input(0, ln(v, 1, 0))
	in.SellerID, in.Intent, in.ShipAddress, in.OrderDiscount = e.seller, intent, addr, discount
	return e.mustCreateM2(in).Order
}

func (e *orderEnv) history(id uuid.UUID, cond string, args ...any) int {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM ord.order_status_history WHERE order_id = $1 AND `+cond,
		append([]any{id}, args...)...)
}

func (e *orderEnv) eventCount(id uuid.UUID, topic string, seq int64) int {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM platform.domain_events WHERE aggregate_id = $1 AND topic = $2 AND seq = $3`,
		id.String(), topic, seq)
}

// new → confirm: confirmed, version + 1, confirmed_at/by, một dòng lịch sử new → confirmed, sự kiện order.confirmed
// với seq = version; version cũ → ErrVersionConflict, không đổi gì.
func TestConfirm_New(t *testing.T) {
	base := newM2Env(t)
	testkit.ForEachSeller(t, []uuid.UUID{base.shop.Seller, base.shop.Partner}, func(t *testing.T, s uuid.UUID) {
		e := *base
		e.t, e.seller = t, s
		o := e.createIntent(order.IntentNew, fullAddress(), 0)
		if o.Status != order.StatusNew || o.Version != 1 {
			t.Fatalf("đơn mới: %+v", o)
		}
		if _, err := e.confirm(o.ID, 0, e.actor, e.now()); !errors.Is(err, order.ErrVersionConflict) {
			t.Errorf("version cũ: %v, mong ErrVersionConflict", err)
		}
		at := e.now().Add(time.Hour)
		got, err := e.confirm(o.ID, 1, e.actor, at)
		if err != nil {
			t.Fatalf("Confirm: %v", err)
		}
		if got.Status != order.StatusConfirmed || got.Version != 2 || got.ConfirmedAt == nil ||
			!got.ConfirmedAt.Equal(at) || got.ConfirmedBy == nil || *got.ConfirmedBy != e.actor.UserID ||
			len(got.Items) != 1 {
			t.Errorf("sau xác nhận: %+v", got)
		}
		if n := e.history(o.ID, `from_status = 'new' AND to_status = 'confirmed' AND actor_id = $2`, e.actor.UserID); n != 1 {
			t.Errorf("lịch sử new → confirmed: %d", n)
		}
		if n := e.eventCount(o.ID, order.TopicOrderConfirmed, 2); n != 1 {
			t.Errorf("order.confirmed seq 2: %d", n)
		}
		if _, err := e.confirm(o.ID, 2, e.actor, at); !errors.Is(err, order.ErrInvalidTransition) {
			t.Errorf("xác nhận lần hai: %v, mong ErrInvalidTransition", err)
		}
		if _, err := e.confirm(uuid.New(), 1, e.actor, at); !errors.Is(err, order.ErrNotFound) {
			t.Errorf("đơn không có: %v, mong ErrNotFound", err)
		}
	})
}

// Đơn của người bán khác (cùng tenant) không thấy được: ErrNotFound (ADR-29).
func TestConfirm_OtherSeller(t *testing.T) {
	e := newM2Env(t)
	o := e.createIntent(order.IntentNew, fullAddress(), 0)
	other := *e
	other.seller = e.partner
	if _, err := other.confirm(o.ID, 1, e.actor, e.now()); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("người bán khác: %v, mong ErrNotFound", err)
	}
}

// DH16: đơn chờ duyệt; nhân viên confirm → ErrNeedsApproval, approve → ErrDiscountNeedsApproval; quản lý approve →
// confirmed, ghi người duyệt, lịch sử có lý do discount_approved (new → new) rồi new → confirmed, sự kiện seq = version.
func TestApproveDiscount_New(t *testing.T) {
	e := newM2Env(t)
	o := e.createIntent(order.IntentConfirmed, fullAddress(), 100001)
	if o.Status != order.StatusNew || o.PendingReason != order.PendingDiscountApproval {
		t.Fatalf("đơn chờ duyệt: %+v", o)
	}
	if _, err := e.confirm(o.ID, 1, e.actor, e.now()); !errors.Is(err, order.ErrNeedsApproval) {
		t.Errorf("nhân viên xác nhận: %v, mong ErrNeedsApproval", err)
	}
	if _, err := e.approve(o.ID, 1, e.actor, e.now()); !errors.Is(err, order.ErrDiscountNeedsApproval) {
		t.Errorf("nhân viên duyệt: %v, mong ErrDiscountNeedsApproval", err)
	}
	if _, err := e.setPending(o.ID, 1, order.PendingAwaitingCustomer, e.now()); !errors.Is(err, order.ErrNeedsApproval) {
		t.Errorf("đặt chờ khách trên đơn chờ duyệt: %v, mong ErrNeedsApproval", err)
	}
	mgr := e.manager()
	at := e.now().Add(time.Hour)
	got, err := e.approve(o.ID, 1, mgr, at)
	if err != nil {
		t.Fatalf("quản lý duyệt: %v", err)
	}
	if got.Status != order.StatusConfirmed || got.PendingReason != "" || got.Version != 3 ||
		got.DiscountApprovedBy == nil || *got.DiscountApprovedBy != mgr.UserID || got.DiscountApprovedAt == nil ||
		!got.DiscountApprovedAt.Equal(at) || got.ConfirmedBy == nil || *got.ConfirmedBy != mgr.UserID {
		t.Errorf("sau duyệt: %+v", got)
	}
	if n := e.history(o.ID, `from_status = 'new' AND to_status = 'new' AND from_pending = 'discount_approval'
		AND to_pending = '' AND reason = 'discount_approved' AND actor_id = $2`, mgr.UserID); n != 1 {
		t.Errorf("lịch sử duyệt: %d", n)
	}
	if n := e.history(o.ID, `from_status = 'new' AND to_status = 'confirmed'`); n != 1 {
		t.Errorf("lịch sử new → confirmed: %d", n)
	}
	if e.eventCount(o.ID, "order.new", 2) != 1 || e.eventCount(o.ID, order.TopicOrderConfirmed, 3) != 1 {
		t.Errorf("sự kiện order.new seq 2, order.confirmed seq 3 thiếu")
	}
	if _, err := e.approve(o.ID, 3, mgr, at); !errors.Is(err, order.ErrInvalidTransition) {
		t.Errorf("duyệt đơn đã xác nhận: %v, mong ErrInvalidTransition", err)
	}
}

// Người có quyền duyệt bấm confirm trên đơn chờ duyệt: ghi duyệt rồi xác nhận trong cùng lần.
func TestConfirm_ApproverConfirmsPending(t *testing.T) {
	e := newM2Env(t)
	o := e.createIntent(order.IntentNew, fullAddress(), 100001)
	mgr := e.manager()
	got, err := e.confirm(o.ID, 1, mgr, e.now())
	if err != nil {
		t.Fatalf("quản lý xác nhận: %v", err)
	}
	if got.Status != order.StatusConfirmed || got.DiscountApprovedBy == nil || *got.DiscountApprovedBy != mgr.UserID {
		t.Errorf("sau xác nhận: %+v", got)
	}
	if n := e.history(o.ID, `reason = 'discount_approved'`); n != 1 {
		t.Errorf("lịch sử duyệt: %d", n)
	}
}

// Đơn không chờ duyệt: approve → ErrInvalidTransition kể cả với quản lý.
func TestApproveDiscount_NotPending(t *testing.T) {
	e := newM2Env(t)
	o := e.createIntent(order.IntentNew, fullAddress(), 0)
	if _, err := e.approve(o.ID, 1, e.manager(), e.now()); !errors.Is(err, order.ErrInvalidTransition) {
		t.Errorf("duyệt đơn không chờ duyệt: %v, mong ErrInvalidTransition", err)
	}
}

// Chốt đơn nháp: thiếu phường → ErrAddressIncomplete; địa chỉ đủ → confirmed, bỏ hạn nháp. Nháp chờ duyệt: quản lý
// duyệt → vẫn draft, bỏ lý do, giữ hạn nháp; rồi nhân viên chốt được.
func TestConfirm_Draft(t *testing.T) {
	e := newM2Env(t)
	noWard := fullAddress()
	noWard.WardCode = ""
	d1 := e.createIntent(order.IntentDraft, noWard, 0)
	if _, err := e.confirm(d1.ID, 1, e.actor, e.now()); !errors.Is(err, order.ErrAddressIncomplete) {
		t.Errorf("nháp thiếu phường: %v, mong ErrAddressIncomplete", err)
	}
	d2 := e.createIntent(order.IntentDraft, fullAddress(), 0)
	if d2.DraftExpiresAt == nil {
		t.Fatalf("nháp không có hạn: %+v", d2)
	}
	got, err := e.confirm(d2.ID, 1, e.actor, e.now())
	if err != nil {
		t.Fatalf("chốt nháp: %v", err)
	}
	if got.Status != order.StatusConfirmed || got.DraftExpiresAt != nil || got.ConfirmedAt == nil {
		t.Errorf("sau chốt nháp: %+v", got)
	}
	if n := e.history(d2.ID, `from_status = 'draft' AND to_status = 'confirmed'`); n != 1 {
		t.Errorf("lịch sử draft → confirmed: %d", n)
	}

	d3 := e.createIntent(order.IntentDraft, fullAddress(), 100001)
	if d3.Status != order.StatusDraft || d3.PendingReason != order.PendingDiscountApproval {
		t.Fatalf("nháp chờ duyệt: %+v", d3)
	}
	if _, err := e.confirm(d3.ID, 1, e.actor, e.now()); !errors.Is(err, order.ErrNeedsApproval) {
		t.Errorf("chốt nháp chờ duyệt: %v, mong ErrNeedsApproval", err)
	}
	if _, err := e.setPending(d3.ID, 1, "", e.now()); !errors.Is(err, order.ErrInvalidTransition) {
		t.Errorf("SetPending trên nháp: %v, mong ErrInvalidTransition", err)
	}
	mgr := e.manager()
	a, err := e.approve(d3.ID, 1, mgr, e.now())
	if err != nil {
		t.Fatalf("duyệt nháp: %v", err)
	}
	if a.Status != order.StatusDraft || a.PendingReason != "" || a.Version != 2 || a.DraftExpiresAt == nil ||
		!a.DraftExpiresAt.Equal(*d3.DraftExpiresAt) || a.DiscountApprovedBy == nil {
		t.Errorf("sau duyệt nháp: %+v", a)
	}
	if e.eventCount(d3.ID, "order.draft", 2) != 1 {
		t.Errorf("thiếu order.draft seq 2")
	}
	if c, err := e.confirm(d3.ID, 2, e.actor, e.now()); err != nil || c.Status != order.StatusConfirmed {
		t.Errorf("chốt nháp đã duyệt: %v %+v", err, c)
	}
}

// SetPending: new → chờ khách (lý do đổi, vẫn new); confirm → confirmed; đặt lý do trên đơn confirmed →
// ErrInvalidTransition; lý do lạ → ErrInvalidInput; đặt đúng lý do đang có → không ghi gì.
func TestSetPending(t *testing.T) {
	e := newM2Env(t)
	o := e.createIntent(order.IntentNew, fullAddress(), 0)
	if _, err := e.setPending(o.ID, 1, "discount_approval", e.now()); !errors.Is(err, order.ErrInvalidInput) {
		t.Errorf("lý do discount_approval: %v, mong ErrInvalidInput", err)
	}
	p, err := e.setPending(o.ID, 1, order.PendingAwaitingCustomer, e.now())
	if err != nil || p.Status != order.StatusNew || p.PendingReason != order.PendingAwaitingCustomer || p.Version != 2 {
		t.Fatalf("chờ khách: %v %+v", err, p)
	}
	if n := e.history(o.ID, `from_status = 'new' AND to_status = 'new' AND to_pending = 'awaiting_customer'`); n != 1 {
		t.Errorf("lịch sử chờ khách: %d", n)
	}
	same, err := e.setPending(o.ID, 2, order.PendingAwaitingCustomer, e.now())
	if err != nil || same.Version != 2 {
		t.Errorf("đặt lại cùng lý do: %v %+v", err, same)
	}
	if _, err := e.setPending(o.ID, 1, order.PendingOutOfStock, e.now()); !errors.Is(err, order.ErrVersionConflict) {
		t.Errorf("version cũ: %v, mong ErrVersionConflict", err)
	}
	c, err := e.confirm(o.ID, 2, e.actor, e.now())
	if err != nil || c.Status != order.StatusConfirmed || c.PendingReason != "" {
		t.Fatalf("xác nhận sau chờ khách: %v %+v", err, c)
	}
	if _, err := e.setPending(o.ID, c.Version, order.PendingAwaitingCustomer, e.now()); !errors.Is(err, order.ErrInvalidTransition) {
		t.Errorf("chờ khách trên đơn confirmed: %v, mong ErrInvalidTransition", err)
	}
}

// Đơn đã hủy: confirm → ErrInvalidTransition.
func TestConfirm_Cancelled(t *testing.T) {
	e := newM2Env(t)
	o := e.createIntent(order.IntentNew, fullAddress(), 0)
	if err := e.transition(&o, order.StatusCancelled, "", e.now()); err != nil {
		t.Fatalf("hủy: %v", err)
	}
	if _, err := e.confirm(o.ID, o.Version, e.actor, e.now()); !errors.Is(err, order.ErrInvalidTransition) {
		t.Errorf("xác nhận đơn hủy: %v, mong ErrInvalidTransition", err)
	}
}
