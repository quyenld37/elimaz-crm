package order

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order/internal/state"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
)

// Xác nhận đơn, duyệt giảm giá, chờ khách, chốt đơn nháp (kế hoạch M2, Task M2-19). Mọi hàm: khóa đơn (ADR-46 nhóm 7),
// so version người gọi gửi lên, đổi trạng thái chỉ qua applyTransition, trong tx của bên gọi (WriteTx); bên gọi ghi
// audit bằng Order.AuditFields.

// Lý do chờ nhân viên đặt được bằng SetPending (CHECK của ord.orders.pending_reason).
const (
	PendingAwaitingCustomer = "awaiting_customer"
	PendingOutOfStock       = "out_of_stock"
)

// reasonDiscountApproved là lý do của dòng lịch sử bỏ chờ duyệt giảm giá (CHECK ^[a-z][a-z0-9_]*$).
const reasonDiscountApproved = "discount_approved"

// Confirm xác nhận đơn new (lý do chờ rỗng, awaiting_customer, out_of_stock) hoặc chốt đơn draft (cần địa chỉ đủ:
// ErrAddressIncomplete) sang confirmed; ghi confirmed_at, confirmed_by, bỏ hạn nháp. Đơn chờ duyệt giảm giá:
// ErrNeedsApproval, trừ khi a có quyền duyệt (a.CanApproveDiscount) — khi đó ghi duyệt (như ApproveDiscount) rồi xác
// nhận trong cùng lần. Trạng thái khác → ErrInvalidTransition; version khác → ErrVersionConflict; không có đơn của
// người bán sellerID → ErrNotFound. Trả đơn đọc lại như Get.
func (w Writer) Confirm(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID, version int64, a Actor, now time.Time,
) (Order, error) {
	return changeLocked(ctx, tx, sellerID, id, version, func(o *Order) error {
		if o.Status != StatusNew && o.Status != StatusDraft {
			return fmt.Errorf("%w: xác nhận đơn %s", ErrInvalidTransition, o.Status)
		}
		if o.PendingReason == PendingDiscountApproval && a.CanApproveDiscount {
			if err := w.approve(ctx, tx, o, a, now.UTC()); err != nil {
				return err
			}
		}
		return w.applyTransition(ctx, tx, o, state.Confirmed, "", "", a, now.UTC())
	})
}

// ApproveDiscount duyệt giảm giá vượt ngưỡng (Q3) của đơn chờ duyệt (lý do discount_approval): đơn new sang confirmed;
// đơn draft bỏ lý do, vẫn draft (còn chờ địa chỉ, giữ hạn nháp). Ghi discount_approved_by/at là a, lịch sử lý do
// discount_approved. a không có quyền duyệt → ErrDiscountNeedsApproval; đơn không chờ duyệt → ErrInvalidTransition;
// version khác → ErrVersionConflict; không có đơn → ErrNotFound.
func (w Writer) ApproveDiscount(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID, version int64, a Actor,
	now time.Time,
) (Order, error) {
	if !a.CanApproveDiscount {
		return Order{}, ErrDiscountNeedsApproval
	}
	return changeLocked(ctx, tx, sellerID, id, version, func(o *Order) error {
		if o.PendingReason != PendingDiscountApproval || (o.Status != StatusNew && o.Status != StatusDraft) {
			return fmt.Errorf("%w: đơn %s không chờ duyệt giảm giá", ErrInvalidTransition, o.Status)
		}
		if err := w.approve(ctx, tx, o, a, now.UTC()); err != nil || o.Status != StatusNew {
			return err
		}
		return w.applyTransition(ctx, tx, o, state.Confirmed, "", "", a, now.UTC())
	})
}

// SetPending đặt hoặc bỏ (reason rỗng) lý do chờ awaiting_customer, out_of_stock của đơn new. Lý do khác →
// ErrInvalidInput; đơn không ở new → ErrInvalidTransition; đơn chờ duyệt giảm giá → ErrNeedsApproval (lý do này chỉ bỏ
// được bằng duyệt); version khác → ErrVersionConflict. Lý do đang đúng là reason → trả đơn, không ghi gì.
func (w Writer) SetPending(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID, version int64, reason string,
	a Actor, now time.Time,
) (Order, error) {
	switch reason {
	case "", PendingAwaitingCustomer, PendingOutOfStock:
	default:
		return Order{}, fmt.Errorf("%w: lý do chờ %q", ErrInvalidInput, reason)
	}
	return changeLocked(ctx, tx, sellerID, id, version, func(o *Order) error {
		switch {
		case o.Status != StatusNew:
			return fmt.Errorf("%w: đặt lý do chờ cho đơn %s", ErrInvalidTransition, o.Status)
		case o.PendingReason == PendingDiscountApproval:
			return ErrNeedsApproval
		case o.PendingReason == reason:
			return nil
		}
		return w.applyTransition(ctx, tx, o, state.New, reason, "", a, now.UTC())
	})
}

// approve bỏ lý do discount_approval của đơn o (new → new, draft → draft; lịch sử lý do discount_approved) rồi ghi người
// duyệt a lên đơn (version vừa tăng).
func (w Writer) approve(ctx context.Context, tx pgx.Tx, o *Order, a Actor, now time.Time) error {
	if err := w.applyTransition(ctx, tx, o, state.Status(o.Status), "", reasonDiscountApproved, a, now); err != nil {
		return err
	}
	n, err := store.New(tx).RecordDiscountApproval(ctx, store.RecordDiscountApprovalParams{
		ActorID: a.id(), Now: now, SellerID: o.SellerID, ID: o.ID, Version: o.Version,
	})
	switch {
	case err != nil:
		return fmt.Errorf("ghi người duyệt giảm giá: %w", err)
	case n != 1:
		return ErrVersionConflict // không thể xảy ra: đơn đang khóa, version vừa đổi trong cùng tx
	}
	return nil
}

// changeLocked khóa đơn id của người bán sellerID (lockOrder: không có → ErrNotFound), so version người gọi gửi lên
// (khác → ErrVersionConflict), chạy change trên phần Order mà applyTransition cần (lockedOrder) rồi đọc lại đơn như Get.
func changeLocked(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID, version int64, change func(o *Order) error,
) (Order, error) {
	r, err := lockOrder(ctx, store.New(tx), sellerID, id)
	if err != nil {
		return Order{}, err
	}
	if r.Version != version {
		return Order{}, ErrVersionConflict
	}
	o := lockedOrder(r)
	if err := change(&o); err != nil {
		return Order{}, err
	}
	return Get(ctx, tx, sellerID, id)
}
