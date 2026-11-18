package fulfillment_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/carriers/fake"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// createJob trả mã và trạng thái lệnh tạo của vận đơn id.
func (e *shipEnv) createJob(id uuid.UUID) (uuid.UUID, string) {
	e.t.Helper()
	var jobID uuid.UUID
	var status string
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT id, status FROM platform.jobs
			WHERE kind = 'fulfillment.create_shipment' AND payload->>'shipmentId' = $1`, id.String()).Scan(&jobID, &status)
	})
	return jobID, status
}

// cancelledEvents đếm shipment.status_changed với trạng thái cancelled của vận đơn id.
func (e *shipEnv) cancelledEvents(id uuid.UUID) int {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'shipment.status_changed'
		AND aggregate_id = $1 AND payload->>'status' = 'cancelled'`, id.String())
}

// expectCancelled: lệnh tạo ở jobStatus; vận đơn cancelled (version 2); đơn gỡ vận đơn hiện hành (version tăng 1 so với
// lúc đã gắn), vẫn confirmed; tồn giữ nguyên; một sự kiện cancelled với seq = version của vận đơn.
func (e *shipEnv) expectCancelled(s fulfillment.Shipment, attachedVersion int64, jobStatus string, v uuid.UUID, stock [3]int32) {
	e.t.Helper()
	if _, st := e.createJob(s.ID); st != jobStatus {
		e.t.Fatalf("lệnh tạo = %s, muốn %s", st, jobStatus)
	}
	got := e.shipment(s.ID)
	if got.Status != fulfillment.StatusCancelled || got.Version != s.Version+1 {
		e.t.Fatalf("vận đơn = %s version %d, muốn cancelled version %d", got.Status, got.Version, s.Version+1)
	}
	o := e.getOrder(s.OrderID)
	if o.CurrentShipmentID != nil || o.Version != attachedVersion+1 || o.Status != "confirmed" {
		e.t.Fatalf("đơn: current %v, version %d (lúc gắn %d), status %s", o.CurrentShipmentID, o.Version, attachedVersion, o.Status)
	}
	e.stockIs(v, stock)
	if n := e.cancelledEvents(s.ID); n != 1 {
		e.t.Fatalf("sự kiện cancelled = %d, muốn 1", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'shipment.status_changed'
		AND aggregate_id = $1 AND seq = $2`, s.ID.String(), got.Version); n != 1 {
		e.t.Fatalf("sự kiện seq = version %d: %d", got.Version, n)
	}
}

// D-03: hãng từ chối yêu cầu tạo (rejected → dead) → vận đơn creating thành cancelled, đơn gỡ vận đơn hiện hành, hàng
// vẫn giữ cho đơn; tạo lại vận đơn cho cùng đơn được, lần sau hãng nhận → awaiting_pickup. Chạy lại lệnh dead cũ (người
// bấm) không gọi hãng: vận đơn không còn creating.
func TestShipment_CreateRejectedCancels(t *testing.T) {
	eachSeller(t, testShipmentCreateRejectedCancels)
}

func testShipmentCreateRejectedCancels(t *testing.T, e *shipEnv) {
	e.carrier.SetMode(fake.ModeOK)
	parcels := e.carrier.Parcels()
	o, v := e.order(2)
	before := e.stock(v)
	s := e.mustCreateShipment(o.ID)
	attached := e.getOrder(o.ID).Version
	e.carrier.SetMode(fake.ModeReject)
	e.drain()
	e.expectCancelled(s, attached, queue.StatusDead, v, before)
	if e.carrier.Parcels() != parcels {
		t.Fatalf("hãng có %d kiện, muốn %d", e.carrier.Parcels(), parcels)
	}

	e.carrier.SetMode(fake.ModeOK)
	e.retryCreate(s.ID)
	e.drain()
	if e.carrier.Parcels() != parcels {
		t.Fatalf("chạy lại lệnh của vận đơn đã hủy tạo kiện: %d, muốn %d", e.carrier.Parcels(), parcels)
	}
	if n := e.cancelledEvents(s.ID); n != 1 || e.shipment(s.ID).Version != s.Version+1 {
		t.Fatalf("chạy lại lệnh đổi vận đơn đã hủy: sự kiện %d, version %d", n, e.shipment(s.ID).Version)
	}

	s2 := e.mustCreateShipment(o.ID)
	e.drain()
	if got := e.shipment(s2.ID); got.Status != fulfillment.StatusAwaitingPickup || got.TrackingCode == "" {
		t.Fatalf("vận đơn tạo lại = %+v", got)
	}
	if got := e.getOrder(o.ID); got.CurrentShipmentID == nil || *got.CurrentShipmentID != s2.ID {
		t.Fatalf("đơn chưa gắn vận đơn tạo lại: %v", got.CurrentShipmentID)
	}
	if e.carrier.Parcels() != parcels+1 {
		t.Fatalf("hãng có %d kiện, muốn %d", e.carrier.Parcels(), parcels+1)
	}
}

// D-03: lệnh chắc chắn chưa gửi mà quá hạn 10 phút (expired) → vận đơn cancelled, đơn gỡ vận đơn hiện hành.
func TestShipment_CreateExpiredCancels(t *testing.T) {
	e := newShipEnv(t)
	o, v := e.order(1)
	before := e.stock(v)
	s := e.mustCreateShipment(o.ID)
	attached := e.getOrder(o.ID).Version
	e.carrier.SetMode(fake.ModeFailBeforeCreate)
	e.drain()
	if _, st := e.createJob(s.ID); st != "pending" {
		t.Fatalf("lệnh = %s, muốn pending", st)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE platform.jobs SET expires_at = now() - interval '1 second', run_at = now()
			WHERE kind = 'fulfillment.create_shipment' AND payload->>'shipmentId' = $1`, s.ID.String())
		return err //nolint:wrapcheck // test
	})
	e.drain()
	e.expectCancelled(s, attached, queue.StatusExpired, v, before)
	if e.carrier.Parcels() != 0 {
		t.Fatalf("hãng có %d kiện, muốn 0", e.carrier.Parcels())
	}
}

// createTimedOut tạo đơn 1 cái và vận đơn mà hãng tạo kiện nhưng trả lời quá giờ (lệnh tạo thành in_doubt).
func (e *shipEnv) createTimedOut() (order.Order, fulfillment.Shipment) {
	e.t.Helper()
	o, _ := e.order(1)
	s := e.mustCreateShipment(o.ID)
	e.carrier.SetMode(fake.ModeTimeoutAfterCreate)
	e.drain()
	return o, s
}

// retryCreate: người bấm chạy lại lệnh tạo của vận đơn id (queue.RetryJob).
func (e *shipEnv) retryCreate(id uuid.UUID) {
	e.t.Helper()
	jobID, _ := e.createJob(id)
	if err := e.app(func(tx pgx.Tx) error {
		_, _, err := queue.RetryJob(e.t.Context(), tx, e.tenant, jobID)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("chạy lại lệnh tạo: %v", err)
	}
}

// inDoubtMarked trả true khi vận đơn id có dấu create_in_doubt_at.
func (e *shipEnv) inDoubtMarked(id uuid.UUID) bool {
	e.t.Helper()
	return e.count("SELECT count(*) FROM fulfillment.shipments WHERE id = $1 AND create_in_doubt_at IS NOT NULL", id) == 1
}

// stillAttached: vận đơn s còn creating (version không đổi), đơn còn gắn s, không có sự kiện cancelled.
func (e *shipEnv) stillAttached(s fulfillment.Shipment) {
	e.t.Helper()
	got := e.shipment(s.ID)
	o := e.getOrder(s.OrderID)
	if got.Status != fulfillment.StatusCreating || got.Version != s.Version || o.CurrentShipmentID == nil ||
		*o.CurrentShipmentID != s.ID || e.cancelledEvents(s.ID) != 0 {
		e.t.Fatalf("vận đơn %s version %d, đơn gắn %v, sự kiện cancelled %d; muốn creating version %d còn gắn",
			got.Status, got.Version, o.CurrentShipmentID, e.cancelledEvents(s.ID), s.Version)
	}
}

// Lệnh từng in_doubt (hãng đã tạo kiện nhưng trả lời quá giờ) → vận đơn có dấu, vẫn creating và gắn; người chạy lại,
// lần này hãng từ chối (vd. mã trùng) → dead nhưng KHÔNG hủy: kiện của lần gửi đầu có thể còn ở hãng (reviewer Task 408).
func TestShipment_InDoubtThenDeadKeeps(t *testing.T) {
	e := newShipEnv(t)
	o, s := e.createTimedOut()
	if _, st := e.createJob(s.ID); st != queue.StatusInDoubt || !e.inDoubtMarked(s.ID) {
		t.Fatalf("lệnh = %s, dấu in_doubt %v; muốn in_doubt, có dấu", st, e.inDoubtMarked(s.ID))
	}
	e.stillAttached(s)
	e.carrier.SetMode(fake.ModeReject)
	e.retryCreate(s.ID)
	e.drain()
	if _, st := e.createJob(s.ID); st != queue.StatusDead {
		t.Fatalf("lệnh = %s, muốn dead", st)
	}
	e.stillAttached(s)
	if _, err := e.createShipment(o.ID); err == nil {
		t.Fatal("tạo được vận đơn thứ hai khi kiện đầu có thể còn ở hãng")
	}
	if e.carrier.Parcels() != 1 {
		t.Fatalf("hãng có %d kiện, muốn 1", e.carrier.Parcels())
	}
}

// Hook gọi thẳng: in_doubt chỉ ghi dấu; vận đơn không có, payload hỏng, vận đơn đã qua creating → không đổi; vận đơn
// creating chưa từng in_doubt gọi dead hai lần → hủy một lần, một sự kiện, đơn gỡ vận đơn.
func TestShipment_CreateTerminalHookDirect(t *testing.T) {
	e := newShipEnv(t)
	hook, ok := e.cmds[0].(queue.TerminalHook)
	if !ok {
		t.Fatal("CreateCommand phải hiện thực queue.TerminalHook")
	}
	call := func(id uuid.UUID, status string, payload []byte) {
		t.Helper()
		if payload == nil {
			payload, _ = json.Marshal(map[string]string{"shipmentId": id.String()})
		}
		j := queue.Job{ID: uuid.New(), Kind: fulfillment.CreateCommandKind().Name, TenantID: &e.tenant, Payload: payload}
		if err := e.app(func(tx pgx.Tx) error { return hook.OnTerminal(t.Context(), j, status, tx) }); err != nil {
			t.Fatalf("OnTerminal(%s): %v", status, err)
		}
	}

	_, _, done := e.shipped(1)
	call(done.ID, queue.StatusInDoubt, nil)
	call(done.ID, queue.StatusDead, nil)
	call(done.ID, queue.StatusExpired, nil)
	if got := e.shipment(done.ID); got.Status != fulfillment.StatusAwaitingPickup || got.Version != done.Version ||
		e.inDoubtMarked(done.ID) {
		t.Fatalf("hook đổi vận đơn đã qua creating: %+v, dấu %v", got, e.inDoubtMarked(done.ID))
	}
	if got := e.getOrder(done.OrderID); got.CurrentShipmentID == nil || *got.CurrentShipmentID != done.ID {
		t.Fatalf("hook gỡ vận đơn đã qua creating: %v", got.CurrentShipmentID)
	}

	o, _ := e.order(1)
	s := e.mustCreateShipment(o.ID)
	call(uuid.New(), queue.StatusDead, nil)
	call(s.ID, queue.StatusDead, []byte(`{"x":1}`))
	e.stillAttached(s)
	if e.inDoubtMarked(s.ID) {
		t.Fatal("có dấu in_doubt khi chưa gọi in_doubt")
	}

	call(s.ID, queue.StatusDead, nil)
	call(s.ID, queue.StatusDead, nil)
	if got := e.shipment(s.ID); got.Status != fulfillment.StatusCancelled || got.Version != s.Version+1 {
		t.Fatalf("hook gọi hai lần: %+v", got)
	}
	if n := e.cancelledEvents(s.ID); n != 1 {
		t.Fatalf("sự kiện cancelled = %d, muốn 1", n)
	}
	if got := e.getOrder(o.ID); got.CurrentShipmentID != nil {
		t.Fatalf("đơn còn gắn vận đơn đã hủy: %v", got.CurrentShipmentID)
	}

	o3, _ := e.order(1)
	s3 := e.mustCreateShipment(o3.ID)
	call(s3.ID, queue.StatusInDoubt, nil)
	call(s3.ID, queue.StatusExpired, nil)
	if !e.inDoubtMarked(s3.ID) {
		t.Fatal("in_doubt không ghi dấu")
	}
	e.stillAttached(s3)
}
