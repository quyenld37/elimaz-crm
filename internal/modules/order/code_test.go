package order_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// twoSellerOrders tạo một đơn của ELIMAZ rồi một đơn của PARTNER1 (e.seller thành PARTNER1).
func (e *orderEnv) twoSellerOrders() (order.Order, order.Order) {
	e.t.Helper()
	mine := e.mustCreate(e.input(0, order.ItemInput{VariantID: e.variant(1000, 5), Qty: 1}))
	e.seller = e.partner
	return mine, e.mustCreate(e.input(0, order.ItemInput{VariantID: e.variant(1000, 5), Qty: 1}))
}

// Codes trả mã đơn theo id của đúng người bán (ADR-29); đơn của người bán khác, id không có, tenant khác bị bỏ; danh
// sách rỗng → map rỗng.
func TestCodes(t *testing.T) {
	e := newOrderEnv(t)
	mine, partner := e.twoSellerOrders()
	codes := func(tenant, seller uuid.UUID, ids []uuid.UUID) map[uuid.UUID]string {
		t.Helper()
		var m map[uuid.UUID]string
		e.staff.Exec(t, tenant, func(tx pgx.Tx) (err error) {
			m, err = order.Codes(t.Context(), tx, seller, ids)
			return err //nolint:wrapcheck // test
		})
		return m
	}
	ids := []uuid.UUID{mine.ID, partner.ID, uuid.New()}
	var got map[uuid.UUID]string
	if err := e.app(func(tx pgx.Tx) (err error) {
		got, err = order.Codes(t.Context(), tx, e.shop.Seller, ids)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[mine.ID] != mine.Code {
		t.Fatalf("mã đơn của người bán ELIMAZ: %v", got)
	}
	if m := codes(e.tenant, e.shop.Partner, ids); len(m) != 1 || m[partner.ID] != partner.Code {
		t.Fatalf("mã đơn của PARTNER1: %v", m)
	}
	if m := codes(e.staff.Tenant(t, "B"), e.shop.Seller, ids); len(m) != 0 {
		t.Fatalf("tenant khác: %v", m)
	}
	if m := codes(e.tenant, e.shop.Seller, nil); m == nil || len(m) != 0 {
		t.Fatalf("rỗng: %v", m)
	}
}

// ItemOrders trả đơn theo mã dòng đơn của đúng người bán (ADR-29); dòng của người bán khác, mã không có, tenant khác bị
// bỏ; danh sách rỗng → map rỗng.
func TestItemOrders(t *testing.T) {
	e := newOrderEnv(t)
	mine, partner := e.twoSellerOrders()
	items := []uuid.UUID{mine.Items[0].ID, partner.Items[0].ID, uuid.New()}
	get := func(tenant, seller uuid.UUID, ids []uuid.UUID) map[uuid.UUID]order.ItemOrder {
		t.Helper()
		var m map[uuid.UUID]order.ItemOrder
		if err := tenancy.WithTenant(t.Context(), e.staff.App, tenant, func(tx pgx.Tx) (err error) {
			m, err = order.ItemOrders(t.Context(), tx, seller, ids)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
		return m
	}
	if m := get(e.tenant, e.shop.Seller, items); len(m) != 1 ||
		m[mine.Items[0].ID] != (order.ItemOrder{OrderID: mine.ID, Code: mine.Code}) {
		t.Fatalf("ELIMAZ: %v", m)
	}
	if m := get(e.tenant, e.shop.Partner, items); len(m) != 1 ||
		m[partner.Items[0].ID] != (order.ItemOrder{OrderID: partner.ID, Code: partner.Code}) {
		t.Fatalf("PARTNER1: %v", m)
	}
	if m := get(e.staff.Tenant(t, "B"), e.shop.Seller, items); len(m) != 0 {
		t.Fatalf("tenant khác: %v", m)
	}
	if m := get(e.tenant, e.shop.Seller, nil); m == nil || len(m) != 0 {
		t.Fatalf("rỗng: %v", m)
	}
}
