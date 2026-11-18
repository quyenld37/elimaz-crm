// Package shiptest là helper test cho phần bán và giao hàng (vận chuyển, sổ COD, test đầu cuối): cửa hàng có tài khoản
// hãng giả fake-1 ở kho MAIN, đơn tay của một khách mới, vận đơn của đơn đó. Gom phần dựng đang lặp ở test của
// fulfillment và cash (Task M1-33).
//
// Khi nào không dùng: mã sản phẩm (chỉ dùng trong test); test chỉ cần tồn kho (dùng invtest); đường đi qua HTTP (gọi
// API thật). Nằm ngoài internal/modules vì nhập customer, order, inventory/invtest mà module fulfillment không được gọi.
package shiptest

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

const (
	// CarrierKey là carrier_key của hãng giả (trùng fakecarrier.CarrierKey và hãng giả trong bộ nhớ của fulfillment).
	CarrierKey = "fake"
	// AccountRef là mã tài khoản hãng giả (như seed-dev), cũng là khóa tuyến carrier_account.
	AccountRef = "fake-1"
)

// Shop là invtest.Shop cộng tài khoản hãng giả fake-1 ở kho MAIN của người bán ELIMAZ.
type Shop struct {
	*invtest.Shop
	Account uuid.UUID
}

// NewShop dựng Shop cho tenant tenantID trong tx (chủ bảng, tenancy.WithTenant của tenantID; ví dụ Staff.Exec). Lỗi
// làm test dừng (chỉ gọi từ goroutine của test).
func NewShop(t testing.TB, tx pgx.Tx, tenantID uuid.UUID) *Shop {
	t.Helper()
	s := &Shop{Shop: invtest.NewShop(t, tx, tenantID)}
	var err error
	s.Account, err = fulfillment.EnsureCarrierAccount(t.Context(), tx, fulfillment.AccountInput{
		SellerID: s.Seller, WarehouseID: s.Warehouse, CarrierKey: CarrierKey, Name: "Hãng giả",
		ExternalAccountRef: AccountRef,
	})
	if err != nil {
		t.Fatalf("shiptest: tài khoản hãng giả: %v", err)
	}
	return s
}

// Order tạo khách "Chị Lan" và đơn tay của người bán ELIMAZ (phí ship shippingFee, các dòng items) trong tx (crm_app,
// tenancy.WithTenant của tenant của Shop), tác nhân userID; sự kiện ghi vào sổ rỗng (không consumer nào).
func (s *Shop) Order(ctx context.Context, tx pgx.Tx, userID uuid.UUID, shippingFee int64, items ...order.ItemInput) (order.Order, error) {
	c, err := customer.Create(ctx, tx, "Chị Lan")
	if err != nil {
		return order.Order{}, fmt.Errorf("shiptest: tạo khách: %w", err)
	}
	w := order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &testkit.RecordingBus{}}
	o, err := w.CreateManual(ctx, tx, order.CreateManualInput{
		IdempotencyKey: uuid.New(), SellerID: s.Seller, CustomerID: c, ShippingFee: shippingFee, Items: items,
		ShipAddress: order.ShipAddress{Name: "Chị Lan", Phone: logging.Phone("0901234567"), Line: "12 Lê Lợi, Q1"},
	}, order.Actor{TenantID: s.Tenant, UserID: userID})
	if err != nil {
		return order.Order{}, fmt.Errorf("shiptest: tạo đơn: %w", err)
	}
	return o, nil
}

// Shipment tạo vận đơn (trạng thái creating, lệnh tạo trong hàng chờ) của đơn orderID bằng tài khoản hãng giả, trong
// tx (crm_app), tác nhân userID.
func (s *Shop) Shipment(ctx context.Context, tx pgx.Tx, orderID, userID uuid.UUID) (fulfillment.Shipment, error) {
	sh, err := fulfillment.CreateForOrder(ctx, tx, orderID, s.Account, fulfillment.Actor{TenantID: s.Tenant, UserID: userID})
	if err != nil {
		return fulfillment.Shipment{}, fmt.Errorf("shiptest: tạo vận đơn: %w", err)
	}
	return sh, nil
}

// CODBalances đọc số dư từng ngăn sổ COD của vận đơn shipmentID qua cash.cod_balances_v trong tx (tenancy.WithTenant
// của tenant; crm_app hay chủ bảng); ngăn số dư 0 vẫn có mặt nếu đã có dòng sổ.
func CODBalances(ctx context.Context, tx pgx.Tx, shipmentID uuid.UUID) (map[string]int64, error) {
	rows, err := tx.Query(ctx, "SELECT account, balance FROM cash.cod_balances_v WHERE shipment_id = $1", shipmentID)
	if err != nil {
		return nil, fmt.Errorf("shiptest: đọc số dư COD: %w", err)
	}
	out := map[string]int64{}
	var (
		a string
		b int64
	)
	if _, err := pgx.ForEachRow(rows, []any{&a, &b}, func() error { out[a] = b; return nil }); err != nil {
		return nil, fmt.Errorf("shiptest: đọc số dư COD: %w", err)
	}
	return out, nil
}
