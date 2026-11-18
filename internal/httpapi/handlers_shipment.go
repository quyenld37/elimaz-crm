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

// Vận chuyển (kế hoạch M1, Task M1-31). fulfillment.ship, fulfillment.read là quyền theo đích kho: guard chỉ kiểm có
// grant; handler kiểm đích bằng canAt với kho của tài khoản hãng (B6), đọc trong cùng transaction. Audit ghi ở đây
// (fulfillment không nhập identity).

const (
	permShip      = identity.Permission("fulfillment.ship")
	permShipRead  = identity.Permission("fulfillment.read")
	statusCreated = http.StatusAccepted
)

// CreateShipment (POST /api/v1/orders/{id}/shipments, fulfillment.ship ở kho của tài khoản hãng) → 202: vận đơn
// creating; lệnh tạo ở hãng chạy nền. Từ chối nghiệp vụ, thiếu quyền ở kho: không lưu theo khóa.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateShipment(ctx context.Context, req *gen.CreateShipmentRequest, params gen.CreateShipmentParams) (gen.CreateShipmentRes, error) {
	out, err := h.refusableWrite(ctx, statusCreated, isShipmentRefusal, "tạo vận đơn",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			wh, err := fulfillment.AccountWarehouse(ctx, tx, req.CarrierAccountId)
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment
			}
			if err := canAt(u, wh, permShip); err != nil {
				return nil, err
			}
			s, err := fulfillment.CreateForOrder(ctx, tx, params.ID, req.CarrierAccountId,
				fulfillment.Actor{TenantID: u.TenantID, UserID: u.UserID})
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment
			}
			return shipmentOut(s), h.audit(ctx, tx, u, "fulfillment.shipment.create", "shipment", s.ID, nil, s.AuditFields())
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Shipment), nil //nolint:forcetypeassert // fn trên trả *gen.Shipment
}

func isShipmentRefusal(err error) bool {
	_, _, ok := fulfillmentError(err)
	return ok
}

// GetShipment (GET /api/v1/shipments/{id}, fulfillment.read ở kho của tài khoản hãng của vận đơn).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetShipment(ctx context.Context, params gen.GetShipmentParams) (gen.GetShipmentRes, error) {
	var s fulfillment.Shipment
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		if s, err = fulfillment.Get(ctx, tx, params.ID); err != nil {
			return err //nolint:wrapcheck // sentinel của fulfillment
		}
		return canAt(u, s.WarehouseID, permShipRead)
	}); err != nil {
		return nil, fmt.Errorf("đọc vận đơn: %w", err)
	}
	return shipmentOut(s), nil
}

func shipmentOut(s fulfillment.Shipment) *gen.Shipment {
	out := &gen.Shipment{
		ID: s.ID, OrderId: s.OrderID, Code: s.Code, CarrierKey: s.CarrierKey, CarrierAccountId: s.CarrierAccountID,
		Status: gen.ShipmentStatus(s.Status), CodAmount: gen.Money(s.CODAmount), CreatedAt: s.CreatedAt,
	}
	if s.TrackingCode != "" {
		out.TrackingCode = gen.NewOptString(s.TrackingCode)
	}
	if s.Fee != nil {
		out.Fee = gen.NewOptMoney(gen.Money(*s.Fee))
	}
	return out
}
