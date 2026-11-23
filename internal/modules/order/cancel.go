package order

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order/internal/state"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
	"elimaz.com.vn/crm/internal/modules/promotion"
)

// Hủy đơn, yêu cầu hủy, phía đơn của hủy vận đơn (kế hoạch M2, Task M2-21; 05 mục 2). Thứ tự khóa (ADR-46): đơn (nhóm
// 7, khóa trước) → lượt mã (nhóm 10, promotion.Release) → số dư tồn (nhóm 11, ReleaseHeldByItems) → lịch sử, sự kiện.

// Lý do hủy (Q18, CHECK orders_cancel_reason_q18_check). Năm lý do đầu nhân viên chọn; CancelDraftExpired,
// CancelDuplicate do hệ thống đặt (hợp đồng API không nhận).
const (
	CancelCustomerChangedMind = "customer_changed_mind"
	CancelOutOfStock          = "out_of_stock"
	CancelUnreachable         = "unreachable"
	CancelWrongInfo           = "wrong_info"
	CancelOther               = "other"
	CancelDraftExpired        = "draft_expired"
	CancelDuplicate           = "duplicate"
)

const (
	// maxCancelNoteRunes: ghi chú hủy tối đa 500 ký tự (hợp đồng CancelOrderRequest.note).
	maxCancelNoteRunes = 500
	// releaseCancelled: lý do nhả giữ hàng, trả lượt mã khi đơn hủy (inventory, promotion).
	releaseCancelled = "order_cancelled"
	// reasonCarrierRejected: lý do ở lịch sử khi hãng từ chối hủy, đơn về confirmed.
	reasonCarrierRejected = "carrier_rejected_cancel"
)

// Cancel hủy đơn id của người bán sellerID ở version (khác version hiện tại → ErrVersionConflict), lý do reason (Q18;
// sai → ErrInvalidInput), ghi chú note (tối đa 500 ký tự, có thể rỗng), trong tx của bên gọi:
//
//   - draft, new, confirmed chưa có vận đơn hiện hành → cancelled: nhả mọi giữ hàng held của đơn, trả lượt mã (Q2:
//     promotion.Release, mã tắt release_on_cancel thì giữ lượt), ghi cancel_reason, cancel_note, cancelled_at; sự kiện
//     order.cancelled (TD01, KM03).
//   - confirmed có vận đơn hiện hành → cancel_requested: ghi lý do, không nhả hàng, không trả lượt; sự kiện
//     order.cancel_requested (M3 nghe để gửi lệnh hủy cho hãng; TD03). Hãng trả lời qua OnShipmentCancelConfirmed,
//     OnShipmentCancelRejected.
//   - cancel_requested, cancelled, delivered, returned → ErrInvalidTransition (đơn đã giao dùng phiếu trả).
//
// Trả đơn sau khi hủy (như Get). Không có đơn → ErrNotFound. Audit do bên gọi ghi (Order.AuditFields).
func (w Writer) Cancel(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID, version int64, reason, note string,
	a Actor, now time.Time,
) (Order, error) {
	if err := w.wired(); err != nil {
		return Order{}, err
	}
	if !validCancelReason(reason) || !optText(note, maxCancelNoteRunes) {
		return Order{}, fmt.Errorf("%w: lý do, ghi chú hủy", ErrInvalidInput)
	}
	now = now.UTC()
	return changeLocked(ctx, tx, sellerID, id, version, func(o *Order) error {
		switch {
		case o.Status == StatusCancelRequested: // chờ hãng trả lời: cancel_requested → cancelled chỉ ở đường của hãng
			return fmt.Errorf("%w: đơn đang chờ hãng xác nhận hủy", ErrInvalidTransition)
		case o.Status == StatusConfirmed && o.CurrentShipmentID != nil:
			return w.requestCancel(ctx, tx, o, reason, note, a, now)
		}
		return w.finishCancel(ctx, tx, o, reason, note, a, now)
	})
}

// OnShipmentCancelConfirmed là phía đơn khi hãng xác nhận đã hủy vận đơn shipmentID của đơn orderID (người bán
// sellerID), trong tx của fulfillment. Khóa đơn trước; vận đơn không phải hiện hành (vận đơn cũ, báo lần hai) →
// false, không đổi gì. Hàng của đơn đã rời kho (giữ hàng in_transit) → ErrInvalidTransition, không đổi gì: phải đi
// đường hoàn (M3 đưa vào "Cần xem").
//
//   - cancel_requested → cancelled: gỡ vận đơn hiện hành, nhả giữ hàng held, trả lượt mã; giữ lý do lúc yêu cầu.
//   - confirmed (chỉ hủy vận đơn để gửi lại) → như DetachShipment: gỡ vận đơn, đơn giữ confirmed, giữ hàng.
//   - trạng thái khác → ErrInvalidTransition.
func (w Writer) OnShipmentCancelConfirmed(ctx context.Context, tx pgx.Tx, sellerID, orderID, shipmentID uuid.UUID,
	a Actor, now time.Time,
) (bool, error) {
	q, o, current, err := w.lockCurrentShipment(ctx, tx, sellerID, orderID, shipmentID)
	if err != nil || !current {
		return false, err
	}
	if o.Status != StatusConfirmed && o.Status != StatusCancelRequested {
		return false, fmt.Errorf("%w: hãng hủy vận đơn của đơn %s", ErrInvalidTransition, o.Status)
	}
	if o.Status == StatusConfirmed {
		cur := lockedOrder(o)
		if _, _, err := reservationsOf(ctx, tx, q, &cur); err != nil {
			return false, err
		}
		return w.DetachShipment(ctx, tx, sellerID, orderID, shipmentID, a, now)
	}
	return w.confirmRequestedCancel(ctx, tx, sellerID, o, a, now)
}

// confirmRequestedCancel đưa đơn o đã khóa đang cancel_requested sang cancelled khi hãng chắc chắn không còn kiện (hãng
// xác nhận hủy, hay lệnh tạo vận đơn chết — DetachShipment): finishCancel với lý do, ghi chú lúc yêu cầu (gỡ vận đơn,
// nhả held, trả lượt mã). Hàng đã in_transit → ErrInvalidTransition.
func (w Writer) confirmRequestedCancel(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID, o store.LockOrderRow,
	a Actor, now time.Time,
) (bool, error) {
	saved, err := Get(ctx, tx, sellerID, o.ID)
	if err != nil {
		return false, err
	}
	reason, note := CancelOther, ""
	if saved.CancelReason != nil {
		reason = *saved.CancelReason
	}
	if saved.CancelNote != nil {
		note = *saved.CancelNote
	}
	cur := lockedOrder(o)
	a.TenantID = o.TenantID
	if err := w.finishCancel(ctx, tx, &cur, reason, note, a, now.UTC()); err != nil {
		return false, err
	}
	return true, nil
}

// OnShipmentCancelRejected là phía đơn khi hãng từ chối hủy vận đơn shipmentID (hàng đã lấy, đang giao…): đơn
// cancel_requested → confirmed, bỏ lý do và ghi chú hủy, vẫn vận đơn và giữ hàng. Vận đơn không phải hiện hành, hay
// đơn đã confirmed (báo lần hai) → false, không đổi gì; trạng thái khác → ErrInvalidTransition.
func (w Writer) OnShipmentCancelRejected(ctx context.Context, tx pgx.Tx, sellerID, orderID, shipmentID uuid.UUID,
	a Actor, now time.Time,
) (bool, error) {
	q, o, current, err := w.lockCurrentShipment(ctx, tx, sellerID, orderID, shipmentID)
	if err != nil || !current || o.Status == StatusConfirmed {
		return false, err
	}
	if o.Status != StatusCancelRequested {
		return false, fmt.Errorf("%w: hãng từ chối hủy của đơn %s", ErrInvalidTransition, o.Status)
	}
	a.TenantID = o.TenantID
	cur := lockedOrder(o)
	if err := w.rejectRequestedCancel(ctx, tx, q, &cur, a, now.UTC()); err != nil {
		return false, err
	}
	return true, nil
}

// rejectRequestedCancel đưa đơn o đã khóa (a.TenantID đã đặt) cancel_requested về confirmed khi hãng không hủy được
// (từ chối, hay đã giao): lịch sử lý do carrier_rejected_cancel, order.confirmed, bỏ lý do và ghi chú hủy; giữ vận
// đơn và giữ hàng.
func (w Writer) rejectRequestedCancel(ctx context.Context, tx pgx.Tx, q *store.Queries, o *Order, a Actor,
	now time.Time,
) error {
	if err := w.applyTransition(ctx, tx, o, state.Confirmed, "", reasonCarrierRejected, a, now); err != nil {
		return err
	}
	return setCancelReason(ctx, q, o, "", "")
}

// requestCancel đổi đơn o (đã khóa) confirmed có vận đơn hiện hành sang cancel_requested và ghi lý do; không đụng
// tồn, mã.
func (w Writer) requestCancel(ctx context.Context, tx pgx.Tx, o *Order, reason, note string, a Actor, now time.Time,
) error {
	if err := w.applyTransition(ctx, tx, o, state.CancelRequested, "", reason, a, now); err != nil {
		return err
	}
	return setCancelReason(ctx, store.New(tx), o, reason, note)
}

// finishCancel đổi đơn o đã khóa (phần lockedOrder; cập nhật như applyTransition) sang cancelled: prepareCancel rồi
// cancelAll với một đơn.
func (w Writer) finishCancel(ctx context.Context, tx pgx.Tx, o *Order, reason, note string, a Actor, now time.Time,
) error {
	p, err := prepareCancel(ctx, tx, o)
	if err != nil {
		return err
	}
	return w.cancelAll(ctx, tx, []cancelPlan{p}, reason, note, a, now)
}

// cancelPlan là một đơn đã khóa sẽ hủy: mã dòng đơn, có giữ hàng held, có vận đơn hiện hành (đơn từ cancel_requested).
type cancelPlan struct {
	o           *Order
	items       []uuid.UUID
	held        bool
	hadShipment bool
}

// prepareCancel kiểm (không ghi gì) đơn o đã khóa được sang cancelled: cạnh của sơ đồ, hàng chưa rời kho; đọc dòng đơn
// và giữ hàng.
func prepareCancel(ctx context.Context, tx pgx.Tx, o *Order) (cancelPlan, error) {
	if err := state.Check(state.Status(o.Status), state.Cancelled, state.Conditions{
		HasCurrentShipment: o.CurrentShipmentID != nil, AddressComplete: o.AddressComplete, PendingReason: o.PendingReason,
	}); err != nil {
		return cancelPlan{}, stateError(err)
	}
	items, states, err := reservationsOf(ctx, tx, store.New(tx), o)
	if err != nil {
		return cancelPlan{}, err
	}
	return cancelPlan{o: o, items: items, held: states[inventory.StateHeld] > 0, hadShipment: o.CurrentShipmentID != nil}, nil
}

// cancelAll hủy các đơn đã khóa (nhóm 7, bên gọi khóa hết trước) theo đúng thứ tự khóa ADR-46 cho cả lần: trả lượt mã
// của từng đơn theo thứ tự plans (nhóm 10; bên gọi nhiều đơn khóa trước mọi mã bằng promotion.LockCoupons), nhả giữ
// hàng held của mọi dòng một lần (nhóm 11, inventory khóa số dư theo biến thể tăng dần), rồi mới đổi trạng thái từng
// đơn (lịch sử, order.cancelled, báo hiệu), ghi lý do, ghi chú, gỡ vận đơn hiện hành.
func (w Writer) cancelAll(ctx context.Context, tx pgx.Tx, plans []cancelPlan, reason, note string, a Actor,
	now time.Time,
) error {
	stock, err := w.stock()
	if err != nil {
		return err
	}
	release := releaseCancelled
	if reason == CancelDraftExpired {
		release = CancelDraftExpired
	}
	var held []uuid.UUID
	for _, p := range plans {
		if _, err := promotion.Release(ctx, tx, p.o.ID, release); err != nil {
			return fmt.Errorf("trả lượt mã của đơn hủy: %w", err)
		}
		if p.held {
			held = append(held, p.items...)
		}
	}
	if len(held) > 0 {
		if err := stock.ReleaseHeldByItems(ctx, tx, held, release, inventory.Actor{Type: a.stockKind(), ID: a.UserID}); err != nil {
			return fmt.Errorf("nhả giữ hàng của đơn hủy: %w", err)
		}
	}
	q := store.New(tx)
	for _, p := range plans {
		if err := w.applyCancelled(ctx, tx, q, p, reason, note, a, now); err != nil {
			return err
		}
	}
	return nil
}

// applyCancelled đổi trạng thái đơn của p sang cancelled, ghi lý do, ghi chú, gỡ vận đơn hiện hành nếu có.
func (w Writer) applyCancelled(ctx context.Context, tx pgx.Tx, q *store.Queries, p cancelPlan, reason, note string,
	a Actor, now time.Time,
) error {
	o := p.o
	if err := w.applyTransition(ctx, tx, o, state.Cancelled, "", reason, a, now); err != nil {
		return err
	}
	if err := setCancelReason(ctx, q, o, reason, note); err != nil {
		return err
	}
	if !p.hadShipment {
		return nil
	}
	n, err := q.ClearShipmentOfCancelled(ctx, store.ClearShipmentOfCancelledParams{
		SellerID: o.SellerID, ID: o.ID, Version: o.Version,
	})
	if err != nil {
		return fmt.Errorf("gỡ vận đơn của đơn hủy: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("%w: gỡ vận đơn của đơn hủy không đúng một dòng", ErrStatusConflict)
	}
	o.CurrentShipmentID = nil
	return nil
}

// reservationsOf trả mã các dòng đơn của o và số dòng giữ hàng theo trạng thái (đọc, không khóa: số dư được khóa ở
// ReleaseHeldByItems); hàng của đơn đã rời kho (có dòng in_transit) → ErrInvalidTransition: đơn không hủy được,
// phải đi đường hoàn.
func reservationsOf(ctx context.Context, tx pgx.Tx, q *store.Queries, o *Order) ([]uuid.UUID, map[string]int, error) {
	items, err := q.OrderItemIDs(ctx, store.OrderItemIDsParams{SellerID: o.SellerID, OrderID: o.ID})
	if err != nil {
		return nil, nil, fmt.Errorf("đọc dòng đơn: %w", err)
	}
	states, err := inventory.ReservationStatesByItems(ctx, tx, items)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc giữ hàng của đơn: %w", err)
	}
	if states[inventory.StateInTransit] > 0 {
		return nil, nil, fmt.Errorf("%w: hàng của đơn đã rời kho, cần đi đường hoàn", ErrInvalidTransition)
	}
	return items, states, nil
}

// setCancelReason ghi lý do, ghi chú hủy của đơn o (version là version sau lần đổi trạng thái vừa xong); rỗng → NULL.
func setCancelReason(ctx context.Context, q *store.Queries, o *Order, reason, note string) error {
	n, err := q.SetCancelReason(ctx, store.SetCancelReasonParams{
		Reason: pgtype.Text{String: reason, Valid: reason != ""}, Note: pgtype.Text{String: note, Valid: note != ""},
		SellerID: o.SellerID, ID: o.ID, Version: o.Version,
	})
	if err != nil {
		return fmt.Errorf("ghi lý do hủy: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("%w: ghi lý do hủy không đúng một dòng", ErrStatusConflict)
	}
	return nil
}

func validCancelReason(r string) bool {
	return slices.Contains([]string{
		CancelCustomerChangedMind, CancelOutOfStock, CancelUnreachable, CancelWrongInfo, CancelOther,
		CancelDraftExpired, CancelDuplicate,
	}, r)
}
