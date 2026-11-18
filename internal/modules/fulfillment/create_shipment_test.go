package fulfillment_test

import (
	"errors"
	"testing"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/carriers/fake"
	"elimaz.com.vn/crm/internal/platform/commands"
)

// Đường thường: tạo vận đơn (creating, gắn vào đơn, một lệnh) → worker gọi hãng → awaiting_pickup có mã hãng, phí;
// hãng giả có đúng 1 kiện; picked_up → shipping, giữ hàng in_transit; delivered → đơn delivered, on_hand giảm.
func TestShipment_HappyPath(t *testing.T) {
	e := newShipEnv(t)
	o, v := e.order(2)
	s := e.mustCreateShipment(o.ID)
	if s.Status != fulfillment.StatusCreating || s.CODAmount != 530000 || s.TrackingCode != "" ||
		!codePattern.MatchString(s.Code) || s.CarrierKey != fake.Key || s.CarrierAccountID != e.account {
		t.Fatalf("vận đơn mới = %+v", s)
	}
	if got := e.getOrder(o.ID); got.CurrentShipmentID == nil || *got.CurrentShipmentID != s.ID {
		t.Fatalf("đơn chưa gắn vận đơn: %+v", got.CurrentShipmentID)
	}
	if n := e.count("SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.create_shipment'"); n != 1 {
		t.Fatalf("lệnh tạo = %d, muốn 1", n)
	}

	e.drain()
	s = e.shipment(s.ID)
	if s.Status != fulfillment.StatusAwaitingPickup || s.TrackingCode == "" || s.Fee == nil || *s.Fee != 25000 {
		t.Fatalf("sau lệnh tạo = %+v", s)
	}
	if e.carrier.Parcels() != 1 {
		t.Fatalf("hãng giả có %d kiện, muốn 1", e.carrier.Parcels())
	}

	e.carrier.Advance(s.TrackingCode, fake.RawPickedUp)
	e.signal(s.TrackingCode, "ev-1")
	e.drain()
	if got := e.shipment(s.ID); got.Status != fulfillment.StatusShipping {
		t.Fatalf("sau picked_up = %s", got.Status)
	}
	e.stockIs(v, [3]int32{5, 0, 2})

	e.carrier.Advance(s.TrackingCode, fake.RawDelivered)
	e.signal(s.TrackingCode, "ev-2")
	e.drain()
	if got := e.shipment(s.ID); got.Status != fulfillment.StatusDelivered {
		t.Fatalf("sau delivered = %s", got.Status)
	}
	if got := e.getOrder(o.ID); got.Status != "delivered" {
		t.Fatalf("đơn = %s, muốn delivered", got.Status)
	}
	e.stockIs(v, [3]int32{3, 0, 0})
	if n := e.count("SELECT count(*) FROM fulfillment.shipment_events WHERE shipment_id = $1", s.ID); n != 2 {
		t.Fatalf("sự kiện vận đơn = %d, muốn 2", n)
	}
	// awaiting_pickup (lệnh tạo), shipping, delivered: mỗi lần tiến một sự kiện shipment.status_changed, seq = version.
	if n := e.count(`SELECT count(DISTINCT seq) FROM platform.domain_events
		WHERE topic = 'shipment.status_changed' AND aggregate_id = $1`, s.ID.String()); n != 3 {
		t.Fatalf("shipment.status_changed = %d, muốn 3", n)
	}
}

// timeout_after_create: hãng đã tạo kiện nhưng trả lời quá giờ → lệnh in_doubt, không gửi lại; ResolveInDoubt tra theo
// mã → done; hãng vẫn 1 kiện; vận đơn được điền mã hãng qua lần tra trạng thái sau đó.
func TestShipment_TimeoutAfterCreate(t *testing.T) {
	e := newShipEnv(t)
	o, _ := e.order(1)
	s := e.mustCreateShipment(o.ID)
	e.carrier.SetMode(fake.ModeTimeoutAfterCreate)
	e.drain()
	if n := e.count("SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.create_shipment' AND status = 'in_doubt'"); n != 1 {
		t.Fatalf("lệnh in_doubt = %d, muốn 1", n)
	}
	if got := e.shipment(s.ID); got.Status != fulfillment.StatusCreating {
		t.Fatalf("vận đơn = %s, muốn creating", got.Status)
	}
	e.carrier.SetMode(fake.ModeOK)
	done, requeued, still, err := commands.ResolveInDoubt(t.Context(), e.staff.App, e.cmds, 10)
	if err != nil || done != 1 || requeued != 0 || still != 0 {
		t.Fatalf("ResolveInDoubt = %d %d %d %v", done, requeued, still, err)
	}
	e.drain()
	if e.carrier.Parcels() != 1 {
		t.Fatalf("hãng giả có %d kiện, muốn 1", e.carrier.Parcels())
	}
	got := e.shipment(s.ID)
	if got.Status != fulfillment.StatusAwaitingPickup || got.TrackingCode != e.carrier.Tracking(s.Code) {
		t.Fatalf("sau tra lại = %+v", got)
	}
}

// fail_before_create: chắc chắn chưa tạo → not_sent, tự gửi lại lần sau; không có kiện nào cho tới khi hãng nhận.
func TestShipment_FailBeforeCreate(t *testing.T) {
	e := newShipEnv(t)
	o, _ := e.order(1)
	s := e.mustCreateShipment(o.ID)
	e.carrier.SetMode(fake.ModeFailBeforeCreate)
	e.drain()
	if n := e.count("SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.create_shipment' AND status = 'pending'"); n != 1 {
		t.Fatalf("lệnh chờ gửi lại = %d, muốn 1", n)
	}
	if e.carrier.Parcels() != 0 || e.shipment(s.ID).Status != fulfillment.StatusCreating {
		t.Fatalf("hãng có %d kiện, vận đơn %s", e.carrier.Parcels(), e.shipment(s.ID).Status)
	}
}

// Vận đơn thứ hai cho cùng đơn khi vận đơn đầu chưa hủy → ErrShipmentExists, không thêm dòng, không thêm lệnh.
func TestShipment_SecondForSameOrder(t *testing.T) {
	e := newShipEnv(t)
	o, _ := e.order(1)
	e.mustCreateShipment(o.ID)
	if _, err := e.createShipment(o.ID); !errors.Is(err, fulfillment.ErrShipmentExists) {
		t.Fatalf("vận đơn thứ hai: %v, muốn ErrShipmentExists", err)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.shipments WHERE order_id = $1", o.ID); n != 1 {
		t.Fatalf("vận đơn = %d, muốn 1", n)
	}
	if n := e.count("SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.create_shipment'"); n != 1 {
		t.Fatalf("lệnh tạo = %d, muốn 1", n)
	}
}

// Unique một phần (tenant_id, order_id) WHERE status <> 'cancelled' là chốt của database: chèn thẳng vận đơn thứ hai
// bằng crm_app → lỗi; mã sai mẫu → lỗi CHECK.
func TestShipment_DatabaseGuards(t *testing.T) {
	e := newShipEnv(t)
	o, _ := e.order(1)
	s := e.mustCreateShipment(o.ID)
	ins := `INSERT INTO fulfillment.shipments (tenant_id, code, order_id, carrier_key, carrier_account_id, status, cod_amount)
		VALUES ($1, $2, $3, 'fake', $4, 'creating', 0)`
	if err := e.appExec(ins, e.tenant, "E261118ABCDEF", o.ID, e.account); err == nil {
		t.Fatal("vận đơn thứ hai cho cùng đơn chèn được")
	}
	o2, _ := e.order(1)
	if err := e.appExec(ins, e.tenant, "X261118ABCDEF", o2.ID, e.account); err == nil {
		t.Fatal("mã sai mẫu chèn được")
	}
	if err := e.appExec("UPDATE fulfillment.shipments SET status = 'cancelled' WHERE id = $1", s.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.appExec(ins, e.tenant, "E261118ABCDEF", o.ID, e.account); err != nil {
		t.Fatalf("vận đơn đầu đã hủy mà không chèn được vận đơn mới: %v", err)
	}
	if err := e.appExec("UPDATE fulfillment.shipment_events SET raw_status = 'x'"); err == nil {
		t.Fatal("shipment_events sửa được bằng crm_app")
	}
}
