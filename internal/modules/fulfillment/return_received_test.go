package fulfillment_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Kho nhận kiện hoàn theo số lượng rồi vận đơn sang Đã hoàn (kế hoạch M3, Task M3-23; TD02/AT11, TD04/AT12). Đơn D1:
// vA ×1 (on_hand 5), vB ×2 (on_hand 10); vận đơn V1 đã lấy hàng.

const returnConsumerName = "fulfillment_return_received"

// returnConsumer trả consumer fulfillment_return_received của module (đăng ký qua Module.Consumers).
func (e *applyEnv) returnConsumer() fulfillment.ReturnReceivedConsumer {
	e.t.Helper()
	m := fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Bus: e.bus, Events: events.NewRegistry(),
		Now: func() time.Time { return testDay().Add(20 * time.Hour) },
	})
	for _, c := range m.Consumers() {
		if rc, ok := c.(fulfillment.ReturnReceivedConsumer); ok && c.Name() == returnConsumerName {
			return rc
		}
	}
	e.t.Fatalf("Module.Consumers không có %s", returnConsumerName)
	return fulfillment.ReturnReceivedConsumer{}
}

// returnedReturning đưa vận đơn s sang returning (lấy hàng rồi hoàn), rồi hãng báo đã hoàn về shop; trả phiếu hoàn.
func (e *applyEnv) returnedReturning(o order.Order, s fulfillment.Shipment, vA, vB uuid.UUID) order.Return {
	e.t.Helper()
	e.apply(s.ID, ev("p", fulfillment.CanonPickedUp, 8, 0), ev("r", fulfillment.CanonReturning, 12, 0))
	e.returningIs(o, s, vA, vB)
	e.apply(s.ID, ev("h", fulfillment.CanonReturnedToShop, 18, 0))
	e.shipmentIs(s.ID, fulfillment.StatusReturning)
	e.stockAll(map[uuid.UUID][3]int32{vA: {5, 0, 1}, vB: {10, 0, 2}})
	var r order.Return
	if err := e.app(func(tx pgx.Tx) (err error) {
		r, err = order.CarrierReturnFor(e.t.Context(), tx, e.seller, s.ID)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return r
}

// receiveQty: kho nhận phiếu r theo số lượng (người làm là nhân viên kho của env).
func (e *applyEnv) receiveQty(r order.Return, lines ...order.ReceiveQtyLine) {
	e.t.Helper()
	w := order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := w.ReceiveReturnQty(e.t.Context(), tx, e.seller, r.ID, r.Version, lines,
			order.Actor{TenantID: e.tenant, UserID: e.actor.UserID}, testDay().Add(19*time.Hour))
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("nhận phiếu hoàn: %v", err)
	}
}

// lastOrderEvent đọc sự kiện topic mới nhất của đơn o.
func (e *applyEnv) lastOrderEvent(o uuid.UUID, topic string) events.StoredEvent {
	e.t.Helper()
	ev := events.StoredEvent{AggregateType: "order", AggregateID: o.String(), Topic: topic}
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT id, tenant_id, seq, payload FROM platform.domain_events
			WHERE aggregate_type = 'order' AND aggregate_id = $1 AND topic = $2 ORDER BY seq DESC LIMIT 1`,
			o.String(), topic).Scan(&ev.ID, &ev.TenantID, &ev.Seq, &ev.Payload)
	})
	return ev
}

// handle chạy consumer c trên sự kiện ev trong một transaction crm_app của tenant.
func (e *applyEnv) handle(c events.Consumer, ev events.StoredEvent) error {
	return e.app(func(tx pgx.Tx) error { return c.Handle(e.t.Context(), ev, tx) })
}

func (e *applyEnv) mustHandle(c events.Consumer, ev events.StoredEvent) {
	e.t.Helper()
	if err := e.handle(c, ev); err != nil {
		e.t.Fatalf("consumer %s: %v", c.Name(), err)
	}
}

// returnedAt đọc returned_at, version của vận đơn id.
func (e *applyEnv) returnedAt(id uuid.UUID) (*time.Time, int64) {
	e.t.Helper()
	var at *time.Time
	var v int64
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT returned_at, version FROM fulfillment.shipments WHERE id = $1`, id).
			Scan(&at, &v)
	})
	return at, v
}

// damagedOf đọc ngăn hỏng của biến thể v.
func (e *applyEnv) damagedOf(v uuid.UUID) int {
	e.t.Helper()
	return e.count(`SELECT damaged FROM inventory.stock_balances WHERE variant_id = $1`, v)
}

// anomalyDetail đọc detail của bất thường kind (mở) của vận đơn id; không có → nil.
func (e *applyEnv) anomalyDetail(id uuid.UUID, kind string) map[string]any {
	e.t.Helper()
	var raw []byte
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		err := tx.QueryRow(e.t.Context(), `SELECT detail FROM fulfillment.shipment_anomalies WHERE shipment_id = $1
			AND kind = $2 AND status = 'open'`, id, kind).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err //nolint:wrapcheck // test
	})
	if raw == nil {
		return nil
	}
	var d map[string]any
	if err := json.Unmarshal(raw, &d); err != nil {
		e.t.Fatal(err)
	}
	return d
}

// TD02/AT11: hãng báo hoàn, rồi đã hoàn về shop → V1 vẫn returning, tồn chưa tăng (có thể bán vA 4, vB 8). Kho nhận
// vA S1, vB S2 → phiếu restocked, D1 returned, in_transit 0; sau consumer V1 returned, returned_at, một
// shipment.status_changed; chạy lại consumer không đổi gì. order.returned mang kind, shipmentId; consumer chỉ số khách
// của M2 đếm returned_count.
func TestReturnReceived_TD02(t *testing.T) {
	e, o, vA, vB, s := newD1Shipment(t)
	r := e.returnedReturning(o, s, vA, vB)
	iA, iB := itemOfOrder(t, o, vA), itemOfOrder(t, o, vB)
	e.receiveQty(r, order.ReceiveQtyLine{OrderItemID: iA, Sellable: 1}, order.ReceiveQtyLine{OrderItemID: iB, Sellable: 2})
	e.orderIs(o.ID, order.StatusReturned)
	e.stockAll(map[uuid.UUID][3]int32{vA: {5, 0, 0}, vB: {10, 0, 0}})
	e.shipmentIs(s.ID, fulfillment.StatusReturning)

	ev := e.lastOrderEvent(o.ID, order.TopicOrderReturned)
	before := e.statusEvents(s.ID)
	c := e.returnConsumer()
	e.mustHandle(c, ev)
	e.shipmentIs(s.ID, fulfillment.StatusReturned)
	at, v := e.returnedAt(s.ID)
	if at == nil || !at.Equal(testDay().Add(20*time.Hour)) {
		t.Fatalf("returned_at = %v", at)
	}
	if n := e.statusEvents(s.ID); n != before+1 {
		t.Fatalf("shipment.status_changed = %d, muốn %d", n, before+1)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.shipment_anomalies WHERE shipment_id = $1 AND kind IN
		('missing_items', 'invalid_edge')`, s.ID); n != 0 {
		t.Errorf("bất thường = %d", n)
	}
	e.mustHandle(c, ev)
	if _, v2 := e.returnedAt(s.ID); v2 != v || e.statusEvents(s.ID) != before+1 {
		t.Errorf("chạy lại consumer đổi vận đơn: version %d → %d", v, v2)
	}

	stats := customer.StatsConsumer{W: customer.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}}
	e.mustHandle(stats, ev)
	var st customer.Stats
	if err := e.app(func(tx pgx.Tx) (err error) {
		st, err = customer.StatsFor(e.t.Context(), tx, o.CustomerID)
		return err //nolint:wrapcheck // test
	}); err != nil || st.ReturnedCount != 1 {
		t.Errorf("returned_count = %d (%v)", st.ReturnedCount, err)
	}
}

// TD04/AT12 ca a: vB S1 D1 M0 → vB on_hand 10, damaged 1 (có thể bán 9); không bất thường; V1 returned.
func TestReturnReceived_TD04Damaged(t *testing.T) {
	e, o, vA, vB, s := newD1Shipment(t)
	r := e.returnedReturning(o, s, vA, vB)
	e.receiveQty(r, order.ReceiveQtyLine{OrderItemID: itemOfOrder(t, o, vA), Sellable: 1},
		order.ReceiveQtyLine{OrderItemID: itemOfOrder(t, o, vB), Sellable: 1, Damaged: 1})
	e.stockAll(map[uuid.UUID][3]int32{vA: {5, 0, 0}, vB: {10, 0, 0}})
	if d := e.damagedOf(vB); d != 1 {
		t.Errorf("vB damaged = %d", d)
	}
	e.mustHandle(e.returnConsumer(), e.lastOrderEvent(o.ID, order.TopicOrderReturned))
	e.shipmentIs(s.ID, fulfillment.StatusReturned)
	if d := e.anomalyDetail(s.ID, "missing_items"); d != nil {
		t.Errorf("missing_items khi không thiếu: %v", d)
	}
}

// TD04/AT12 ca b: vB S0 D1 M1 → vB on_hand 9 (bước loss 1), damaged 1 (có thể bán 8); V1 returned và bất thường
// missing_items với detail.missingQty = 1.
func TestReturnReceived_TD04Missing(t *testing.T) {
	e, o, vA, vB, s := newD1Shipment(t)
	r := e.returnedReturning(o, s, vA, vB)
	e.receiveQty(r, order.ReceiveQtyLine{OrderItemID: itemOfOrder(t, o, vA), Sellable: 1},
		order.ReceiveQtyLine{OrderItemID: itemOfOrder(t, o, vB), Damaged: 1, Missing: 1})
	e.stockAll(map[uuid.UUID][3]int32{vA: {5, 0, 0}, vB: {9, 0, 0}})
	if d := e.damagedOf(vB); d != 1 {
		t.Errorf("vB damaged = %d", d)
	}
	if n := e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE variant_id = $1 AND step = 'loss'
		AND delta = -1`, vB); n != 1 {
		t.Errorf("sổ loss của vB = %d", n)
	}
	e.mustHandle(e.returnConsumer(), e.lastOrderEvent(o.ID, order.TopicOrderReturned))
	e.shipmentIs(s.ID, fulfillment.StatusReturned)
	if d := e.anomalyDetail(s.ID, "missing_items"); d == nil || d["missingQty"] != float64(1) {
		t.Fatalf("missing_items detail = %v", d)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.shipments WHERE id = $1 AND needs_review`, s.ID); n != 1 {
		t.Error("vận đơn chưa đặt needs_review")
	}
}

// Sự kiện không thuộc vận đơn (phiếu khách trả, không shipmentId) → bỏ qua. Sự kiện phiếu hãng mà vận đơn chưa
// returning (còn awaiting_pickup) → không đổi trạng thái, mở "Cần xem" invalid_edge với lý do return_without_returning.
// Payload hỏng → lỗi vĩnh viễn.
func TestReturnReceived_Guards(t *testing.T) {
	e, o, _, _, s := newD1Shipment(t)
	c := e.returnConsumer()
	payload := func(kind string, shipment *uuid.UUID) json.RawMessage {
		b, err := json.Marshal(map[string]any{
			"orderId": o.ID, "sellerId": e.seller, "customerId": o.CustomerID, "status": "returned", "version": 9,
			"returnId": uuid.New(), "kind": kind, "shipmentId": shipment, "missingQty": 0,
		})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	base := events.StoredEvent{
		ID: uuid.New(), TenantID: e.tenant, AggregateType: "order", AggregateID: o.ID.String(),
		Seq: 9, Topic: order.TopicOrderReturned,
	}
	before := e.statusEvents(s.ID)

	ign := base
	ign.Payload = payload(order.ReturnCustomer, &s.ID)
	e.mustHandle(c, ign)
	ign.Payload = payload(order.ReturnCarrier, nil)
	e.mustHandle(c, ign)
	e.shipmentIs(s.ID, fulfillment.StatusAwaitingPickup)
	if d := e.anomalyDetail(s.ID, "invalid_edge"); d != nil {
		t.Fatalf("bất thường khi bỏ qua: %v", d)
	}

	bad := base
	bad.Payload = payload(order.ReturnCarrier, &s.ID)
	e.mustHandle(c, bad)
	e.shipmentIs(s.ID, fulfillment.StatusAwaitingPickup)
	if d := e.anomalyDetail(s.ID, "invalid_edge"); d == nil || d["reason"] != "return_without_returning" ||
		d["status"] != string(fulfillment.StatusAwaitingPickup) {
		t.Fatalf("invalid_edge detail = %v", d)
	}
	if n := e.statusEvents(s.ID); n != before {
		t.Errorf("shipment.status_changed phát khi không đổi: %d → %d", before, n)
	}

	broken := base
	broken.Payload = json.RawMessage(`{"kind":`)
	if err := e.handle(c, broken); !isPermanent(err) {
		t.Errorf("payload hỏng: %v", err)
	}
	other := base
	other.Payload = payload(order.ReturnCarrier, new(uuid.New()))
	if err := e.handle(c, other); !isPermanent(err) {
		t.Errorf("vận đơn không có: %v", err)
	}
}

func itemOfOrder(t *testing.T, o order.Order, v uuid.UUID) uuid.UUID {
	t.Helper()
	for _, it := range o.Items {
		if it.VariantID == v {
			return it.ID
		}
	}
	t.Fatalf("đơn không có biến thể %s", v)
	return uuid.Nil
}

func isPermanent(err error) bool { return err != nil && errors.Is(err, queue.ErrPermanent) }
