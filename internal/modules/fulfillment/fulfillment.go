// Package fulfillment là vận chuyển (spec 05 mục 2). Bản M1 (kế hoạch M1, Task M1-31, lát mỏng Q-X1): tài khoản hãng,
// tạo vận đơn cho đơn (CreateForOrder: vận đơn creating, gắn làm vận đơn hiện hành của đơn, lệnh
// fulfillment.create_shipment — một transaction), lệnh tạo vận đơn có in_doubt (CreateCommand), webhook của hãng chỉ là
// tín hiệu (SignalHandler ghi lệnh fulfillment.refresh_status), trạng thái thật đọc qua API của hãng (RefreshCommand)
// rồi áp lên đơn và tồn qua order. M3 mở rộng (hãng thật, hủy, hoàn, báo phí), không tạo lại.
//
// Gọi hãng chỉ ở Execute/Lookup của lệnh (ngoài transaction), qua CarrierAdapter; bản gọi mạng ở
// internal/adapters/<hãng>. Module không nhập net/http.
//
// Thứ tự khóa (docs/adr/0046-thu-tu-khoa.md): tạo vận đơn khóa ord.orders (nhóm 7) trước rồi chèn vận đơn (nhóm 8);
// đường bắt đầu từ vận đơn (tra trạng thái) khóa đơn trước (order.Lock) rồi fulfillment.shipments, chèn
// shipment_events, rồi số dư (nhóm 11, trong order.Writer.ApplyShipmentProgress).
//
// Khi nào không dùng: module khác không đọc bảng fulfillment.* (đọc view fulfillment.shipments_v); không đổi trạng
// thái vận đơn bằng SQL ngoài gói này (mất sự kiện, mất áp lên đơn).
package fulfillment

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// RouteKindCarrierAccount là platform.tenant_routes.kind của webhook hãng: khóa là mã tài khoản ở hãng.
const RouteKindCarrierAccount = "carrier_account"

// codeAttempts: số lần sinh mã khi trùng unique.
const codeAttempts = 3

// Actor là nhân viên tạo vận đơn (uuid.Nil → không ghi created_by). fulfillment không nhập identity.
type Actor struct {
	TenantID, UserID uuid.UUID
}

// Shipment là một vận đơn kèm kho, người bán của tài khoản hãng (để kiểm quyền theo kho).
type Shipment struct {
	ID, OrderID, CarrierAccountID uuid.UUID
	Code, CarrierKey              string
	// TrackingCode rỗng khi hãng chưa trả mã.
	TrackingCode string
	Status       Status
	CODAmount    int64
	// Fee nil khi chưa biết phí.
	Fee                   *int64
	Version               int64
	CreatedAt             time.Time
	WarehouseID, SellerID uuid.UUID
	accountRef            string
}

// AuditFields là phần sau của dòng audit cho vận đơn: chỉ mã, trạng thái, số tiền.
func (s Shipment) AuditFields() map[string]any {
	return map[string]any{
		"code": s.Code, "orderId": s.OrderID.String(), "carrierAccountId": s.CarrierAccountID.String(),
		"status": string(s.Status), "codAmount": s.CODAmount, "version": s.Version,
	}
}

// AccountInput là tài khoản hãng cần có (seed, test).
type AccountInput struct {
	SellerID, WarehouseID                uuid.UUID
	CarrierKey, Name, ExternalAccountRef string
}

// EnsureCarrierAccount chèn tài khoản hãng của tenant trong ngữ cảnh tx (cần quyền INSERT: chủ bảng — crm_app chỉ đọc), hoặc
// trả mã của tài khoản đã có cùng (CarrierKey, ExternalAccountRef). Chạy lại được.
func EnsureCarrierAccount(ctx context.Context, tx pgx.Tx, in AccountInput) (uuid.UUID, error) {
	id, err := store.New(tx).InsertCarrierAccount(ctx, store.InsertCarrierAccountParams{
		SellerID: in.SellerID, CarrierKey: in.CarrierKey, Name: in.Name, ExternalAccountRef: in.ExternalAccountRef,
		WarehouseID: in.WarehouseID,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("chèn tài khoản hãng: %w", err)
	}
	return id, nil
}

// AccountWarehouse trả kho của tài khoản hãng id (đích quyền fulfillment.ship); không có → ErrAccountNotFound.
func AccountWarehouse(ctx context.Context, tx pgx.Tx, id uuid.UUID) (uuid.UUID, error) {
	a, err := account(ctx, store.New(tx), id)
	if err != nil {
		return uuid.Nil, err
	}
	return a.WarehouseID, nil
}

func account(ctx context.Context, q *store.Queries, id uuid.UUID) (store.GetCarrierAccountRow, error) {
	a, err := q.GetCarrierAccount(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrAccountNotFound
	}
	if err != nil {
		return a, fmt.Errorf("đọc tài khoản hãng: %w", err)
	}
	return a, nil
}

// Get trả vận đơn id; không có (hay tenant khác) → ErrNotFound.
func Get(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Shipment, error) {
	r, err := store.New(tx).GetShipment(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Shipment{}, ErrNotFound
	}
	if err != nil {
		return Shipment{}, fmt.Errorf("đọc vận đơn: %w", err)
	}
	s := Shipment{
		ID: r.ID, OrderID: r.OrderID, CarrierAccountID: r.CarrierAccountID, Code: r.Code, CarrierKey: r.CarrierKey,
		TrackingCode: r.TrackingCode.String, Status: Status(r.Status), CODAmount: r.CodAmount, Version: r.Version,
		CreatedAt: r.CreatedAt, WarehouseID: r.WarehouseID, SellerID: r.SellerID, accountRef: r.ExternalAccountRef,
	}
	if r.Fee.Valid {
		fee := r.Fee.Int64
		s.Fee = &fee
	}
	return s, nil
}

// CreateForOrder tạo vận đơn creating cho đơn orderID bằng tài khoản hãng accountID, trong tx của bên gọi: khóa đơn
// (nhóm 7), đọc đơn theo người bán của tài khoản (lấy COD), chèn vận đơn (mã NewShipmentCode, trùng mã thì sinh lại tối
// đa 3 lần), gắn làm vận đơn hiện hành (order.AttachShipment), ghi lệnh fulfillment.create_shipment. Lỗi: tài khoản
// không có hay đã tắt → ErrAccountNotFound; đơn đã có vận đơn hiện hành hay vận đơn chưa hủy →
// ErrShipmentExists; đơn không có, của người bán khác, giữ hàng ở kho khác kho của tài khoản, không ở confirmed →
// ErrOrderNotShippable.
func CreateForOrder(ctx context.Context, tx pgx.Tx, orderID, accountID uuid.UUID, actor Actor) (Shipment, error) {
	q := store.New(tx)
	a, err := account(ctx, q, accountID)
	if err != nil {
		return Shipment{}, err
	}
	if a.Status != "active" {
		return Shipment{}, ErrAccountNotFound
	}
	if err := order.Lock(ctx, tx, a.SellerID, orderID); err != nil {
		if errors.Is(err, order.ErrNotFound) {
			return Shipment{}, ErrOrderNotShippable
		}
		return Shipment{}, fmt.Errorf("khóa đơn: %w", err)
	}
	o, err := order.Get(ctx, tx, a.SellerID, orderID)
	if errors.Is(err, order.ErrNotFound) {
		return Shipment{}, ErrOrderNotShippable
	}
	if err != nil {
		return Shipment{}, fmt.Errorf("đọc đơn: %w", err)
	}
	if o.CurrentShipmentID != nil {
		return Shipment{}, ErrShipmentExists
	}
	if o.Status != order.StatusConfirmed || o.WarehouseID != a.WarehouseID {
		// Kho của tài khoản hãng phải là kho giữ hàng của đơn: hãng lấy ở kho tài khoản, tồn trừ ở kho giữ hàng (ADR-18).
		return Shipment{}, ErrOrderNotShippable
	}
	s, err := insertShipment(ctx, q, o, a, actor)
	if err != nil {
		return Shipment{}, err
	}
	if err := order.AttachShipment(ctx, tx, a.SellerID, orderID, s.ID); err != nil {
		if errors.Is(err, order.ErrShipmentAlreadyAttached) {
			return Shipment{}, ErrShipmentExists
		}
		return Shipment{}, fmt.Errorf("gắn vận đơn vào đơn: %w", err)
	}
	if _, _, err := queue.NewQueue().Enqueue(ctx, tx, CreateCommandKind(), queue.NewJob{
		TenantID: &actor.TenantID, Payload: commandPayload{ShipmentID: s.ID},
		DedupeKey: "create:" + s.ID.String(), OrderKey: shipmentOrderKey(s.ID),
	}); err != nil {
		return Shipment{}, fmt.Errorf("ghi lệnh tạo vận đơn: %w", err)
	}
	return s, nil
}

// insertShipment chèn vận đơn creating; không dòng nào: đơn đã có vận đơn chưa hủy → ErrShipmentExists, ngược lại mã
// trùng → sinh lại.
func insertShipment(ctx context.Context, q *store.Queries, o order.Order, a store.GetCarrierAccountRow, actor Actor,
) (Shipment, error) {
	for range codeAttempts {
		id, err := uuid.NewV7()
		if err != nil {
			return Shipment{}, fmt.Errorf("sinh mã vận đơn: %w", err)
		}
		code := NewShipmentCode(time.Now(), rand.Reader)
		created, err := q.InsertShipment(ctx, store.InsertShipmentParams{
			ID: id, Code: code, OrderID: o.ID, CarrierKey: a.CarrierKey, CarrierAccountID: a.ID, CodAmount: o.CODAmount,
			CreatedBy: pgtype.UUID{Bytes: actor.UserID, Valid: actor.UserID != uuid.Nil},
		})
		if err == nil {
			return Shipment{
				ID: id, OrderID: o.ID, CarrierAccountID: a.ID, Code: code, CarrierKey: a.CarrierKey, Status: StatusCreating,
				CODAmount: o.CODAmount, Version: 1, CreatedAt: created, WarehouseID: a.WarehouseID, SellerID: a.SellerID,
				accountRef: a.ExternalAccountRef,
			}, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return Shipment{}, fmt.Errorf("chèn vận đơn: %w", err)
		}
		active, err := q.HasActiveShipment(ctx, o.ID)
		if err != nil {
			return Shipment{}, fmt.Errorf("kiểm vận đơn của đơn: %w", err)
		}
		if active {
			return Shipment{}, ErrShipmentExists
		}
	}
	return Shipment{}, errCodeExhausted
}

// shipmentOrderKey: các lệnh của một vận đơn chạy lần lượt.
func shipmentOrderKey(id uuid.UUID) string { return "shipment:" + id.String() }
