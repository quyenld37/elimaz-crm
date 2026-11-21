// Package fulfillment là vận chuyển (spec 05 mục 2). Bản M1 (kế hoạch M1, Task M1-31, lát mỏng Q-X1): tài khoản hãng,
// tạo vận đơn cho đơn (CreateForOrder: vận đơn creating, gắn làm vận đơn hiện hành của đơn, lệnh
// fulfillment.create_shipment — một transaction), lệnh tạo vận đơn có in_doubt (CreateCommand), webhook của hãng chỉ là
// tín hiệu (handler vào của vùng inbound, Task M3-10, ghi lệnh fulfillment.refresh_status), trạng thái thật đọc qua API
// của hãng (RefreshCommand)
// rồi áp lên đơn và tồn qua order. M3 mở rộng (hãng thật, hủy, hoàn, báo phí), không tạo lại.
//
// Chủ dữ liệu (Task M3-01): mọi bảng fulfillment.* (tài khoản hãng, vận đơn, sự kiện hãng, việc gọi lại và nhật ký,
// yêu cầu đổi địa chỉ, vận đơn cần xem, báo giá, lô tem, mã địa giới của hãng). Cột địa chỉ lấy hàng của
// inventory.warehouses thuộc inventory, cột số lượng tốt/hỏng/thiếu của ord.return_items thuộc order: migration M3 chỉ
// thêm cột, module chủ ghi.
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
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// RouteKindCarrierAccount là platform.tenant_routes.kind của webhook hãng: khóa là route_key của tài khoản hãng (Q-8).
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
}

// AuditFields là phần sau của dòng audit cho vận đơn: chỉ mã, trạng thái, số tiền.
func (s Shipment) AuditFields() map[string]any {
	return map[string]any{
		"code": s.Code, "orderId": s.OrderID.String(), "carrierAccountId": s.CarrierAccountID.String(),
		"status": string(s.Status), "codAmount": s.CODAmount, "version": s.Version,
	}
}

// SeedAccountInput là tài khoản hãng cần có (seed, test).
type SeedAccountInput struct {
	SellerID, WarehouseID                uuid.UUID
	CarrierKey, Name, ExternalAccountRef string
}

// EnsureCarrierAccount chèn tài khoản hãng của tenant trong ngữ cảnh tx (cần quyền INSERT: chủ bảng — crm_app chỉ đọc), hoặc
// trả mã của tài khoản đã có cùng (CarrierKey, ExternalAccountRef). Chạy lại được. Tuyến webhook: EnsureAccountRoute.
func EnsureCarrierAccount(ctx context.Context, tx pgx.Tx, in SeedAccountInput) (uuid.UUID, error) {
	id, err := store.New(tx).InsertCarrierAccount(ctx, store.InsertCarrierAccountParams{
		SellerID: in.SellerID, CarrierKey: in.CarrierKey, Name: in.Name, ExternalAccountRef: in.ExternalAccountRef,
		WarehouseID: in.WarehouseID,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("chèn tài khoản hãng: %w", err)
	}
	return id, nil
}

// EnsureAccountRoute đăng ký tuyến webhook carrier_account:<route_key> của tài khoản hãng id về tenant của tx
// (tenancy.PutRoute; seed, test dựng tài khoản bằng EnsureCarrierAccount); trả route_key. Chạy lại được. Không có tài
// khoản → ErrAccountNotFound.
func EnsureAccountRoute(ctx context.Context, tx pgx.Tx, id uuid.UUID) (string, error) {
	a, err := store.New(tx).GetAccount(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrAccountNotFound
	}
	if err != nil {
		return "", fmt.Errorf("đọc tài khoản hãng: %w", err)
	}
	if err := tenancy.PutRoute(ctx, tx, RouteKindCarrierAccount, a.RouteKey); err != nil {
		return "", fmt.Errorf("đăng ký tuyến webhook của tài khoản hãng: %w", err)
	}
	return a.RouteKey, nil
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
		CreatedAt: r.CreatedAt, WarehouseID: r.WarehouseID, SellerID: r.SellerID,
	}
	if r.Fee.Valid {
		fee := r.Fee.Int64
		s.Fee = &fee
	}
	return s, nil
}

// CreateForOrder (M1, giữ chữ ký) tạo vận đơn creating cho đơn orderID bằng tài khoản hãng accountID, trong tx của
// bên gọi: Service.Ship với giá trị mặc định (cân nặng theo cài đặt vận chuyển, dịch vụ và tùy chọn của tài khoản),
// không kiểm phạm vi kho, sự kiện order.updated ghi vào sổ sự kiện rỗng. Chỉ cho seed, test; lớp HTTP gọi Service.Ship
// (sổ sự kiện của tiến trình). Lỗi như Ship.
func CreateForOrder(ctx context.Context, tx pgx.Tx, orderID, accountID uuid.UUID, actor Actor) (Shipment, error) {
	svc := &Service{d: Deps{Queue: queue.NewQueue(), Events: events.NewRegistry(), Now: time.Now}}
	v, err := svc.Ship(ctx, tx, ShipInput{OrderID: orderID, CarrierAccountID: accountID},
		order.Actor{TenantID: actor.TenantID, UserID: actor.UserID, Type: "staff"})
	if err != nil {
		return Shipment{}, err
	}
	return Get(ctx, tx, v.ID)
}

// shipmentOrderKey: các lệnh của một vận đơn chạy lần lượt.
func shipmentOrderKey(id uuid.UUID) string { return "shipment:" + id.String() }
