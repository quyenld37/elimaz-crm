package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
)

// UpdateOrder (PATCH /api/v1/orders/{id}, order.update): sửa đơn của người bán mặc định khi version còn khớp (Task
// M2-20; order.Writer.Edit, một transaction cùng giữ hàng và mã giảm giá) → 200 thân đơn. Từ chối nghiệp vụ (version
// cũ, đã gửi hãng, hết hàng, mã bị từ chối, giảm giá cần duyệt…) không lưu theo khóa. Audit order.update ghi tên các
// trường đổi (không giá trị địa chỉ, SĐT), mốc là now đã đưa vào Writer.Edit; không trường nào đổi thì không ghi gì.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UpdateOrder(ctx context.Context, req *gen.UpdateOrderRequest, params gen.UpdateOrderParams) (gen.UpdateOrderRes, error) {
	in := editOrderInput(req)
	out, err := h.orderWriteAsDefaultSeller(ctx, http.StatusOK, isOrderEditRefusal, "sửa đơn", func(tx pgx.Tx,
		w order.Writer, u identity.AuthUser, seller uuid.UUID, a order.Actor,
	) (any, error) {
		now := h.now().UTC()
		res, err := w.Edit(ctx, tx, seller, params.ID, in, a, now)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của order, promotion
		}
		if len(res.Fields) > 0 {
			after := res.Order.AuditFields()
			after["fields"] = res.Fields
			after["pendingReason"] = res.Order.PendingReason
			// Mục sửa của lịch sử đơn: cùng đồng hồ với dòng lịch sử trạng thái (now của module), không now() của
			// database (Task 436).
			if err := identity.RecordAudit(ctx, tx, identity.AuditEntry{
				TenantID: u.TenantID, ActorType: "staff", ActorID: u.UserID.String(), Action: "order.update",
				EntityType: "order", EntityID: res.Order.ID.String(), After: after, RequestID: RequestID(ctx), At: now,
			}); err != nil {
				return nil, fmt.Errorf("ghi audit order.update: %w", err)
			}
		}
		return orderOut(res.Order), nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Order), nil //nolint:forcetypeassert // change trên trả *gen.Order
}

// editOrderInput dịch thân updateOrder sang order.EditInput: trường không gửi là không sửa; lines có mặt là danh sách
// dòng mong muốn (hợp đồng đòi ít nhất một dòng).
func editOrderInput(req *gen.UpdateOrderRequest) order.EditInput {
	in := order.EditInput{
		Version: req.Version, CareBy: optUUID(req.CareBy), TrafficSourceID: optUUID(req.TrafficSourceId),
	}
	if req.Lines != nil {
		ls := make([]order.EditLine, 0, len(req.Lines))
		for _, l := range req.Lines {
			ls = append(ls, order.EditLine{
				ItemID: optUUID(l.ItemId), VariantID: l.VariantId, Quantity: l.Qty, LineDiscount: int64(l.LineDiscount.Or(0)),
			})
		}
		in.Lines = &ls
	}
	if v, ok := req.OrderDiscount.Get(); ok {
		d := int64(v)
		in.OrderDiscount = &d
	}
	if v, ok := req.ShippingFee.Get(); ok {
		f := int64(v)
		in.ShippingFee = &f
	}
	if v, ok := req.CouponCode.Get(); ok {
		in.CouponCode = &v
	}
	if v, ok := req.CarrierNote.Get(); ok {
		in.CarrierNote = &v
	}
	if a, ok := req.ShipAddress.Get(); ok {
		in.ShipAddress = shipAddressIn(a)
	}
	return in
}

// isOrderEditRefusal: lỗi nghiệp vụ của lần sửa đơn (trả cho client, không lưu theo khóa).
func isOrderEditRefusal(err error) bool {
	for _, e := range []error{
		order.ErrNotFound, order.ErrVersionConflict, order.ErrInvalidTransition, order.ErrLockedByShipment,
		order.ErrAddressIncomplete, order.ErrDiscountNeedsApproval, order.ErrInvalidInput, order.ErrOutOfStock,
		order.ErrNotForSale, order.ErrMixedSellers, order.ErrVariantNotFound, order.ErrTotalOverflow,
		order.ErrLineDiscountExceeds, order.ErrOrderDiscountExceeds, order.ErrTrafficSourceInactive,
		promotion.ErrCouponRejected, customer.ErrUnknownAdminUnit, customer.ErrWardNotInProvince,
	} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}
