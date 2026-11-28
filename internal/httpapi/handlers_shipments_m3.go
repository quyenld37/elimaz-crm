package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Vùng shipments_m3 (kế hoạch M3, Task M3-12): vận đơn của đơn, gửi lại, xử lý "không rõ". Quyền theo kho: danh sách
// theo kho của đơn (fulfillment.read), gửi lại và xử lý theo kho của tài khoản hãng của vận đơn (fulfillment.ship,
// kiểm dưới khóa); thiếu phạm vi → 403 như getShipment.

const defaultShipmentPage = 50

// ListOrderShipments (GET /api/v1/orders/{id}/shipments, fulfillment.read ở kho của đơn): mọi vận đơn của đơn, mới
// nhất trước. Không có đơn hay thiếu phạm vi → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListOrderShipments(ctx context.Context, params gen.ListOrderShipmentsParams) (gen.ListOrderShipmentsRes, error) {
	svc, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	var after *uuid.UUID
	if c, ok := params.Cursor.Get(); ok {
		id, err := uuid.Parse(c)
		if err != nil {
			return nil, fmt.Errorf("con trỏ trang: %w", fulfillment.ErrInvalidPage)
		}
		after = &id
	}
	limit := int(params.Limit.Or(defaultShipmentPage))
	var views []fulfillment.ShipmentView
	var more bool
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) error {
		var wh uuid.UUID
		var err error
		if err := sellerOrder(ctx, tx, params.ID, fulfillment.ErrOrderNotFound); err != nil {
			return err
		}
		views, wh, more, err = svc.OrderShipments(ctx, tx, params.ID, after, limit)
		if err != nil {
			return err //nolint:wrapcheck // sentinel của fulfillment
		}
		if canAt(u, wh, permShipRead) != nil {
			return fulfillment.ErrOrderNotFound
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("đọc vận đơn của đơn: %w", err)
	}
	out := &gen.ShipmentPage{Items: make([]gen.Shipment, 0, len(views))}
	for _, v := range views {
		out.Items = append(out.Items, *shipmentViewOut(v))
	}
	if more && len(views) > 0 {
		out.NextCursor = gen.NewOptString(views[len(views)-1].ID.String())
	}
	return out, nil
}

// ResendShipment (POST /api/v1/shipments/{id}/resend, fulfillment.ship) → 202: lần gửi mới cho vận đơn chắc chắn
// chưa tạo ở hãng; trạng thái khác → 409 create_in_progress.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ResendShipment(ctx context.Context, params gen.ResendShipmentParams) (gen.ResendShipmentRes, error) {
	svc, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	out, err := h.sellerShipmentWrite(ctx, statusCreated, isShipmentRefusal, "gửi lại vận đơn", params.ID,
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			v, err := svc.Resend(ctx, tx, params.ID, orderActor(u), shipAuthorize(u, permShip, nil))
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment
			}
			return shipmentViewOut(v), h.audit(ctx, tx, u, "fulfillment.shipment.resend", "shipment", v.ID, nil, v.AuditFields())
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Shipment), nil //nolint:forcetypeassert // fn trên trả *gen.Shipment
}

// ResolveShipmentCreate (POST /api/v1/shipments/{id}/resolve-create, fulfillment.ship): nhân viên tra trang hãng cho
// vận đơn "không rõ" — created (kèm mã hãng) hay not_created. Audit có kết quả và lý do.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ResolveShipmentCreate(ctx context.Context, req *gen.ResolveShipmentCreateRequest, params gen.ResolveShipmentCreateParams) (gen.ResolveShipmentCreateRes, error) {
	svc, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	in := fulfillment.ResolveInput{Result: string(req.Result), TrackingCode: req.TrackingCode.Or(""), Reason: req.Reason}
	out, err := h.sellerShipmentWrite(ctx, http.StatusOK, isShipmentRefusal, "xử lý vận đơn không rõ", params.ID,
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			v, err := svc.ResolveCreate(ctx, tx, params.ID, in, orderActor(u), shipAuthorize(u, permShip, nil))
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment
			}
			after := v.AuditFields()
			// Lý do là chữ nhân viên gõ: che SĐT, địa chỉ có số trước khi vào audit (04 mục 8).
			after["result"], after["reason"] = in.Result, logging.ScrubText(in.Reason)
			if in.TrackingCode != "" {
				after["trackingCode"] = in.TrackingCode
			}
			return shipmentViewOut(v), h.audit(ctx, tx, u, "fulfillment.shipment.resolve_create", "shipment", v.ID, nil, after)
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Shipment), nil //nolint:forcetypeassert // fn trên trả *gen.Shipment
}
