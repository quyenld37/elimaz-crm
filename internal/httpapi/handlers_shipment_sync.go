package httpapi

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Vùng shipment_sync (kế hoạch M3, Task M3-11): tra lại trạng thái vận đơn.

// SyncShipment (POST /api/v1/shipments/{id}/sync, fulfillment.read ở kho của tài khoản hãng của vận đơn) → 202 {jobId}:
// ghi một lệnh tra ở hãng; bấm nhiều lần trong một phút trả cùng việc. Vận đơn lạ (hay tenant khác) → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SyncShipment(ctx context.Context, params gen.SyncShipmentParams) (gen.SyncShipmentRes, error) {
	out, err := h.accountWrite(ctx, http.StatusAccepted, "tra lại vận đơn",
		func(tx pgx.Tx, s *fulfillment.Service, u identity.AuthUser, a fulfillment.Actor) (any, error) {
			sh, err := fulfillment.Get(ctx, tx, params.ID)
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment
			}
			if err := canAt(u, sh.WarehouseID, permShipRead); err != nil {
				return nil, err
			}
			job, err := s.SyncShipment(ctx, tx, sh.ID, a)
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment
			}
			return &gen.JobAccepted{JobId: job}, h.audit(ctx, tx, u, "fulfillment.shipment.sync", "shipment", sh.ID, nil,
				map[string]any{"jobId": job.String()})
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.JobAccepted), nil //nolint:forcetypeassert // fn trên trả *gen.JobAccepted
}
