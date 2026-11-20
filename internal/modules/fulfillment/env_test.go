package fulfillment_test

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/carriers/fake"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// TestMain đặt số lượt rapid 200, trừ khi người chạy đã đặt -rapid.checks hay RAPID_CHECKS.
func TestMain(m *testing.M) {
	flag.Parse()
	testkit.SetRapidChecks(200)
	os.Exit(m.Run())
}

// shipEnv: database thử, tenant A (shiptest.Shop: tài khoản hãng giả fake-1 của ELIMAZ, fake-2 của PARTNER1 ở kho
// MAIN), hãng giả trong bộ nhớ, worker chạy hai lệnh của fulfillment (lệnh tra dựng từ Service có hãng giả). seller
// là người bán của đơn trong test (mặc định ELIMAZ), account, accountRef là tài khoản hãng của người bán đó.
type shipEnv struct {
	t          *testing.T
	staff      *testkit.Staff
	shop       *shiptest.Shop
	tenant     uuid.UUID
	seller     uuid.UUID
	account    uuid.UUID
	accountRef string
	carrier    *fake.Carrier
	cmds       []commands.Handler
	worker     *queue.Worker
	actor      fulfillment.Actor
}

func newShipEnv(t *testing.T) *shipEnv {
	t.Helper()
	e := &shipEnv{t: t, staff: testkit.OpenStaff(t), carrier: fake.New()}
	e.tenant = e.staff.Tenant(t, "A")
	e.actor = fulfillment.Actor{TenantID: e.tenant, UserID: e.staff.User(t, e.tenant, "kho@elimaz.test", "active")}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		e.shop = shiptest.NewShop(t, tx, e.tenant)
		e.seller, e.account, e.accountRef = e.shop.Seller, e.shop.Account, shiptest.AccountRef
		return nil
	})
	o := fulfillment.CommandOptions{
		DB: e.staff.App, Adapters: []fulfillment.CarrierAdapter{e.carrier},
		Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &testkit.RecordingBus{},
	}
	svc := fulfillment.New(fulfillment.Deps{
		DB: o.DB, Queue: o.Queue, Events: o.Events, Bus: o.Realtime, Carriers: mustRegistry(t, e.carrier),
	}).Service
	e.useCommands([]commands.Handler{fulfillment.NewCreateCommand(o), fulfillment.NewRefreshCommand(svc)})
	return e
}

// useCommands đặt lệnh của worker là hs (dựng lại worker).
func (e *shipEnv) useCommands(hs []commands.Handler) {
	e.cmds = hs
	e.worker = queue.NewWorker(e.staff.App, nil, queue.WorkerOptions{
		WorkerID: "test/" + uuid.NewString(), Commands: commands.NewRunner(e.staff.App, hs),
	})
}

// eachSeller chạy body trên cùng một database lần lượt với từng người bán làm người bán của đơn (KH14, ADR-29).
func eachSeller(t *testing.T, body func(t *testing.T, e *shipEnv)) {
	t.Helper()
	base := newShipEnv(t)
	testkit.ForEachSeller(t, []uuid.UUID{base.shop.Seller, base.shop.Partner}, func(t *testing.T, s uuid.UUID) {
		e := *base
		e.t, e.seller, e.account = t, s, base.shop.AccountOf(s)
		if s == base.shop.Partner {
			e.accountRef = shiptest.PartnerAccountRef
		}
		body(t, &e)
	})
}

func (e *shipEnv) app(fn func(tx pgx.Tx) error) error {
	return tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, fn) //nolint:wrapcheck // test
}

// order tạo biến thể on_hand 5 và đơn qty cái (phí ship 30000) của người bán e.seller; trả đơn và biến thể.
func (e *shipEnv) order(qty int32) (order.Order, uuid.UUID) {
	e.t.Helper()
	var v uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		v = e.shop.Variant(e.t, tx, e.seller, 250000, 5)
		return nil
	})
	var o order.Order
	if err := e.app(func(tx pgx.Tx) (err error) {
		o, err = e.shop.Order(e.t.Context(), tx, e.seller, e.actor.UserID, 30000, order.ItemInput{VariantID: v, Qty: qty})
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("tạo đơn: %v", err)
	}
	return o, v
}

func (e *shipEnv) createShipment(orderID uuid.UUID) (fulfillment.Shipment, error) {
	var s fulfillment.Shipment
	err := e.app(func(tx pgx.Tx) (err error) {
		s, err = e.shop.Shipment(e.t.Context(), tx, e.seller, orderID, e.actor.UserID)
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
		return order.Get(ctx, tx, e.seller, id)
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

// signal giao một tín hiệu webhook của tài khoản hãng e.account (khóa tuyến route_key của nó) cho handler vào của
// module (nguồn "fake"), trong tx crm_app. Khóa sự kiện là "<accountRef>/<eventID>": hai lượt eachSeller dùng cùng
// eventID trên một database mà không bị chống trùng gộp. Lỗi làm test đỏ.
func (e *shipEnv) signal(tracking, eventID string) {
	e.t.Helper()
	sig := fulfillment.Signal{RouteKey: e.routeKey(e.account), EventKey: e.accountRef + "/" + eventID, TrackingCode: tracking}
	if err := e.trySignal(sig, nil); err != nil {
		e.t.Fatalf("tín hiệu %s: %v", eventID, err)
	}
}

// trySignal giao tín hiệu sig cho handler vào của module có một hãng giả nhận webhook (khóa "fake"); reg nil: không
// đếm chỉ số.
func (e *shipEnv) trySignal(sig fulfillment.Signal, reg *metrics.Registry) error {
	e.t.Helper()
	return e.trySignalFrom(hookCarrier{Carrier: e.carrier}, sig, reg)
}

// trySignalFrom như trySignal với hãng a (nguồn a.Key()).
func (e *shipEnv) trySignalFrom(a fulfillment.CarrierAdapter, sig fulfillment.Signal, reg *metrics.Registry) error {
	e.t.Helper()
	hs := fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Carriers: mustRegistry(e.t, a), Metrics: reg,
	}).InboundHandlers()
	if len(hs) != 1 || hs[0].Source() != a.Key() {
		e.t.Fatalf("handler vào = %d, muốn 1 của nguồn %s", len(hs), a.Key())
	}
	data, err := json.Marshal(sig)
	if err != nil {
		e.t.Fatal(err)
	}
	return e.app(func(tx pgx.Tx) error {
		return hs[0].Handle(e.t.Context(), webhooks.Inbound{
			Type: fulfillment.SignalType, Data: data, JobID: uuid.New(), TenantID: e.tenant,
		}, tx)
	})
}

// routeKey trả route_key của tài khoản hãng account.
func (e *shipEnv) routeKey(account uuid.UUID) string {
	e.t.Helper()
	return readAs(e, func(ctx context.Context, tx pgx.Tx) (string, error) {
		var rk string
		err := tx.QueryRow(ctx, "SELECT route_key FROM fulfillment.carrier_accounts WHERE id = $1", account).Scan(&rk)
		return rk, err //nolint:wrapcheck // test
	})
}

// hookCarrier là hãng giả trong bộ nhớ có khai khả năng Webhook (module dựng handler vào cho nó).
type hookCarrier struct {
	*fake.Carrier
	key string
}

func (h hookCarrier) Key() string {
	if h.key != "" {
		return h.key
	}
	return h.Carrier.Key()
}

func (hookCarrier) Capabilities() fulfillment.Capabilities {
	return fulfillment.Capabilities{Webhook: true}
}

// ownerExec chạy câu bằng chủ bảng trong ngữ cảnh tenant A (sửa dữ liệu như kẻ có quyền database, dựng tình huống).
func (e *shipEnv) ownerExec(sql string, args ...any) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), sql, args...)
		return err //nolint:wrapcheck // test
	})
}

// appExec chạy một câu bằng crm_app trong ngữ cảnh tenant A.
func (e *shipEnv) appExec(sql string, args ...any) error {
	return e.app(func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), sql, args...)
		return err //nolint:wrapcheck // test
	})
}
