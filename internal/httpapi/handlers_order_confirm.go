package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// ConfirmOrder (POST /api/v1/orders/{id}/confirm, order.confirm): xác nhận đơn new hoặc chốt đơn draft của người bán
// mặc định (Task M2-19). Đơn chờ duyệt giảm giá → 422 needs_approval, trừ khi người gọi có order.discount.approve
// (máy chủ tự tính từ grant; khi đó ghi duyệt rồi xác nhận).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ConfirmOrder(ctx context.Context, req *gen.VersionRequest, params gen.ConfirmOrderParams) (gen.ConfirmOrderRes, error) {
	return h.orderChange(ctx, "order.confirm", func(tx pgx.Tx, w order.Writer, seller uuid.UUID, a order.Actor, now time.Time) (order.Order, error) {
		return w.Confirm(ctx, tx, seller, params.ID, req.Version, a, now)
	})
}

// ApproveOrderDiscount (POST /api/v1/orders/{id}/approve-discount, order.discount.approve): duyệt giảm giá vượt ngưỡng
// (Q3): đơn new sang confirmed, đơn draft bỏ lý do chờ duyệt (vẫn draft).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ApproveOrderDiscount(ctx context.Context, req *gen.VersionRequest, params gen.ApproveOrderDiscountParams) (gen.ApproveOrderDiscountRes, error) {
	return h.orderChange(ctx, "order.approve_discount", func(tx pgx.Tx, w order.Writer, seller uuid.UUID, a order.Actor, now time.Time) (order.Order, error) {
		return w.ApproveDiscount(ctx, tx, seller, params.ID, req.Version, a, now)
	})
}

// SetOrderPending (POST /api/v1/orders/{id}/pending, order.confirm): đặt hoặc bỏ (không gửi reason) lý do chờ khách,
// hết hàng của đơn new.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SetOrderPending(ctx context.Context, req *gen.SetOrderPendingRequest, params gen.SetOrderPendingParams) (gen.SetOrderPendingRes, error) {
	reason := string(req.Reason.Or(""))
	return h.orderChange(ctx, "order.set_pending", func(tx pgx.Tx, w order.Writer, seller uuid.UUID, a order.Actor, now time.Time) (order.Order, error) {
		return w.SetPending(ctx, tx, seller, params.ID, req.Version, reason, a, now)
	})
}

// orderChange chạy change trên đơn của người bán mặc định trong WriteTx (refusableWrite: lỗi nghiệp vụ của đơn không
// lưu theo khóa), ghi audit action cùng tx (order không nhập identity), trả thân đơn 200.
func (h *handler) orderChange(ctx context.Context, action string,
	change func(tx pgx.Tx, w order.Writer, seller uuid.UUID, a order.Actor, now time.Time) (order.Order, error),
) (*gen.Order, error) {
	w := order.Writer{Events: h.d.Events, Queue: queue.NewQueue(), Realtime: h.d.Realtime}
	out, err := h.refusableWrite(ctx, http.StatusOK, isOrderChangeRefusal, action, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		seller, err := catalog.DefaultSeller(ctx, tx)
		if err != nil {
			return nil, fmt.Errorf("người bán mặc định: %w", err)
		}
		o, err := change(tx, w, seller, orderActor(u), time.Now().UTC())
		if err != nil {
			return nil, err
		}
		after := o.AuditFields()
		after["pendingReason"] = o.PendingReason
		if o.DiscountApprovedBy != nil {
			after["discountApprovedBy"] = o.DiscountApprovedBy.String()
		}
		if err := identity.RecordAudit(ctx, tx, identity.AuditEntry{
			TenantID: u.TenantID, ActorType: "staff", ActorID: u.UserID.String(), Action: action,
			EntityType: "order", EntityID: o.ID.String(), After: after, RequestID: RequestID(ctx),
		}); err != nil {
			return nil, fmt.Errorf("ghi audit %s: %w", action, err)
		}
		return orderOut(o), nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Order), nil //nolint:forcetypeassert // change trên trả *gen.Order
}

// isOrderChangeRefusal: lỗi nghiệp vụ của xác nhận, duyệt, đặt lý do chờ (trả cho client, không lưu theo khóa).
func isOrderChangeRefusal(err error) bool {
	for _, e := range []error{
		order.ErrNotFound, order.ErrVersionConflict, order.ErrInvalidTransition, order.ErrAddressIncomplete,
		order.ErrNeedsApproval, order.ErrDiscountNeedsApproval, order.ErrInvalidInput,
	} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}
