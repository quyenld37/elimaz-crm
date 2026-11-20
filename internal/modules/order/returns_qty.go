package order

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order/internal/pricing"
	"elimaz.com.vn/crm/internal/modules/order/internal/state"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
)

// Kho nhận phiếu trả theo số lượng tốt, hỏng, thiếu trên từng dòng (kế hoạch M3, Task M3-23; Q-34; tiền hoàn theo số
// đã về của phiếu khách trả, đổi: M3-24). ReceiveReturn
// (M2, tình trạng cả dòng) dựng dòng số lượng rồi đi cùng một thân với ReceiveReturnQty.

// reasonCarrierMissing: lý do của chứng từ thất lạc khi kiện hoàn của hãng về thiếu món (TD04, Q-27).
const reasonCarrierMissing = "carrier_return_missing"

// ReceiveQtyLine là số lượng kho nhận của một dòng phiếu: tốt (nhập lại kho bán được), hỏng, thiếu.
type ReceiveQtyLine struct {
	OrderItemID                uuid.UUID
	Sellable, Damaged, Missing int32
}

// condition là tình trạng ghi trên dòng: một loại số khác 0 → tên loại đó; từ hai loại → ConditionMixed.
func (l ReceiveQtyLine) condition() string {
	switch {
	case l.Damaged == 0 && l.Missing == 0:
		return ConditionSellable
	case l.Sellable == 0 && l.Missing == 0:
		return ConditionDamaged
	case l.Sellable == 0 && l.Damaged == 0:
		return ConditionMissing
	default:
		return ConditionMixed
	}
}

// qtyLine là một dòng phiếu đã khóa cùng số kho nhận của nó.
type qtyLine struct {
	item store.LockReturnItemsRow
	ReceiveQtyLine
}

// missingOf là tổng số món thiếu của các dòng.
func missingOf(lines []qtyLine) int32 {
	var n int32
	for _, ln := range lines {
		n += ln.Missing
	}
	return n
}

// buildLines dựng dòng số lượng từ đầu vào của bên gọi, khi đã biết các dòng phiếu (đã khóa).
type buildLines func(items []store.LockReturnItemsRow) ([]ReceiveQtyLine, error)

// ReceiveReturnQty là kho nhận hàng của phiếu returnID (người bán sellerID) ở version theo số lượng từng dòng lines,
// trong tx của bên gọi (lớp HTTP đã kiểm order.return.receive). Khóa đơn → phiếu → dòng phiếu; phiếu không requested →
// ErrReturnNotAllowed; version khác → ErrVersionConflict. Mỗi dòng phiếu đúng một dòng nhận, Sellable + Damaged +
// Missing = số của dòng (thiếu dòng, dòng lạ, trùng, lệch tổng → ErrQtyMismatch; số âm → ErrInvalidInput). Ghi ba số,
// tình trạng (một loại: tên loại; trộn: mixed), tiền hoàn của số đã về (receivedRefund: món thiếu không hoàn); phiếu
// sang restocked.
//
//   - carrier_return: chỉ khi đơn confirmed và vận đơn của phiếu là vận đơn hiện hành của đơn (Q-T91-8; khác →
//     ErrInvalidTransition). Nhả giữ hàng in_transit của mọi dòng (inventory.ReleaseInTransitByItems), số hỏng vào ngăn
//     hỏng (MoveToDamaged), số thiếu ghi thất lạc một lần cho cả phiếu (RecordLoss, ref = mã phiếu, lý do
//     carrier_return_missing). Đơn confirmed → returned; order.returned mang payload ReturnReceived (kind, shipmentId,
//     missingQty; fulfillment nghe để đưa vận đơn sang returned).
//   - customer_return, exchange (M3-24): tốt → on_hand +; hỏng → on_hand + rồi damaged +; thiếu → không ghi kho. Đơn
//     gốc giữ trạng thái (version + 1); order.returned khi mọi dòng đơn đã trả đủ (theo số của dòng phiếu, kể cả món
//     thiếu) qua các phiếu đã nhận, không thì order.partially_returned (payload ReturnReceived: kind, refundAmount).
//   - Loại khác (partial_delivery: M3-26) → ErrReturnNotAllowed.
func (w Writer) ReceiveReturnQty(ctx context.Context, tx pgx.Tx, sellerID, returnID uuid.UUID, version int64,
	lines []ReceiveQtyLine, a Actor, now time.Time,
) (Return, error) {
	return w.receiveQty(ctx, tx, sellerID, returnID, version, func([]store.LockReturnItemsRow) ([]ReceiveQtyLine, error) {
		return lines, nil
	}, a, now)
}

// conditionLines dựng dòng số lượng từ tình trạng cả dòng của M2: mỗi dòng phiếu đúng một lần, tình trạng hợp lệ (sai →
// ErrInvalidInput); cả số của dòng vào đúng một loại.
func conditionLines(lines []ReceiveLine) buildLines {
	return func(items []store.LockReturnItemsRow) ([]ReceiveQtyLine, error) {
		if len(lines) != len(items) {
			return nil, fmt.Errorf("%w: cần đúng %d dòng nhận", ErrInvalidInput, len(items))
		}
		conds := make(map[uuid.UUID]string, len(lines))
		for _, ln := range lines {
			if !slices.Contains([]string{ConditionSellable, ConditionDamaged, ConditionMissing}, ln.Condition) {
				return nil, fmt.Errorf("%w: tình trạng %q", ErrInvalidInput, ln.Condition)
			}
			conds[ln.OrderItemID] = ln.Condition
		}
		if len(conds) != len(items) {
			return nil, fmt.Errorf("%w: dòng nhận trùng", ErrInvalidInput)
		}
		out := make([]ReceiveQtyLine, 0, len(items))
		for _, it := range items {
			c, ok := conds[it.OrderItemID]
			if !ok {
				return nil, fmt.Errorf("%w: thiếu dòng %s hoặc dòng lạ", ErrInvalidInput, it.OrderItemID)
			}
			ln := ReceiveQtyLine{OrderItemID: it.OrderItemID}
			switch c {
			case ConditionSellable:
				ln.Sellable = it.Quantity
			case ConditionDamaged:
				ln.Damaged = it.Quantity
			default:
				ln.Missing = it.Quantity
			}
			out = append(out, ln)
		}
		return out, nil
	}
}

// qtySplit ghép lines vào các dòng phiếu items (theo thứ tự của items): số âm → ErrInvalidInput; số dòng khác, dòng
// lạ, dòng trùng, tổng ba số khác số của dòng → ErrQtyMismatch.
func qtySplit(items []store.LockReturnItemsRow, lines []ReceiveQtyLine) ([]qtyLine, error) {
	by := make(map[uuid.UUID]ReceiveQtyLine, len(lines))
	for _, ln := range lines {
		if ln.Sellable < 0 || ln.Damaged < 0 || ln.Missing < 0 {
			return nil, fmt.Errorf("%w: số âm ở dòng %s", ErrInvalidInput, ln.OrderItemID)
		}
		if _, dup := by[ln.OrderItemID]; dup {
			return nil, fmt.Errorf("%w: dòng %s trùng", ErrQtyMismatch, ln.OrderItemID)
		}
		by[ln.OrderItemID] = ln
	}
	if len(by) != len(items) {
		return nil, fmt.Errorf("%w: cần đúng %d dòng nhận, có %d", ErrQtyMismatch, len(items), len(by))
	}
	out := make([]qtyLine, 0, len(items))
	for _, it := range items {
		ln, ok := by[it.OrderItemID]
		if !ok {
			return nil, fmt.Errorf("%w: thiếu dòng %s hoặc dòng lạ", ErrQtyMismatch, it.OrderItemID)
		}
		// Từng số ≤ quantity ≤ 10.000 thì tổng không tràn int32; số lớn hơn quantity đã sai.
		if ln.Sellable > it.Quantity || ln.Damaged > it.Quantity || ln.Missing > it.Quantity ||
			ln.Sellable+ln.Damaged+ln.Missing != it.Quantity {
			return nil, fmt.Errorf("%w: dòng %s nhận %d/%d/%d, phiếu %d", ErrQtyMismatch, it.OrderItemID, ln.Sellable,
				ln.Damaged, ln.Missing, it.Quantity)
		}
		out = append(out, qtyLine{item: it, ReceiveQtyLine: ln})
	}
	return out, nil
}

// receiveQty là khung chung của ReceiveReturn và ReceiveReturnQty: khóa đơn → phiếu (requested, version) → dòng phiếu
// (changeReturn), dựng dòng số lượng bằng build rồi nhận.
func (w Writer) receiveQty(ctx context.Context, tx pgx.Tx, sellerID, returnID uuid.UUID, version int64, build buildLines,
	a Actor, now time.Time,
) (Return, error) {
	now = now.UTC()
	return w.changeReturn(ctx, tx, sellerID, returnID, version, ReturnRequested, false, func(l lockedReturn) error {
		return w.receive(ctx, tx, sellerID, l, build, a, now)
	})
}

// receive là thân nhận hàng trên phiếu đã khóa ở requested.
func (w Writer) receive(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID, l lockedReturn, build buildLines,
	a Actor, now time.Time,
) error {
	stock, err := w.stock()
	if err != nil {
		return err
	}
	switch {
	case l.r.Kind != ReturnCustomer && l.r.Kind != ReturnExchange && l.r.Kind != ReturnCarrier:
		return fmt.Errorf("%w: phiếu loại %s", ErrReturnNotAllowed, l.r.Kind)
	case l.r.Kind == ReturnCarrier && l.order.Status != StatusConfirmed:
		return fmt.Errorf("%w: nhận hàng hoàn của đơn %s", ErrInvalidTransition, l.order.Status)
	case l.r.Kind == ReturnCarrier && (!l.r.ShipmentID.Valid || l.r.ShipmentID != l.order.CurrentShipmentID):
		return fmt.Errorf("%w: phiếu %s của vận đơn không còn hiện hành", ErrInvalidTransition, l.r.Code)
	}
	items, err := l.q.LockReturnItems(ctx, l.r.ID)
	if err != nil {
		return fmt.Errorf("khóa dòng phiếu trả: %w", err)
	}
	in, err := build(items)
	if err != nil {
		return err
	}
	lines, err := qtySplit(items, in)
	if err != nil {
		return err
	}
	full, err := Get(ctx, tx, sellerID, l.order.ID)
	if err != nil {
		return err
	}
	total, err := markReceived(ctx, l, lines, a, now)
	if err != nil {
		return err
	}
	a.TenantID = l.order.TenantID
	who := inventory.Actor{Type: a.kind(), ID: a.UserID}
	if l.r.Kind == ReturnCarrier {
		return w.receiveCarrier(ctx, tx, stock, l, lines, full, who, a, now)
	}
	return w.receiveCustomer(ctx, tx, stock, l, lines, full, total, who, a, now)
}

// receivedRefund là tiền hoàn của dòng phiếu đã nhận: phần của Sellable + Damaged đơn vị trong tiền hoàn R đã chia lúc
// tạo phiếu cho Quantity đơn vị, bằng pricing.RefundFor(R, Quantity, 0, Sellable+Damaged, 0) — không thiếu → đúng R;
// không món nào về → 0; còn lại floor(R × (Sellable+Damaged) / Quantity). Món thiếu không hoàn (luật missing của M2).
// Phiếu hoàn của hãng có R = 0.
func receivedRefund(ln qtyLine) int64 {
	return pricing.RefundFor(ln.item.RefundAmount, int64(ln.item.Quantity), 0, int64(ln.Sellable+ln.Damaged), 0)
}

// markReceived ghi ba số, tình trạng, tiền hoàn (receivedRefund) từng dòng và phiếu requested → restocked; trả tổng
// tiền hoàn.
func markReceived(ctx context.Context, l lockedReturn, lines []qtyLine, a Actor, now time.Time) (int64, error) {
	var total int64
	for _, ln := range lines {
		refund := receivedRefund(ln)
		if err := l.q.SetReturnItemQty(ctx, store.SetReturnItemQtyParams{
			Condition: ln.condition(), RefundAmount: refund, SellableQty: ln.Sellable, DamagedQty: ln.Damaged,
			MissingQty: ln.Missing, ID: ln.item.ID,
		}); err != nil {
			return 0, fmt.Errorf("ghi số nhận của dòng phiếu: %w", err)
		}
		total += refund
	}
	_, err := l.q.MarkReturnReceived(ctx, store.MarkReturnReceivedParams{
		RefundAmount: total, ReceivedBy: a.id(), Now: now, ID: l.r.ID, Version: l.r.Version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrVersionConflict
	}
	if err != nil {
		return 0, fmt.Errorf("ghi phiếu đã nhận: %w", err)
	}
	return total, nil
}

// receiveCarrier nhả giữ hàng in_transit của mọi dòng (kiện hoàn đã về kho), chuyển số hỏng sang ngăn hỏng, ghi số
// thiếu là thất lạc, rồi đơn confirmed → returned (order.returned, payload ReturnReceived). Mọi dòng số dư của phiếu đã
// bị khóa ở bước nhả (ADR-46 nhóm 11), nên MoveToDamaged, RecordLoss chỉ chạm dòng đã khóa.
func (w Writer) receiveCarrier(ctx context.Context, tx pgx.Tx, stock inventory.Writer, l lockedReturn,
	lines []qtyLine, full Order, who inventory.Actor, a Actor, now time.Time,
) error {
	back := make([]uuid.UUID, 0, len(lines))
	type damaged struct {
		v   uuid.UUID
		qty int32
	}
	var hurt []damaged
	var lost []inventory.LossLine
	for _, ln := range lines {
		back = append(back, ln.OrderItemID)
		if ln.Damaged == 0 && ln.Missing == 0 {
			continue
		}
		v, err := variantOf(full, ln.OrderItemID)
		if err != nil {
			return err
		}
		if ln.Damaged > 0 {
			hurt = append(hurt, damaged{v: v, qty: ln.Damaged})
		}
		if ln.Missing > 0 {
			lost = append(lost, inventory.LossLine{VariantID: v, WarehouseID: full.WarehouseID, Qty: ln.Missing})
		}
	}
	if err := stock.ReleaseInTransitByItems(ctx, tx, back, who); err != nil {
		return fmt.Errorf("nhả hàng hoàn về kho: %w", err)
	}
	slices.SortFunc(hurt, func(x, y damaged) int { return bytes.Compare(x.v[:], y.v[:]) })
	for _, d := range hurt {
		if err := stock.MoveToDamaged(ctx, tx, l.r.ID, d.v, full.WarehouseID, d.qty, who); err != nil {
			return fmt.Errorf("chuyển hàng hoàn hỏng: %w", err)
		}
	}
	if len(lost) > 0 {
		if err := stock.RecordLoss(ctx, tx, lost, l.r.ID, reasonCarrierMissing, who); err != nil {
			return fmt.Errorf("ghi hàng hoàn thiếu: %w", err)
		}
	}
	cur := lockedOrder(l.order)
	if err := w.applyTransitionWith(ctx, tx, &cur, state.Returned, "", reasonCarrierReturned, transitionOpt{
		topic: TopicOrderReturned,
		returned: &ReturnReceived{
			ReturnID: l.r.ID, Kind: l.r.Kind, ShipmentID: uuidPtr(l.r.ShipmentID), MissingQty: missingOf(lines),
		},
	}, a, now); err != nil {
		return fmt.Errorf("đơn sang returned: %w", err)
	}
	return nil
}
