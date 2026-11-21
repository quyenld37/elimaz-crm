package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Vùng shipment_lists (kế hoạch M3, Task M3-20): đơn kẹt, "Cần xem". Danh sách và số đếm chỉ gồm vận đơn ở kho người
// gọi có fulfillment.read (toàn shop hay đúng kho đó; kho của tài khoản hãng). Đóng việc Cần xem: fulfillment.correct
// (quyền toàn shop, guard kiểm) và phải thấy kho của vận đơn (fulfillment.read ở đó), không thì 404 như việc không có.

// ListStuckShipments (GET /api/v1/shipments/stuck, fulfillment.read theo kho): vận đơn kẹt, kẹt lâu nhất trước.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListStuckShipments(ctx context.Context, params gen.ListStuckShipmentsParams) (gen.ListStuckShipmentsRes, error) {
	svc, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	f := fulfillment.StuckFilter{
		Status: string(params.Status.Or("")), CarrierAccountID: optUUID(params.CarrierAccountId),
		Cursor: params.Cursor.Or(""), Limit: int(params.Limit.Or(0)),
	}
	var rows []fulfillment.StuckShipment
	var next string
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		f.AllWarehouses, f.WarehouseIDs = readableWarehouses(u, permShipRead)
		rows, next, err = svc.ListStuck(ctx, tx, f)
		return err //nolint:wrapcheck // sentinel của fulfillment
	}); err != nil {
		return nil, fmt.Errorf("đọc đơn kẹt: %w", err)
	}
	out := &gen.StuckShipmentPage{Items: make([]gen.StuckShipment, 0, len(rows))}
	for _, r := range rows {
		it := gen.StuckShipment{
			ShipmentId: r.ShipmentID, OrderId: gen.NewOptUUID(r.OrderID), OrderCode: r.OrderCode, CarrierKey: r.CarrierKey,
			AccountName: gen.NewOptString(r.AccountName), Status: gen.ShipmentStatus(r.Status),
			Label: gen.NewOptString(r.Label), StuckSince: r.StuckSince, HoursSinceUpdate: r.HoursSinceUpdate,
		}
		if r.TrackingCode != "" {
			it.TrackingCode = gen.NewOptString(r.TrackingCode)
		}
		if r.LastSyncError != "" {
			it.LastSyncError = gen.NewOptString(r.LastSyncError)
		}
		if r.LastCarrierEventAt != nil {
			it.LastCarrierEventAt = gen.NewOptDateTime(*r.LastCarrierEventAt)
		}
		if r.LastSyncedAt != nil {
			it.LastSyncedAt = gen.NewOptDateTime(*r.LastSyncedAt)
		}
		out.Items = append(out.Items, it)
	}
	if next != "" {
		out.NextCursor = gen.NewOptString(next)
	}
	return out, nil
}

// ListShipmentsForReview (GET /api/v1/shipments/review, fulfillment.read theo kho): việc Cần xem (cũ nhất trước, câu
// hướng dẫn) kèm số đếm các danh sách vận hành.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListShipmentsForReview(ctx context.Context, params gen.ListShipmentsForReviewParams) (gen.ListShipmentsForReviewRes, error) {
	svc, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	f := fulfillment.ReviewFilter{Cursor: params.Cursor.Or(""), Limit: int(params.Limit.Or(0))}
	var p fulfillment.ReviewPage
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		f.AllWarehouses, f.WarehouseIDs = readableWarehouses(u, permShipRead)
		p, err = svc.ListForReview(ctx, tx, f)
		return err //nolint:wrapcheck // sentinel của fulfillment
	}); err != nil {
		return nil, fmt.Errorf("đọc việc cần xem: %w", err)
	}
	c := p.Counts
	out := &gen.ShipmentReviewPage{
		Items: make([]gen.ShipmentReviewItem, 0, len(p.Items)),
		Counts: gen.ShipmentReviewCounts{
			Review: int32n(c.Review), Stuck: int32n(c.Stuck), CallbacksOpen: int32n(c.CallbacksOpen),
			CallbacksOverdue: int32n(c.CallbacksOverdue), InDoubt: int32n(c.InDoubt),
		},
	}
	for _, it := range p.Items {
		out.Items = append(out.Items, gen.ShipmentReviewItem{
			AnomalyId: it.AnomalyID, ShipmentId: it.ShipmentID, OrderId: gen.NewOptUUID(it.OrderID), OrderCode: it.OrderCode,
			Kind: it.Kind, Label: it.Label, Detail: gen.NewOptShipmentReviewItemDetail(it.Detail), CreatedAt: it.CreatedAt,
		})
	}
	if p.Next != "" {
		out.NextCursor = gen.NewOptString(p.Next)
	}
	return out, nil
}

// int32n chặn số đếm vào int32 của hợp đồng.
func int32n(n int) int32 { return int32(min(max(n, 0), 1<<31-1)) }

// ResolveShipmentAnomaly (POST /api/v1/shipment-anomalies/{id}/resolve, fulfillment.correct) → 200: việc đã đóng.
// in_doubt_create, manual_cancel, in_doubt_cancel (có đường riêng) hay việc đã đóng → 409 invalid_transition; không có, hay kho ngoài phạm vi fulfillment.read → 404.
// Audit chỉ có loại, trạng thái (không chép cách xử lý: chữ tự do có thể có SĐT).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ResolveShipmentAnomaly(ctx context.Context, req *gen.ResolveShipmentAnomalyRequest, params gen.ResolveShipmentAnomalyParams) (gen.ResolveShipmentAnomalyRes, error) {
	svc, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	out, err := h.refusableWrite(ctx, http.StatusOK, isShipmentRefusal, "đóng việc cần xem",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			a, err := svc.ResolveAnomaly(ctx, tx, params.ID, fulfillment.AnomalyResolveInput{
				Resolution: req.Resolution, Authorize: shipAuthorize(u, permShipRead, fulfillment.ErrAnomalyNotFound),
			}, fulfillment.Actor{TenantID: u.TenantID, UserID: u.UserID})
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment
			}
			after := map[string]any{"kind": a.Kind, "status": a.Status, "shipmentId": a.ShipmentID.String()}
			return anomalyOut(a), h.audit(ctx, tx, u, "fulfillment.anomaly.resolve", "shipment_anomaly", a.ID,
				map[string]any{"status": "open"}, after)
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.ShipmentAnomaly), nil //nolint:forcetypeassert // fn trên trả *gen.ShipmentAnomaly
}

func anomalyOut(a fulfillment.Anomaly) *gen.ShipmentAnomaly {
	out := &gen.ShipmentAnomaly{
		ID: a.ID, ShipmentId: a.ShipmentID, Kind: a.Kind, Status: gen.ShipmentAnomalyStatus(a.Status),
		Resolution: gen.NewOptString(a.Resolution), CreatedAt: a.CreatedAt,
	}
	if a.ResolvedAt != nil {
		out.ResolvedAt = gen.NewOptDateTime(*a.ResolvedAt)
	}
	return out
}
