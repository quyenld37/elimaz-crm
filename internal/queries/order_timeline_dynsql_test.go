package queries_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/queries"
)

// shipTimelineFixture: đơn o có vận đơn thật (shiptest) với before sự kiện sẵn có (không tính duplicate_state), thêm sự
// kiện hãng trùng trạng thái dup, sự kiện nhân viên staffEv (người làm user), sự kiện hãng bỏ qua vì cũ older (có
// actor_id, phải bị bỏ), phiếu chi pay −50.000 lúc at + 3 phút; đơn empty không vận đơn, không phiếu; tenant B rỗng.
type shipTimelineFixture struct {
	*oneShop
	b                        uuid.UUID
	o, empty                 order.Order
	ship                     uuid.UUID
	dup, staffEv, older, pay uuid.UUID
	before                   int
	at                       time.Time
}

func newShipTimelineFixture(t *testing.T) *shipTimelineFixture {
	t.Helper()
	f := &shipTimelineFixture{oneShop: newOneShop(t)}
	f.b = f.s.Tenant(t, "B")
	f.o, f.empty = f.order(t), f.order(t)
	if err := tenancy.WithTenant(t.Context(), f.s.App, f.a, func(tx pgx.Tx) error {
		sh, err := f.shop.Shipment(t.Context(), tx, f.shop.Seller, f.o.ID, f.user)
		f.ship = sh.ID
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	f.at = time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	f.dup, f.staffEv, f.older, f.pay = uuid.New(), uuid.New(), uuid.New(), uuid.New()
	f.s.Exec(t, f.a, func(tx pgx.Tx) error {
		if err := tx.QueryRow(t.Context(), `SELECT count(*) FROM fulfillment.shipment_events
			WHERE shipment_id = $1 AND ignored_reason IS DISTINCT FROM 'duplicate_state'`, f.ship).Scan(&f.before); err != nil {
			return err //nolint:wrapcheck // test
		}
		if _, err := tx.Exec(t.Context(), `INSERT INTO fulfillment.shipment_events (id, tenant_id, shipment_id, event_key,
				raw_status, status, occurred_at, raw, canonical, source, applied, ignored_reason, actor_id) VALUES
			($2, $1, $5, 'dup', 'x', 'awaiting_pickup', $6, '{}', 'awaiting_pickup', 'carrier_webhook', false, 'duplicate_state', NULL),
			($3, $1, $5, 'st', 'x', 'delivery_failed', $6 + interval '1 minute', '{}', 'delivery_failed', 'staff', true, NULL, $7),
			($4, $1, $5, 'old', 'x', 'shipping', $6 + interval '2 minute', '{}', 'in_transit', 'carrier_api', false, 'older', $7)`,
			f.a, f.dup, f.staffEv, f.older, f.ship, f.at, f.user); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(t.Context(), `INSERT INTO cash.payments (id, tenant_id, order_id, method, amount, received_at,
			actor_id) VALUES ($1, $2, $3, 'refund', -50000, $4, $5)`, f.pay, f.a, f.o.ID, f.at.Add(3*time.Minute), f.user)
		return err //nolint:wrapcheck // test
	})
	return f
}

// read đọc OrderShippingTimeline của đơn id, người bán seller, trong tenant tenant (crm_app).
func (f *shipTimelineFixture) read(t *testing.T, tenant, seller, id uuid.UUID) []queries.OrderTimelineEntry {
	t.Helper()
	var out []queries.OrderTimelineEntry
	if err := tenancy.WithTenant(t.Context(), f.s.App, tenant, func(tx pgx.Tx) error {
		var err error
		out, err = queries.OrderShippingTimeline(t.Context(), tx, seller, id)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// shipmentEntries đếm mục vận đơn của got và kiểm chúng theo thời gian tăng dần.
func shipmentEntries(t *testing.T, got []queries.OrderTimelineEntry) int {
	t.Helper()
	n := 0
	var last time.Time
	for _, e := range got {
		if e.Kind != queries.TimelineShipment {
			continue
		}
		if n > 0 && e.At.Before(last) {
			t.Errorf("sự kiện %s không theo thời gian tăng", e.RefID)
		}
		n, last = n+1, e.At
	}
	return n
}

// OrderShippingTimeline trên PostgreSQL thật (kế hoạch M3, Task M3-28): sự kiện trùng trạng thái không hiện; nhân
// viên → staff có người làm; hãng → carrier không người làm (actor_id của dòng bị bỏ); phiếu chi đúng số tiền âm;
// người bán kia, tenant B và đơn không có gì → không mục vận đơn / rỗng.
func TestOrderShippingTimeline(t *testing.T) {
	f := newShipTimelineFixture(t)
	got := f.read(t, f.a, f.shop.Seller, f.o.ID)
	if n := shipmentEntries(t, got); n != f.before+2 {
		t.Errorf("%d mục vận đơn, muốn %d (có sẵn) + 2", n, f.before)
	}
	byRef := map[uuid.UUID]queries.OrderTimelineEntry{}
	for _, e := range got {
		byRef[e.RefID] = e
	}
	if _, ok := byRef[f.dup]; ok {
		t.Error("sự kiện duplicate_state hiện")
	}
	if e := byRef[f.staffEv]; e.ActorType != "staff" || e.ActorID == nil || *e.ActorID != f.user ||
		e.Summary["status"] != "delivery_failed" || e.Summary["shipmentCode"] == "" || e.Summary["ignoredReason"] != "" {
		t.Errorf("sự kiện nhân viên: %+v", e)
	}
	if e := byRef[f.older]; e.ActorType != "carrier" || e.ActorID != nil || e.Summary["ignoredReason"] != "older" ||
		e.Summary["canonical"] != "in_transit" {
		t.Errorf("sự kiện hãng bỏ qua: %+v", e)
	}
	if e := byRef[f.pay]; e.Kind != queries.TimelinePayment || e.ActorType != "staff" || e.ActorID == nil ||
		*e.ActorID != f.user || e.Summary["method"] != "refund" || e.Summary["amount"] != "-50000" ||
		!e.At.Equal(f.at.Add(3*time.Minute)) {
		t.Errorf("phiếu chi: %+v", e)
	}
	if n := shipmentEntries(t, f.read(t, f.a, f.shop.Partner, f.o.ID)); n != 0 {
		t.Errorf("người bán kia thấy %d sự kiện vận đơn", n)
	}
	if n := len(f.read(t, f.b, f.shop.Seller, f.o.ID)); n != 0 {
		t.Errorf("tenant B thấy %d mục", n)
	}
	if n := len(f.read(t, f.a, f.shop.Seller, f.empty.ID)); n != 0 {
		t.Errorf("đơn chưa vận đơn, chưa thu: %d mục", n)
	}
}
