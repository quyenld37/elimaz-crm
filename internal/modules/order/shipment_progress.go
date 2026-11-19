package order

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
)

// ShipmentStep là một bước của vận đơn được áp lên đơn và tồn (kế hoạch M1, Task M1-30b).
type ShipmentStep string

// Các bước vận đơn order biết áp.
const (
	// StepPickedUp: hãng đã lấy hàng — giữ hàng held → in_transit.
	StepPickedUp ShipmentStep = "picked_up"
	// StepDelivered: giao xong — giữ hàng in_transit → consumed (on_hand −), đơn delivered.
	StepDelivered ShipmentStep = "delivered"
)

// AttachShipment (M1, giữ chữ ký — Q30) là lớp bọc của AttachShipmentBy cho fulfillment.CreateForOrder: cùng lõi
// attachShipment (khóa đơn, kiểm, gắn, version + 1), giờ lấy từ database (now() của transaction). Không phát
// order.updated: hàm gói không có Writer (Events, Queue) để phát đúng; M3 gọi AttachShipmentBy (Task M2-18, quyết định
// 1 trong docs/tasks/task-75.md). Lỗi như AttachShipmentBy.
func AttachShipment(ctx context.Context, tx pgx.Tx, sellerID, orderID, shipmentID uuid.UUID) error {
	q := store.New(tx)
	now, err := q.TxNow(ctx)
	if err != nil {
		return fmt.Errorf("đọc giờ transaction: %w", err)
	}
	_, _, err = attachShipment(ctx, q, sellerID, orderID, shipmentID, now)
	return err
}

// ApplyShipmentProgress (M1, giữ chữ ký — Q30) là lớp bọc của OnShipmentEvent với người làm carrier, giờ lấy từ
// database (now() của transaction). Hành vi và lỗi như OnShipmentEvent.
func (w Writer) ApplyShipmentProgress(ctx context.Context, tx pgx.Tx, sellerID, orderID, shipmentID uuid.UUID,
	step ShipmentStep,
) (applied bool, err error) {
	now, err := store.New(tx).TxNow(ctx)
	if err != nil {
		return false, fmt.Errorf("đọc giờ transaction: %w", err)
	}
	return w.OnShipmentEvent(ctx, tx, sellerID, orderID, shipmentID, step, Actor{Type: inventory.ActorCarrier}, now)
}

// Lock khóa đơn id (FOR UPDATE) trong tx của bên gọi, cho đường bắt đầu từ dòng ở nhóm sau (vận đơn, ADR-46 nhóm 7
// trước nhóm 8): bên gọi khóa đơn trước rồi mới khóa dòng của mình. Không có (hay của người bán, tenant khác) →
// ErrNotFound.
func Lock(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID) error {
	_, err := lockOrder(ctx, store.New(tx), sellerID, id)
	return err
}

// lockOrder khóa đơn id của người bán sellerID (FOR UPDATE); không có (hay của người bán, tenant khác) → ErrNotFound.
func lockOrder(ctx context.Context, q *store.Queries, sellerID, id uuid.UUID) (store.LockOrderRow, error) {
	o, err := q.LockOrder(ctx, store.LockOrderParams{SellerID: sellerID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.LockOrderRow{}, ErrNotFound
	}
	if err != nil {
		return store.LockOrderRow{}, fmt.Errorf("khóa đơn: %w", err)
	}
	return o, nil
}
