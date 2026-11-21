package queries

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Loại mục và nguồn của dòng thời gian đơn mà gói này dựng (cùng chuỗi với order.Timeline*, order.Actor*; openapi.yaml
// TimelineItem.kind, actorType).
const (
	TimelineShipment = "shipment"
	TimelinePayment  = "payment"

	actorStaff   = "staff"
	actorSystem  = "system"
	actorCarrier = "carrier"
)

// OrderTimelineEntry là một mục vận đơn hay thu tiền của dòng thời gian đơn. Kind: TimelineShipment (RefID: sự kiện
// vận đơn) hoặc TimelinePayment (RefID: phiếu thu/chi). ActorType: staff (ActorID là nhân viên), system, carrier.
// Summary chỉ mang mã, trạng thái, loại, số tiền (không dữ liệu cá nhân, không payload của hãng).
type OrderTimelineEntry struct {
	At        time.Time
	Kind      string
	RefID     uuid.UUID
	ActorID   *uuid.UUID
	ActorType string
	Summary   map[string]string
}

// orderShipmentEventsSQL: sự kiện của mọi vận đơn của đơn (cùng người bán, ADR-29), trừ sự kiện bỏ qua vì trùng trạng
// thái (duplicate_state); sự kiện đã áp, sự kiện M1 (applied NULL) và sự kiện bỏ qua vì lý do khác đều hiện.
const orderShipmentEventsSQL = `SELECT e.id, e.occurred_at, COALESCE(e.source, ''), e.status, COALESCE(e.canonical, ''),
       COALESCE(e.ignored_reason, ''), e.actor_id, s.code
FROM fulfillment.shipments_v AS s
JOIN fulfillment.shipment_events_v AS e ON e.shipment_id = s.id
WHERE s.order_id = $1 AND s.seller_id = $2 AND e.ignored_reason IS DISTINCT FROM 'duplicate_state'
ORDER BY e.occurred_at, e.id`

// orderPaymentsSQL: phiếu thu/chi ngoài COD của đơn (cash.payments_v; người ghi luôn là nhân viên).
const orderPaymentsSQL = `SELECT p.id, p.received_at, p.method, p.amount, p.actor_id
FROM cash.payments_v AS p
WHERE p.order_id = $1
ORDER BY p.received_at, p.id`

// OrderShippingTimeline trả mục vận đơn và thu tiền của đơn orderID (người bán sellerID), theo thời gian tăng dần.
// Bên gọi đã kiểm đơn thuộc người bán (order.Timeline). Nguồn của sự kiện vận đơn (I09): carrier_api, carrier_webhook →
// carrier; staff → staff (có người làm); system và sự kiện M1 chưa ghi nguồn → system. Chạy trong tenancy.WithTenant.
func OrderShippingTimeline(ctx context.Context, tx pgx.Tx, sellerID, orderID uuid.UUID) ([]OrderTimelineEntry, error) {
	rows, err := tx.Query(ctx, orderShipmentEventsSQL, orderID, sellerID)
	if err != nil {
		return nil, fmt.Errorf("đọc sự kiện vận đơn của đơn: %w", err)
	}
	out, err := pgx.CollectRows(rows, scanShipmentEntry)
	if err != nil {
		return nil, fmt.Errorf("đọc sự kiện vận đơn của đơn: %w", err)
	}
	err = scanEach(ctx, tx, orderPaymentsSQL, orderID, func(rows pgx.Rows) error {
		var (
			e      OrderTimelineEntry
			method string
			amount int64
			actor  uuid.UUID
		)
		if err := rows.Scan(&e.RefID, &e.At, &method, &amount, &actor); err != nil {
			return err //nolint:wrapcheck // scanEach bọc
		}
		e.Kind, e.ActorType, e.ActorID = TimelinePayment, actorStaff, &actor
		e.Summary = map[string]string{"method": method, "amount": strconv.FormatInt(amount, 10)}
		out = append(out, e)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("đọc phiếu thu của đơn: %w", err)
	}
	return out, nil
}

// scanShipmentEntry đọc một dòng của orderShipmentEventsSQL thành mục shipment.
func scanShipmentEntry(r pgx.CollectableRow) (OrderTimelineEntry, error) {
	var (
		e                                        OrderTimelineEntry
		source, status, canonical, ignored, code string
	)
	if err := r.Scan(&e.RefID, &e.At, &source, &status, &canonical, &ignored, &e.ActorID, &code); err != nil {
		return OrderTimelineEntry{}, err //nolint:wrapcheck // bên gọi bọc
	}
	e.Kind, e.ActorType = TimelineShipment, shipmentActorType(source)
	if e.ActorType != actorStaff {
		e.ActorID = nil
	}
	e.Summary = map[string]string{
		"shipmentCode": code, "status": status, "canonical": canonical, "ignoredReason": ignored,
	}
	maps.DeleteFunc(e.Summary, func(_, v string) bool { return v == "" })
	return e, nil
}

// shipmentActorType đổi nguồn của sự kiện vận đơn sang nguồn của mục dòng thời gian.
func shipmentActorType(source string) string {
	switch source {
	case "carrier_api", "carrier_webhook":
		return actorCarrier
	case "staff":
		return actorStaff
	}
	return actorSystem
}
