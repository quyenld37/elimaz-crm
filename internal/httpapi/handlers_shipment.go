package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Vận chuyển (kế hoạch M1, Task M1-31; M3 Task M3-12). fulfillment.ship, fulfillment.read là quyền theo đích kho:
// guard chỉ kiểm có grant; handler kiểm đích bằng canAt (B6). Gửi hãng kiểm theo kho của ĐƠN, dưới khóa đơn (Service
// gọi lại Authorize); thiếu phạm vi → 404 như đơn không có. Audit ghi ở đây (fulfillment không nhập identity).

const (
	permShip      = identity.Permission("fulfillment.ship")
	permShipRead  = identity.Permission("fulfillment.read")
	statusCreated = http.StatusAccepted
)

// shipAuthorize: người gọi phải có perm ở kho; thiếu → hidden (404 không lộ sự tồn tại) hay errForbiddenTarget.
func shipAuthorize(u identity.AuthUser, perm identity.Permission, hidden error) fulfillment.Authorize {
	return func(wh uuid.UUID) error {
		if err := canAt(u, wh, perm); err != nil {
			if hidden != nil {
				return hidden
			}
			return err
		}
		return nil
	}
}

// CreateShipment (POST /api/v1/orders/{id}/shipments, fulfillment.ship ở kho của đơn) → 202: vận đơn creating; lệnh
// tạo ở hãng chạy nền. Từ chối nghiệp vụ, thiếu phạm vi: không lưu theo khóa. Đơn đổi còn phải cấn trừ tiền món trả
// (cash.ExchangeCreditPending, kiểm sau Ship: đơn đã khóa, phạm vi đã kiểm) → 409 exchange_credit_pending, transaction
// lùi; skipExchangeCredit = true bỏ kiểm (khách trả đủ COD, phần món trả thành nghĩa vụ hoàn khi kho nhận), audit ghi
// cờ (Task 524).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateShipment(ctx context.Context, req *gen.CreateShipmentRequest, params gen.CreateShipmentParams) (gen.CreateShipmentRes, error) {
	svc, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	skip := req.SkipExchangeCredit.Or(false)
	out, err := h.refusableWrite(ctx, statusCreated, isCreateShipmentRefusal, "tạo vận đơn",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			v, err := svc.Ship(ctx, tx, shipInput(params.ID, req, u), orderActor(u))
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment
			}
			fields := v.AuditFields()
			if skip {
				fields["skipExchangeCredit"] = true
			} else if err := exchangeCreditReady(ctx, tx, v.OrderID); err != nil {
				return nil, err
			}
			return shipmentViewOut(v), h.audit(ctx, tx, u, "fulfillment.shipment.create", "shipment", v.ID, nil, fields)
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Shipment), nil //nolint:forcetypeassert // fn trên trả *gen.Shipment
}

func shipInput(orderID uuid.UUID, req *gen.CreateShipmentRequest, u identity.AuthUser) fulfillment.ShipInput {
	in := fulfillment.ShipInput{
		OrderID: orderID, CarrierAccountID: req.CarrierAccountId, Service: req.Service.Or(""),
		QuoteID: optUUID(req.QuoteId), WeightGrams: int(req.WeightGrams.Or(0)), LengthCm: int(req.LengthCm.Or(0)),
		WidthCm: int(req.WidthCm.Or(0)), HeightCm: int(req.HeightCm.Or(0)),
		DeclaredValue: int64(req.DeclaredValue.Or(0)), TrackingCode: req.TrackingCode.Or(""),
		Authorize: shipAuthorize(u, permShip, fulfillment.ErrOrderNotFound),
	}
	if o, ok := req.Options.Get(); ok {
		in.Options = carrierOptionsIn(o)
	}
	if f, ok := req.FeeActual.Get(); ok {
		fee := int64(f)
		in.FeeActual = &fee
	}
	return in
}

// exchangeCreditReady: đơn orderID không phải đơn đổi còn phải cấn trừ; còn → cash.ErrExchangeCreditPending.
func exchangeCreditReady(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) error {
	pending, err := cash.ExchangeCreditPending(ctx, tx, orderID)
	if err != nil {
		return fmt.Errorf("kiểm phiếu cấn trừ của đơn đổi: %w", err)
	}
	if pending {
		return cash.ErrExchangeCreditPending
	}
	return nil
}

// isCreateShipmentRefusal: từ chối của gửi hãng (không lưu theo khóa: gửi lại sau khi có phiếu cấn trừ thì qua).
func isCreateShipmentRefusal(err error) bool {
	return isShipmentRefusal(err) || errors.Is(err, cash.ErrExchangeCreditPending)
}

func isShipmentRefusal(err error) bool {
	_, _, ok := fulfillmentError(err)
	return ok
}

// GetShipment (GET /api/v1/shipments/{id}, fulfillment.read ở kho của tài khoản hãng của vận đơn): ShipmentView.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetShipment(ctx context.Context, params gen.GetShipmentParams) (gen.GetShipmentRes, error) {
	svc, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	var v fulfillment.ShipmentView
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		if v, err = svc.ShipmentView(ctx, tx, params.ID); err != nil {
			return err //nolint:wrapcheck // sentinel của fulfillment
		}
		return canAt(u, v.WarehouseID, permShipRead)
	}); err != nil {
		return nil, fmt.Errorf("đọc vận đơn: %w", err)
	}
	return shipmentViewOut(v), nil
}

// shipmentViewOut dựng thân Shipment (mọi trường M1 giữ tên; trường M3 tùy chọn, có khi có giá trị). Không ship_to.
func shipmentViewOut(v fulfillment.ShipmentView) *gen.Shipment {
	out := &gen.Shipment{
		ID: v.ID, OrderId: v.OrderID, Code: v.Code, CarrierKey: v.CarrierKey, CarrierAccountId: v.CarrierAccountID,
		Status: gen.ShipmentStatus(v.Status), CodAmount: gen.Money(v.CODAmount), CreatedAt: v.CreatedAt,
		AccountName: gen.NewOptString(v.AccountName), Mode: gen.NewOptCarrierAccountMode(gen.CarrierAccountMode(v.Mode)),
		Service: gen.NewOptString(v.Service), Label: gen.NewOptString(v.Label),
		SubStatus: gen.NewOptString(v.SubStatus), CreateState: gen.NewOptShipmentCreateState(gen.ShipmentCreateState(v.CreateState)),
		LastStatusAt: gen.NewOptDateTime(v.LastStatusAt), NeedsReview: gen.NewOptBool(v.NeedsReview),
		LabelReady: gen.NewOptBool(v.LabelReady), Partial: gen.NewOptBool(v.Partial), Version: gen.NewOptInt64(v.Version),
		Options: gen.NewOptCarrierOptions(carrierOptionsOut(v.Options)),
	}
	if v.TrackingCode != "" {
		out.TrackingCode = gen.NewOptString(v.TrackingCode)
	}
	if v.CancelKind != "" {
		out.CancelKind = gen.NewOptString(v.CancelKind)
	}
	if v.LastSyncError != "" {
		out.LastSyncError = gen.NewOptString(v.LastSyncError)
	}
	setMoney(&out.Fee, v.Fee)
	setMoney(&out.FeeActual, v.FeeActual)
	setMoney(&out.CodCollectedAmount, v.CODCollectedAmount)
	if v.WeightGrams != nil {
		out.WeightGrams = gen.NewOptInt32(int32(min(*v.WeightGrams, 1<<31-1))) //nolint:gosec // đã chặn
	}
	for dst, src := range map[*gen.OptDateTime]*time.Time{
		&out.PickedUpAt: v.PickedUpAt, &out.DeliveredAt: v.DeliveredAt, &out.LastCarrierEventAt: v.LastCarrierEventAt,
		&out.LastSyncedAt: v.LastSyncedAt, &out.StuckSince: v.StuckSince,
	} {
		if src != nil {
			*dst = gen.NewOptDateTime(*src)
		}
	}
	return out
}

func setMoney(dst *gen.OptMoney, v *int64) {
	if v != nil {
		*dst = gen.NewOptMoney(gen.Money(*v))
	}
}
