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

// Vùng corrections (kế hoạch M3, Task M3-27): đảo "Thành công" khi nhân viên sửa nhầm. Quyền fulfillment.correct
// (không theo kho, như giao một phần) do guard kiểm theo x-permission; module xem trước và áp trong một transaction.
// Lý do qua logging.ScrubText trước khi vào module (lịch sử vận đơn) và audit.

// PreviewShipmentCorrection (POST /api/v1/shipments/{id}/corrections/preview) → 200: câu tác động, câu chặn, token
// (không đổi dữ liệu; cùng Idempotency-Key → cùng thân). Lý do chỉ khoảng trắng → 422; vận đơn không đảo được (chưa
// giao, đã giao một phần…) → 409 invalid_transition; vận đơn lạ → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) PreviewShipmentCorrection(ctx context.Context, req *gen.ShipmentCorrectionRequest, params gen.PreviewShipmentCorrectionParams) (gen.PreviewShipmentCorrectionRes, error) {
	svc, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	in := fulfillment.CorrectionInput{To: fulfillment.Status(req.To), Reason: logging.ScrubText(req.Reason)}
	out, err := h.refusableWrite(ctx, http.StatusOK, isShipmentRefusal, "xem trước đảo Thành công",
		func(tx pgx.Tx, _ identity.AuthUser) (any, error) {
			p, err := svc.PreviewCorrection(ctx, tx, params.ID, in)
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment
			}
			return &gen.ShipmentCorrectionPreview{
				ShipmentId: p.ShipmentID, From: string(p.From), To: string(p.To), Token: p.Token,
				// Trường bắt buộc là mảng: nil → [] (không null).
				Effects: append([]string{}, p.Effects...), Blocking: append([]string{}, p.Blocking...),
			}, nil
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.ShipmentCorrectionPreview), nil //nolint:forcetypeassert // fn trên trả *gen.ShipmentCorrectionPreview
}

// ApplyShipmentCorrection (POST /api/v1/shipments/{id}/corrections) → 200: vận đơn returning (đơn bỏ Thành công, đảo
// phiếu xuất, phiếu hoàn). Token khác bản tính lại → 409 preview_stale; tiền đã về shop → 409 correction_blocked; vận
// đơn không đảo được → 409 invalid_transition. Audit có trạng thái trước, sau và lý do đã che SĐT, địa chỉ có số.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ApplyShipmentCorrection(ctx context.Context, req *gen.ApplyShipmentCorrectionRequest, params gen.ApplyShipmentCorrectionParams) (gen.ApplyShipmentCorrectionRes, error) {
	svc, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	reason := logging.ScrubText(req.Reason)
	in := fulfillment.CorrectionInput{To: fulfillment.Status(req.To), Reason: reason, PreviewToken: req.PreviewToken}
	out, err := h.refusableWrite(ctx, http.StatusOK, isShipmentRefusal, "đảo Thành công",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			v, err := svc.ApplyCorrection(ctx, tx, params.ID, in, orderActor(u))
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment
			}
			after := v.AuditFields()
			after["from"], after["to"], after["reason"] = string(fulfillment.StatusDelivered), string(req.To), reason
			return shipmentViewOut(v), h.audit(ctx, tx, u, "fulfillment.shipment.correction", "shipment", v.ID, nil, after)
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Shipment), nil //nolint:forcetypeassert // fn trên trả *gen.Shipment
}
