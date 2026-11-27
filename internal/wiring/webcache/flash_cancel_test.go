package webcache_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/wiring/webcache"
)

// order.cancelled (M5 Task 202): đơn có dòng flash sale → đúng một báo hiệu flash_item mang mã món (tra bằng
// order.FlashItemOf trong tx của sự kiện); đơn thường → không báo hiệu.
func TestConsumer_OrderCancelledFlash(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	tenant := e.staff.Tenant(t, "A")
	flashOrder, plainOrder, item := seedFlashCancelled(t, e, tenant)
	for _, c := range []struct {
		id   uuid.UUID
		want []realtime.Entity
	}{
		{flashOrder, []realtime.Entity{{Type: webcache.EntityFlashItem, ID: item.String()}}},
		{plainOrder, nil},
	} {
		bus := &realtimetest.RecordingBus{}
		con := webcache.NewConsumer(bus, nil)
		ev := stored(t, tenant, "order.cancelled", "order", c.id.String(), order.Changed{OrderID: c.id, Status: "cancelled"})
		if err := tenancy.WithTenant(ctx, e.staff.App, tenant, func(tx pgx.Tx) error {
			return con.Handle(ctx, ev, tx)
		}); err != nil {
			t.Fatal(err)
		}
		var got []realtime.Entity
		for _, s := range bus.Signals() {
			if !s.System || s.Topic != webcache.TopicInvalidate || s.TenantID != tenant {
				t.Fatalf("báo hiệu sai dạng: %+v", s)
			}
			got = append(got, s.Entity)
		}
		if !slices.Equal(got, c.want) {
			t.Fatalf("đơn %s: báo hiệu %v, muốn %v", c.id, got, c.want)
		}
	}
}

// seedFlashCancelled chèn bằng owner một đợt flash một món và hai đơn đã hủy: một có dòng flash, một dòng thường.
func seedFlashCancelled(t *testing.T, e *env, tenant uuid.UUID) (flashOrder, plainOrder, item uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	e.staff.Exec(t, tenant, func(tx pgx.Tx) error {
		shop := invtest.NewShop(t, tx, tenant)
		v := shop.Variant(t, tx, shop.Seller, 350000, 5)
		var sale, cust uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO promotion.flash_sales (tenant_id, name, starts_at, ends_at, created_by)
			VALUES ($1, 'Đợt', now(), now() + interval '1 hour', $2) RETURNING id`, tenant, uuid.New()).Scan(&sale); err != nil {
			return err //nolint:wrapcheck // test
		}
		if err := tx.QueryRow(ctx, `INSERT INTO promotion.flash_sale_items (tenant_id, flash_sale_id, seller_id, variant_id,
			warehouse_id, sale_price, quantity) VALUES ($1, $2, $3, $4, $5, 199000, 3) RETURNING id`,
			tenant, sale, shop.Seller, v, shop.Warehouse).Scan(&item); err != nil {
			return err //nolint:wrapcheck // test
		}
		if err := tx.QueryRow(ctx, `INSERT INTO customer.customers (id, tenant_id, root_id, full_name)
			VALUES (uuidv7(), $1, NULL, 'Khách') RETURNING id`, tenant).Scan(&cust); err != nil {
			return err //nolint:wrapcheck // test
		}
		for i, fi := range []any{item, nil} {
			o, err := insertCancelledOrder(ctx, tx, tenant, shop.Seller, cust, shop.Warehouse, v, fi)
			if err != nil {
				return err
			}
			if i == 0 {
				flashOrder = o
			} else {
				plainOrder = o
			}
		}
		return nil
	})
	return flashOrder, plainOrder, item
}

// insertCancelledOrder chèn bằng owner một đơn web đã hủy một dòng (fi: món flash, nil: dòng thường).
func insertCancelledOrder(ctx context.Context, tx pgx.Tx, tenant, seller, cust, wh, v uuid.UUID, fi any) (uuid.UUID, error) {
	var o uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO ord.orders (id, tenant_id, seller_id, code, customer_id, channel,
		idempotency_key, status, ship_address, subtotal, cod_amount, grand_total, warehouse_id)
		VALUES (uuidv7(), $1, $2, 'DH' || lpad(nextval('ord.order_code_seq')::text, 7, '0'), $3, 'web', gen_random_uuid(),
		'cancelled', '{}', 199000, 199000, 199000, $4) RETURNING id`, tenant, seller, cust, wh).Scan(&o); err != nil {
		return uuid.Nil, err //nolint:wrapcheck // test
	}
	_, err := tx.Exec(ctx, `INSERT INTO ord.order_items (tenant_id, seller_id, order_id, position, variant_id,
		sku_snapshot, name_snapshot, quantity, unit_price, net_amount, flash_sale_item_id)
		VALUES ($1, $2, $3, 1, $4, 'SKU', 'Áo', 1, 199000, 199000, $5)`, tenant, seller, o, v, fi)
	return o, err //nolint:wrapcheck // test
}
