package order

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

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

// AttachShipment gắn vận đơn shipmentID làm vận đơn hiện hành của đơn orderID của người bán sellerID (05 mục 2, B1;
// ADR-29), trong tx của bên gọi
// (fulfillment, cùng transaction với việc tạo vận đơn). Khóa đơn trước (ADR-46 nhóm 7). Chỉ đơn confirmed và chưa có
// vận đơn hiện hành; gắn cùng vận đơn lần hai → không đổi, không lỗi; đã có vận đơn khác → ErrShipmentAlreadyAttached;
// đơn không ở confirmed → ErrStatusConflict; không có đơn → ErrNotFound. Gắn tăng version của đơn.
func AttachShipment(ctx context.Context, tx pgx.Tx, sellerID, orderID, shipmentID uuid.UUID) error {
	q := store.New(tx)
	o, err := lockOrder(ctx, q, sellerID, orderID)
	if err != nil {
		return err
	}
	if o.CurrentShipmentID.Valid {
		if uuid.UUID(o.CurrentShipmentID.Bytes) == shipmentID {
			return nil
		}
		return ErrShipmentAlreadyAttached
	}
	if o.Status != StatusConfirmed {
		return fmt.Errorf("%w: gắn vận đơn vào đơn %s", ErrStatusConflict, o.Status)
	}
	n, err := q.SetCurrentShipment(ctx, store.SetCurrentShipmentParams{
		ShipmentID: pgtype.UUID{Bytes: shipmentID, Valid: true}, SellerID: sellerID, ID: orderID,
	})
	if err != nil {
		return fmt.Errorf("gắn vận đơn: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("%w: gắn vận đơn không đổi dòng nào", ErrStatusConflict)
	}
	return nil
}

// DetachShipment gỡ vận đơn shipmentID khỏi vai vận đơn hiện hành của đơn orderID của người bán sellerID, trong tx của
// bên gọi (fulfillment: lệnh tạo vận đơn kết thúc mà hãng chắc chắn chưa tạo, vận đơn sang cancelled — D-03). Khóa đơn
// trước (ADR-46 nhóm 7). Không xét trạng thái đơn: vận đơn chưa từng tồn tại ở hãng thì đơn không còn vận đơn nào; giữ
// hàng không đổi (đơn vẫn chờ giao). Đơn đang gắn vận đơn khác (hay không gắn) → không đổi, nil (gọi lại an toàn);
// không có đơn → ErrNotFound. Gỡ tăng version của đơn.
func DetachShipment(ctx context.Context, tx pgx.Tx, sellerID, orderID, shipmentID uuid.UUID) error {
	q := store.New(tx)
	switch o, err := lockOrder(ctx, q, sellerID, orderID); {
	case err != nil:
		return err
	case !o.CurrentShipmentID.Valid || uuid.UUID(o.CurrentShipmentID.Bytes) != shipmentID:
		return nil
	}
	if _, err := q.ClearCurrentShipment(ctx, store.ClearCurrentShipmentParams{
		SellerID: sellerID, ID: orderID, ShipmentID: pgtype.UUID{Bytes: shipmentID, Valid: true},
	}); err != nil {
		return fmt.Errorf("gỡ vận đơn hiện hành: %w", err)
	}
	return nil
}

// ApplyShipmentProgress áp bước step của vận đơn shipmentID lên đơn orderID của người bán sellerID và tồn, trong tx của bên gọi
// (fulfillment). Khóa đơn trước (ADR-46: ord.orders rồi số dư, giữ hàng). Chỉ vận đơn hiện hành được đổi đơn và tồn
// (05 mục 2, B1): vận đơn khác (hay đơn chưa gắn vận đơn) → applied=false, không đổi gì. Gọi lại cùng bước (hay bước
// đã qua) → applied=false, không đổi gì.
//
//   - picked_up: mọi dòng giữ hàng held của đơn → in_transit.
//   - delivered: dòng còn held → in_transit (hãng bỏ qua tín hiệu lấy hàng), rồi in_transit → consumed (on_hand −);
//     đơn confirmed → delivered, một dòng lịch sử (tác nhân carrier), sự kiện order.delivered (seq = version mới).
//
// Lỗi (bên gọi lùi cả transaction, không bao giờ applied=false sau khi đã ghi): bước lạ → ErrInvalidInput; không có
// đơn → ErrNotFound; đơn không ở confirmed/delivered, hay không còn dòng giữ hàng held/in_transit nào (bất biến hỏng)
// → ErrStatusConflict; lỗi của inventory trả nguyên (không nuốt inventory.ErrStateConflict).
func (w Writer) ApplyShipmentProgress(ctx context.Context, tx pgx.Tx, sellerID, orderID, shipmentID uuid.UUID,
	step ShipmentStep,
) (applied bool, err error) {
	if step != StepPickedUp && step != StepDelivered {
		return false, fmt.Errorf("%w: bước vận đơn %q", ErrInvalidInput, step)
	}
	stock, err := w.stock()
	if err != nil {
		return false, err
	}
	q := store.New(tx)
	o, err := lockOrder(ctx, q, sellerID, orderID)
	if err != nil {
		return false, err
	}
	if !o.CurrentShipmentID.Valid || uuid.UUID(o.CurrentShipmentID.Bytes) != shipmentID {
		return false, nil
	}
	switch o.Status {
	case StatusDelivered:
		return false, nil
	case StatusConfirmed:
	default:
		return false, fmt.Errorf("%w: áp %s lên đơn %s", ErrStatusConflict, step, o.Status)
	}
	items, err := q.OrderItemIDs(ctx, store.OrderItemIDsParams{SellerID: sellerID, OrderID: orderID})
	if err != nil {
		return false, fmt.Errorf("đọc dòng đơn: %w", err)
	}
	states, err := inventory.ReservationStatesByItems(ctx, tx, items)
	if err != nil {
		return false, fmt.Errorf("đọc giữ hàng của đơn: %w", err)
	}
	held := states[inventory.StateHeld] > 0
	if !held && states[inventory.StateInTransit] == 0 {
		return false, fmt.Errorf("%w: đơn confirmed không còn giữ hàng held, in_transit", ErrStatusConflict)
	}
	carrier := inventory.Actor{Type: inventory.ActorCarrier}
	if held {
		if err := stock.StartTransitByItems(ctx, tx, items, carrier); err != nil {
			return false, fmt.Errorf("chuyển giữ hàng sang đang giao: %w", err)
		}
	}
	if step == StepPickedUp {
		return held, nil
	}
	if err := stock.ConsumeByItems(ctx, tx, items, carrier); err != nil {
		return false, fmt.Errorf("trừ tồn khi giao xong: %w", err)
	}
	if err := w.markDelivered(ctx, tx, o); err != nil {
		return false, err
	}
	return true, nil
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
