package queries_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/queries"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// pickingFixture: tenant A có đơn 1 (vA × 1, vB × 2) và đơn 2 (vB × 1) cùng vận đơn s1, s2; vận đơn đã hủy cancelled
// của đơn 1; tenant B rỗng.
type pickingFixture struct {
	s                 *testkit.Staff
	a, b              uuid.UUID
	shop              *shiptest.Shop
	vB                uuid.UUID
	o1, o2            order.Order
	s1, s2, cancelled uuid.UUID
}

func newPickingFixture(t *testing.T) *pickingFixture {
	t.Helper()
	f := &pickingFixture{s: testkit.OpenStaff(t)}
	f.a, f.b = f.s.Tenant(t, "A"), f.s.Tenant(t, "B")
	user := f.s.User(t, f.a, "kho@elimaz.test", "active")
	var vA uuid.UUID
	f.s.Exec(t, f.a, func(tx pgx.Tx) error {
		f.shop = shiptest.NewShop(t, tx, f.a)
		vA = f.shop.Variant(t, tx, f.shop.Seller, 100000, 10)
		f.vB = f.shop.Variant(t, tx, f.shop.Seller, 200000, 10)
		_, err := tx.Exec(t.Context(), `UPDATE catalog.variants SET color = 'Đen', size = 'M' WHERE id = $1`, f.vB)
		return err //nolint:wrapcheck // test
	})
	if err := tenancy.WithTenant(t.Context(), f.s.App, f.a, func(tx pgx.Tx) error {
		var err error
		if f.o1, err = f.shop.Order(t.Context(), tx, f.shop.Seller, user, 30000,
			order.ItemInput{VariantID: vA, Qty: 1}, order.ItemInput{VariantID: f.vB, Qty: 2}); err != nil {
			return err //nolint:wrapcheck // test
		}
		if f.o2, err = f.shop.Order(t.Context(), tx, f.shop.Seller, user, 0, order.ItemInput{VariantID: f.vB, Qty: 1}); err != nil {
			return err //nolint:wrapcheck // test
		}
		sh1, err := f.shop.Shipment(t.Context(), tx, f.shop.Seller, f.o1.ID, user)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		sh2, err := f.shop.Shipment(t.Context(), tx, f.shop.Seller, f.o2.ID, user)
		f.s1, f.s2 = sh1.ID, sh2.ID
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	// Vận đơn đã hủy của đơn 1 (chủ bảng): chọn cả hai vận đơn của đơn 1 không đếm hàng hai lần.
	f.s.Exec(t, f.a, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `INSERT INTO fulfillment.shipments (tenant_id, code, order_id, carrier_key,
			carrier_account_id, status, cod_amount, seller_id, cancel_kind)
			VALUES ($1, 'E261121HUY001', $2, 'fake', $3, 'cancelled', 0, $4, 'manual')
			RETURNING id`, f.a, f.o1.ID, f.shop.Account, f.shop.Seller).Scan(&f.cancelled)
	})
	return f
}

func (f *pickingFixture) picking(t *testing.T, tenant uuid.UUID, ids ...uuid.UUID) queries.PickingList {
	t.Helper()
	var got queries.PickingList
	if err := tenancy.WithTenant(t.Context(), f.s.App, tenant, func(tx pgx.Tx) error {
		var err error
		got, err = queries.Picking(t.Context(), tx, ids)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	return got
}

// TestPickingList (Task M3-16, VC04): vB có ở 2 đơn (2 + 1) → một dòng qty 3, tên ghép từ catalog.variants_v
// (product_name, color / size), orderCodes có hai mã theo thứ tự; vA một dòng qty 1; dòng theo SKU; mỗi vận đơn một dòng
// có mã đơn, mã vận đơn, số món, COD, kho; mã lạ, vận đơn của tenant khác không có; hai vận đơn cùng đơn (một đã hủy)
// không đếm hàng hai lần.
func TestPickingList(t *testing.T) {
	f := newPickingFixture(t)
	var name string
	f.s.Exec(t, f.a, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT product_name FROM catalog.variants_v WHERE variant_id = $1`, f.vB).Scan(&name)
	})
	got := f.picking(t, f.a, f.s2, f.s1, f.cancelled, f.s1, uuid.New())
	codes := []string{f.o1.Code, f.o2.Code}
	slices.Sort(codes)
	i := slices.IndexFunc(got.Lines, func(l queries.PickingLine) bool { return l.Qty == 3 })
	if len(got.Lines) != 2 || i < 0 || got.Lines[i].Name != name || got.Lines[i].Variant != "Đen / M" ||
		!slices.Equal(got.Lines[i].OrderCodes, codes) || got.Lines[i].SKU == "" {
		t.Fatalf("dòng = %+v, muốn 2 dòng, vB qty 3 tên %q biến thể Đen / M mã đơn %v", got.Lines, name, codes)
	}
	if !slices.IsSortedFunc(got.Lines, func(x, y queries.PickingLine) int { return strings.Compare(x.SKU, y.SKU) }) {
		t.Fatalf("dòng không theo SKU: %+v", got.Lines)
	}
	f.checkOrders(t, got.Orders)
	if got := f.picking(t, f.b, f.s1); len(got.Orders) != 0 || len(got.Lines) != 0 {
		t.Fatalf("tenant B thấy vận đơn của A: %+v", got)
	}
}

// checkOrders: 3 vận đơn (2 đang giao + 1 đã hủy; mã lạ không có), kho, hãng, mã của đơn 1, số món mỗi đơn.
func (f *pickingFixture) checkOrders(t *testing.T, orders []queries.PickingOrder) {
	t.Helper()
	if len(orders) != 3 {
		t.Fatalf("vận đơn = %+v, muốn 3", orders)
	}
	items := map[uuid.UUID]int{}
	for _, o := range orders {
		items[o.ShipmentID] = o.ItemCount
		if o.WarehouseID != f.shop.Warehouse || o.Carrier != "fake" || o.Code == "" || o.ShipmentCode == "" {
			t.Fatalf("dòng đơn = %+v", o)
		}
		if o.ShipmentID == f.s1 && (o.Code != f.o1.Code || o.CODAmount != f.o1.CODAmount) {
			t.Fatalf("đơn 1 = %+v, muốn mã %s COD %d", o, f.o1.Code, f.o1.CODAmount)
		}
	}
	if items[f.s1] != 3 || items[f.s2] != 1 {
		t.Fatalf("số món = %v, muốn đơn 1: 3, đơn 2: 1", items)
	}
}

// TestOrderWarehouses: kho của từng đơn qua ord.orders_v; mã lạ không có trong kết quả.
func TestOrderWarehouses(t *testing.T) {
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "A")
	user := s.User(t, a, "kho@elimaz.test", "active")
	var shop *shiptest.Shop
	var v uuid.UUID
	s.Exec(t, a, func(tx pgx.Tx) error {
		shop = shiptest.NewShop(t, tx, a)
		v = shop.Variant(t, tx, shop.Seller, 100000, 10)
		return nil
	})
	var got map[uuid.UUID]uuid.UUID
	var o order.Order
	if err := tenancy.WithTenant(t.Context(), s.App, a, func(tx pgx.Tx) error {
		var err error
		if o, err = shop.Order(t.Context(), tx, shop.Seller, user, 0, order.ItemInput{VariantID: v, Qty: 1}); err != nil {
			return err //nolint:wrapcheck // test
		}
		got, err = queries.OrderWarehouses(t.Context(), tx, []uuid.UUID{o.ID, uuid.New()})
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[o.ID] != shop.Warehouse {
		t.Fatalf("kho = %v, muốn %s", got, shop.Warehouse)
	}
}
