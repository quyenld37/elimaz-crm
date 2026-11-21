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

// Vùng partial_delivery (kế hoạch M3, Task M3-26): giao một phần bằng ngoại lệ có quyền. Quyền fulfillment.correct
// (không theo kho) do guard kiểm theo x-permission; module làm cả lần ghi trong một transaction.

// RecordPartialDelivery (POST /api/v1/shipments/{id}/partial-delivery) → 200: vận đơn delivered, partial, tiền đã thu;
// phần chưa giao thành phiếu partial_delivery (kho nhận qua receiveReturn). Số giao sai → 422 qty_mismatch; tiền thu
// vượt COD → 422 payment_exceeds_total; vận đơn không ghi được (đã giao, đang hoàn…) → 409 invalid_transition. Audit có
// dòng giao, tiền thu, mã phiếu, lý do đã che SĐT, địa chỉ có số.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) RecordPartialDelivery(ctx context.Context, req *gen.RecordPartialDeliveryRequest, params gen.RecordPartialDeliveryParams) (gen.RecordPartialDeliveryRes, error) {
	svc, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	reason := logging.ScrubText(req.Reason)
	lines := make([]fulfillment.PartialLine, len(req.Items))
	audited := make([]map[string]any, len(req.Items))
	for i, it := range req.Items {
		lines[i] = fulfillment.PartialLine{OrderItemID: it.OrderItemId, DeliveredQty: it.DeliveredQty}
		audited[i] = map[string]any{"orderItemId": it.OrderItemId.String(), "deliveredQty": it.DeliveredQty}
	}
	in := fulfillment.PartialInput{Lines: lines, CODCollected: int64(req.CodCollected), Reason: reason}
	out, err := h.refusableWrite(ctx, http.StatusOK, isShipmentRefusal, "ghi giao một phần",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			r, err := svc.RecordPartialDelivery(ctx, tx, params.ID, in, orderActor(u))
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment
			}
			after := r.Shipment.AuditFields()
			after["reason"], after["items"], after["codCollected"] = reason, audited, in.CODCollected
			after["returnId"], after["returnCode"], after["partial"] = r.ReturnID.String(), r.ReturnCode, true
			return shipmentViewOut(r.Shipment), h.audit(ctx, tx, u, "fulfillment.shipment.partial_delivery", "shipment",
				r.Shipment.ID, nil, after)
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Shipment), nil //nolint:forcetypeassert // fn trên trả *gen.Shipment
}
