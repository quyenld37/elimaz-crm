package httpapi

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Vùng shipment_cancel (kế hoạch M3, Task M3-14): hủy vận đơn để đổi hãng. Hủy đơn đi qua cancelOrder của M2 (consumer
// của fulfillment hủy ở hãng). Quyền fulfillment.cancel theo kho của tài khoản hãng của vận đơn, kiểm dưới khóa; thiếu
// phạm vi → 403 như resendShipment.

const permCancel = identity.Permission("fulfillment.cancel")

// CancelShipment (POST /api/v1/shipments/{id}/cancel) → 200: vận đơn đang hủy (hãng hủy qua API: lệnh hủy chạy nền;
// không: việc manual_cancel), hay đã hủy (hãng chắc không có kiện). Đã đi, đang hủy → 409 shipment_not_cancellable;
// lệnh tạo chưa có kết quả → 409 create_in_progress (carrierChecked: nhân viên đã tra hãng → hủy staff_after_unknown).
// Audit có lý do đã che SĐT, địa chỉ có số.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CancelShipment(ctx context.Context, req *gen.CancelShipmentRequest, params gen.CancelShipmentParams) (gen.CancelShipmentRes, error) {
	svc, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	reason := logging.ScrubText(req.Reason)
	checked := req.CarrierChecked.Or(false)
	out, err := h.sellerShipmentWrite(ctx, http.StatusOK, isShipmentRefusal, "hủy vận đơn", params.ID,
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			v, err := svc.RequestCancel(ctx, tx, params.ID, fulfillment.CancelInput{
				Intent: "replace", Reason: reason, CarrierChecked: checked,
				Authorize: shipAuthorize(u, permCancel, nil),
			}, orderActor(u))
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment
			}
			after := v.AuditFields()
			after["reason"], after["carrierChecked"], after["cancelKind"] = reason, checked, v.CancelKind
			return shipmentViewOut(v), h.audit(ctx, tx, u, "fulfillment.shipment.cancel", "shipment", v.ID, nil, after)
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Shipment), nil //nolint:forcetypeassert // fn trên trả *gen.Shipment
}
