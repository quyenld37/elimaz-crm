package fulfillment_test

import (
	"testing"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/carriers/fake"
)

// Webhook cùng eventId gửi 3 lần → 1 lệnh tra trạng thái, 1 dòng shipment_events; đơn và tồn đổi 1 lần (VC08 bản đầu).
func TestRefresh_SameEventThreeTimes(t *testing.T) {
	eachSeller(t, testRefreshSameEventThreeTimes)
}

func testRefreshSameEventThreeTimes(t *testing.T, e *shipEnv) {
	_, v, s := e.shipped(2)
	e.carrier.Advance(s.TrackingCode, fake.RawPickedUp)
	for range 3 {
		e.signal(s.TrackingCode, "ev-dup")
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.refresh_status'
		AND payload->>'shipmentId' = $1`, s.ID.String()); n != 1 {
		t.Fatalf("lệnh tra = %d, muốn 1", n)
	}
	e.drain()
	for range 3 {
		e.signal(s.TrackingCode, "ev-dup")
	}
	e.drain()
	if n := e.count("SELECT count(*) FROM fulfillment.shipment_events WHERE shipment_id = $1", s.ID); n != 1 {
		t.Fatalf("sự kiện vận đơn = %d, muốn 1", n)
	}
	if got := e.shipment(s.ID); got.Status != fulfillment.StatusShipping {
		t.Fatalf("vận đơn = %s", got.Status)
	}
	e.stockIs(v, [3]int32{5, 0, 2})
	// Lệnh tra khác eventId nhưng trạng thái hãng không đổi → cùng event_key, không thêm dòng, không đổi gì.
	e.signal(s.TrackingCode, "ev-other")
	e.drain()
	if n := e.count("SELECT count(*) FROM fulfillment.shipment_events WHERE shipment_id = $1", s.ID); n != 1 {
		t.Fatalf("sự kiện vận đơn = %d, muốn 1", n)
	}
	e.stockIs(v, [3]int32{5, 0, 2})
}

// forge: webhook nói "đã giao" nhưng API của hãng vẫn trả trạng thái thật (created) → vận đơn, đơn, tồn không đổi.
func TestRefresh_ForgedSignal(t *testing.T) {
	e := newShipEnv(t)
	o, v, s := e.shipped(2)
	e.signal(s.TrackingCode, "forged-delivered")
	e.drain()
	if got := e.shipment(s.ID); got.Status != fulfillment.StatusAwaitingPickup || got.Version != s.Version {
		t.Fatalf("vận đơn = %s v%d, muốn awaiting_pickup v%d", got.Status, got.Version, s.Version)
	}
	if got := e.getOrder(o.ID); got.Status != "confirmed" {
		t.Fatalf("đơn = %s", got.Status)
	}
	e.stockIs(v, [3]int32{5, 2, 0})
}

// Tin shipping tới sau delivered → không lùi, chỉ thêm dòng sự kiện (VC09 bản đầu).
func TestRefresh_LateShippingAfterDelivered(t *testing.T) {
	e := newShipEnv(t)
	o, v, s := e.shipped(1)
	e.carrier.Advance(s.TrackingCode, fake.RawDelivered)
	e.signal(s.TrackingCode, "ev-delivered")
	e.drain()
	if got := e.shipment(s.ID); got.Status != fulfillment.StatusDelivered {
		t.Fatalf("vận đơn = %s", got.Status)
	}
	e.stockIs(v, [3]int32{4, 0, 0})

	e.carrier.Advance(s.TrackingCode, fake.RawPickedUp) // hãng báo lại "đang giao" muộn
	e.signal(s.TrackingCode, "ev-late")
	e.drain()
	got := e.shipment(s.ID)
	if got.Status != fulfillment.StatusDelivered {
		t.Fatalf("vận đơn lùi về %s", got.Status)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.shipment_events WHERE shipment_id = $1", s.ID); n != 2 {
		t.Fatalf("sự kiện vận đơn = %d, muốn 2", n)
	}
	if got := e.getOrder(o.ID); got.Status != "delivered" {
		t.Fatalf("đơn = %s", got.Status)
	}
	e.stockIs(v, [3]int32{4, 0, 0})
}

// Tín hiệu của mã hãng lạ (không có vận đơn) hay của tài khoản khác → bỏ qua, không lệnh nào.
func TestSignal_UnknownShipmentIgnored(t *testing.T) {
	e := newShipEnv(t)
	e.signal("TRK-NOPE", "ev-x")
	if n := e.count("SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.refresh_status'"); n != 0 {
		t.Fatalf("lệnh tra = %d, muốn 0", n)
	}
}
