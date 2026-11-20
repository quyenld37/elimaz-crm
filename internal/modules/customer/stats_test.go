package customer_test

import (
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

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

// statsEnv: database thử có cửa hàng thật (người bán, kho, biến thể) để tạo đơn, phiếu qua module order (kế hoạch
// Task M2-25: "đơn và phiếu tạo qua module order"). Sự kiện order.* và customer.* đọc lại từ platform.domain_events rồi
// giao cho consumer customer_stats theo thứ tự test chọn.
type statsEnv struct {
	t      *testing.T
	staff  *testkit.Staff
	tenant uuid.UUID
	shop   *invtest.Shop
	oa     order.Actor
	cw     customer.Writer
	actor  customer.Actor
	now    time.Time
	// fed: sự kiện đã giao cho consumer (không giao lại khi gọi feedNew).
	fed map[uuid.UUID]bool
}

func newStatsEnv(t *testing.T) *statsEnv {
	t.Helper()
	s := testkit.OpenStaff(t)
	s.SeedAdminUnits(t)
	e := &statsEnv{
		t: t, staff: s, tenant: s.Tenant(t, "A"), now: time.Date(2026, 11, 19, 8, 30, 0, 0, time.UTC),
		cw:    customer.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &testkit.RecordingBus{}},
		actor: customer.Actor{Type: customer.ActorStaff, ID: uuid.New()},
		fed:   map[uuid.UUID]bool{},
	}
	e.oa = order.Actor{TenantID: e.tenant, UserID: s.User(t, e.tenant, "nv-m225@elimaz.test", "active")}
	s.Exec(t, e.tenant, func(tx pgx.Tx) error {
		e.shop = invtest.NewShop(t, tx, e.tenant)
		return order.EnsureDefaults(t.Context(), tx, e.tenant)
	})
	return e
}

func (e *statsEnv) app(fn func(tx pgx.Tx) error) {
	e.t.Helper()
	if err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, fn); err != nil {
		e.t.Fatal(err)
	}
}

func (e *statsEnv) ow() order.Writer {
	return order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &testkit.RecordingBus{}}
}

func (e *statsEnv) newCustomer(name string) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.app(func(tx pgx.Tx) error {
		c, err := e.cw.CreateCustomer(e.t.Context(), tx, customer.CreateInput{FullName: name}, e.actor, e.now)
		id = c.ID
		return err //nolint:wrapcheck // test
	})
	return id
}

// confirmed tạo đơn confirmed của khách c: mỗi giá trong prices là một biến thể ×1, phí giao 30.000.
func (e *statsEnv) confirmed(c uuid.UUID, prices ...int64) order.Order {
	e.t.Helper()
	lines := make([]order.LineInput, 0, len(prices))
	for _, p := range prices {
		var v uuid.UUID
		e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
			v = e.shop.Variant(e.t, tx, e.shop.Seller, p, 5)
			return nil
		})
		lines = append(lines, order.LineInput{VariantID: v, Quantity: 1})
	}
	var o order.Order
	e.app(func(tx pgx.Tx) error {
		res, err := e.ow().Create(e.t.Context(), tx, order.CreateInput{
			IdempotencyKey: uuid.New(), Channel: order.ChannelManual, Intent: order.IntentConfirmed, SellerID: e.shop.Seller,
			WarehouseID: e.shop.Warehouse,
			Customer:    customer.OrderCustomerInput{CustomerID: &c}, Lines: lines, ShippingFee: 30000,
			ShipAddress: &order.ShipAddress{
				Name: "Chị Lan", Phone: logging.Phone("0901234567"), Line: "12 Lê Lợi", ProvinceCode: "T01", WardCode: "T01001",
			},
		}, e.oa, e.now)
		o = res.Order
		return err //nolint:wrapcheck // test
	})
	if o.Status != order.StatusConfirmed {
		e.t.Fatalf("đơn chưa confirmed: %s", o.Status)
	}
	return o
}

func (e *statsEnv) step(o order.Order, s uuid.UUID, st order.ShipmentStep) {
	e.t.Helper()
	carrier := order.Actor{TenantID: e.tenant, Type: inventory.ActorCarrier}
	e.app(func(tx pgx.Tx) error {
		_, err := e.ow().OnShipmentEvent(e.t.Context(), tx, e.shop.Seller, o.ID, s, st, carrier, e.now)
		return err //nolint:wrapcheck // test
	})
}

func (e *statsEnv) attach(o order.Order) uuid.UUID {
	e.t.Helper()
	s := uuid.New()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		testkit.StubShipment(e.t, tx, o.ID, s)
		return nil
	})
	e.app(func(tx pgx.Tx) error {
		return e.ow().AttachShipmentBy(e.t.Context(), tx, e.shop.Seller, o.ID, s, e.oa, e.now)
	})
	return s
}

// delivered tạo đơn của khách c rồi giao xong.
func (e *statsEnv) delivered(c uuid.UUID, prices ...int64) order.Order {
	e.t.Helper()
	o := e.confirmed(c, prices...)
	e.step(o, e.attach(o), order.StepDelivered)
	return o
}

// carrierReturned tạo đơn của khách c, hãng lấy hàng, hoàn về, kho nhận → đơn returned.
func (e *statsEnv) carrierReturned(c uuid.UUID, price int64) order.Order {
	e.t.Helper()
	o := e.confirmed(c, price)
	s := e.attach(o)
	e.step(o, s, order.StepPickedUp)
	carrier := order.Actor{TenantID: e.tenant, Type: inventory.ActorCarrier}
	e.app(func(tx pgx.Tx) error {
		if _, err := e.ow().OnShipmentReturning(e.t.Context(), tx, e.shop.Seller, o.ID, s, carrier, e.now); err != nil {
			return err //nolint:wrapcheck // test
		}
		r, err := order.CarrierReturnFor(e.t.Context(), tx, e.shop.Seller, s)
		return e.receiveAll(tx, r, err)
	})
	return o
}

func sellable(r order.Return) []order.ReceiveLine {
	out := make([]order.ReceiveLine, 0, len(r.Items))
	for _, it := range r.Items {
		out = append(out, order.ReceiveLine{OrderItemID: it.OrderItemID, Condition: order.ConditionSellable})
	}
	return out
}

func (e *statsEnv) cancelled(c uuid.UUID, price int64) order.Order {
	e.t.Helper()
	o := e.confirmed(c, price)
	e.app(func(tx pgx.Tx) error {
		_, err := e.ow().Cancel(e.t.Context(), tx, e.shop.Seller, o.ID, o.Version, order.CancelCustomerChangedMind, "",
			e.oa, e.now)
		return err //nolint:wrapcheck // test
	})
	return o
}

// customerReturn: khách trả dòng thứ i của đơn o đã giao, kho nhận (bán lại được).
func (e *statsEnv) customerReturn(o order.Order, i int) {
	e.t.Helper()
	e.app(func(tx pgx.Tx) error {
		r, err := e.ow().CreateReturn(e.t.Context(), tx, e.shop.Seller, o.ID, order.CreateReturnInput{
			Kind: order.ReturnCustomer, Reason: "không vừa",
			Lines: []order.ReturnLineInput{{OrderItemID: o.Items[i].ID, Quantity: 1}},
		}, e.oa, e.now)
		return e.receiveAll(tx, r, err)
	})
}

// receiveAll: kho nhận mọi dòng của phiếu r (bán lại được); err là lỗi của bước lấy phiếu (khác nil → trả luôn).
func (e *statsEnv) receiveAll(tx pgx.Tx, r order.Return, err error) error {
	if err != nil {
		return err
	}
	_, err = e.ow().ReceiveReturn(e.t.Context(), tx, e.shop.Seller, r.ID, r.Version, sellable(r), e.oa, e.now)
	return err //nolint:wrapcheck // test
}

// pending trả các sự kiện order.*, customer.* chưa giao cho consumer, theo thứ tự ghi.
func (e *statsEnv) pending() []events.StoredEvent {
	e.t.Helper()
	var out []events.StoredEvent
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), `SELECT id, tenant_id, aggregate_type, aggregate_id, seq, topic, payload
			FROM platform.domain_events WHERE topic LIKE 'order.%' OR topic LIKE 'customer.%' ORDER BY id`)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		defer rows.Close()
		for rows.Next() {
			var ev events.StoredEvent
			if err := rows.Scan(&ev.ID, &ev.TenantID, &ev.AggregateType, &ev.AggregateID, &ev.Seq, &ev.Topic,
				&ev.Payload); err != nil {
				return err //nolint:wrapcheck // test
			}
			if !e.fed[ev.ID] {
				out = append(out, ev)
			}
		}
		return rows.Err()
	})
	return out
}

func (e *statsEnv) consumer() customer.StatsConsumer {
	return customer.StatsConsumer{W: e.cw, Now: func() time.Time { return e.now }}
}

// handle giao ev cho consumer trong một transaction crm_app (như worker).
func (e *statsEnv) handle(ev events.StoredEvent) {
	e.t.Helper()
	c := e.consumer()
	e.app(func(tx pgx.Tx) error { return c.Handle(e.t.Context(), ev, tx) })
	e.fed[ev.ID] = true
}

// feed giao mọi sự kiện chưa giao, theo thứ tự ghi (reverse: ngược thứ tự ghi).
func (e *statsEnv) feed(reverse bool) {
	e.t.Helper()
	evs := e.pending()
	if reverse {
		for i, j := 0, len(evs)-1; i < j; i, j = i+1, j-1 {
			evs[i], evs[j] = evs[j], evs[i]
		}
	}
	c := e.consumer()
	topics := map[string]bool{}
	for _, tp := range c.Topics() {
		topics[tp] = true
	}
	for _, ev := range evs {
		if topics[ev.Topic] {
			e.handle(ev)
		} else {
			e.fed[ev.ID] = true
		}
	}
}

func (e *statsEnv) stats(id uuid.UUID) customer.Stats {
	e.t.Helper()
	var s customer.Stats
	e.app(func(tx pgx.Tx) error {
		var err error
		s, err = customer.StatsFor(e.t.Context(), tx, id)
		return err //nolint:wrapcheck // test
	})
	return s
}

// rawStats đọc cột chỉ số của chính hồ sơ id (không theo gốc).
func (e *statsEnv) rawStats(id uuid.UUID) [4]int64 {
	e.t.Helper()
	var r [4]int64
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT order_count, delivered_count, returned_count, total_spent
			FROM customer.customers WHERE id = $1`, id).Scan(&r[0], &r[1], &r[2], &r[3])
	})
	return r
}

type wantStats struct {
	orders, delivered, returned int
	spent                       int64
	rate                        int
}

func (e *statsEnv) expect(label string, id uuid.UUID, w wantStats) {
	e.t.Helper()
	s := e.stats(id)
	if s.OrderCount != w.orders || s.DeliveredCount != w.delivered || s.ReturnedCount != w.returned ||
		s.TotalSpent != w.spent || s.ReturnRateBP != w.rate || s.LastOrderAt == nil {
		e.t.Errorf("%s: %+v, muốn %+v", label, s, w)
	}
}

func (e *statsEnv) mergeAndFeed(survivor, merged uuid.UUID) customer.Merge {
	e.t.Helper()
	var m customer.Merge
	e.app(func(tx pgx.Tx) error {
		var err error
		m, err = e.cw.MergeCustomers(e.t.Context(), tx, survivor, merged, customer.MatchedByStaff, e.actor, e.now)
		return err //nolint:wrapcheck // test
	})
	e.feed(false)
	return m
}

// Kế hoạch M2-25, ca 1–3 và 7: khách A có đơn delivered 690.000, đơn returned (hoàn qua hãng), đơn cancelled — sự kiện
// giao ngược thứ tự ghi (order.returned trước order.delivered) → 2/1/1/690.000, tỷ lệ 5000; khách trả một món 250.000
// (kho đã nhận) → 440.000. Gộp B (delivered 300.000) vào A → A 2 giao, 740.000, B về 0; StatsFor(B) là chỉ số của A;
// hoàn tác → A 440.000, B 300.000. order.delivery_reversed giả → tính lại từ dữ liệu gốc (số bị sửa tay bị ghi đè).
func TestStatsFromSource(t *testing.T) {
	e := newStatsEnv(t)
	a, b := e.newCustomer("Khách A"), e.newCustomer("Khách B")
	o1 := e.delivered(a, 250000, 410000)
	if o1.GrandTotal != 690000 {
		t.Fatalf("đơn 1: %d", o1.GrandTotal)
	}
	e.carrierReturned(a, 200000)
	e.cancelled(a, 100000)
	e.feed(true)
	e.expect("A sau ba đơn (sai thứ tự)", a, wantStats{orders: 2, delivered: 1, returned: 1, spent: 690000, rate: 5000})

	e.customerReturn(o1, 0)
	e.feed(false)
	e.expect("A sau khi khách trả một món", a, wantStats{2, 1, 1, 440000, 5000})

	e.delivered(b, 270000)
	e.feed(true)
	e.expect("B", b, wantStats{1, 1, 0, 300000, 0})

	m := e.mergeAndFeed(a, b)
	e.expect("A sau gộp", a, wantStats{3, 2, 1, 740000, 3333})
	e.expect("StatsFor(B) khi đã gộp", b, wantStats{3, 2, 1, 740000, 3333})
	if r := e.rawStats(b); r != [4]int64{} {
		t.Errorf("hồ sơ con B phải về 0: %v", r)
	}

	e.app(func(tx pgx.Tx) error { return e.cw.UndoMerge(t.Context(), tx, m.ID, e.actor, e.now) })
	e.feed(false)
	e.expect("A sau hoàn tác", a, wantStats{2, 1, 1, 440000, 5000})
	e.expect("B sau hoàn tác", b, wantStats{1, 1, 0, 300000, 0})

	// Sửa tay chỉ số (mô phỏng số lệch), rồi sự kiện giả order.delivery_reversed của đơn 1 → tính lại từ đơn, phiếu.
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE customer.customers SET delivered_count = 9, total_spent = 1 WHERE id = $1`, a)
		return err //nolint:wrapcheck // test
	})
	p, err := json.Marshal(map[string]any{"orderId": o1.ID, "customerId": a, "status": "confirmed", "version": 99})
	if err != nil {
		t.Fatal(err)
	}
	e.handle(events.StoredEvent{
		ID: uuid.New(), TenantID: e.tenant, AggregateType: "order", AggregateID: o1.ID.String(), Seq: 99,
		Topic: customer.TopicOrderDeliveryReversed, Payload: p,
	})
	e.expect("A sau order.delivery_reversed", a, wantStats{2, 1, 1, 440000, 5000})
}

// Review độc lập M2, A-03: gộp B (đã mua, customer) vào gốc A còn lead (tạo tay, chưa đơn) → sau khi tính lại chỉ số,
// gốc A thành customer (nhóm đã có đơn). Khách lead không đơn nào vẫn là lead sau khi tính lại.
func TestStatsPromotesRootAfterMerge(t *testing.T) {
	e := newStatsEnv(t)
	a, b, c := e.newCustomer("Khách A"), e.newCustomer("Khách B"), e.newCustomer("Khách C")
	e.delivered(b, 270000)
	e.feed(true)
	lifecycle := func(id uuid.UUID) string {
		var l string
		e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(t.Context(), `SELECT lifecycle FROM customer.customers WHERE id = $1`, id).Scan(&l)
		})
		return l
	}
	if la, lb := lifecycle(a), lifecycle(b); la != "lead" || lb != "customer" {
		t.Fatalf("trước gộp: A %s, B %s", la, lb)
	}
	e.mergeAndFeed(a, b)
	if l := lifecycle(a); l != "customer" {
		t.Errorf("gốc A sau gộp: %s, mong customer", l)
	}
	e.app(func(tx pgx.Tx) error { return e.cw.RecomputeStats(t.Context(), tx, c, e.now) })
	if l := lifecycle(c); l != "lead" {
		t.Errorf("C không đơn sau tính lại: %s, mong lead", l)
	}
}

// Consumer: payload hỏng hay khách không có → lỗi vĩnh viễn; chạy lại cùng sự kiện cho cùng kết quả.
func TestStatsConsumerPayload(t *testing.T) {
	e := newStatsEnv(t)
	c := e.consumer()
	for name, p := range map[string]string{
		"hỏng": `{`, "thiếu khách": `{"orderId":"` + uuid.NewString() + `"}`,
		"khách lạ": `{"customerId":"` + uuid.NewString() + `"}`,
	} {
		err := tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
			return c.Handle(t.Context(), events.StoredEvent{
				ID: uuid.New(), TenantID: e.tenant, Topic: order.TopicOrderCreated,
				Payload: json.RawMessage(p),
			}, tx)
		})
		if !errorsIsPermanent(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	a := e.newCustomer("Khách A")
	e.delivered(a, 100000)
	evs := e.pending()
	for range 2 {
		for _, ev := range evs {
			if ev.Topic == order.TopicOrderDelivered {
				e.handle(ev)
			}
		}
	}
	e.expect("chạy lại hai lần", a, wantStats{1, 1, 0, 130000, 0})

	// Tập rỗng: khách chỉ có đơn đã hủy → mọi chỉ số 0, last_order_at NULL (sum, max trên tập rỗng).
	d := e.newCustomer("Khách D")
	e.cancelled(d, 100000)
	e.feed(false)
	if s := e.stats(d); s != (customer.Stats{}) {
		t.Errorf("khách chỉ có đơn hủy: %+v", s)
	}
}

// Song song: 8 lượt giao lại sự kiện đơn của A và B (consumer) chạy cùng lúc với lần gộp B vào A — không deadlock, không
// lỗi; sau khi giao customer.merged, gốc A có chỉ số của cả nhóm và B về 0.
func TestStatsConcurrentWithMerge(t *testing.T) {
	e := newStatsEnv(t)
	a, b := e.newCustomer("Khách A"), e.newCustomer("Khách B")
	e.delivered(a, 100000)
	e.delivered(b, 200000)
	evs := e.pending()
	c := e.consumer()
	errs := make(chan error, 64)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range 8 {
		wg.Go(func() {
			<-start
			for j := range evs {
				ev := evs[(i+j)%len(evs)]
				if !slices.Contains(c.Topics(), ev.Topic) {
					continue
				}
				errs <- tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
					return c.Handle(t.Context(), ev, tx)
				})
			}
		})
	}
	wg.Go(func() {
		<-start
		errs <- tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
			_, err := e.cw.MergeCustomers(t.Context(), tx, a, b, customer.MatchedByStaff, e.actor, e.now)
			return err //nolint:wrapcheck // test
		})
	})
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("chạy song song: %v", err)
		}
	}
	for _, ev := range evs {
		e.fed[ev.ID] = true
	}
	e.feed(false) // customer.merged
	e.expect("A sau gộp song song", a, wantStats{2, 2, 0, 360000, 0})
	if r := e.rawStats(b); r != [4]int64{} {
		t.Errorf("B phải về 0: %v", r)
	}
}
