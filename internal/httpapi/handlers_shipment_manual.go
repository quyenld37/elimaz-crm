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

// Vùng shipment_manual (kế hoạch M3, Task M3-15): cập nhật tay trạng thái vận đơn của tài khoản nhập tay, hãng "tự
// giao", hay vận đơn api có việc hủy tay. Quyền fulfillment.manual_status theo kho của tài khoản hãng của vận đơn, kiểm
// dưới khóa; thiếu phạm vi → 403 như cancelShipment.

const permManualStatus = identity.Permission("fulfillment.manual_status")

// SetShipmentManualStatus (POST /api/v1/shipments/{id}/manual-status) → 200: vận đơn sau khi ghi tay (cùng máy trạng
// thái, cùng tác động như vận đơn API). Lý do chỉ khoảng trắng → 422; vận đơn không ghi tay được, máy trạng thái không
// nhận → 409 invalid_transition; version lệch → 409 version_conflict. Audit có lý do đã che SĐT, địa chỉ có số.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SetShipmentManualStatus(ctx context.Context, req *gen.SetShipmentManualStatusRequest, params gen.SetShipmentManualStatusParams) (gen.SetShipmentManualStatusRes, error) {
	svc, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	reason := logging.ScrubText(req.Reason)
	in := fulfillment.ManualStatusInput{To: fulfillment.Status(req.To), At: req.At, Reason: reason}
	if c, ok := req.CodCollected.Get(); ok {
		v := int64(c)
		in.CODCollected = &v
	}
	if v, ok := req.Version.Get(); ok {
		in.Version = &v
	}
	out, err := h.sellerShipmentWrite(ctx, http.StatusOK, isShipmentRefusal, "ghi tay trạng thái vận đơn", params.ID,
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			in.Authorize = shipAuthorize(u, permManualStatus, nil)
			v, err := svc.SetManualStatus(ctx, tx, params.ID, in, orderActor(u))
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment
			}
			after := v.AuditFields()
			after["to"], after["at"], after["reason"] = string(req.To), req.At.UTC(), reason
			if in.CODCollected != nil {
				after["codCollected"] = *in.CODCollected
			}
			return shipmentViewOut(v), h.audit(ctx, tx, u, "fulfillment.shipment.manual_status", "shipment", v.ID, nil, after)
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Shipment), nil //nolint:forcetypeassert // fn trên trả *gen.Shipment
}
