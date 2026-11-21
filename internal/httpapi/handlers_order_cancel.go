package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
)

// CancelOrder (POST /api/v1/orders/{id}/cancel, order.cancel; Task M2-21): hủy đơn của người bán mặc định ở version
// với lý do nhân viên chọn (enum hợp đồng, Q18) → 200 đơn sau hủy: chưa gửi hãng → cancelled (nhả hàng, trả lượt mã);
// đã gửi hãng → cancel_requested. Lỗi nghiệp vụ (version cũ 409, trạng thái không hủy được 422) không lưu theo khóa.
// Audit order.cancel ghi trạng thái trước/sau, lý do, có ghi chú hay không (không ghi nội dung ghi chú).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CancelOrder(ctx context.Context, req *gen.CancelOrderRequest, params gen.CancelOrderParams) (gen.CancelOrderRes, error) {
	note := req.Note.Or("")
	out, err := h.orderWriteAsDefaultSeller(ctx, http.StatusOK, isOrderCancelRefusal, "hủy đơn", func(tx pgx.Tx,
		w order.Writer, u identity.AuthUser, seller uuid.UUID, a order.Actor,
	) (any, error) {
		before, err := order.Get(ctx, tx, seller, params.ID)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của order
		}
		o, err := w.Cancel(ctx, tx, seller, params.ID, req.Version, string(req.Reason), note, a, h.now().UTC())
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của order
		}
		after := o.AuditFields()
		after["cancelReason"], after["hasNote"] = string(req.Reason), note != ""
		return orderOut(o), h.audit(ctx, tx, u, "order.cancel", "order", o.ID, before.AuditFields(), after)
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Order), nil //nolint:forcetypeassert // fn trên trả *gen.Order
}

// isOrderCancelRefusal: lỗi nghiệp vụ của order (đã ánh xạ ở errors.go) là câu trả lời không đổi gì.
func isOrderCancelRefusal(err error) bool {
	_, _, ok := orderError(err)
	return ok
}
