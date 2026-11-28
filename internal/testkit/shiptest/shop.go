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
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
)

const (
	// CarrierKey là carrier_key của hãng giả (trùng fake.CarrierKey của internal/adapters/carrier/fake và hãng giả trong bộ nhớ của fulfillment).
	CarrierKey = "fake"
	// AccountRef là mã tài khoản hãng giả (như seed-dev), cũng là khóa tuyến carrier_account.
	AccountRef = "fake-1"
	// PartnerAccountRef là mã tài khoản hãng giả của người bán đối tác PARTNER1.
	PartnerAccountRef = "fake-2"
)

// Shop là invtest.Shop cộng tài khoản hãng giả ở kho MAIN cho từng người bán: fake-1 của ELIMAZ (Account), fake-2 của
// PARTNER1 (PartnerAccount) — vận đơn dùng tài khoản của người bán của đơn (ADR-29).
type Shop struct {
	*invtest.Shop
	Account, PartnerAccount uuid.UUID
}

// NewShop dựng Shop cho tenant tenantID trong tx (chủ bảng, tenancy.WithTenant của tenantID; ví dụ Staff.Exec). Lỗi
// làm test dừng (chỉ gọi từ goroutine của test).
func NewShop(t testing.TB, tx pgx.Tx, tenantID uuid.UUID) *Shop {
	t.Helper()
	s := &Shop{Shop: invtest.NewShop(t, tx, tenantID)}
	for _, a := range []struct {
		dst    *uuid.UUID
		seller uuid.UUID
		ref    string
	}{{&s.Account, s.Seller, AccountRef}, {&s.PartnerAccount, s.Partner, PartnerAccountRef}} {
		id, err := fulfillment.EnsureCarrierAccount(t.Context(), tx, fulfillment.SeedAccountInput{
			SellerID: a.seller, WarehouseID: s.Warehouse, CarrierKey: CarrierKey, Name: "Hãng giả", ExternalAccountRef: a.ref,
		})
		if err != nil {
			t.Fatalf("shiptest: tài khoản hãng giả %s: %v", a.ref, err)
		}
		*a.dst = id
	}
	return s
}

// AccountOf trả tài khoản hãng giả của người bán seller (Seller hay Partner của Shop).
func (s *Shop) AccountOf(seller uuid.UUID) uuid.UUID {
	if seller == s.Partner {
		return s.PartnerAccount
	}
	return s.Account
}

// ManualAccount tạo tài khoản hãng key mode manual, active (nhập mã tay hay tự giao, Task M3-15) của người bán seller ở
// kho warehouse, mã ở hãng ref, trong tx (chủ bảng, tenancy.WithTenant của tenant). Lỗi làm test dừng.
func ManualAccount(t testing.TB, tx pgx.Tx, seller, warehouse uuid.UUID, key, ref string) uuid.UUID {
	t.Helper()
	id, err := fulfillment.EnsureCarrierAccount(t.Context(), tx, fulfillment.SeedAccountInput{
		SellerID: seller, WarehouseID: warehouse, CarrierKey: key, Name: "Tay " + ref, ExternalAccountRef: ref,
	})
	if err != nil {
		t.Fatalf("shiptest: tài khoản tay %s: %v", ref, err)
	}
	if _, err := tx.Exec(t.Context(), "UPDATE fulfillment.carrier_accounts SET mode = 'manual', status = 'active' WHERE id = $1", id); err != nil {
		t.Fatalf("shiptest: đổi tài khoản %s sang mode manual: %v", ref, err)
	}
	return id
}

// Order tạo khách "Chị Lan" và đơn tay của người bán seller (phí ship shippingFee, các dòng items) trong tx (crm_app,
// tenancy.WithTenant của tenant của Shop), tác nhân userID; sự kiện ghi vào sổ rỗng (không consumer nào).
func (s *Shop) Order(ctx context.Context, tx pgx.Tx, seller, userID uuid.UUID, shippingFee int64, items ...order.ItemInput,
) (order.Order, error) {
	return s.OrderTo(ctx, tx, seller, userID, shippingFee,
		order.ShipAddress{Name: "Chị Lan", Phone: logging.Phone("0901234567"), Line: "12 Lê Lợi, Q1"}, items...)
}

// OrderTo như Order với địa chỉ giao addr (khách mới tên người nhận của addr): đơn cần địa chỉ đã khớp mã địa giới của
// hãng (hãng giả thật của e2e, Task M3-48).
func (s *Shop) OrderTo(ctx context.Context, tx pgx.Tx, seller, userID uuid.UUID, shippingFee int64, addr order.ShipAddress,
	items ...order.ItemInput,
) (order.Order, error) {
	c, err := customer.Create(ctx, tx, addr.Name)
	if err != nil {
		return order.Order{}, fmt.Errorf("shiptest: tạo khách: %w", err)
	}
	w := order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &realtimetest.RecordingBus{}}
	o, err := w.CreateManual(ctx, tx, order.CreateManualInput{
		IdempotencyKey: uuid.New(), SellerID: seller, CustomerID: c, ShippingFee: shippingFee, Items: items,
		ShipAddress: addr,
	}, order.Actor{TenantID: s.Tenant, UserID: userID})
	if err != nil {
		return order.Order{}, fmt.Errorf("shiptest: tạo đơn: %w", err)
	}
	return o, nil
}

// Shipment tạo vận đơn (trạng thái creating, lệnh tạo trong hàng chờ) của đơn orderID của người bán seller bằng tài
// khoản hãng giả của người bán đó, trong tx (crm_app), tác nhân userID. Cân nặng cố định (shipWeightGrams), không đọc
// cài đặt vận chuyển: test dữ liệu M1 dựng vận đơn trên schema chưa có platform.settings (trước 0304).
func (s *Shop) Shipment(ctx context.Context, tx pgx.Tx, seller, orderID, userID uuid.UUID) (fulfillment.Shipment, error) {
	svc := fulfillment.New(fulfillment.Deps{Queue: queue.NewQueue(), Events: events.NewRegistry()}).Service
	v, err := svc.Ship(ctx, tx, fulfillment.ShipInput{
		OrderID: orderID, CarrierAccountID: s.AccountOf(seller), WeightGrams: shipWeightGrams,
	}, order.Actor{TenantID: s.Tenant, UserID: userID, Type: "staff"})
	if err != nil {
		return fulfillment.Shipment{}, fmt.Errorf("shiptest: tạo vận đơn: %w", err)
	}
	sh, err := fulfillment.Get(ctx, tx, v.ID)
	if err != nil {
		return fulfillment.Shipment{}, fmt.Errorf("shiptest: đọc vận đơn: %w", err)
	}
	return sh, nil
}

// shipWeightGrams là cân nặng kiện của vận đơn thử.
const shipWeightGrams = 500

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

// Children là mã các việc con của một vận đơn mà API gọi theo mã riêng (Task 527, kiểm người bán ADR-29).
type Children struct {
	Callback, Anomaly, AddressChange, LabelBatch uuid.UUID
}

// ChildrenOf chèn (chủ bảng, trong tx tenancy.WithTenant của tenant) một việc gọi lại mở, một việc cần xem
// invalid_edge mở, một yêu cầu đổi địa chỉ tay pending (người yêu cầu userID) và một lô nhãn pending của tài khoản hãng
// của vận đơn shipmentID (đơn orderID); trả mã từng việc. Lỗi làm test dừng.
func ChildrenOf(t testing.TB, tx pgx.Tx, tenantID, shipmentID, orderID, userID uuid.UUID) Children {
	t.Helper()
	ctx := t.Context()
	var c Children
	for _, r := range []struct {
		dst  *uuid.UUID
		sql  string
		args []any
	}{
		{&c.Callback, `INSERT INTO fulfillment.callback_tasks (tenant_id, shipment_id, order_id, attempt_no, failed_at, due_at)
			VALUES ($1, $2, $3, 1, now(), now()) RETURNING id`, []any{tenantID, shipmentID, orderID}},
		{&c.Anomaly, `INSERT INTO fulfillment.shipment_anomalies (tenant_id, shipment_id, kind)
			VALUES ($1, $2, 'invalid_edge') RETURNING id`, []any{tenantID, shipmentID}},
		{&c.AddressChange, `INSERT INTO fulfillment.address_change_requests (tenant_id, shipment_id, new_address, mode,
			status, requested_by) VALUES ($1, $2, '{}', 'manual', 'pending', $3) RETURNING id`, []any{tenantID, shipmentID, userID}},
		{&c.LabelBatch, `INSERT INTO fulfillment.label_batches (tenant_id, carrier_account_id, shipment_ids, status,
			created_by) SELECT $1, carrier_account_id, ARRAY[$2::uuid], 'pending', $3 FROM fulfillment.shipments
			WHERE id = $2 RETURNING id`, []any{tenantID, shipmentID, userID}},
	} {
		if err := tx.QueryRow(ctx, r.sql, r.args...).Scan(r.dst); err != nil {
			t.Fatalf("shiptest: việc con của vận đơn: %v", err)
		}
	}
	return c
}
