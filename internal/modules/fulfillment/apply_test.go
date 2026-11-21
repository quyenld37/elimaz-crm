package fulfillment_test

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/shipstate"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// applyEnv là shipEnv cộng Service của M3 (ApplyTracking, AttachTrackingCode) với sổ sự kiện, báo hiệu ghi lại.
type applyEnv struct {
	*shipEnv
	svc *fulfillment.Service
	bus *testkit.RecordingBus
}

// testDay là ngày của mọi sự kiện trong test (giờ UTC).
func testDay() time.Time { return time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC) }

func newApplyEnv(t *testing.T) *applyEnv {
	t.Helper()
	e := &applyEnv{shipEnv: newShipEnv(t), bus: &testkit.RecordingBus{}}
	e.svc = fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Bus: e.bus, Events: events.NewRegistry(),
	}).Service
	return e
}

// d1 tạo đơn D1 như Task 97: biến thể vA (on_hand 5) 1 cái, vB (on_hand 10) 2 cái; COD = tiền hàng + phí ship.
func (e *applyEnv) d1() (o order.Order, vA, vB uuid.UUID) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		vA = e.shop.Variant(e.t, tx, e.seller, 200000, 5)
		vB = e.shop.Variant(e.t, tx, e.seller, 200000, 10)
		return nil
	})
	if err := e.app(func(tx pgx.Tx) (err error) {
		o, err = e.shop.Order(e.t.Context(), tx, e.seller, e.actor.UserID, 30000,
			order.ItemInput{VariantID: vA, Qty: 1}, order.ItemInput{VariantID: vB, Qty: 2})
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("tạo đơn D1: %v", err)
	}
	return o, vA, vB
}

// trackingCode sinh mã hãng duy nhất trong database thử.
func trackingCode() string { return "FK" + strings.ToUpper(uuid.NewString()[:12]) }

// attach gắn mã hãng code cho vận đơn id trong một transaction crm_app.
func (e *applyEnv) attach(id uuid.UUID, code string, fee *int64) (fulfillment.AttachResult, error) {
	var r fulfillment.AttachResult
	err := e.app(func(tx pgx.Tx) (err error) {
		r, err = e.svc.AttachTrackingCode(e.t.Context(), tx, id, code, fee)
		return err //nolint:wrapcheck // test
	})
	return r, err
}

// awaiting tạo vận đơn cho đơn o rồi gắn mã hãng (creating → awaiting_pickup) qua AttachTrackingCode.
func (e *applyEnv) awaiting(o order.Order) fulfillment.Shipment {
	e.t.Helper()
	s := e.mustCreateShipment(o.ID)
	if _, err := e.attach(s.ID, trackingCode(), nil); err != nil {
		e.t.Fatalf("gắn mã hãng: %v", err)
	}
	got := e.shipment(s.ID)
	if got.Status != fulfillment.StatusAwaitingPickup {
		e.t.Fatalf("vận đơn sau gắn mã = %s, muốn awaiting_pickup", got.Status)
	}
	return got
}

// ev là sự kiện chuẩn có khóa key lúc hh:mm của ngày thử.
func ev(key string, c fulfillment.Canonical, hh, mm int) fulfillment.CarrierEvent {
	return fulfillment.CarrierEvent{
		Key: key, RawStatus: "raw-" + string(c), Canonical: c,
		OccurredAt: testDay().Add(time.Duration(hh)*time.Hour + time.Duration(mm)*time.Minute),
	}
}

func (e *applyEnv) input(id uuid.UUID, evs ...fulfillment.CarrierEvent) fulfillment.ApplyInput {
	return fulfillment.ApplyInput{
		ShipmentID: id, Tracking: fulfillment.Tracking{Events: evs}, Source: fulfillment.SrcCarrierAPI,
		Actor:    order.Actor{TenantID: e.tenant, Type: inventory.ActorCarrier},
		SyncedAt: testDay().Add(23 * time.Hour),
	}
}

func (e *applyEnv) tryApply(in fulfillment.ApplyInput) (fulfillment.ApplyReport, error) {
	var r fulfillment.ApplyReport
	err := e.app(func(tx pgx.Tx) (err error) {
		r, err = e.svc.ApplyTracking(e.t.Context(), tx, in)
		return err //nolint:wrapcheck // test
	})
	return r, err
}

// apply áp các sự kiện evs lên vận đơn id trong một transaction crm_app; lỗi làm test đỏ.
func (e *applyEnv) apply(id uuid.UUID, evs ...fulfillment.CarrierEvent) fulfillment.ApplyReport {
	e.t.Helper()
	r, err := e.tryApply(e.input(id, evs...))
	if err != nil {
		e.t.Fatalf("ApplyTracking: %v", err)
	}
	return r
}

func (e *applyEnv) stockAll(want map[uuid.UUID][3]int32) {
	e.t.Helper()
	for v, w := range want {
		e.stockIs(v, w)
	}
}

// exports đếm dòng phiếu xuất (sổ tồn on_hand âm của đơn) của các biến thể vs.
func (e *applyEnv) exports(vs ...uuid.UUID) int {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE doc_type = 'order' AND bucket = 'on_hand'
		AND delta < 0 AND variant_id = ANY($1)`, vs)
}

// statusEvents đếm sự kiện shipment.status_changed của vận đơn id.
func (e *shipEnv) statusEvents(id uuid.UUID) int {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'shipment.status_changed'
		AND aggregate_type = 'shipment' AND aggregate_id = $1`, id.String())
}

func (e *applyEnv) orderIs(id uuid.UUID, status string) {
	e.t.Helper()
	if got := e.getOrder(id); got.Status != status {
		e.t.Fatalf("đơn = %s, muốn %s", got.Status, status)
	}
}

func (e *applyEnv) shipmentIs(id uuid.UUID, st fulfillment.Status) {
	e.t.Helper()
	if got := e.shipment(id); got.Status != st {
		e.t.Fatalf("vận đơn = %s, muốn %s", got.Status, st)
	}
}

// VC05, VC06, VC08, VC09: lấy hàng → giao xong; gửi lại cùng hành trình hai lần không đổi gì; tin cũ sau thành công
// chỉ vào lịch sử.
func TestApply_PickupDeliverDuplicateLate(t *testing.T) {
	e, o, vA, vB, s := newD1Shipment(t)
	before := e.statusEvents(s.ID)

	r := e.apply(s.ID, ev("e1", fulfillment.CanonPickedUp, 8, 0))
	if r.Inserted != 1 || r.Applied != 1 || r.From != shipstate.AwaitingPickup || r.To != shipstate.Shipping ||
		!slices.Equal(r.Effects, []shipstate.Effect{shipstate.EffPickedUp}) {
		t.Fatalf("VC05 báo cáo = %+v", r)
	}
	e.shipmentIs(s.ID, fulfillment.StatusShipping)
	e.stockAll(map[uuid.UUID][3]int32{vA: {5, 0, 1}, vB: {10, 0, 2}})
	if n := e.statusEvents(s.ID) - before; n != 1 {
		t.Fatalf("VC05 shipment.status_changed = %d, muốn 1", n)
	}
	var from, to string
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT payload->>'from', payload->>'status' FROM platform.domain_events
			WHERE topic = 'shipment.status_changed' AND aggregate_id = $1 ORDER BY seq DESC LIMIT 1`, s.ID.String()).
			Scan(&from, &to)
	})
	if from != "awaiting_pickup" || to != "shipping" {
		t.Fatalf("VC05 payload from %q to %q", from, to)
	}
	if got := e.shipment(s.ID); got.Version != s.Version+1 {
		t.Fatalf("version = %d, muốn %d", got.Version, s.Version+1)
	}

	e.apply(s.ID, ev("e2", fulfillment.CanonDelivered, 15, 0))
	e.shipmentIs(s.ID, fulfillment.StatusDelivered)
	e.orderIs(o.ID, order.StatusDelivered)
	e.stockAll(map[uuid.UUID][3]int32{vA: {4, 0, 0}, vB: {8, 0, 0}})
	if n := e.exports(vA, vB); n != 2 {
		t.Fatalf("VC06 dòng phiếu xuất = %d, muốn 2", n)
	}
	var pickedUp, delivered, lastEvent *time.Time
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT picked_up_at, delivered_at, last_carrier_event_at
			FROM fulfillment.shipments WHERE id = $1`, s.ID).Scan(&pickedUp, &delivered, &lastEvent)
	})
	if pickedUp == nil || !pickedUp.Equal(testDay().Add(8*time.Hour)) || delivered == nil ||
		!delivered.Equal(testDay().Add(15*time.Hour)) || lastEvent == nil || !lastEvent.Equal(testDay().Add(15*time.Hour)) {
		t.Fatalf("mốc picked_up %v delivered %v last_event %v", pickedUp, delivered, lastEvent)
	}

	events := e.statusEvents(s.ID)
	for range 2 {
		r := e.apply(s.ID, ev("e1", fulfillment.CanonPickedUp, 8, 0), ev("e2", fulfillment.CanonDelivered, 15, 0))
		if r.Inserted != 0 || r.Applied != 0 {
			t.Fatalf("VC08 gửi lại: %+v, muốn Inserted 0", r)
		}
	}
	if n := e.exports(vA, vB); n != 2 {
		t.Fatalf("VC08 dòng phiếu xuất = %d, muốn 2", n)
	}
	if n := e.statusEvents(s.ID); n != events {
		t.Fatalf("VC08 sự kiện nội bộ = %d, muốn %d", n, events)
	}

	r = e.apply(s.ID, ev("e9", fulfillment.CanonInTransit, 12, 0))
	if r.Inserted != 1 || r.Applied != 0 || r.Ignored != 1 {
		t.Fatalf("VC09 báo cáo = %+v", r)
	}
	var applied bool
	var reason string
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT applied, ignored_reason FROM fulfillment.shipment_events
			WHERE shipment_id = $1 AND event_key = 'e9'`, s.ID).Scan(&applied, &reason)
	})
	if applied || reason != "older" {
		t.Fatalf("VC09 dòng lịch sử applied %v lý do %q, muốn false older", applied, reason)
	}
	e.orderIs(o.ID, order.StatusDelivered)
	e.shipmentIs(s.ID, fulfillment.StatusDelivered)
}

// VC07: hai lần giao thất bại tạo hai việc gọi lại (lần 1, 2; hạn = lúc thất bại + 24 giờ), giao xong đóng cả hai.
func TestApply_FailedTwiceThenDelivered(t *testing.T) {
	e, o, vA, vB, s := newD1Shipment(t)
	e.apply(s.ID, ev("e1", fulfillment.CanonPickedUp, 8, 0))
	e.apply(s.ID, ev("e2", fulfillment.CanonDeliveryFailed, 10, 0))
	e.shipmentIs(s.ID, fulfillment.StatusDeliveryFailed)
	type task struct {
		attempt int
		due     time.Time
		status  string
		outcome *string
	}
	tasks := func() []task {
		var out []task
		e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
			rows, err := tx.Query(t.Context(), `SELECT attempt_no, due_at, status, outcome FROM fulfillment.callback_tasks
				WHERE shipment_id = $1 ORDER BY attempt_no`, s.ID)
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			var k task
			_, err = pgx.ForEachRow(rows, []any{&k.attempt, &k.due, &k.status, &k.outcome}, func() error {
				out = append(out, k)
				return nil
			})
			return err //nolint:wrapcheck // test
		})
		return out
	}
	got := tasks()
	if len(got) != 1 || got[0].attempt != 1 || !got[0].due.Equal(testDay().Add(34*time.Hour)) || got[0].status != "open" {
		t.Fatalf("việc gọi lại sau lần thất bại 1 = %+v", got)
	}
	e.apply(s.ID, ev("e3", fulfillment.CanonOutForDelivery, 11, 0))
	e.apply(s.ID, ev("e4", fulfillment.CanonDeliveryFailed, 14, 0))
	if got = tasks(); len(got) != 2 || got[1].attempt != 2 || !got[1].due.Equal(testDay().Add(38*time.Hour)) {
		t.Fatalf("việc gọi lại sau lần thất bại 2 = %+v", got)
	}
	e.apply(s.ID, ev("e5", fulfillment.CanonDelivered, 16, 0))
	for _, k := range tasks() {
		if k.status != "closed" || k.outcome == nil || *k.outcome != "delivered" {
			t.Fatalf("việc gọi lại sau giao xong = %+v, muốn closed delivered", k)
		}
	}
	if n := e.exports(vA, vB); n != 2 {
		t.Fatalf("dòng phiếu xuất = %d, muốn 2", n)
	}
	e.orderIs(o.ID, order.StatusDelivered)
}

// Thiếu tin lấy hàng: awaiting_pickup nhận thẳng Delivered → held → in_transit → consumed trong một transaction.
func TestApply_DeliveredWithoutPickup(t *testing.T) {
	e, o, vA, vB, s := newD1Shipment(t)
	r := e.apply(s.ID, ev("d", fulfillment.CanonDelivered, 15, 0))
	if !slices.Equal(r.Effects, []shipstate.Effect{shipstate.EffPickedUp, shipstate.EffDelivered}) {
		t.Fatalf("tác động = %v", r.Effects)
	}
	e.stockAll(map[uuid.UUID][3]int32{vA: {4, 0, 0}, vB: {8, 0, 0}})
	if n := e.exports(vA); n != 1 {
		t.Fatalf("dòng phiếu xuất vA = %d, muốn 1", n)
	}
	if n := e.exports(vB); n != 1 {
		t.Fatalf("dòng phiếu xuất vB = %d, muốn 1", n)
	}
	e.orderIs(o.ID, order.StatusDelivered)
}

// Một lô nhiều sự kiện lộn thứ tự: áp theo OccurredAt tăng dần; mỗi bước đổi trạng thái một shipment.status_changed.
func TestApply_BatchSortedByTime(t *testing.T) {
	e, _, vA, vB, s := newD1Shipment(t)
	before := e.statusEvents(s.ID)
	r := e.apply(s.ID, ev("c", fulfillment.CanonDelivered, 15, 0), ev("a", fulfillment.CanonPickedUp, 8, 0),
		ev("b", fulfillment.CanonInTransit, 9, 0))
	if r.Inserted != 3 || r.Applied != 2 || r.Ignored != 1 || r.From != shipstate.AwaitingPickup ||
		r.To != shipstate.Delivered {
		t.Fatalf("báo cáo = %+v", r)
	}
	if n := e.statusEvents(s.ID) - before; n != 2 {
		t.Fatalf("shipment.status_changed = %d, muốn 2 (awaiting→shipping, shipping→delivered)", n)
	}
	e.stockAll(map[uuid.UUID][3]int32{vA: {4, 0, 0}, vB: {8, 0, 0}})
	if sig := e.bus.Signals(); !slices.ContainsFunc(sig, func(x realtime.Signal) bool {
		return x.Topic == "shipment.changed" && x.Requires == "fulfillment.read" && x.Scope != nil &&
			x.Scope.ID == s.WarehouseID.String() && x.Entity.ID == s.ID.String()
	}) {
		t.Fatalf("không có báo hiệu shipment.changed ở kho của tài khoản hãng: %+v", sig)
	}
	var synced *time.Time
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT last_synced_at FROM fulfillment.shipments WHERE id = $1`, s.ID).
			Scan(&synced)
	})
	if synced == nil || !synced.Equal(testDay().Add(23*time.Hour)) {
		t.Fatalf("last_synced_at = %v", synced)
	}
}

// Phí thật và tiền COD hãng báo được ghi; sync_error, stuck_since xóa.
func TestApply_FeeAndCODCollected(t *testing.T) {
	e, _, _, _, s := newD1Shipment(t)
	if err := e.staff.TryExec(t.Context(), e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE fulfillment.shipments SET last_sync_error = 'x', stuck_since = now()
			WHERE id = $1`, s.ID)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	fee, cod := int64(32000), int64(430000)
	in := e.input(s.ID, ev("d", fulfillment.CanonDelivered, 15, 0))
	in.Tracking.FeeActual, in.Tracking.CODCollected = &fee, &cod
	if _, err := e.tryApply(in); err != nil {
		t.Fatal(err)
	}
	var gotFee, gotCOD *int64
	var syncErr *string
	var stuck *time.Time
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT fee_actual, cod_collected_amount, last_sync_error, stuck_since
			FROM fulfillment.shipments WHERE id = $1`, s.ID).Scan(&gotFee, &gotCOD, &syncErr, &stuck)
	})
	if gotFee == nil || *gotFee != fee || gotCOD == nil || *gotCOD != cod || syncErr != nil || stuck != nil {
		t.Fatalf("fee %v cod %v sync_error %v stuck %v", gotFee, gotCOD, syncErr, stuck)
	}
	var codCollected int64
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT (payload->>'codCollected')::bigint FROM platform.domain_events
			WHERE topic = 'shipment.status_changed' AND aggregate_id = $1 ORDER BY seq DESC LIMIT 1`, s.ID.String()).
			Scan(&codCollected)
	})
	if codCollected != cod {
		t.Fatalf("payload codCollected = %d, muốn %d", codCollected, cod)
	}
}

// VC11: V1 hủy để thay (cancel_kind replace), đơn gắn V3; tin Cancelled muộn của V1 chỉ vào lịch sử V1, tin PickedUp
// của V1 mở anomaly not_current_moved; D1 vẫn gắn V3, tồn không đổi.
func TestApply_LateEventsOfReplacedShipment(t *testing.T) {
	e := newApplyEnv(t)
	o, vA, vB := e.d1()
	v1 := e.awaiting(o)
	if err := e.staff.TryExec(t.Context(), e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE fulfillment.shipments SET cancel_intent = 'replace' WHERE id = $1`, v1.ID)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	r := e.apply(v1.ID, ev("c1", fulfillment.CanonCancelled, 9, 0))
	if !slices.Equal(r.Effects, []shipstate.Effect{shipstate.EffCancelConfirmed}) {
		t.Fatalf("tác động hủy = %v", r.Effects)
	}
	e.cancelKindIs(v1.ID, "replace")
	e.orderIs(o.ID, order.StatusConfirmed)
	v3 := e.awaiting(o)
	stock := map[uuid.UUID][3]int32{vA: {5, 1, 0}, vB: {10, 2, 0}}
	e.stockAll(stock)

	r = e.apply(v1.ID, ev("c2", fulfillment.CanonCancelled, 10, 0))
	if r.Inserted != 1 || r.Applied != 0 {
		t.Fatalf("tin hủy muộn: %+v", r)
	}
	r = e.apply(v1.ID, ev("p1", fulfillment.CanonPickedUp, 11, 0))
	if r.Applied != 0 || !slices.Equal(r.Anomalies, []string{shipstate.AnomalyNotCurrentMoved}) {
		t.Fatalf("tin lấy hàng của vận đơn cũ: %+v", r)
	}
	var reason string
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT ignored_reason FROM fulfillment.shipment_events
			WHERE shipment_id = $1 AND event_key = 'p1'`, v1.ID).Scan(&reason)
	})
	if reason != shipstate.ReasonNotCurrent {
		t.Fatalf("lý do = %q, muốn not_current", reason)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.shipment_anomalies WHERE shipment_id = $1
		AND kind = 'not_current_moved' AND status = 'open'`, v1.ID); n != 1 {
		t.Fatalf("anomaly not_current_moved = %d, muốn 1", n)
	}
	if got := e.getOrder(o.ID); got.CurrentShipmentID == nil || *got.CurrentShipmentID != v3.ID {
		t.Fatalf("đơn gắn %v, muốn V3 %s", got.CurrentShipmentID, v3.ID)
	}
	e.stockAll(stock)
	if n := e.count(`SELECT count(*) FROM fulfillment.shipment_events WHERE shipment_id = $1`, v3.ID); n != 0 {
		t.Fatalf("lịch sử V3 = %d, muốn 0", n)
	}
}

// Hãng xác nhận hủy khi đơn đang cancel_requested → đơn cancelled, nhả hàng; vận đơn cancelled (order_cancel).
func TestApply_CancelConfirmedReleasesStock(t *testing.T) {
	e, o, vA, vB, s := newD1Shipment(t)
	e.cancelRequested(o, s.ID)
	e.apply(s.ID, ev("c", fulfillment.CanonCancelled, 9, 0))
	e.shipmentIs(s.ID, fulfillment.StatusCancelled)
	e.orderIs(o.ID, order.StatusCancelled)
	e.stockAll(map[uuid.UUID][3]int32{vA: {5, 0, 0}, vB: {10, 0, 0}})
	e.cancelKindIs(s.ID, "order_cancel")
}

// Hãng tự hủy (nhân viên không yêu cầu) → cancel_kind carrier_cancelled; đơn confirmed, gỡ vận đơn, giữ hàng.
func TestApply_CarrierCancelledWithoutRequest(t *testing.T) {
	e, o, vA, vB, s := newD1Shipment(t)
	e.apply(s.ID, ev("c", fulfillment.CanonCancelled, 9, 0))
	e.cancelKindIs(s.ID, "carrier_cancelled")
	if got := e.getOrder(o.ID); got.Status != order.StatusConfirmed || got.CurrentShipmentID != nil {
		t.Fatalf("đơn = %s gắn %v", got.Status, got.CurrentShipmentID)
	}
	e.stockAll(map[uuid.UUID][3]int32{vA: {5, 1, 0}, vB: {10, 2, 0}})
}

// Hãng báo hủy khi hàng đã rời kho (đơn cancel_requested, hàng in_transit) → không nhả hàng, anomaly cancel_conflict,
// vận đơn vẫn cancelled theo hãng.
func TestApply_CancelConflictWhenInTransit(t *testing.T) {
	e, o, vA, vB, s := newD1Shipment(t)
	e.cancelRequested(o, s.ID)
	if err := e.app(func(tx pgx.Tx) error {
		w := order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: e.bus}
		_, err := w.OnShipmentEvent(t.Context(), tx, e.seller, o.ID, s.ID, order.StepPickedUp,
			order.Actor{TenantID: e.tenant, Type: inventory.ActorCarrier}, time.Now())
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	r := e.apply(s.ID, ev("c", fulfillment.CanonCancelled, 9, 0))
	if !slices.Contains(r.Anomalies, "cancel_conflict") {
		t.Fatalf("anomaly = %v, muốn cancel_conflict", r.Anomalies)
	}
	e.shipmentIs(s.ID, fulfillment.StatusCancelled)
	e.orderIs(o.ID, order.StatusCancelRequested)
	e.stockAll(map[uuid.UUID][3]int32{vA: {5, 0, 1}, vB: {10, 0, 2}})
	var review bool
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT needs_review FROM fulfillment.shipments WHERE id = $1`, s.ID).Scan(&review)
	})
	if !review {
		t.Fatal("needs_review = false, muốn true")
	}
}

// Task 431: vận đơn awaiting_pickup, đơn đã cancel_requested, hãng báo PickedUp → chỉ EffPickedUp, đơn giữ
// cancel_requested, hàng in_transit.
func TestApply_PickedUpWhileOrderCancelRequested(t *testing.T) {
	e, o, vA, vB, s := newD1Shipment(t)
	e.requestOrderCancel(o)
	r := e.apply(s.ID, ev("p", fulfillment.CanonPickedUp, 8, 0))
	if !slices.Equal(r.Effects, []shipstate.Effect{shipstate.EffPickedUp}) {
		t.Fatalf("tác động = %v", r.Effects)
	}
	e.orderIs(o.ID, order.StatusCancelRequested)
	e.stockAll(map[uuid.UUID][3]int32{vA: {5, 0, 1}, vB: {10, 0, 2}})
}

// Q-T91-2: vận đơn cancel_requested, đơn cancel_requested, hãng báo Returning → đơn về confirmed rồi phiếu hoàn của
// hãng; vận đơn returning; hàng in_transit.
func TestApply_ReturningWhileCancelRequested(t *testing.T) {
	e, o, vA, vB, s := newD1Shipment(t)
	e.cancelRequested(o, s.ID)
	r := e.apply(s.ID, ev("r", fulfillment.CanonReturning, 12, 0))
	want := []shipstate.Effect{shipstate.EffPickedUp, shipstate.EffCancelRejected, shipstate.EffReturning}
	if !slices.Equal(r.Effects, want) {
		t.Fatalf("tác động = %v, muốn %v", r.Effects, want)
	}
	e.returningIs(o, s, vA, vB)
}

// Q-T91-2 (vận đơn chưa nhận yêu cầu hủy): vận đơn awaiting_pickup, đơn cancel_requested, hãng báo Returning → đơn
// về confirmed rồi phiếu hoàn (Quyết định 7), không lỗi.
func TestApply_ReturningAwaitingWhileOrderCancelRequested(t *testing.T) {
	e, o, _, _, s := newD1Shipment(t)
	e.requestOrderCancel(o)
	e.apply(s.ID, ev("r", fulfillment.CanonReturning, 12, 0))
	e.orderIs(o.ID, order.StatusConfirmed)
	if n := e.count(`SELECT count(*) FROM ord.returns WHERE shipment_id = $1 AND kind = 'carrier_return'`, s.ID); n != 1 {
		t.Fatalf("phiếu hoàn = %d, muốn 1", n)
	}
}

// Đang giao rồi hoàn: phiếu hoàn của hãng, việc gọi lại còn mở đóng với carrier_returning; hãng báo đã hoàn về shop
// ghi carrier_reported_returned_at, vận đơn vẫn returning.
func TestApply_ReturningClosesCallbacks(t *testing.T) {
	e, o, _, _, s := newD1Shipment(t)
	e.apply(s.ID, ev("p", fulfillment.CanonPickedUp, 8, 0), ev("f", fulfillment.CanonDeliveryFailed, 10, 0))
	e.apply(s.ID, ev("r", fulfillment.CanonReturning, 12, 0))
	if n := e.count(`SELECT count(*) FROM fulfillment.callback_tasks WHERE shipment_id = $1 AND status = 'closed'
		AND outcome = 'carrier_returning'`, s.ID); n != 1 {
		t.Fatalf("việc gọi lại đóng carrier_returning = %d, muốn 1", n)
	}
	e.apply(s.ID, ev("h", fulfillment.CanonReturnedToShop, 18, 0))
	e.shipmentIs(s.ID, fulfillment.StatusReturning)
	var at *time.Time
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT carrier_reported_returned_at FROM fulfillment.shipments WHERE id = $1`,
			s.ID).Scan(&at)
	})
	if at == nil || !at.Equal(testDay().Add(18*time.Hour)) {
		t.Fatalf("carrier_reported_returned_at = %v", at)
	}
	e.orderIs(o.ID, order.StatusConfirmed)
}

// TD10: hãng sửa "đã giao" thành "hoàn" (tra API) → đảo phiếu xuất, đơn về confirmed, phiếu hoàn.
func TestApply_DeliveryReversed(t *testing.T) {
	e, o, vA, vB, s := newD1Shipment(t)
	e.apply(s.ID, ev("p", fulfillment.CanonPickedUp, 8, 0), ev("d", fulfillment.CanonDelivered, 15, 0))
	r := e.apply(s.ID, ev("r", fulfillment.CanonReturning, 17, 0))
	if !slices.Equal(r.Effects, []shipstate.Effect{shipstate.EffDeliveryReversed, shipstate.EffReturning}) {
		t.Fatalf("tác động = %v", r.Effects)
	}
	e.returningIs(o, s, vA, vB)
}

// Mã lạ, giao một phần, cạnh sai → không áp, anomaly mở một lần (lần hai cùng loại không thêm), needs_review.
func TestApply_AnomaliesOpenOnce(t *testing.T) {
	e, _, _, _, s := newD1Shipment(t)
	r := e.apply(s.ID, ev("u1", fulfillment.CanonUnknown, 8, 0), ev("u2", fulfillment.CanonUnknown, 9, 0),
		ev("pd", fulfillment.CanonPartiallyDelivered, 10, 0))
	if r.Applied != 0 || r.Ignored != 3 {
		t.Fatalf("báo cáo = %+v", r)
	}
	for kind, want := range map[string]int{"unmapped_status": 1, "partial_reported": 1} {
		if n := e.count(`SELECT count(*) FROM fulfillment.shipment_anomalies WHERE shipment_id = $1 AND kind = $2`,
			s.ID, kind); n != want {
			t.Fatalf("anomaly %s = %d, muốn %d", kind, n, want)
		}
	}
	e.shipmentIs(s.ID, fulfillment.StatusAwaitingPickup)
	var status string
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT status FROM fulfillment.shipment_events WHERE shipment_id = $1
			AND event_key = 'u1'`, s.ID).Scan(&status)
	})
	if status != "" {
		t.Fatalf("status của sự kiện lạ = %q, muốn rỗng", status)
	}
}

// Tenant B gọi ApplyTracking với vận đơn của tenant A → ErrNotFound, không ghi gì.
func TestApply_OtherTenantNotFound(t *testing.T) {
	e, _, _, _, s := newD1Shipment(t)
	b := e.staff.Tenant(t, "B")
	err := tenancy.WithTenant(t.Context(), e.staff.App, b, func(tx pgx.Tx) error {
		_, err := e.svc.ApplyTracking(t.Context(), tx, e.input(s.ID, ev("x", fulfillment.CanonPickedUp, 8, 0)))
		return err //nolint:wrapcheck // test
	})
	if !errors.Is(err, fulfillment.ErrNotFound) {
		t.Fatalf("lỗi = %v, muốn ErrNotFound", err)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.shipment_events WHERE shipment_id = $1`, s.ID); n != 0 {
		t.Fatalf("lịch sử = %d, muốn 0", n)
	}
}

// Trạng thái con, lý do có SĐT (như thân thô của hãng có receiver_phone) → dòng lưu không chứa SĐT.
func TestApply_RawHasNoPhone(t *testing.T) {
	e, _, _, _, s := newD1Shipment(t)
	x := ev("f", fulfillment.CanonDeliveryFailed, 10, 0)
	x.SubStatus = `{"receiver_phone":"0901234567"}`
	x.Reason = "receiver_phone=0901234567 không nghe máy"
	e.apply(s.ID, ev("p", fulfillment.CanonPickedUp, 8, 0), x)
	if n := e.count(`SELECT count(*) FROM fulfillment.shipment_events WHERE shipment_id = $1
		AND (raw::text LIKE '%0901234567%' OR sub_status LIKE '%0901234567%')`, s.ID); n != 0 {
		t.Fatalf("dòng lịch sử chứa SĐT: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.callback_tasks WHERE shipment_id = $1
		AND failed_reason LIKE '%0901234567%'`, s.ID); n != 0 {
		t.Fatalf("việc gọi lại chứa SĐT: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.shipments WHERE id = $1 AND sub_status LIKE '%0901234567%'`,
		s.ID); n != 0 {
		t.Fatalf("vận đơn chứa SĐT: %d", n)
	}
}

// Khóa sự kiện rỗng → ErrInvalidTracking, không ghi gì.
func TestApply_InvalidEventKey(t *testing.T) {
	e, _, _, _, s := newD1Shipment(t)
	_, err := e.tryApply(e.input(s.ID, ev("", fulfillment.CanonPickedUp, 8, 0)))
	if !errors.Is(err, fulfillment.ErrInvalidTracking) {
		t.Fatalf("lỗi = %v, muốn ErrInvalidTracking", err)
	}
}

// Song song: 10 goroutine cùng áp Delivered (khóa sự kiện khác nhau) → đúng một EffDelivered, không deadlock.
func TestApply_ConcurrentDelivered(t *testing.T) {
	e, _, vA, vB, s := newD1Shipment(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		delivered int
		errs      []error
	)
	for i := range 10 {
		wg.Go(func() {
			var r fulfillment.ApplyReport
			err := tenancy.WithTenant(ctx, e.staff.App, e.tenant, func(tx pgx.Tx) (err error) {
				r, err = e.svc.ApplyTracking(ctx, tx, e.input(s.ID,
					ev("d"+strconv.Itoa(i), fulfillment.CanonDelivered, 15, i)))
				return err //nolint:wrapcheck // test
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
			}
			if slices.Contains(r.Effects, shipstate.EffDelivered) {
				delivered++
			}
		})
	}
	wg.Wait()
	if len(errs) > 0 {
		t.Fatalf("lỗi: %v", errs)
	}
	if delivered != 1 {
		t.Fatalf("EffDelivered = %d lần, muốn 1", delivered)
	}
	e.stockAll(map[uuid.UUID][3]int32{vA: {4, 0, 0}, vB: {8, 0, 0}})
	if n := e.exports(vA, vB); n != 2 {
		t.Fatalf("dòng phiếu xuất = %d, muốn 2", n)
	}
}

// cancelRequested đưa đơn o sang cancel_requested (Writer.Cancel khi có vận đơn hiện hành) và vận đơn id sang
// cancel_requested (cancel_intent order_cancel) như lệnh hủy của Task 14 đã gửi.
func (e *applyEnv) cancelRequested(o order.Order, id uuid.UUID) {
	e.t.Helper()
	e.requestOrderCancel(o)
	if err := e.staff.TryExec(e.t.Context(), e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), `UPDATE fulfillment.shipments SET status = 'cancel_requested',
			cancel_intent = 'order_cancel', version = version + 1 WHERE id = $1`, id)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
}

// requestOrderCancel đưa đơn o (confirmed, có vận đơn hiện hành) sang cancel_requested.
func (e *applyEnv) requestOrderCancel(o order.Order) {
	e.t.Helper()
	cur := e.getOrder(o.ID)
	if err := e.app(func(tx pgx.Tx) error {
		w := order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: e.bus}
		_, err := w.Cancel(e.t.Context(), tx, e.seller, o.ID, cur.Version, order.CancelCustomerChangedMind, "",
			order.Actor{TenantID: e.tenant, UserID: e.actor.UserID}, time.Now())
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("yêu cầu hủy đơn: %v", err)
	}
	e.orderIs(o.ID, order.StatusCancelRequested)
}

// newD1Shipment: môi trường, đơn D1 (vA 1 cái, vB 2 cái) và vận đơn hiện hành awaiting_pickup của đơn.
func newD1Shipment(t *testing.T) (*applyEnv, order.Order, uuid.UUID, uuid.UUID, fulfillment.Shipment) {
	t.Helper()
	e := newApplyEnv(t)
	o, vA, vB := e.d1()
	return e, o, vA, vB, e.awaiting(o)
}

// cancelKindIs kiểm cancel_kind của vận đơn id.
func (e *applyEnv) cancelKindIs(id uuid.UUID, want string) {
	e.t.Helper()
	var kind string
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT cancel_kind FROM fulfillment.shipments WHERE id = $1`, id).Scan(&kind)
	})
	if kind != want {
		e.t.Fatalf("cancel_kind = %q, muốn %q", kind, want)
	}
}

// returningIs: vận đơn s returning, đơn o confirmed, hàng D1 đang đi (in_transit), đúng một phiếu hoàn requested.
func (e *applyEnv) returningIs(o order.Order, s fulfillment.Shipment, vA, vB uuid.UUID) {
	e.t.Helper()
	e.shipmentIs(s.ID, fulfillment.StatusReturning)
	e.orderIs(o.ID, order.StatusConfirmed)
	e.stockAll(map[uuid.UUID][3]int32{vA: {5, 0, 1}, vB: {10, 0, 2}})
	if n := e.count(`SELECT count(*) FROM ord.returns WHERE shipment_id = $1 AND kind = 'carrier_return'
		AND status = 'requested'`, s.ID); n != 1 {
		e.t.Fatalf("phiếu hoàn = %d, muốn 1", n)
	}
}
