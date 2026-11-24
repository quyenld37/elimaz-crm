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
// đang gắn đúng vận đơn này; vận đơn khác hay chưa gắn → applied=false, không đổi gì (gọi lại an toàn). Theo trạng
// thái đơn (Task 431, B-02):
//
//   - confirmed: đặt NULL, version + 1, order.updated, báo hiệu; đơn giữ confirmed (để gửi hãng khác), giữ hàng không
//     đổi → applied=true.
//   - cancel_requested: hãng chắc chắn không còn kiện nên lời yêu cầu hủy thành công — như OnShipmentCancelConfirmed:
//     đơn cancelled, gỡ vận đơn, nhả held, trả lượt mã, giữ lý do → applied=true. Hàng đã rời kho (in_transit) →
//     ErrInvalidTransition, không đổi gì.
//   - trạng thái khác (delivered, returned) → ErrInvalidTransition, không đổi gì.
//
// Không có đơn → ErrNotFound.
func (w Writer) DetachShipment(ctx context.Context, tx pgx.Tx, sellerID, orderID, shipmentID uuid.UUID, a Actor,
	now time.Time,
) (applied bool, err error) {
	q, o, current, err := w.lockCurrentShipment(ctx, tx, sellerID, orderID, shipmentID)
	if err != nil || !current {
		return false, err
	}
	switch o.Status {
	case StatusConfirmed:
	case StatusCancelRequested:
		return w.confirmRequestedCancel(ctx, tx, sellerID, o, a, now)
	default:
		return false, fmt.Errorf("%w: gỡ vận đơn của đơn %s", ErrInvalidTransition, o.Status)
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
// đã delivered → applied=false (AT47: một lần trừ tồn). Đơn confirmed hay cancel_requested (hãng lấy, giao trước khi
// kịp hủy; Task 431, B-01, kế hoạch M3 VC12):
//
//   - picked_up: mọi dòng giữ hàng held của đơn → in_transit (G6). Đơn giữ trạng thái: đơn cancel_requested chờ lời
//     hãng về lệnh hủy (M3 gọi OnShipmentCancelRejected ngay sau, đưa về confirmed).
//   - delivered: dòng còn held → in_transit (hãng bỏ qua tín hiệu lấy hàng), rồi in_transit → consumed (on_hand −);
//     đơn cancel_requested về confirmed (như hãng từ chối hủy: lý do carrier_rejected_cancel, bỏ lý do hủy,
//     order.confirmed), rồi confirmed → delivered qua applyTransition (lịch sử với người làm a, giờ now;
//     order.delivered seq = version).
//
// Lỗi (bên gọi lùi cả transaction, không bao giờ applied=false sau khi đã ghi): bước lạ → ErrInvalidInput; không có
// đơn → ErrNotFound; đơn không ở confirmed/cancel_requested/delivered, hay không còn dòng giữ hàng held/in_transit nào
// (bất biến hỏng) → ErrStatusConflict; lỗi của inventory trả nguyên (không nuốt inventory.ErrStateConflict).
func (w Writer) OnShipmentEvent(ctx context.Context, tx pgx.Tx, sellerID, orderID, shipmentID uuid.UUID,
	step ShipmentStep, a Actor, now time.Time,
) (applied bool, err error) {
	if step != StepPickedUp && step != StepDelivered {
		return false, fmt.Errorf("%w: bước vận đơn %q", ErrInvalidInput, step)
	}
	d, ok, err := w.beginShipmentStep(ctx, tx, sellerID, orderID, shipmentID, step, a)
	if err != nil || !ok {
		return false, err
	}
	if step == StepPickedUp {
		return d.held, nil
	}
	if err := d.stock.ConsumeByItems(ctx, tx, d.items, d.who); err != nil {
		return false, fmt.Errorf("trừ tồn khi giao xong: %w", err)
	}
	if err := w.finishDelivered(ctx, tx, d, transitionOpt{}, a, now); err != nil {
		return false, err
	}
	return true, nil
}

// shipmentStep là phần đã làm của một bước vận đơn (beginShipmentStep): đơn đã khóa, dòng đơn, giữ hàng còn held lúc
// bắt đầu (đã chuyển sang in_transit), bộ ghi tồn và người làm theo kiểu của inventory.
type shipmentStep struct {
	q     *store.Queries
	o     store.LockOrderRow
	items []uuid.UUID
	held  bool
	stock inventory.Writer
	who   inventory.Actor
}

// beginShipmentStep là phần đầu chung của bước step (OnShipmentEvent, MarkDeliveredPartial): khóa đơn (ADR-46 nhóm 7);
// vận đơn không hiện hành hay đơn đã delivered → ok=false, không đổi gì; đơn không ở confirmed/cancel_requested hay
// không còn giữ hàng held/in_transit → ErrStatusConflict; giữ hàng held → in_transit (StartTransitByItems).
func (w Writer) beginShipmentStep(ctx context.Context, tx pgx.Tx, sellerID, orderID, shipmentID uuid.UUID,
	step ShipmentStep, a Actor,
) (d shipmentStep, ok bool, err error) {
	if d.stock, err = w.stock(); err != nil {
		return d, false, err
	}
	q, o, current, err := lockForShipment(ctx, tx, sellerID, orderID, shipmentID)
	if err != nil || !current {
		return d, false, err
	}
	d.q, d.o = q, o
	switch o.Status {
	case StatusDelivered:
		return d, false, nil
	case StatusConfirmed, StatusCancelRequested:
	default:
		return d, false, fmt.Errorf("%w: áp %s lên đơn %s", ErrStatusConflict, step, o.Status)
	}
	if d.items, err = q.OrderItemIDs(ctx, store.OrderItemIDsParams{SellerID: sellerID, OrderID: orderID}); err != nil {
		return d, false, fmt.Errorf("đọc dòng đơn: %w", err)
	}
	states, err := inventory.ReservationStatesByItems(ctx, tx, d.items)
	if err != nil {
		return d, false, fmt.Errorf("đọc giữ hàng của đơn: %w", err)
	}
	d.held = states[inventory.StateHeld] > 0
	if !d.held && states[inventory.StateInTransit] == 0 {
		return d, false, fmt.Errorf("%w: đơn %s không còn giữ hàng held, in_transit", ErrStatusConflict, o.Status)
	}
	d.who = inventory.Actor{Type: a.stockKind(), ID: a.UserID}
	if d.held {
		if err := d.stock.StartTransitByItems(ctx, tx, d.items, d.who); err != nil {
			return d, false, fmt.Errorf("chuyển giữ hàng sang đang giao: %w", err)
		}
	}
	return d, true, nil
}

// finishDelivered là phần cuối chung của "giao xong" sau khi đã trừ tồn: đơn cancel_requested về confirmed (hãng giao
// trước khi kịp hủy), rồi confirmed → delivered qua applyTransitionWith với opt.
func (w Writer) finishDelivered(ctx context.Context, tx pgx.Tx, d shipmentStep, opt transitionOpt, a Actor,
	now time.Time,
) error {
	cur := lockedOrder(d.o)
	a.TenantID = d.o.TenantID
	if d.o.Status == StatusCancelRequested { // sau số dư (ADR-46 nhóm 11), trước lịch sử, sự kiện
		if err := w.rejectRequestedCancel(ctx, tx, d.q, &cur, a, now); err != nil {
			return fmt.Errorf("đơn giao xong khi đang yêu cầu hủy: %w", err)
		}
	}
	if err := w.applyTransitionWith(ctx, tx, &cur, state.Delivered, "", "", opt, a, now); err != nil {
		return fmt.Errorf("đơn sang delivered: %w", err)
	}
	return nil
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

// lockCurrentShipment như lockForShipment, sau khi kiểm Writer đủ Events, Queue (wired): đường ghi của móc nối vận
// đơn (DetachShipment, OnShipmentCancelConfirmed, OnShipmentCancelRejected).
func (w Writer) lockCurrentShipment(ctx context.Context, tx pgx.Tx, sellerID, orderID, shipmentID uuid.UUID,
) (*store.Queries, store.LockOrderRow, bool, error) {
	if err := w.wired(); err != nil {
		return nil, store.LockOrderRow{}, false, err
	}
	return lockForShipment(ctx, tx, sellerID, orderID, shipmentID)
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
