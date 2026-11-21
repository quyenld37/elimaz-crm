package fulfillment_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/carriers/fake"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// Hủy vận đơn, đổi hãng, hủy đơn đã gửi hãng (kế hoạch M3, Task M3-14): TD03, VC11, VC12 và các ca biên. Hãng giả trong
// bộ nhớ hủy kiện theo chế độ (fake.CancelMode*); lệnh hủy chạy trong worker cùng lệnh tạo và lệnh tra.

// newCancelEnv: shipEnv có thêm lệnh hủy trong worker.
func newCancelEnv(t *testing.T) *shipEnv {
	t.Helper()
	e := newShipEnv(t)
	e.useCommands(append(e.cmds, fulfillment.NewCancelCommand(e.svc)))
	return e
}

// orderCancelConsumer trả consumer order.cancel_requested của module (cùng hãng giả với e).
func (e *shipEnv) orderCancelConsumer() events.Consumer { //nolint:ireturn // consumer của module chỉ lộ qua interface
	e.t.Helper()
	m := fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Events: events.NewRegistry(), Bus: &realtimetest.RecordingBus{},
		Carriers: mustRegistry(e.t, e.carrier),
	})
	for _, c := range m.Consumers() {
		if c.Name() == fulfillment.OrderCancelConsumerName {
			return c
		}
	}
	e.t.Fatalf("không có consumer %s", fulfillment.OrderCancelConsumerName)
	return nil
}

// deliverCancelRequested giao sự kiện order.cancel_requested của đơn id cho consumer (như worker giao).
func (e *shipEnv) deliverCancelRequested(id uuid.UUID) error {
	p, err := json.Marshal(order.Changed{OrderID: id, SellerID: e.seller, Status: order.StatusCancelRequested})
	if err != nil {
		return err //nolint:wrapcheck // test
	}
	c := e.orderCancelConsumer()
	return e.app(func(tx pgx.Tx) error {
		return c.Handle(e.t.Context(), events.StoredEvent{
			ID: uuid.New(), TenantID: e.tenant, AggregateType: "order", AggregateID: id.String(),
			Topic: "order.cancel_requested", Payload: p,
		}, tx)
	})
}

// cancelOrder: khách hủy đơn o (M2 cancelOrder → cancel_requested) rồi worker giao sự kiện cho fulfillment.
func (e *shipEnv) cancelOrder(o order.Order) {
	e.t.Helper()
	e.requestCancel(o)
	if err := e.deliverCancelRequested(o.ID); err != nil {
		e.t.Fatalf("giao order.cancel_requested: %v", err)
	}
}

// cancelShipment gọi RequestCancel của nhân viên (đổi hãng) cho vận đơn id.
func (e *shipEnv) cancelShipment(id uuid.UUID, checked bool) (fulfillment.ShipmentView, error) {
	var v fulfillment.ShipmentView
	err := e.app(func(tx pgx.Tx) (err error) {
		v, err = e.svc.RequestCancel(e.t.Context(), tx, id, fulfillment.CancelInput{
			Intent: "replace", Reason: "Đổi sang hãng khác", CarrierChecked: checked,
		}, e.orderActor())
		return err //nolint:wrapcheck // test
	})
	return v, err
}

// cancelJobs đếm lệnh hủy của vận đơn id.
func (e *shipEnv) cancelJobs(id uuid.UUID) int {
	e.t.Helper()
	return e.count("SELECT count(*) FROM platform.jobs WHERE kind = $1 AND dedupe_key = $2",
		fulfillment.CancelCommandKind().Name, "cancel:"+id.String())
}

// expectView kiểm trạng thái, loại hủy của vận đơn id.
func (e *shipEnv) expectView(id uuid.UUID, status, kind string) fulfillment.ShipmentView {
	e.t.Helper()
	v := e.view(id)
	if v.Status != status || v.CancelKind != kind {
		e.t.Fatalf("vận đơn = %s %q, muốn %s %q", v.Status, v.CancelKind, status, kind)
	}
	return v
}

// expectOrder kiểm trạng thái đơn id và vận đơn hiện hành (nil: không có).
func (e *shipEnv) expectOrder(id uuid.UUID, status string, current *uuid.UUID) {
	e.t.Helper()
	got := e.getOrder(id)
	same := (got.CurrentShipmentID == nil) == (current == nil) &&
		(current == nil || *got.CurrentShipmentID == *current)
	if got.Status != status || !same {
		e.t.Fatalf("đơn = %s gắn %v, muốn %s gắn %v", got.Status, got.CurrentShipmentID, status, current)
	}
}

// syncNow ghi lệnh tra trạng thái của vận đơn id (syncShipment) rồi chạy worker.
func (e *shipEnv) syncNow(id uuid.UUID) {
	e.t.Helper()
	if err := e.app(func(tx pgx.Tx) error {
		_, err := e.svc.SyncShipment(e.t.Context(), tx, id, e.actor)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("tra ngay: %v", err)
	}
	e.drain()
}

// TD03: vận đơn chờ thu gom, khách hủy đơn → đơn cancel_requested, hàng vẫn giữ; hãng xác nhận hủy → đơn cancelled,
// nhả hàng, vận đơn cancelled order_cancel; gọi hãng hủy 1 lần.
func TestCancel_TD03OrderCancelConfirmed(t *testing.T) {
	e := newCancelEnv(t)
	o, v, s := e.shipped(1)
	e.cancelOrder(o)
	e.expectOrder(o.ID, order.StatusCancelRequested, &s.ID)
	e.stockIs(v, [3]int32{5, 1, 0})
	e.expectView(s.ID, "cancel_requested", "")
	if n := e.cancelJobs(s.ID); n != 1 {
		t.Fatalf("lệnh hủy = %d, muốn 1", n)
	}
	e.drain()
	e.expectOrder(o.ID, order.StatusCancelled, nil)
	e.stockIs(v, [3]int32{5, 0, 0})
	e.expectView(s.ID, "cancelled", "order_cancel")
	if e.carrier.Calls("cancel") != 1 {
		t.Fatalf("gọi hãng hủy %d lần, muốn 1", e.carrier.Calls("cancel"))
	}
}

// VC11: đổi hãng → hãng xác nhận → đơn confirmed không vận đơn, hàng vẫn giữ; gửi lại bằng tài khoản B → V2 chờ thu
// gom; tin muộn Cancelled, PickedUp của V1 → đơn vẫn gắn V2, tồn không đổi, not_current_moved cho tin PickedUp.
func TestCancel_VC11Replace(t *testing.T) {
	e := newCancelEnv(t)
	o, v, s := e.shipped(1)
	if got, err := e.cancelShipment(s.ID, false); err != nil || got.Status != "cancel_requested" {
		t.Fatalf("đổi hãng: %v %+v", err, got)
	}
	e.expectOrder(o.ID, order.StatusConfirmed, &s.ID)
	e.drain()
	e.expectView(s.ID, "cancelled", "replace")
	e.expectOrder(o.ID, order.StatusConfirmed, nil)
	e.stockIs(v, [3]int32{5, 1, 0})
	var b uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) (err error) {
		b, err = fulfillment.EnsureCarrierAccount(t.Context(), tx, fulfillment.SeedAccountInput{
			SellerID: e.seller, WarehouseID: e.shop.Warehouse, CarrierKey: shiptest.CarrierKey, Name: "Hãng giả B",
			ExternalAccountRef: "fake-b",
		})
		return err //nolint:wrapcheck // test
	})
	v2, err := e.ship(fulfillment.ShipInput{OrderID: o.ID, CarrierAccountID: b})
	if err != nil {
		t.Fatalf("gửi lại bằng tài khoản B: %v", err)
	}
	e.drain()
	e.expectView(v2.ID, "awaiting_pickup", "")
	now := time.Now().UTC()
	if err := e.app(func(tx pgx.Tx) error {
		_, err := e.svc.ApplyTracking(t.Context(), tx, fulfillment.ApplyInput{
			ShipmentID: s.ID, Source: fulfillment.SrcCarrierAPI, SyncedAt: now,
			Tracking: fulfillment.Tracking{Events: []fulfillment.CarrierEvent{
				{Key: "late-cancelled", RawStatus: "cancelled", Canonical: fulfillment.CanonCancelled, OccurredAt: now.Add(time.Minute)},
				{Key: "late-picked", RawStatus: "picked_up", Canonical: fulfillment.CanonPickedUp, OccurredAt: now.Add(2 * time.Minute)},
			}},
		})
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatalf("tin muộn của V1: %v", err)
	}
	e.expectOrder(o.ID, order.StatusConfirmed, &v2.ID)
	e.stockIs(v, [3]int32{5, 1, 0})
	e.expectView(s.ID, "cancelled", "replace")
	if n := e.openAnomalies(s.ID, "not_current_moved"); n != 1 {
		t.Fatalf("not_current_moved của V1 = %d, muốn 1", n)
	}
}

// VC12: hãng trả "đã lấy hàng, không hủy được" → đơn về confirmed, vận đơn shipping, hàng in_transit (không trả sổ).
func TestCancel_VC12RejectedPickedUp(t *testing.T) {
	e := newCancelEnv(t)
	o, v, s := e.shipped(1)
	e.carrier.Advance(s.TrackingCode, fake.RawPickedUp)
	e.cancelOrder(o)
	e.drain()
	e.expectOrder(o.ID, order.StatusConfirmed, &s.ID)
	e.expectView(s.ID, "shipping", "")
	e.stockIs(v, [3]int32{5, 0, 1})
}

// Hãng trả "đang xử lý" (CancelPending) → vận đơn giữ cancel_requested, không "Cần xem" giả khi lần tra thấy vẫn chờ
// thu gom; tra bù thấy Cancelled → như TD03; thấy PickedUp → như VC12.
func TestCancel_PendingThenTrack(t *testing.T) {
	for _, c := range []struct {
		name, raw, shipStatus, orderStatus string
		stock                              [3]int32
	}{
		{"hãng hủy", fake.RawCancelled, "cancelled", order.StatusCancelled, [3]int32{5, 0, 0}},
		{"hãng lấy hàng", fake.RawPickedUp, "shipping", order.StatusConfirmed, [3]int32{5, 0, 1}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newCancelEnv(t)
			o, v, s := e.shipped(1)
			e.carrier.SetCancelMode(fake.CancelModePending)
			e.cancelOrder(o)
			e.drain()
			e.expectView(s.ID, "cancel_requested", "")
			e.expectOrder(o.ID, order.StatusCancelRequested, &s.ID)
			e.stockIs(v, [3]int32{5, 1, 0})
			if n := e.openAnomalies(s.ID, "invalid_edge"); n != 0 {
				t.Fatalf("invalid_edge = %d sau lần tra thấy vẫn chờ thu gom", n)
			}
			e.carrier.Advance(s.TrackingCode, c.raw)
			e.syncNow(s.ID)
			if got := e.view(s.ID); got.Status != c.shipStatus {
				t.Fatalf("vận đơn = %s, muốn %s", got.Status, c.shipStatus)
			}
			if got := e.getOrder(o.ID); got.Status != c.orderStatus {
				t.Fatalf("đơn = %s, muốn %s", got.Status, c.orderStatus)
			}
			e.stockIs(v, c.stock)
		})
	}
}

// Hàng đã đi (shipping): đổi hãng → ErrShipmentNotCancellable; hủy đơn → cancel_conflict, không gọi hãng.
func TestCancel_AfterPickup(t *testing.T) {
	e := newCancelEnv(t)
	o, v, s := e.shipped(1)
	e.carrier.Advance(s.TrackingCode, fake.RawPickedUp)
	e.syncNow(s.ID)
	e.expectView(s.ID, "shipping", "")
	if _, err := e.cancelShipment(s.ID, false); !errors.Is(err, fulfillment.ErrShipmentNotCancellable) {
		t.Fatalf("đổi hãng khi shipping: %v, muốn ErrShipmentNotCancellable", err)
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := e.svc.RequestCancel(t.Context(), tx, s.ID, fulfillment.CancelInput{Intent: "order_cancel"},
			order.Actor{TenantID: e.tenant, Type: "system"})
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatalf("hủy đơn khi shipping: %v", err)
	}
	if e.openAnomalies(s.ID, "cancel_conflict") != 1 || e.cancelJobs(s.ID) != 0 || e.carrier.Calls("cancel") != 0 {
		t.Fatalf("cancel_conflict %d, lệnh %d, gọi %d", e.openAnomalies(s.ID, "cancel_conflict"), e.cancelJobs(s.ID),
			e.carrier.Calls("cancel"))
	}
	e.expectOrder(o.ID, order.StatusConfirmed, &s.ID)
	e.stockIs(v, [3]int32{5, 0, 1})
}

// "Không rõ" (unknown): đổi hãng → ErrCreateInProgress; nhân viên đã tra hãng (carrierChecked) → cancelled
// staff_after_unknown, lệnh tạo bỏ, việc in_doubt_create đóng, đơn confirmed không vận đơn, hàng vẫn giữ.
func TestCancel_UnknownCreate(t *testing.T) {
	e := newCancelEnv(t)
	ord, v := e.order(1)
	s := e.mustCreateShipment(ord.ID)
	e.carrier.SetMode(fake.ModeTimeoutNoCreate)
	e.drain()
	o := ord.ID
	if _, err := e.cancelShipment(s.ID, false); !errors.Is(err, fulfillment.ErrCreateInProgress) {
		t.Fatalf("đổi hãng khi unknown: %v, muốn ErrCreateInProgress", err)
	}
	if _, err := e.cancelShipment(s.ID, true); err != nil {
		t.Fatalf("đổi hãng sau khi tra: %v", err)
	}
	e.expectView(s.ID, "cancelled", "staff_after_unknown")
	e.expectOrder(o, order.StatusConfirmed, nil)
	if _, st := e.createJob(s.ID); st != queue.StatusExpired || e.openAnomalies(s.ID, "in_doubt_create") != 0 {
		t.Fatalf("lệnh tạo = %s, việc mở %d", st, e.openAnomalies(s.ID, "in_doubt_create"))
	}
	e.stockIs(v, [3]int32{5, 1, 0})
}

// Khách hủy đơn trong lúc lệnh tạo đang gọi hãng (sau khi Execute đọc trạng thái đơn), hãng tạo được kiện → khi gắn
// mã hãng, lệnh hủy được ghi ngay; hãng xác nhận → đơn cancelled, nhả hàng. Hai thứ tự: sự kiện tới trước khi lệnh tạo
// xong (vận đơn ghi ý định), hay sau (vận đơn đã cancel_requested, không ghi lệnh thứ hai).
func TestCancel_DuringCreate(t *testing.T) {
	for _, early := range []bool{true, false} {
		t.Run(map[bool]string{true: "sự kiện trước", false: "sự kiện sau"}[early], func(t *testing.T) {
			e := newCancelEnv(t)
			o, v := e.order(1)
			s := e.mustCreateShipment(o.ID)
			e.cancelInCreate(o.ID, early)
			e.drain()
			if !early {
				if err := e.deliverCancelRequested(o.ID); err != nil {
					t.Fatalf("giao sự kiện sau: %v", err)
				}
				e.drain()
			}
			e.expectView(s.ID, "cancelled", "order_cancel")
			e.expectOrder(o.ID, order.StatusCancelled, nil)
			e.stockIs(v, [3]int32{5, 0, 0})
			if e.cancelJobs(s.ID) != 1 || e.carrier.Calls("cancel") != 1 || e.carrier.Calls("create") != 1 {
				t.Fatalf("lệnh hủy %d, gọi hủy %d, gọi tạo %d", e.cancelJobs(s.ID), e.carrier.Calls("cancel"),
					e.carrier.Calls("create"))
			}
		})
	}
}

// cancelInCreate: khách hủy đơn orderID trong lúc lệnh tạo gọi hãng; deliver: sự kiện order.cancel_requested tới
// consumer ngay lúc đó (trước khi lệnh tạo xong).
func (e *shipEnv) cancelInCreate(orderID uuid.UUID, deliver bool) {
	e.carrier.SetCreateHook(func() {
		e.carrier.SetCreateHook(nil)
		if err := e.tryRequestCancel(orderID); err != nil {
			e.t.Errorf("yêu cầu hủy trong lúc gửi: %v", err)
		}
		if !deliver {
			return
		}
		if err := e.deliverCancelRequested(orderID); err != nil {
			e.t.Errorf("giao sự kiện trong lúc gửi: %v", err)
		}
	})
}

// Vận đơn đã not_sent rồi khách mới hủy đơn → vận đơn cancelled ngay, đơn cancelled, không gọi hãng hủy.
func TestCancel_NotSentThenOrderCancel(t *testing.T) {
	e := newCancelEnv(t)
	o, s := e.notSentShipment()
	e.cancelOrder(o)
	v := e.expectView(s.ID, "cancelled", "order_cancel")
	if v.CreateState != "not_sent" {
		t.Fatalf("create_state = %s", v.CreateState)
	}
	e.expectOrder(o.ID, order.StatusCancelled, nil)
	if e.carrier.Calls("cancel") != 0 || e.cancelJobs(s.ID) != 0 {
		t.Fatalf("gọi hãng hủy %d lần", e.carrier.Calls("cancel"))
	}
}

// Sự kiện order.cancel_requested giao lại hai lần → một lệnh hủy, một lần gọi hãng.
func TestCancel_EventRedelivered(t *testing.T) {
	e := newCancelEnv(t)
	o, _, s := e.shipped(1)
	e.cancelOrder(o)
	if err := e.deliverCancelRequested(o.ID); err != nil {
		t.Fatalf("giao lần hai: %v", err)
	}
	if n := e.cancelJobs(s.ID); n != 1 {
		t.Fatalf("lệnh hủy = %d, muốn 1", n)
	}
	e.drain()
	if err := e.deliverCancelRequested(o.ID); err != nil { // đơn đã cancelled: không làm gì
		t.Fatalf("giao lần ba: %v", err)
	}
	if e.carrier.Calls("cancel") != 1 || e.cancelJobs(s.ID) != 1 {
		t.Fatalf("gọi hãng hủy %d lần", e.carrier.Calls("cancel"))
	}
}

// Lệnh hủy quá giờ sau khi gửi, CancelIdempotent = false → in_doubt, in_doubt_cancel, chạy worker 3 lần không gọi lại;
// tra lại thấy Cancelled → done (đóng in_doubt_cancel), lệnh tra áp → đơn cancelled.
func TestCancel_InDoubtThenLookup(t *testing.T) {
	e := newCancelEnv(t)
	o, v, s := e.shipped(1)
	e.carrier.SetCancelMode(fake.CancelModeTimeoutAfterSend)
	e.cancelOrder(o)
	for range 3 {
		testkit.MakeDueSoon(t, e.staff.Owner) // lệnh hủy vừa ghi có thể lệch hạn vì đồng hồ PostgreSQL lùi (Task 451)
		if _, err := e.worker.RunOnce(t.Context(), 10); err != nil {
			t.Fatal(err)
		}
	}
	if e.carrier.Calls("cancel") != 1 || e.openAnomalies(s.ID, "in_doubt_cancel") != 1 {
		t.Fatalf("gọi hủy %d lần, in_doubt_cancel %d", e.carrier.Calls("cancel"), e.openAnomalies(s.ID, "in_doubt_cancel"))
	}
	e.expectView(s.ID, "cancel_requested", "")
	if done, _, _ := e.resolveInDoubt(); done != 1 {
		t.Fatalf("tra lại done = %d, muốn 1", done)
	}
	e.drain()
	e.expectView(s.ID, "cancelled", "order_cancel")
	e.expectOrder(o.ID, order.StatusCancelled, nil)
	e.stockIs(v, [3]int32{5, 0, 0})
	if e.carrier.Calls("cancel") != 1 || e.openAnomalies(s.ID, "in_doubt_cancel") != 0 {
		t.Fatalf("gọi hủy %d lần sau tra lại, in_doubt_cancel còn %d", e.carrier.Calls("cancel"),
			e.openAnomalies(s.ID, "in_doubt_cancel"))
	}
}

// Hãng không hủy qua API → cancel_requested, việc manual_cancel, không lệnh; lệnh hủy chắc chưa gửi (dead) → việc
// manual_cancel; bấm đổi hãng lần hai → ErrShipmentNotCancellable.
func TestCancel_ManualAndDead(t *testing.T) {
	e := newCancelEnv(t)
	_, _, s := e.shipped(1)
	e.carrier.SetCancelSupported(false)
	if _, err := e.cancelShipment(s.ID, false); err != nil {
		t.Fatalf("đổi hãng khi hãng không hủy qua API: %v", err)
	}
	if e.openAnomalies(s.ID, "manual_cancel") != 1 || e.cancelJobs(s.ID) != 0 {
		t.Fatalf("manual_cancel %d, lệnh %d", e.openAnomalies(s.ID, "manual_cancel"), e.cancelJobs(s.ID))
	}
	if _, err := e.cancelShipment(s.ID, false); !errors.Is(err, fulfillment.ErrShipmentNotCancellable) {
		t.Fatalf("bấm lần hai: %v", err)
	}
	e.carrier.SetCancelSupported(true)
	_, _, s2 := e.shipped(1)
	e.carrier.SetCancelMode(fake.CancelModeNotSent)
	if _, err := e.cancelShipment(s2.ID, false); err != nil {
		t.Fatal(err)
	}
	e.drain()
	if st := e.count("SELECT count(*) FROM platform.jobs WHERE dedupe_key = $1 AND status = 'dead'", "cancel:"+s2.ID.String()); st != 1 {
		t.Fatalf("lệnh hủy dead = %d", st)
	}
	if e.openAnomalies(s2.ID, "manual_cancel") != 1 || e.view(s2.ID).Status != "cancel_requested" {
		t.Fatalf("manual_cancel %d, vận đơn %s", e.openAnomalies(s2.ID, "manual_cancel"), e.view(s2.ID).Status)
	}
}

// AutoRetryNotSent của lệnh hủy: chỉ true khi mọi hãng hủy qua API khai CancelIdempotent (Q-24).
func TestCancelCommand_AutoRetry(t *testing.T) {
	e := newShipEnv(t)
	if fulfillment.NewCancelCommand(e.svc).AutoRetryNotSent() {
		t.Fatal("mặc định phải không tự thử lại")
	}
	e.carrier.SetCancelIdempotent(true)
	if !fulfillment.NewCancelCommand(e.svc).AutoRetryNotSent() {
		t.Fatal("hãng khai CancelIdempotent mà không tự thử lại")
	}
	var _ commands.Looker = fulfillment.NewCancelCommand(e.svc)
}
