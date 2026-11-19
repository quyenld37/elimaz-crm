package order

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order/internal/state"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
)

// Móc nối vận đơn cho M3 (kế hoạch M2, Task M2-18; tên chuẩn R1): fulfillment gọi AttachShipmentBy, DetachShipment,
// OnShipmentEvent trong transaction của nó (hoặc lớp bọc M1 AttachShipment, ApplyShipmentProgress). Các hàm chỉ nhận
// mã vận đơn, không đọc trạng thái, mã hay phí của vận đơn. Mọi hàm khóa đơn trước (ADR-46 nhóm 7, trước mọi dòng nhóm
// sau); sự kiện mang TenantID của dòng đơn đã khóa.

// AttachShipmentBy gắn vận đơn shipmentID làm vận đơn hiện hành của đơn orderID của người bán sellerID (05 mục 2, B1;
// ADR-29), trong tx của bên gọi: cần đơn confirmed, không chờ lý do nào, chưa có vận đơn hiện hành; version + 1, sự
// kiện order.updated (seq = version mới), báo hiệu. Gắn lại cùng vận đơn → không đổi, nil. Lỗi: đã có vận đơn khác →
// ErrShipmentAlreadyAttached; đơn không confirmed hay còn pending_reason → ErrInvalidTransition; không có đơn →
// ErrNotFound.
func (w Writer) AttachShipmentBy(ctx context.Context, tx pgx.Tx, sellerID, orderID, shipmentID uuid.UUID, _ Actor,
	now time.Time,
) error {
	if err := w.wired(); err != nil {
		return err
	}
	o, version, err := attachShipment(ctx, store.New(tx), sellerID, orderID, shipmentID, now)
	if err != nil || version == 0 {
		return err
	}
	return w.emitUpdated(ctx, tx, o, version)
}

// attachShipment là lõi gắn vận đơn dùng chung cho AttachShipmentBy và lớp bọc M1 AttachShipment: khóa đơn, kiểm,
// ghi. Trả dòng đơn đã khóa và version mới; version 0 khi đã gắn đúng vận đơn này (không đổi gì).
func attachShipment(ctx context.Context, q *store.Queries, sellerID, orderID, shipmentID uuid.UUID, now time.Time,
) (store.LockOrderRow, int64, error) {
	o, err := lockOrder(ctx, q, sellerID, orderID)
	if err != nil {
		return o, 0, err
	}
	if o.CurrentShipmentID.Valid {
		if uuid.UUID(o.CurrentShipmentID.Bytes) == shipmentID {
			return o, 0, nil
		}
		return o, 0, ErrShipmentAlreadyAttached
	}
	if o.Status != StatusConfirmed || o.PendingReason != "" {
		return o, 0, fmt.Errorf("%w: gắn vận đơn vào đơn %s (chờ %q)", ErrInvalidTransition, o.Status, o.PendingReason)
	}
	version, err := q.AttachCurrentShipment(ctx, store.AttachCurrentShipmentParams{
		ShipmentID: pgtype.UUID{Bytes: shipmentID, Valid: true}, Now: now, SellerID: sellerID, ID: orderID,
		Version: o.Version,
	})
	if err != nil {
		// Đơn đã khóa và version đọc dưới khóa: không dòng nào là lỗi, không phải xung đột thường.
		return o, 0, fmt.Errorf("gắn vận đơn: %w", err)
	}
	return o, version, nil
}

// DetachShipment gỡ vận đơn shipmentID khỏi vai vận đơn hiện hành của đơn orderID của người bán sellerID, trong tx của
// bên gọi (fulfillment: lệnh tạo vận đơn kết thúc mà hãng chắc chắn chưa tạo, D-03; đổi hãng, 05 mục 2). Chỉ khi đơn
// đang gắn đúng vận đơn này: đặt NULL, version + 1, order.updated, báo hiệu; đơn giữ trạng thái (confirmed, để gửi
// hãng khác), giữ hàng không đổi → applied=true. Vận đơn khác hay chưa gắn → applied=false, không đổi gì (gọi lại an
// toàn). Không có đơn → ErrNotFound.
func (w Writer) DetachShipment(ctx context.Context, tx pgx.Tx, sellerID, orderID, shipmentID uuid.UUID, _ Actor,
	now time.Time,
) (applied bool, err error) {
	if err := w.wired(); err != nil {
		return false, err
	}
	q, o, current, err := lockForShipment(ctx, tx, sellerID, orderID, shipmentID)
	if err != nil || !current {
		return false, err
	}
	version, err := q.DetachCurrentShipment(ctx, store.DetachCurrentShipmentParams{
		Now: now, SellerID: sellerID, ID: orderID, Version: o.Version,
		ShipmentID: pgtype.UUID{Bytes: shipmentID, Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("gỡ vận đơn hiện hành: %w", err)
	}
	if err := w.emitUpdated(ctx, tx, o, version); err != nil {
		return false, err
	}
	return true, nil
}

// OnShipmentEvent áp bước step của vận đơn shipmentID lên đơn orderID của người bán sellerID và tồn, trong tx của bên
// gọi (fulfillment). Khóa đơn trước rồi số dư, giữ hàng (ADR-46). Chỉ vận đơn hiện hành đổi được đơn và tồn (05 mục 2,
// B1): vận đơn khác (vận đơn cũ tới muộn) hay đơn chưa gắn vận đơn → applied=false, không đổi gì. Bước đã qua hay đơn
// đã delivered → applied=false (AT47: một lần trừ tồn).
//
//   - picked_up: mọi dòng giữ hàng held của đơn → in_transit (G6).
//   - delivered: dòng còn held → in_transit (hãng bỏ qua tín hiệu lấy hàng), rồi in_transit → consumed (on_hand −);
//     đơn confirmed → delivered qua applyTransition (lịch sử với người làm a, giờ now; order.delivered seq = version).
//
// Lỗi (bên gọi lùi cả transaction, không bao giờ applied=false sau khi đã ghi): bước lạ → ErrInvalidInput; không có
// đơn → ErrNotFound; đơn không ở confirmed/delivered, hay không còn dòng giữ hàng held/in_transit nào (bất biến hỏng)
// → ErrStatusConflict; lỗi của inventory trả nguyên (không nuốt inventory.ErrStateConflict).
func (w Writer) OnShipmentEvent(ctx context.Context, tx pgx.Tx, sellerID, orderID, shipmentID uuid.UUID,
	step ShipmentStep, a Actor, now time.Time,
) (applied bool, err error) {
	if step != StepPickedUp && step != StepDelivered {
		return false, fmt.Errorf("%w: bước vận đơn %q", ErrInvalidInput, step)
	}
	stock, err := w.stock()
	if err != nil {
		return false, err
	}
	q, o, current, err := lockForShipment(ctx, tx, sellerID, orderID, shipmentID)
	if err != nil || !current {
		return false, err
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
	who := inventory.Actor{Type: a.kind(), ID: a.UserID}
	if held {
		if err := stock.StartTransitByItems(ctx, tx, items, who); err != nil {
			return false, fmt.Errorf("chuyển giữ hàng sang đang giao: %w", err)
		}
	}
	if step == StepPickedUp {
		return held, nil
	}
	if err := stock.ConsumeByItems(ctx, tx, items, who); err != nil {
		return false, fmt.Errorf("trừ tồn khi giao xong: %w", err)
	}
	cur := lockedOrder(o)
	a.TenantID = o.TenantID
	if err := w.applyTransition(ctx, tx, &cur, state.Delivered, "", "", a, now); err != nil {
		return false, fmt.Errorf("đơn sang delivered: %w", err)
	}
	return true, nil
}

// lockForShipment khóa đơn orderID (ADR-46 nhóm 7) và cho biết shipmentID có đang là vận đơn hiện hành của đơn không;
// trả cả bộ truy vấn trên tx.
func lockForShipment(ctx context.Context, tx pgx.Tx, sellerID, orderID, shipmentID uuid.UUID,
) (q *store.Queries, o store.LockOrderRow, current bool, err error) {
	q = store.New(tx)
	o, err = lockOrder(ctx, q, sellerID, orderID)
	if err != nil {
		return q, o, false, err
	}
	return q, o, o.CurrentShipmentID.Valid && uuid.UUID(o.CurrentShipmentID.Bytes) == shipmentID, nil
}

// wired trả errWriterNotWired khi Writer thiếu Events hoặc Queue (đường ghi phát sự kiện).
func (w Writer) wired() error {
	_, err := w.stock()
	return err
}

// lockedOrder chép phần của dòng đơn đã khóa mà applyTransition, emit, signal cần.
func lockedOrder(o store.LockOrderRow) Order {
	return Order{
		ID: o.ID, SellerID: o.SellerID, CustomerID: o.CustomerID, Status: o.Status, PendingReason: o.PendingReason,
		CurrentShipmentID: uuidPtr(o.CurrentShipmentID), AddressComplete: o.AddressComplete, Version: o.Version,
	}
}

// emitUpdated phát order.updated (seq = version mới) và báo hiệu cho đơn o vừa đổi trường (không đổi trạng thái).
func (w Writer) emitUpdated(ctx context.Context, tx pgx.Tx, o store.LockOrderRow, version int64) error {
	cur := lockedOrder(o)
	cur.Version = version
	if err := w.emit(ctx, tx, o.TenantID, cur, TopicOrderUpdated, version); err != nil {
		return err
	}
	return w.signal(ctx, tx, o.TenantID, cur)
}
