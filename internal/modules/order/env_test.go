package order_test

import (
	"flag"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// TestMain đặt số lượt rapid 200 (kế hoạch Task M1-30), trừ khi người chạy đã đặt -rapid.checks hay RAPID_CHECKS.
func TestMain(m *testing.M) {
	flag.Parse()
	testkit.SetRapidChecks(200)
	os.Exit(m.Run())
}

// orderEnv: một database thử, tenant A (invtest.Shop: người bán ELIMAZ, người bán PARTNER1, kho MAIN) và nhân viên
// thao tác. seller là người bán của đơn trong test (mặc định ELIMAZ; eachSeller chạy với từng người), partner là
// người bán còn lại.
type orderEnv struct {
	t                                  *testing.T
	staff                              *testkit.Staff
	shop                               *invtest.Shop
	tenant, seller, partner, warehouse uuid.UUID
	actor                              order.Actor
}

func newOrderEnv(t *testing.T) *orderEnv {
	t.Helper()
	e := &orderEnv{t: t, staff: testkit.OpenStaff(t)}
	e.tenant = e.staff.Tenant(t, "A")
	e.actor = order.Actor{TenantID: e.tenant, UserID: e.staff.User(t, e.tenant, "nv@elimaz.test", "active")}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		e.shop = invtest.NewShop(t, tx, e.tenant)
		return nil
	})
	e.seller, e.partner, e.warehouse = e.shop.Seller, e.shop.Partner, e.shop.Warehouse
	return e
}

// eachSeller chạy body trên cùng một database lần lượt với từng người bán làm người bán của đơn (KH14, ADR-29).
func eachSeller(t *testing.T, body func(t *testing.T, e *orderEnv)) {
	t.Helper()
	base := newOrderEnv(t)
	testkit.ForEachSeller(t, []uuid.UUID{base.shop.Seller, base.shop.Partner}, func(t *testing.T, s uuid.UUID) {
		e := *base
		e.t, e.seller, e.partner = t, s, base.shop.Partner
		if s == base.shop.Partner {
			e.partner = base.shop.Seller
		}
		body(t, &e)
	})
}

// variant tạo biến thể của người bán e.seller giá price, nạp onHand (0: không nạp).
func (e *orderEnv) variant(price int64, onHand int32) uuid.UUID {
	e.t.Helper()
	return e.variantOf(e.seller, price, onHand)
}

// variantOf tạo biến thể của người bán seller giá price, nạp onHand.
func (e *orderEnv) variantOf(seller uuid.UUID, price int64, onHand int32) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		id = e.shop.Variant(e.t, tx, seller, price, onHand)
		return nil
	})
	return id
}

func (e *orderEnv) writer() order.Writer {
	return order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &testkit.RecordingBus{}}
}

// app chạy fn bằng crm_app trong ngữ cảnh tenant của e.
func (e *orderEnv) app(fn func(tx pgx.Tx) error) error {
	return tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, fn) //nolint:wrapcheck // test
}

func (e *orderEnv) input(fee int64, items ...order.ItemInput) order.CreateManualInput {
	return order.CreateManualInput{
		IdempotencyKey: uuid.New(), SellerID: e.seller, Items: items, ShippingFee: fee,
		ShipAddress: order.ShipAddress{Name: "Chị Lan", Phone: logging.Phone("0901234567"), Line: "12 Lê Lợi, Q1"},
	}
}

// create tạo khách mới rồi đơn nhập tay trong một transaction crm_app (như handler createOrder).
func (e *orderEnv) create(in order.CreateManualInput) (order.Order, error) {
	var o order.Order
	err := e.app(func(tx pgx.Tx) error {
		c, err := customer.Create(e.t.Context(), tx, "Chị Lan")
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		in.CustomerID = c
		o, err = e.writer().CreateManual(e.t.Context(), tx, in, e.actor)
		return err //nolint:wrapcheck // test
	})
	return o, err
}

func (e *orderEnv) mustCreate(in order.CreateManualInput) order.Order {
	e.t.Helper()
	o, err := e.create(in)
	if err != nil {
		e.t.Fatalf("tạo đơn: %v", err)
	}
	return o
}

// balance trả số dư của biến thể v ở kho MAIN.
func (e *orderEnv) balance(v uuid.UUID) inventory.Balance {
	e.t.Helper()
	var b []inventory.Balance
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		b, err = inventory.Availability(e.t.Context(), tx, e.warehouse, []uuid.UUID{v})
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return b[0]
}

func (e *orderEnv) count(q string, args ...any) int {
	e.t.Helper()
	return e.staff.CountIn(e.t, e.tenant, q, args...)
}

func item(v uuid.UUID, q int32) order.ItemInput { return order.ItemInput{VariantID: v, Qty: q} }

// stubShipment tạo dòng vận đơn s (đã hủy) cho đơn o bằng chủ bảng, để gắn s làm vận đơn hiện hành thỏa khóa ngoại.
func (e *orderEnv) stubShipment(o, s uuid.UUID) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		testkit.StubShipment(e.t, tx, o, s)
		return nil
	})
}
