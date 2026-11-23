package fulfillment_test

import (
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

func (e *applyEnv) createdEvents(id string) int {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'shipment.created'
		AND aggregate_type = 'shipment' AND aggregate_id = $1`, id)
}

func (e *applyEnv) anomalies(id any, kind string) int {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM fulfillment.shipment_anomalies WHERE shipment_id = $1 AND kind = $2
		AND status = 'open'`, id, kind)
}

// V5 creating + mã → awaiting_pickup, created, phí, một shipment.created và một shipment.status_changed; gọi lại cùng
// mã → Already, không thêm sự kiện; mã khác → anomaly tracking_conflict, needs_review, vận đơn giữ mã cũ.
func TestAttachTrackingCode(t *testing.T) {
	t.Parallel()
	e := newApplyEnv(t)
	o, _, _ := e.d1()
	v5 := e.mustCreateShipment(o.ID)
	code := trackingCode()
	fee := int64(28000)
	r, err := e.attach(v5.ID, code, &fee)
	if err != nil || r.Already || r.Conflict {
		t.Fatalf("gắn mã: %+v %v", r, err)
	}
	got := e.shipment(v5.ID)
	if got.Status != fulfillment.StatusAwaitingPickup || got.TrackingCode != code || got.Fee == nil || *got.Fee != fee {
		t.Fatalf("vận đơn = %+v", got)
	}
	var state string
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT create_state FROM fulfillment.shipments WHERE id = $1`, v5.ID).Scan(&state)
	})
	if state != "created" {
		t.Fatalf("create_state = %q", state)
	}
	if n := e.createdEvents(v5.ID.String()); n != 1 {
		t.Fatalf("shipment.created = %d, muốn 1", n)
	}
	if n := e.statusEvents(v5.ID); n != 1 {
		t.Fatalf("shipment.status_changed = %d, muốn 1", n)
	}

	r, err = e.attach(v5.ID, code, nil)
	if err != nil || !r.Already {
		t.Fatalf("gắn lại cùng mã: %+v %v", r, err)
	}
	if n := e.createdEvents(v5.ID.String()); n != 1 {
		t.Fatalf("shipment.created sau gắn lại = %d, muốn 1", n)
	}

	r, err = e.attach(v5.ID, trackingCode(), nil)
	if err != nil || !r.Conflict {
		t.Fatalf("mã khác: %+v %v", r, err)
	}
	if n := e.anomalies(v5.ID, "tracking_conflict"); n != 1 {
		t.Fatalf("anomaly tracking_conflict = %d, muốn 1", n)
	}
	if got := e.shipment(v5.ID); got.TrackingCode != code {
		t.Fatalf("mã hãng đổi thành %s", got.TrackingCode)
	}
}

// Mã đang thuộc V6 cùng hãng → anomaly ở V5, V5 vẫn creating; transaction dùng tiếp được (savepoint).
func TestAttachTrackingCode_TakenByOther(t *testing.T) {
	t.Parallel()
	e := newApplyEnv(t)
	o5, _, _ := e.d1()
	o6, _, _ := e.d1()
	v5, v6 := e.mustCreateShipment(o5.ID), e.mustCreateShipment(o6.ID)
	code := trackingCode()
	if _, err := e.attach(v6.ID, code, nil); err != nil {
		t.Fatal(err)
	}
	r, err := e.attach(v5.ID, code, nil)
	if err != nil || !r.Conflict {
		t.Fatalf("mã của V6: %+v %v", r, err)
	}
	if got := e.shipment(v5.ID); got.Status != fulfillment.StatusCreating || got.TrackingCode != "" {
		t.Fatalf("V5 = %s %q, muốn creating không mã", got.Status, got.TrackingCode)
	}
	if n := e.anomalies(v5.ID, "tracking_conflict"); n != 1 {
		t.Fatalf("anomaly = %d, muốn 1", n)
	}
	var review bool
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT needs_review FROM fulfillment.shipments WHERE id = $1`, v5.ID).Scan(&review)
	})
	if !review {
		t.Fatal("needs_review = false")
	}
}

// Hai goroutine cùng gắn một mã cho một vận đơn → một lần phát shipment.created.
func TestAttachTrackingCode_Concurrent(t *testing.T) {
	t.Parallel()
	e := newApplyEnv(t)
	o, _, _ := e.d1()
	s := e.mustCreateShipment(o.ID)
	code := trackingCode()
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Go(func() { _, errs[i] = e.attach(s.ID, code, nil) })
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := e.createdEvents(s.ID.String()); n != 1 {
		t.Fatalf("shipment.created = %d, muốn 1", n)
	}
	if n := e.anomalies(s.ID, "tracking_conflict"); n != 0 {
		t.Fatalf("anomaly = %d, muốn 0", n)
	}
}
