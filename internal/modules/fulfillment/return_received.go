package fulfillment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Vận đơn sang Đã hoàn khi kho nhận kiện hoàn (kế hoạch M3, Task M3-23; Q-9, Q-27, Q-T91-8).

const (
	// ReturnReceivedConsumerName là tên consumer (việc event.fulfillment_return_received); đổi tên làm mất tiến độ.
	ReturnReceivedConsumerName = "fulfillment_return_received"

	// anomalyMissingItems: kiện hoàn về thiếu món (detail {missingQty}), cho khiếu nại hãng.
	anomalyMissingItems = "missing_items"
	// anomalyInvalidEdge với detail.reason = reasonReturnWithoutReturning: kho nhận phiếu hoàn mà vận đơn chưa ở
	// returning (Q-T91-8; loại return_without_returning chưa có trong CHECK của 0300).
	anomalyInvalidEdge           = "invalid_edge"
	reasonReturnWithoutReturning = "return_without_returning"
)

// ReturnReceivedConsumer nghe order.returned, order.partially_returned (events.Consumer; returnsConsumers đăng ký). Chỉ
// xử lý phiếu carrier_return, partial_delivery có shipmentId: khóa đơn (order.LockSnapshot, ADR-46 nhóm 7) → vận đơn
// (nhóm 8). Vận đơn returning → returned, returned_at, shipment.status_changed (Seq = version mới), báo hiệu; đã
// returned → sự kiện lặp, không đổi gì; delivered có partial → giữ delivered; trạng thái khác → "Cần xem" invalid_edge
// (reason return_without_returning). missingQty > 0 → "Cần xem" missing_items (detail {missingQty}). Payload hỏng,
// không có đơn hay vận đơn, vận đơn của đơn khác → queue.ErrPermanent.
type ReturnReceivedConsumer struct {
	s *Service
}

// Name trả ReturnReceivedConsumerName.
func (ReturnReceivedConsumer) Name() string { return ReturnReceivedConsumerName }

// Topics trả hai sự kiện kho nhận phiếu của order.
func (ReturnReceivedConsumer) Topics() []string {
	return []string{order.TopicOrderReturned, order.TopicOrderPartiallyReturned}
}

// Handle áp sự kiện e trong tx của worker.
func (c ReturnReceivedConsumer) Handle(ctx context.Context, e events.StoredEvent, tx pgx.Tx) error {
	var p order.ReturnReceived
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return fmt.Errorf("sự kiện %s: payload không hợp lệ: %w", e.ID, queue.ErrPermanent)
	}
	if p.Kind != order.ReturnCarrier && p.Kind != order.ReturnPartialDelivery || p.ShipmentID == nil {
		return nil
	}
	if c.s.d.Events == nil || c.s.d.Queue == nil {
		return errNotWired
	}
	q := store.New(tx)
	sh, err := lockForReceipt(ctx, tx, q, p)
	if err != nil {
		return fmt.Errorf("sự kiện %s: %w", e.ID, err)
	}
	if Status(sh.Status) == StatusReturned {
		return nil // sự kiện lặp
	}
	changed, err := c.apply(ctx, tx, q, sh)
	if err != nil {
		return err
	}
	if p.MissingQty > 0 {
		opened, err := openAnomaly(ctx, q, sh.ID, anomalyMissingItems, map[string]any{"missingQty": p.MissingQty})
		if err != nil {
			return err
		}
		changed = changed || opened
	}
	if changed {
		return c.s.publishShipment(ctx, tx, sh.TenantID, sh.ID, sh.WarehouseID)
	}
	return nil
}

// lockForReceipt khóa đơn (nhóm 7) rồi vận đơn của phiếu p (nhóm 8). Không có đơn, vận đơn, hay vận đơn của đơn khác
// → queue.ErrPermanent.
func lockForReceipt(ctx context.Context, tx pgx.Tx, q *store.Queries, p order.ReturnReceived,
) (store.LockShipmentForReceiptRow, error) {
	if _, err := order.LockSnapshot(ctx, tx, p.SellerID, p.OrderID); errors.Is(err, order.ErrNotFound) {
		return store.LockShipmentForReceiptRow{}, fmt.Errorf("đơn không có: %w", queue.ErrPermanent)
	} else if err != nil {
		return store.LockShipmentForReceiptRow{}, fmt.Errorf("khóa đơn của phiếu hoàn: %w", err)
	}
	sh, err := q.LockShipmentForReceipt(ctx, *p.ShipmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return sh, fmt.Errorf("vận đơn không có: %w", queue.ErrPermanent)
	}
	if err != nil {
		return sh, fmt.Errorf("khóa vận đơn của phiếu hoàn: %w", err)
	}
	if sh.OrderID != p.OrderID {
		return sh, fmt.Errorf("vận đơn của đơn khác: %w", queue.ErrPermanent)
	}
	return sh, nil
}

// apply: vận đơn returning → returned; delivered có partial → giữ; trạng thái khác → "Cần xem" invalid_edge (reason
// return_without_returning). changed: có đổi hay mở bất thường mới.
func (c ReturnReceivedConsumer) apply(ctx context.Context, tx pgx.Tx, q *store.Queries,
	sh store.LockShipmentForReceiptRow,
) (changed bool, err error) {
	switch {
	case Status(sh.Status) == StatusReturning:
		return true, c.markReturned(ctx, tx, q, sh)
	case Status(sh.Status) == StatusDelivered && sh.Partial:
		return false, nil
	default:
		return openAnomaly(ctx, q, sh.ID, anomalyInvalidEdge, map[string]any{
			"reason": reasonReturnWithoutReturning, "status": sh.Status,
		})
	}
}

// markReturned đưa vận đơn sh (đã khóa, returning) sang returned và phát shipment.status_changed.
func (c ReturnReceivedConsumer) markReturned(ctx context.Context, tx pgx.Tx, q *store.Queries,
	sh store.LockShipmentForReceiptRow,
) error {
	now := c.s.d.Now().UTC()
	version, err := q.MarkShipmentReturned(ctx, store.MarkShipmentReturnedParams{
		Now: now, ID: sh.ID,
	})
	if err != nil {
		return fmt.Errorf("vận đơn sang returned: %w", err)
	}
	if _, _, err := c.s.d.Events.Emit(ctx, tx, c.s.d.Queue, events.Event{
		TenantID: sh.TenantID, AggregateType: aggregateShipment, AggregateID: sh.ID.String(), Seq: version,
		Topic: TopicShipmentStatusChanged,
		Payload: StatusChanged{
			ShipmentID: sh.ID, Status: StatusReturned, CODAmount: sh.CodAmount, OrderID: sh.OrderID,
			From: StatusReturning, OccurredAt: &now,
		},
	}); err != nil {
		return fmt.Errorf("phát %s: %w", TopicShipmentStatusChanged, err)
	}
	return nil
}
