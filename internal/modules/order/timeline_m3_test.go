package order_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/queries"
)

// m3EventSQL chèn một sự kiện vận đơn (chủ bảng; dữ liệu đọc của dòng thời gian).
const m3EventSQL = `INSERT INTO fulfillment.shipment_events
    (id, tenant_id, shipment_id, event_key, raw_status, status, occurred_at, raw, canonical, source, applied,
     ignored_reason, actor_id)
SELECT @id::uuid, tenant_id, id, @id::text, @raw::text, @status::text, @at::timestamptz, '{}', @canonical::text,
       @source::text, @applied::boolean, @ignored::text, @actor::uuid
FROM fulfillment.shipments WHERE id = @shipment_id::uuid`

type m3Event struct {
	id                                 uuid.UUID
	source, status, canonical, ignored string
	actor                              *uuid.UUID
	at                                 time.Time
}

// orderTimelineM3 dựng dòng thời gian như handler getOrderTimeline: order.Timeline, cộng mục vận đơn, thu tiền của
// queries.OrderShippingTimeline, sắp lại bằng order.SortTimeline.
func (e *orderEnv) orderTimelineM3(seller, id uuid.UUID) []order.OrderTimelineItem {
	e.t.Helper()
	var items []order.OrderTimelineItem
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		if items, err = order.Timeline(e.t.Context(), tx, seller, id, auditSource); err != nil {
			return err //nolint:wrapcheck // test
		}
		ship, err := queries.OrderShippingTimeline(e.t.Context(), tx, seller, id)
		for i := range ship {
			x := &ship[i]
			items = append(items, order.OrderTimelineItem{
				Kind: x.Kind, RefID: x.RefID, At: x.At, Summary: x.Summary, ActorType: x.ActorType, ActorID: x.ActorID,
			})
		}
		order.SortTimeline(items)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("dòng thời gian: %v", err)
	}
	return items
}

// timelineM3Want là một mục mong đợi: loại, mã tham chiếu, nguồn, người làm (nil: không có), các khóa summary.
type timelineM3Want struct {
	kind      string
	ref       uuid.UUID
	actorType string
	actor     *uuid.UUID
	summary   map[string]string
}

// checkTimelineM3Item so mục i của items với w, và mục i không mới hơn mục trước.
func checkTimelineM3Item(t *testing.T, i int, items []order.OrderTimelineItem, w timelineM3Want) {
	t.Helper()
	it := items[i]
	if it.Kind != w.kind || it.RefID != w.ref || it.ActorType != w.actorType || (w.actor == nil) != (it.ActorID == nil) ||
		(w.actor != nil && *it.ActorID != *w.actor) {
		t.Errorf("mục %d: %s %s %s %v, muốn %s %s %s %v", i, it.Kind, it.RefID, it.ActorType, it.ActorID, w.kind, w.ref,
			w.actorType, w.actor)
	}
	for k, v := range w.summary {
		if it.Summary[k] != v {
			t.Errorf("mục %d summary[%s] = %q, muốn %q", i, k, it.Summary[k], v)
		}
	}
	if i > 0 && it.At.After(items[i-1].At) {
		t.Errorf("mục %d (%s) mới hơn mục trước", i, it.At)
	}
}

// seedTimelineM3 gắn vận đơn cho đơn d1 rồi ghi 4 sự kiện vận đơn (hãng chờ thu gom; hãng trùng trạng thái; nhân viên
// báo thất bại; hãng báo cũ bị bỏ qua) lúc base + 1…4 phút, một phiếu thu (+5), một phiếu trả tạo (+6) và nhận (+7).
func (e *orderEnv) seedTimelineM3(d1 order.Order) (evs []m3Event, payID, retID uuid.UUID) {
	t := e.t
	t.Helper()
	ship := e.shipM3(d1, m3Ship{carrier: "fake_a", status: "delivery_failed", packing: "packed"})
	user := e.actor.UserID
	base := e.dbNow().Add(time.Hour)
	at := func(m int) time.Time { return base.Add(time.Duration(m) * time.Minute) }
	evs = []m3Event{
		{id: uuid.New(), source: "carrier_api", status: "awaiting_pickup", canonical: "awaiting_pickup", at: at(1)},
		{id: uuid.New(), source: "carrier_webhook", status: "awaiting_pickup", canonical: "awaiting_pickup", ignored: "duplicate_state", at: at(2)},
		{id: uuid.New(), source: "staff", status: "delivery_failed", canonical: "delivery_failed", actor: &user, at: at(3)},
		{id: uuid.New(), source: "carrier_api", status: "shipping", canonical: "in_transit", ignored: "older", at: at(4)},
	}
	payID, retID = uuid.New(), uuid.New()
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		for _, ev := range evs {
			var ignored *string
			if ev.ignored != "" {
				ignored = &ev.ignored
			}
			if _, err := tx.Exec(t.Context(), m3EventSQL, pgx.NamedArgs{
				"id": ev.id, "raw": "R-" + ev.status, "status": ev.status, "at": ev.at, "canonical": ev.canonical,
				"source": ev.source, "applied": ev.ignored == "", "ignored": ignored, "actor": ev.actor, "shipment_id": ship,
			}); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		if _, err := tx.Exec(t.Context(), `INSERT INTO cash.payments (id, tenant_id, order_id, method, amount, received_at,
				actor_id) VALUES ($1, $2, $3, 'bank_transfer', 100000, $4, $5)`, payID, e.tenant, d1.ID, at(5), user); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(t.Context(), `INSERT INTO ord.returns (id, tenant_id, code, order_id, kind, status, created_by,
				created_at, received_by, received_at) VALUES ($1, $2, 'TH0000001', $3, 'customer_return', 'received', $4, $5, $4, $6)`,
			retID, e.tenant, d1.ID, user, at(6), at(7))
		return err //nolint:wrapcheck // test
	})

	return evs, payID, retID
}

// Dòng thời gian của D1 sau khi gửi hãng (kế hoạch M3, Task M3-28; I09): các mốc theo thời gian, mới nhất trước — phiếu
// trả (nhận, tạo), phiếu thu, sự kiện hãng bỏ qua vì cũ, sự kiện nhân viên báo thất bại, sự kiện hãng chờ thu gom, tạo
// đơn; sự kiện trùng trạng thái (duplicate_state) không hiện; dòng hãng actorType = carrier không người làm, dòng nhân
// viên actorType = staff có người làm; summary chỉ mã, trạng thái, loại, số tiền.
func TestTimelineM3_ShipmentPaymentReturn(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	d1, _, _ := e.d1()
	evs, payID, retID := e.seedTimelineM3(d1)
	user := e.actor.UserID
	items := e.orderTimelineM3(e.seller, d1.ID)
	wants := []timelineM3Want{
		{order.TimelineReturn, retID, "staff", &user, map[string]string{"code": "TH0000001", "kind": "customer_return", "status": "received", "step": "received"}},
		{order.TimelineReturn, retID, "staff", &user, map[string]string{"code": "TH0000001", "kind": "customer_return", "status": "received", "step": "created"}},
		{order.TimelinePayment, payID, "staff", &user, map[string]string{"method": "bank_transfer", "amount": "100000"}},
		{order.TimelineShipment, evs[3].id, "carrier", nil, nil},
		{order.TimelineShipment, evs[2].id, "staff", &user, nil},
		{order.TimelineShipment, evs[0].id, "carrier", nil, nil},
	}
	if len(items) != len(wants)+1 {
		t.Fatalf("%d mục, muốn %d: %+v", len(items), len(wants)+1, items)
	}
	for i, w := range wants {
		checkTimelineM3Item(t, i, items, w)
	}
	if s := items[3].Summary; s["ignoredReason"] != "older" || s["status"] != "shipping" || s["shipmentCode"] == "" {
		t.Errorf("sự kiện bỏ qua vì cũ: %v", s)
	}
	if s := items[4].Summary; s["status"] != "delivery_failed" || s["canonical"] != "delivery_failed" || s["ignoredReason"] != "" {
		t.Errorf("sự kiện nhân viên: %v", s)
	}
	last := items[len(items)-1]
	if last.Kind != order.TimelineStatus || last.Summary["to"] != order.StatusConfirmed || last.ActorType != "staff" {
		t.Errorf("mục cuối phải là tạo đơn: %+v", last)
	}
	for _, it := range items {
		if it.RefID == evs[1].id {
			t.Error("sự kiện duplicate_state hiện trong dòng thời gian")
		}
	}

	// Người bán kia: không thấy sự kiện vận đơn của đơn (vận đơn lọc theo người bán, ADR-29).
	if err := e.app(func(tx pgx.Tx) error {
		ship, err := queries.OrderShippingTimeline(t.Context(), tx, e.partner, d1.ID)
		for _, s := range ship {
			if s.Kind == queries.TimelineShipment {
				t.Errorf("người bán kia thấy sự kiện vận đơn %s", s.RefID)
			}
		}
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
}
