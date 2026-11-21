package queries_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/catalogtest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/queries"
	"elimaz.com.vn/crm/internal/testkit"
)

// agedOrderItem chèn kho, khách, đơn mã code và một dòng đơn của biến thể v bằng chủ bảng (gói queries chỉ đọc);
// trả mã đơn và mã dòng đơn.
func agedOrderItem(t *testing.T, tx pgx.Tx, tenant, seller, v uuid.UUID, code string) (order, item uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	var wh uuid.UUID
	customer := uuid.New()
	order = uuid.New()
	if err := tx.QueryRow(ctx, `INSERT INTO inventory.warehouses (tenant_id, operated_by_seller_id, code, name)
		VALUES ($1, $2, $3, 'Kho') RETURNING id`, tenant, seller, "K"+code).Scan(&wh); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO customer.customers (id, tenant_id, root_id, full_name)
		VALUES ($1, $2, $1, 'Khách thử')`, customer, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ord.orders (id, tenant_id, seller_id, code, customer_id, channel,
		idempotency_key, status, ship_address, subtotal, cod_amount, grand_total, warehouse_id)
		VALUES ($1, $2, $3, $4, $5, 'manual', $1, 'confirmed', '{}', 0, 0, 0, $6)`,
		order, tenant, seller, code, customer, wh); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO ord.order_items (tenant_id, seller_id, order_id, position, variant_id,
		sku_snapshot, name_snapshot, quantity, unit_price, net_amount)
		VALUES ($1, $2, $3, 1, $4, 'SKU', 'Dòng thử', 1, 0, 0) RETURNING id`, tenant, seller, order, v).Scan(&item); err != nil {
		t.Fatal(err)
	}
	return order, item
}

// TestAgedRefs (Task M3-22): dòng đơn → mã đơn và số đơn; biến thể → SKU (mọi người bán của tenant); mã của tenant
// khác, mã không có → không có trong kết quả; danh sách rỗng → rỗng, không lỗi.
func TestAgedRefs(t *testing.T) {
	s := testkit.OpenStaff(t)
	a, b := s.Tenant(t, "A"), s.Tenant(t, "B")
	var vA, vP, vB, orderA, itemA, itemB uuid.UUID
	s.Exec(t, a, func(tx pgx.Tx) error {
		elimaz, partner := catalogtest.Sellers(t, tx, a)
		vA = catalogtest.VariantWithSKU(t, tx, a, elimaz, catalogtest.Product(t, tx, a, elimaz, "AO01"), "AO01-S", 1000)
		vP = catalogtest.VariantWithSKU(t, tx, a, partner, catalogtest.Product(t, tx, a, partner, "AO01"), "P-AO01-M", 1000)
		orderA, itemA = agedOrderItem(t, tx, a, elimaz, vA, "DH0000007")
		return nil
	})
	s.Exec(t, b, func(tx pgx.Tx) error {
		elimaz, _ := catalogtest.Sellers(t, tx, b)
		vB = catalogtest.VariantWithSKU(t, tx, b, elimaz, catalogtest.Product(t, tx, b, elimaz, "AO01"), "B-AO01", 1000)
		_, itemB = agedOrderItem(t, tx, b, elimaz, vB, "DH0000008")
		return nil
	})
	var orders map[uuid.UUID]queries.AgedOrderRef
	var skus map[uuid.UUID]string
	if err := tenancy.WithTenant(t.Context(), s.App, a, func(tx pgx.Tx) error {
		var err error
		orders, skus, err = queries.AgedRefs(t.Context(), tx, []uuid.UUID{itemA, itemB, uuid.New()}, []uuid.UUID{vA, vP, vB})
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 || orders[itemA] != (queries.AgedOrderRef{OrderID: orderA, Code: "DH0000007"}) {
		t.Errorf("đơn: %+v", orders)
	}
	if len(skus) != 2 || skus[vA] != "AO01-S" || skus[vP] != "P-AO01-M" {
		t.Errorf("SKU: %+v", skus)
	}
	if err := tenancy.WithTenant(t.Context(), s.App, a, func(tx pgx.Tx) error {
		var err error
		orders, skus, err = queries.AgedRefs(t.Context(), tx, nil, nil)
		return err //nolint:wrapcheck // test
	}); err != nil || len(orders) != 0 || len(skus) != 0 {
		t.Errorf("rỗng: %v %v %v", orders, skus, err)
	}
}
