package fulfillment_test

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/carriers/fake"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
)

// TestMain đặt số lượt rapid 200, trừ khi người chạy đã đặt -rapid.checks hay RAPID_CHECKS.
func TestMain(m *testing.M) {
	flag.Parse()
	testkit.SetRapidChecks(200)
	os.Exit(m.Run())
}

const accountRef = "fake-1"

// shipEnv: database thử, tenant A (invtest.Shop), tài khoản hãng giả fake-1 ở kho MAIN, hãng giả trong bộ nhớ, worker
// chạy hai lệnh của fulfillment.
type shipEnv struct {
	t       *testing.T
	staff   *testkit.Staff
	shop    *invtest.Shop
	tenant  uuid.UUID
	account uuid.UUID
	carrier *fake.Carrier
	cmds    []commands.Handler
	worker  *queue.Worker
	actor   fulfillment.Actor
}

func newShipEnv(t *testing.T) *shipEnv {
	t.Helper()
	e := &shipEnv{t: t, staff: testkit.OpenStaff(t), carrier: fake.New()}
	e.tenant = e.staff.Tenant(t, "A")
	e.actor = fulfillment.Actor{TenantID: e.tenant, UserID: e.staff.User(t, e.tenant, "kho@elimaz.test", "active")}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		e.shop = invtest.NewShop(t, tx, e.tenant)
		var err error
		e.account, err = fulfillment.EnsureCarrierAccount(t.Context(), tx, fulfillment.AccountInput{
			SellerID: e.shop.Seller, WarehouseID: e.shop.Warehouse, CarrierKey: fake.Key, Name: "Hãng giả",
			ExternalAccountRef: accountRef,
		})
		return err //nolint:wrapcheck // test
	})
	o := fulfillment.CommandOptions{
		DB: e.staff.App, Adapters: []fulfillment.CarrierAdapter{e.carrier},
		Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &testkit.RecordingBus{},
	}
	e.cmds = []commands.Handler{fulfillment.NewCreateCommand(o), fulfillment.NewRefreshCommand(o)}
	e.worker = queue.NewWorker(e.staff.App, nil, queue.WorkerOptions{
		WorkerID: "test/" + uuid.NewString(), Commands: commands.NewRunner(e.staff.App, e.cmds),
	})
	return e
}

func (e *shipEnv) app(fn func(tx pgx.Tx) error) error {
	return tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, fn) //nolint:wrapcheck // test
}

// order tạo biến thể on_hand 5 và đơn qty cái (phí ship 30000) của ELIMAZ; trả đơn và biến thể.
func (e *shipEnv) order(qty int32) (order.Order, uuid.UUID) {
	e.t.Helper()
	var v uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		v = e.shop.Variant(e.t, tx, e.shop.Seller, 250000, 5)
		return nil
	})
	var o order.Order
	if err := e.app(func(tx pgx.Tx) error {
		c, err := customer.Create(e.t.Context(), tx, "Chị Lan")
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		w := order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &testkit.RecordingBus{}}
		o, err = w.CreateManual(e.t.Context(), tx, order.CreateManualInput{
			IdempotencyKey: uuid.New(), SellerID: e.shop.Seller, CustomerID: c, ShippingFee: 30000,
			Items:       []order.ItemInput{{VariantID: v, Qty: qty}},
			ShipAddress: order.ShipAddress{Name: "Chị Lan", Phone: logging.Phone("0901234567"), Line: "12 Lê Lợi, Q1"},
		}, order.Actor{TenantID: e.tenant, UserID: e.actor.UserID})
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("tạo đơn: %v", err)
	}
	return o, v
}

func (e *shipEnv) createShipment(orderID uuid.UUID) (fulfillment.Shipment, error) {
	var s fulfillment.Shipment
	err := e.app(func(tx pgx.Tx) error {
		var err error
		s, err = fulfillment.CreateForOrder(e.t.Context(), tx, orderID, e.account, e.actor)
		return err //nolint:wrapcheck // test
	})
	return s, err
}

func (e *shipEnv) mustCreateShipment(orderID uuid.UUID) fulfillment.Shipment {
	e.t.Helper()
	s, err := e.createShipment(orderID)
	if err != nil {
		e.t.Fatalf("tạo vận đơn: %v", err)
	}
	return s
}

// drain chạy worker tới khi không còn việc nào nhận được.
func (e *shipEnv) drain() {
	e.t.Helper()
	for range 20 {
		n, err := e.worker.RunOnce(context.Background(), 10)
		if err != nil {
			e.t.Fatalf("RunOnce: %v", err)
		}
		if n == 0 {
			return
		}
	}
	e.t.Fatal("worker vẫn còn việc sau 20 lượt")
}

// readAs đọc một giá trị bằng crm_app trong ngữ cảnh tenant A; lỗi làm test đỏ.
func readAs[T any](e *shipEnv, read func(ctx context.Context, tx pgx.Tx) (T, error)) T { //nolint:ireturn // T là kiểu cụ thể của bên gọi
	e.t.Helper()
	var v T
	if err := e.app(func(tx pgx.Tx) (err error) {
		v, err = read(e.t.Context(), tx)
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return v
}

func (e *shipEnv) shipment(id uuid.UUID) fulfillment.Shipment {
	e.t.Helper()
	return readAs(e, func(ctx context.Context, tx pgx.Tx) (fulfillment.Shipment, error) {
		return fulfillment.Get(ctx, tx, id)
	})
}

func (e *shipEnv) getOrder(id uuid.UUID) order.Order {
	e.t.Helper()
	return readAs(e, func(ctx context.Context, tx pgx.Tx) (order.Order, error) {
		return order.Get(ctx, tx, e.shop.Seller, id)
	})
}

// stock trả (on_hand, held, in_transit) của biến thể v ở kho MAIN.
func (e *shipEnv) stock(v uuid.UUID) [3]int32 {
	e.t.Helper()
	b := readAs(e, func(ctx context.Context, tx pgx.Tx) ([]inventory.Balance, error) {
		return inventory.Availability(ctx, tx, e.shop.Warehouse, []uuid.UUID{v})
	})
	return [3]int32{b[0].OnHand, b[0].Held, b[0].InTransit}
}

// stockIs kiểm (on_hand, held, in_transit) của biến thể v.
func (e *shipEnv) stockIs(v uuid.UUID, want [3]int32) {
	e.t.Helper()
	if got := e.stock(v); got != want {
		e.t.Errorf("tồn (on_hand, held, in_transit) = %v, muốn %v", got, want)
	}
}

// shipped tạo đơn qty cái và vận đơn đã qua lệnh tạo (awaiting_pickup); trả đơn, biến thể, vận đơn.
func (e *shipEnv) shipped(qty int32) (order.Order, uuid.UUID, fulfillment.Shipment) {
	e.t.Helper()
	o, v := e.order(qty)
	s := e.mustCreateShipment(o.ID)
	e.drain()
	return o, v, e.shipment(s.ID)
}

func (e *shipEnv) count(q string, args ...any) int {
	e.t.Helper()
	return e.staff.CountIn(e.t, e.tenant, q, args...)
}

// signal giao một tín hiệu webhook (đã định tuyến về tenant A) cho handler của nguồn fakecarrier, trong tx crm_app.
func (e *shipEnv) signal(tracking, eventID string) {
	e.t.Helper()
	data, err := json.Marshal(fulfillment.Signal{
		CarrierKey: fake.Key, AccountKey: accountRef, TrackingCode: tracking, EventID: eventID,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	h := fulfillment.NewSignalHandler("fakecarrier")
	if err := e.app(func(tx pgx.Tx) error {
		return h.Handle(e.t.Context(), webhooks.Inbound{
			Type: fulfillment.SignalType, Data: data, JobID: uuid.New(), TenantID: e.tenant,
		}, tx)
	}); err != nil {
		e.t.Fatalf("tín hiệu %s: %v", eventID, err)
	}
}

// appExec chạy một câu bằng crm_app trong ngữ cảnh tenant A.
func (e *shipEnv) appExec(sql string, args ...any) error {
	return e.app(func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), sql, args...)
		return err //nolint:wrapcheck // test
	})
}
