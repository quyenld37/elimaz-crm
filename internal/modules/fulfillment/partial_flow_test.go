package fulfillment_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
)

// Giao một phần bằng ngoại lệ có quyền (kế hoạch M3, Task M3-26; TD09). Đơn D1: vA ×1 (on_hand 5), vB ×2 (on_hand 10),
// COD 630.000; V1 đang giao. Khách nhận vA ×1, vB ×1, trả 1 vB; hãng thu 430.000.

const td09Collected = 430000

// recordPartial ghi giao một phần cho vận đơn id (người làm là nhân viên của env).
func (e *applyEnv) recordPartial(id uuid.UUID, collected int64, lines ...fulfillment.PartialLine,
) (fulfillment.PartialResult, error) {
	var r fulfillment.PartialResult
	err := e.app(func(tx pgx.Tx) (err error) {
		r, err = e.svc.RecordPartialDelivery(e.t.Context(), tx, id, fulfillment.PartialInput{
			Lines: lines, CODCollected: collected, Reason: "Khách chỉ nhận một phần",
		}, order.Actor{TenantID: e.tenant, UserID: e.actor.UserID})
		return err //nolint:wrapcheck // test
	})
	return r, err
}

// td09Lines: vA giao 1, vB giao 1 (của 2).
func td09Lines(o order.Order, vA, vB uuid.UUID) []fulfillment.PartialLine {
	return []fulfillment.PartialLine{
		{OrderItemID: itemOf(o, vA), DeliveredQty: 1}, {OrderItemID: itemOf(o, vB), DeliveredQty: 1},
	}
}

func itemOf(o order.Order, v uuid.UUID) uuid.UUID {
	for _, it := range o.Items {
		if it.VariantID == v {
			return it.ID
		}
	}
	return uuid.Nil
}

// partialReturnOf đọc phiếu partial_delivery của vận đơn s.
func (e *applyEnv) partialReturnOf(o order.Order, s uuid.UUID) order.Return {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT id FROM ord.returns WHERE order_id = $1 AND shipment_id = $2
			AND kind = 'partial_delivery'`, o.ID, s).Scan(&id)
	})
	return readAs(e.shipEnv, func(ctx context.Context, tx pgx.Tx) (order.Return, error) {
		return order.GetReturn(ctx, tx, e.seller, id)
	})
}

// lastShipmentEvent đọc shipment.status_changed mới nhất của vận đơn id.
func (e *applyEnv) lastShipmentEvent(id uuid.UUID) (events.StoredEvent, fulfillment.StatusChanged) {
	e.t.Helper()
	ev := events.StoredEvent{AggregateType: "shipment", AggregateID: id.String(), Topic: fulfillment.TopicShipmentStatusChanged}
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT id, tenant_id, seq, payload FROM platform.domain_events
			WHERE aggregate_type = 'shipment' AND aggregate_id = $1 AND topic = $2 ORDER BY seq DESC LIMIT 1`,
			id.String(), ev.Topic).Scan(&ev.ID, &ev.TenantID, &ev.Seq, &ev.Payload)
	})
	var p fulfillment.StatusChanged
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		e.t.Fatal(err)
	}
	return ev, p
}

// TD09: sau lệnh D1 delivered; vA on_hand 4; vB on_hand 9, in_transit 1; phiếu partial_delivery 1 vB requested (vận
// đơn V1, tiền hoàn 0, mã TH…); V1 delivered, partial, cod_collected_amount 430.000; shipment.status_changed có
// codCollected, partial; sổ COD: collected 430.000, returned 200.000. Kho nhận 1 vB S1 → vB in_transit 0, on_hand 9
// (hàng đi đường chưa rời sổ; có thể bán tăng 1), phiếu restocked, D1 vẫn delivered, order.partially_returned (partial_delivery, refundAmount 0,
// shipmentId); consumer fulfillment_return_received giữ V1 delivered, partial.
func TestPartialDelivery_TD09(t *testing.T) {
	e, o, vA, vB, s := newD1Shipment(t)
	e.apply(s.ID, ev("p", fulfillment.CanonPickedUp, 8, 0))
	e.stockAll(map[uuid.UUID][3]int32{vA: {5, 0, 1}, vB: {10, 0, 2}})
	before := e.statusEvents(s.ID)

	res, err := e.recordPartial(s.ID, td09Collected, td09Lines(o, vA, vB)...)
	if err != nil {
		t.Fatalf("ghi giao một phần: %v", err)
	}
	e.orderIs(o.ID, order.StatusDelivered)
	e.stockAll(map[uuid.UUID][3]int32{vA: {4, 0, 0}, vB: {9, 0, 1}})
	v := res.Shipment
	if v.Status != string(fulfillment.StatusDelivered) || !v.Partial || v.CODCollectedAmount == nil ||
		*v.CODCollectedAmount != td09Collected || v.DeliveredAt == nil {
		t.Fatalf("vận đơn sau ghi: %s partial=%v cod=%v delivered=%v", v.Status, v.Partial, v.CODCollectedAmount,
			v.DeliveredAt)
	}
	r := e.partialReturnOf(o, s.ID)
	if r.ID != res.ReturnID || r.Status != order.ReturnRequested || r.Kind != order.ReturnPartialDelivery ||
		r.RefundAmount != 0 || r.ShipmentID == nil || *r.ShipmentID != s.ID || len(r.Code) < 9 || r.Code[:2] != "TH" {
		t.Fatalf("phiếu: %+v", r)
	}
	if len(r.Items) != 1 || r.Items[0].OrderItemID != itemOf(o, vB) || r.Items[0].Quantity != 1 {
		t.Fatalf("dòng phiếu: %+v", r.Items)
	}
	if n := e.statusEvents(s.ID); n != before+1 {
		t.Fatalf("shipment.status_changed: %d, muốn %d", n, before+1)
	}
	stored, p := e.lastShipmentEvent(s.ID)
	if p.Status != fulfillment.StatusDelivered || p.From != fulfillment.StatusShipping || !p.Partial ||
		p.CODCollected == nil || *p.CODCollected != td09Collected || !slices.Contains(p.Effects, "delivered") ||
		stored.Seq != v.Version {
		t.Fatalf("payload status_changed: %+v (seq %d, version %d)", p, stored.Seq, v.Version)
	}
	e.mustHandle(cash.Consumer{}, stored)
	b := readAs(e.shipEnv, func(ctx context.Context, tx pgx.Tx) (cash.Balances, error) {
		return cash.BalancesFor(ctx, tx, s.ID)
	})
	if b.ToCollect != 0 || b.CarrierHolding != td09Collected || b.SalesClearing != -td09Collected {
		t.Fatalf("sổ COD: %+v; muốn to_collect 0, carrier_holding 430000, sales_clearing −430000", b)
	}

	e.receiveQty(r, order.ReceiveQtyLine{OrderItemID: itemOf(o, vB), Sellable: 1})
	e.stockAll(map[uuid.UUID][3]int32{vA: {4, 0, 0}, vB: {9, 0, 0}})
	if got := e.partialReturnOf(o, s.ID); got.Status != order.ReturnRestocked {
		t.Fatalf("phiếu sau nhận: %s", got.Status)
	}
	e.orderIs(o.ID, order.StatusDelivered)
	oe := e.lastOrderEvent(o.ID, order.TopicOrderPartiallyReturned)
	var rp order.ReturnReceived
	if err := json.Unmarshal(oe.Payload, &rp); err != nil {
		t.Fatal(err)
	}
	if rp.Kind != order.ReturnPartialDelivery || rp.RefundAmount != 0 || rp.ShipmentID == nil || *rp.ShipmentID != s.ID ||
		rp.ReturnID != r.ID {
		t.Fatalf("order.partially_returned: %+v", rp)
	}
	e.mustHandle(e.returnConsumer(), oe)
	if got := e.shipment(s.ID); got.Status != fulfillment.StatusDelivered {
		t.Fatalf("vận đơn sau nhận: %s", got.Status)
	}
	if v2 := e.view(s.ID); !v2.Partial {
		t.Fatal("vận đơn mất partial sau nhận")
	}
}

// Ghi chú: Availability trả on_hand (số thực trong kho, gồm hàng đang đi đường). vB sau giao một phần: on_hand 9,
// in_transit 1 → có thể bán 8; sau nhận: on_hand 9, in_transit 0 → có thể bán 9 (tăng 1).

// view đọc ShipmentView của vận đơn id.
func (e *applyEnv) view(id uuid.UUID) fulfillment.ShipmentView {
	e.t.Helper()
	return readAs(e.shipEnv, func(ctx context.Context, tx pgx.Tx) (fulfillment.ShipmentView, error) {
		return e.svc.ShipmentView(ctx, tx, id)
	})
}

// Từ chối: giao đủ, giao 0, thiếu dòng → ErrPartialQty; thu 700.000 > COD → ErrPartialCOD; vận đơn chờ thu gom không
// có partial_reported, hay đã giao → ErrPartialNotAllowed; không có vận đơn → ErrNotFound. Mọi lần từ chối không đổi
// đơn, tồn, vận đơn, không có phiếu.
func TestPartialDelivery_Rejects(t *testing.T) {
	e, o, vA, vB, s := newD1Shipment(t)
	a, b := itemOf(o, vA), itemOf(o, vB)
	if _, err := e.recordPartial(s.ID, 0, td09Lines(o, vA, vB)...); !errors.Is(err, fulfillment.ErrPartialNotAllowed) {
		t.Errorf("chờ thu gom, không anomaly: %v; muốn ErrPartialNotAllowed", err)
	}
	e.apply(s.ID, ev("p", fulfillment.CanonPickedUp, 8, 0))
	cases := []struct {
		name      string
		collected int64
		lines     []fulfillment.PartialLine
		want      error
	}{
		{
			"giao đủ", 0,
			[]fulfillment.PartialLine{{OrderItemID: a, DeliveredQty: 1}, {OrderItemID: b, DeliveredQty: 2}},
			fulfillment.ErrPartialQty,
		},
		{"giao 0", 0, []fulfillment.PartialLine{{OrderItemID: a}, {OrderItemID: b}}, fulfillment.ErrPartialQty},
		{"thiếu dòng", 0, []fulfillment.PartialLine{{OrderItemID: b, DeliveredQty: 1}}, fulfillment.ErrPartialQty},
		{"thu hơn COD", 700000, td09Lines(o, vA, vB), fulfillment.ErrPartialCOD},
	}
	for _, c := range cases {
		if _, err := e.recordPartial(s.ID, c.collected, c.lines...); !errors.Is(err, c.want) {
			t.Errorf("%s: %v; muốn %v", c.name, err, c.want)
		}
	}
	if _, err := e.recordPartial(uuid.New(), 0, td09Lines(o, vA, vB)...); !errors.Is(err, fulfillment.ErrNotFound) {
		t.Errorf("không có vận đơn: %v", err)
	}
	e.orderIs(o.ID, order.StatusConfirmed)
	e.shipmentIs(s.ID, fulfillment.StatusShipping)
	e.stockAll(map[uuid.UUID][3]int32{vA: {5, 0, 1}, vB: {10, 0, 2}})
	if n := e.count(`SELECT count(*) FROM ord.returns WHERE order_id = $1`, o.ID); n != 0 {
		t.Fatalf("phiếu sau từ chối: %d", n)
	}
	if _, err := e.recordPartial(s.ID, td09Collected, td09Lines(o, vA, vB)...); err != nil {
		t.Fatal(err)
	}
	if _, err := e.recordPartial(s.ID, td09Collected, td09Lines(o, vA, vB)...); !errors.Is(err,
		fulfillment.ErrPartialNotAllowed) {
		t.Errorf("ghi lần hai: %v; muốn ErrPartialNotAllowed", err)
	}
	if n := e.count(`SELECT count(*) FROM ord.returns WHERE order_id = $1`, o.ID); n != 1 {
		t.Fatalf("phiếu sau hai lần ghi: %d", n)
	}
}

// Hãng báo PartiallyDelivered khi vận đơn còn chờ thu gom → anomaly partial_reported mở, V1 cần xem; sau lệnh → đóng
// (resolved), needs_review false; giữ hàng held → in_transit rồi giao một phần.
func TestPartialDelivery_ClosesPartialReported(t *testing.T) {
	e, o, vA, vB, s := newD1Shipment(t)
	e.apply(s.ID, ev("pd", fulfillment.CanonPartiallyDelivered, 9, 0))
	if e.anomalyDetail(s.ID, "partial_reported") == nil || !e.view(s.ID).NeedsReview {
		t.Fatal("hãng báo giao một phần mà không có partial_reported mở")
	}
	if _, err := e.recordPartial(s.ID, td09Collected, td09Lines(o, vA, vB)...); err != nil {
		t.Fatalf("ghi giao một phần khi có partial_reported: %v", err)
	}
	if e.anomalyDetail(s.ID, "partial_reported") != nil {
		t.Fatal("partial_reported vẫn mở")
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.shipment_anomalies WHERE shipment_id = $1
		AND kind = 'partial_reported' AND status = 'resolved' AND resolved_by = $2`, s.ID, e.actor.UserID); n != 1 {
		t.Fatalf("partial_reported đã đóng: %d", n)
	}
	if v := e.view(s.ID); v.NeedsReview || !v.Partial || v.Status != string(fulfillment.StatusDelivered) {
		t.Fatalf("vận đơn: %s partial=%v needs_review=%v", v.Status, v.Partial, v.NeedsReview)
	}
	e.stockAll(map[uuid.UUID][3]int32{vA: {4, 0, 0}, vB: {9, 0, 1}})
}

// Giao thất bại (có việc gọi lại mở) rồi khách nhận một phần → việc gọi lại đóng với kết quả delivered; payload có
// tác động close_callbacks.
func TestPartialDelivery_FromDeliveryFailed(t *testing.T) {
	e, o, vA, vB, s := newD1Shipment(t)
	e.apply(s.ID, ev("p", fulfillment.CanonPickedUp, 8, 0), ev("f", fulfillment.CanonDeliveryFailed, 10, 0))
	e.shipmentIs(s.ID, fulfillment.StatusDeliveryFailed)
	if _, err := e.recordPartial(s.ID, 0, td09Lines(o, vA, vB)...); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.callback_tasks WHERE shipment_id = $1 AND status = 'closed'
		AND outcome = 'delivered'`, s.ID); n != 1 {
		t.Fatalf("việc gọi lại đóng delivered: %d", n)
	}
	_, p := e.lastShipmentEvent(s.ID)
	if p.From != fulfillment.StatusDeliveryFailed || !slices.Contains(p.Effects, "close_callbacks") ||
		p.CODCollected == nil || *p.CODCollected != 0 {
		t.Fatalf("payload: %+v", p)
	}
}

// Quy luật trên database: số giao ngẫu nhiên hợp lệ (đơn vA ×1, vB ×2) → mỗi biến thể on_hand giảm đúng số giao,
// in_transit còn đúng phần chưa giao, phiếu có đúng phần chưa giao; kho nhận phần còn lại (tốt, hỏng, thiếu ngẫu
// nhiên) → in_transit 0, on_hand = ban đầu − giao − thiếu.
func TestPartialDelivery_Property(t *testing.T) {
	base := newApplyEnv(t)
	rapid.Check(t, func(rt *rapid.T) {
		e := &applyEnv{shipEnv: &shipEnv{}, svc: base.svc, bus: base.bus}
		*e.shipEnv = *base.shipEnv
		o, vA, vB := e.d1()
		s := e.awaiting(o)
		e.apply(s.ID, ev("p", fulfillment.CanonPickedUp, 8, 0))
		ga := rapid.Int32Range(0, 1).Draw(rt, "giao vA")
		gb := rapid.Int32Range(0, 2).Draw(rt, "giao vB")
		if ga+gb == 0 || ga+gb == 3 {
			gb = 1
		}
		if _, err := e.recordPartial(s.ID, 0, fulfillment.PartialLine{OrderItemID: itemOf(o, vA), DeliveredQty: ga},
			fulfillment.PartialLine{OrderItemID: itemOf(o, vB), DeliveredQty: gb}); err != nil {
			rt.Fatal(err)
		}
		e.stockAll(map[uuid.UUID][3]int32{vA: {5 - ga, 0, 1 - ga}, vB: {10 - gb, 0, 2 - gb}})
		r := e.partialReturnOf(o, s.ID)
		var lines []order.ReceiveQtyLine
		var lost [2]int32
		for _, it := range r.Items {
			q := int32(it.Quantity) //nolint:gosec // ≤ 2
			miss := rapid.Int32Range(0, q).Draw(rt, "thiếu")
			dmg := rapid.Int32Range(0, q-miss).Draw(rt, "hỏng")
			lines = append(lines, order.ReceiveQtyLine{
				OrderItemID: it.OrderItemID, Sellable: q - miss - dmg,
				Damaged: dmg, Missing: miss,
			})
			if it.OrderItemID == itemOf(o, vA) {
				lost[0] = miss
			} else {
				lost[1] = miss
			}
		}
		e.receiveQty(r, lines...)
		e.stockAll(map[uuid.UUID][3]int32{vA: {5 - ga - lost[0], 0, 0}, vB: {10 - gb - lost[1], 0, 0}})
	})
}

// partialUnchanged: sau khi bị từ chối đảo, D1 giao một phần vẫn y nguyên: vận đơn delivered, partial; đơn delivered;
// tồn như TD09; phiếu partial_delivery requested; không có phiếu carrier_return.
func (e *applyEnv) partialUnchanged(o order.Order, sID, vA, vB uuid.UUID) {
	e.t.Helper()
	if v := e.view(sID); v.Status != string(fulfillment.StatusDelivered) || !v.Partial {
		e.t.Fatalf("vận đơn: %s partial=%v", v.Status, v.Partial)
	}
	e.orderIs(o.ID, order.StatusDelivered)
	e.stockAll(map[uuid.UUID][3]int32{vA: {4, 0, 0}, vB: {9, 0, 1}})
	if r := e.partialReturnOf(o, sID); r.Status != order.ReturnRequested {
		e.t.Fatalf("phiếu partial_delivery: %s", r.Status)
	}
	if n := e.count(`SELECT count(*) FROM ord.returns WHERE order_id = $1 AND kind = 'carrier_return'`, o.ID); n != 0 {
		e.t.Fatalf("phiếu carrier_return: %d, muốn 0", n)
	}
}

// Hãng sửa "đã giao" thành "hoàn" trên vận đơn đã giao một phần → không áp (đảo cả đơn sẽ làm phiếu partial_delivery
// treo, phần chưa giao tính hai lần), mở invalid_edge "Cần xem".
func TestPartialDelivery_CarrierReversalRefused(t *testing.T) {
	e, o, vA, vB, s := newD1Shipment(t)
	e.apply(s.ID, ev("p", fulfillment.CanonPickedUp, 8, 0))
	if _, err := e.recordPartial(s.ID, td09Collected, td09Lines(o, vA, vB)...); err != nil {
		t.Fatal(err)
	}
	if r := e.apply(s.ID, ev("r", fulfillment.CanonReturning, 12, 0)); r.Applied != 0 {
		t.Fatalf("áp %d sự kiện đảo, muốn 0", r.Applied)
	}
	if e.anomalyDetail(s.ID, "invalid_edge") == nil {
		t.Fatal("không mở invalid_edge")
	}
	e.partialUnchanged(o, s.ID, vA, vB)
}

// Nhân viên ghi tay "đang hoàn" lên vận đơn tay đã giao một phần → ErrManualNotAllowed, không đổi gì.
func TestPartialDelivery_ManualReversalRefused(t *testing.T) {
	e := newApplyEnv(t)
	o, vA, vB := e.d1()
	acc := e.manualAccount("best", "best-tay-partial")
	sv, err := e.ship(fulfillment.ShipInput{OrderID: o.ID, CarrierAccountID: acc, TrackingCode: trackingCode()})
	if err != nil {
		t.Fatalf("tạo vận đơn tay: %v", err)
	}
	now := time.Now().UTC()
	e.mustSetManual(sv.ID, fulfillment.ManualStatusInput{
		To: fulfillment.StatusShipping, At: now.Add(-2 * time.Hour), Reason: "Đã lấy hàng",
	})
	if _, err := e.recordPartial(sv.ID, td09Collected, td09Lines(o, vA, vB)...); err != nil {
		t.Fatal(err)
	}
	_, err = e.setManual(sv.ID, fulfillment.ManualStatusInput{
		To: fulfillment.StatusReturning, At: now.Add(-time.Hour), Reason: "Khách đổi ý",
	})
	if !errors.Is(err, fulfillment.ErrManualNotAllowed) {
		t.Fatalf("ghi tay returning sau giao một phần: %v, muốn ErrManualNotAllowed", err)
	}
	e.partialUnchanged(o, sv.ID, vA, vB)
}
