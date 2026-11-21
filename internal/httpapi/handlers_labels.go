package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/queries"
)

// Vùng labels (kế hoạch M3, Task M3-16): in nhãn theo lô, phiếu nhặt hàng, đóng gói. fulfillment.print,
// fulfillment.pack là quyền theo kho: guard chỉ kiểm có grant; handler kiểm kho của **đơn** của từng vận đơn/đơn (như
// createShipment). Một vận đơn hay đơn ngoài phạm vi → cả yêu cầu 404 (không in lộ, không cho biết có). In không đổi
// trạng thái vận đơn hay đóng gói (AT08). Audit ghi ở đây (fulfillment, order không nhập identity), chỉ mã.

const (
	permPrint = identity.Permission("fulfillment.print")
	permPack  = identity.Permission("fulfillment.pack")
)

// CreateLabelBatch (POST /api/v1/label-batches, fulfillment.print) → 202: mỗi tài khoản hãng một lô pending và một
// lệnh tải nhãn chạy nền; vận đơn không in được nhãn hãng trả trong skipped kèm lý do.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateLabelBatch(ctx context.Context, req *gen.CreateLabelBatchRequest, _ gen.CreateLabelBatchParams) (gen.CreateLabelBatchRes, error) {
	svc, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	out, err := h.refusableWrite(ctx, http.StatusAccepted, isShipmentRefusal, "in nhãn",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			res, err := svc.CreateLabelBatch(ctx, tx, req.ShipmentIds, fulfillment.Actor{TenantID: u.TenantID, UserID: u.UserID},
				shipAuthorize(u, permPrint, fulfillment.ErrNotFound))
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment
			}
			body := &gen.LabelBatchCreated{
				Batches: make([]gen.LabelBatchSummary, 0, len(res.Batches)),
				Skipped: make([]gen.LabelBatchSkipped, 0, len(res.Skipped)),
			}
			for _, b := range res.Batches {
				body.Batches = append(body.Batches, gen.LabelBatchSummary{
					ID: b.ID, CarrierAccountId: b.CarrierAccountID, Count: int32(b.Count), //nolint:gosec // ≤ 200
				})
				if err := h.audit(ctx, tx, u, "fulfillment.label_batch.create", "label_batch", b.ID, nil,
					map[string]any{"carrierAccountId": b.CarrierAccountID.String(), "count": b.Count}); err != nil {
					return nil, err
				}
			}
			for _, s := range res.Skipped {
				body.Skipped = append(body.Skipped, gen.LabelBatchSkipped{ShipmentId: s.ShipmentID, Reason: s.Reason})
			}
			return body, nil
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.LabelBatchCreated), nil //nolint:forcetypeassert // fn trên trả *gen.LabelBatchCreated
}

// GetLabelBatch (GET /api/v1/label-batches/{id}, fulfillment.print ở mọi kho của các đơn trong lô): trạng thái lô,
// url ký sẵn 5 phút khi ready. Không có lô hay thiếu phạm vi → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetLabelBatch(ctx context.Context, params gen.GetLabelBatchParams) (gen.GetLabelBatchRes, error) {
	svc, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	var v fulfillment.LabelBatchView
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) error {
		var err error
		v, err = svc.LabelBatch(ctx, tx, params.ID, shipAuthorize(u, permPrint, fulfillment.ErrNotFound))
		return err //nolint:wrapcheck // sentinel của fulfillment
	}); err != nil {
		return nil, fmt.Errorf("đọc lô nhãn: %w", err)
	}
	out := &gen.LabelBatch{
		ID: v.ID, CarrierAccountId: v.CarrierAccountID, Status: gen.LabelBatchStatus(v.Status),
		Count: int32(v.Count), CreatedAt: v.CreatedAt, //nolint:gosec // ≤ 200 (CHECK của bảng)
	}
	if v.URL != "" {
		out.URL = gen.NewOptString(v.URL)
	}
	if v.ErrorCode != "" {
		out.ErrorCode = gen.NewOptString(v.ErrorCode)
	}
	return out, nil
}

// GetPickingList (GET /api/v1/picking-list?shipmentIds=, fulfillment.print ở kho của đơn của mọi vận đơn): dòng hàng
// gộp theo SKU và danh sách đơn; không tên, SĐT, địa chỉ người nhận. Mã lạ hay thiếu phạm vi → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetPickingList(ctx context.Context, params gen.GetPickingListParams) (gen.GetPickingListRes, error) {
	ids := map[uuid.UUID]bool{}
	for _, id := range params.ShipmentIds {
		ids[id] = true
	}
	var pl queries.PickingList
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) error {
		var err error
		if pl, err = queries.Picking(ctx, tx, params.ShipmentIds); err != nil {
			return err //nolint:wrapcheck // bọc ở dưới
		}
		if len(pl.Orders) != len(ids) {
			return fulfillment.ErrNotFound
		}
		for _, o := range pl.Orders {
			if canAt(u, o.WarehouseID, permPrint) != nil {
				return fulfillment.ErrNotFound
			}
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("đọc phiếu nhặt hàng: %w", err)
	}
	out := &gen.PickingList{
		Lines: make([]gen.PickingLine, 0, len(pl.Lines)), Orders: make([]gen.PickingOrder, 0, len(pl.Orders)),
	}
	for _, l := range pl.Lines {
		line := gen.PickingLine{Sku: l.SKU, Name: l.Name, Qty: int32(l.Qty), OrderCodes: l.OrderCodes} //nolint:gosec // số món của ≤ 200 đơn
		if l.Variant != "" {
			line.Variant = gen.NewOptString(l.Variant)
		}
		out.Lines = append(out.Lines, line)
	}
	for _, o := range pl.Orders {
		po := gen.PickingOrder{
			Code: o.Code, ShipmentCode: o.ShipmentCode, Carrier: o.Carrier, ItemCount: int32(o.ItemCount), //nolint:gosec // số món của một đơn
			CodAmount: gen.Money(o.CODAmount),
		}
		if o.TrackingCode != "" {
			po.TrackingCode = gen.NewOptString(o.TrackingCode)
		}
		out.Orders = append(out.Orders, po)
	}
	return out, nil
}

// SetPackingStatus (POST /api/v1/orders/packing, fulfillment.pack ở kho của mọi đơn): đưa các đơn của người bán mặc
// định sang picking hay packed (chỉ đi tới, order.Writer.SetPackingStatus); kết quả từng đơn. Phạm vi kiểm trên các đơn
// đã khóa, cùng transaction: một đơn ngoài phạm vi hay không có → 404, không đơn nào đổi.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SetPackingStatus(ctx context.Context, req *gen.SetPackingStatusRequest, _ gen.SetPackingStatusParams) (gen.SetPackingStatusRes, error) {
	to := string(req.To)
	out, err := h.orderWriteAsDefaultSeller(ctx, http.StatusOK, isOrderChangeRefusal, "đóng gói",
		func(tx pgx.Tx, w order.Writer, u identity.AuthUser, seller uuid.UUID, a order.Actor) (any, error) {
			res, err := w.SetPackingStatus(ctx, tx, seller, req.OrderIds, to, a, time.Now().UTC())
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của order
			}
			return h.packingResults(ctx, tx, u, res, to)
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.PackingResultList), nil //nolint:forcetypeassert // fn trên trả *gen.PackingResultList
}

// packingResults kiểm u có fulfillment.pack ở kho của mọi đơn trong res (đã khóa; một đơn ngoài phạm vi hay không có
// → order.ErrNotFound, cả lệnh lùi), ghi audit cho đơn đã đổi, trả thân kết quả.
func (h *handler) packingResults(ctx context.Context, tx pgx.Tx, u identity.AuthUser, res []order.PackingResult, to string,
) (*gen.PackingResultList, error) {
	ids := make([]uuid.UUID, len(res))
	for i, r := range res {
		ids[i] = r.OrderID
	}
	whs, err := queries.OrderWarehouses(ctx, tx, ids)
	if err != nil {
		return nil, err //nolint:wrapcheck // đã bọc trong queries
	}
	body := &gen.PackingResultList{Items: make([]gen.PackingResult, 0, len(res))}
	for _, r := range res {
		if wh, ok := whs[r.OrderID]; !ok || canAt(u, wh, permPack) != nil {
			return nil, order.ErrNotFound
		}
		body.Items = append(body.Items, gen.PackingResult{OrderId: r.OrderID, Result: gen.PackingResultResult(r.Result)})
	}
	for _, r := range res {
		if r.Result != order.PackingUpdated {
			continue
		}
		if err := h.audit(ctx, tx, u, "order.packing", "order", r.OrderID, nil, map[string]any{"packingStatus": to}); err != nil {
			return nil, err
		}
	}
	return body, nil
}
