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
	"elimaz.com.vn/crm/internal/modules/order/internal/state"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
)

// Kho nhận phiếu trả theo số lượng tốt, hỏng, thiếu trên từng dòng (kế hoạch M3, Task M3-23; Q-34). ReceiveReturn
// (M2, tình trạng cả dòng) dựng dòng số lượng rồi đi cùng một thân với ReceiveReturnQty.

// ReceiveQtyLine là số lượng kho nhận của một dòng phiếu: tốt (nhập lại kho bán được), hỏng, thiếu.
type ReceiveQtyLine struct {
	OrderItemID                uuid.UUID
	Sellable, Damaged, Missing int32
}

// condition là tình trạng ghi trên dòng: một loại số khác 0 → tên loại đó.
func (l ReceiveQtyLine) condition() string {
	switch {
	case l.Damaged == 0 && l.Missing == 0:
		return ConditionSellable
	case l.Sellable == 0 && l.Missing == 0:
		return ConditionDamaged
	default:
		return ConditionMissing
	}
}

// qtyLine là một dòng phiếu đã khóa cùng số kho nhận của nó.
type qtyLine struct {
	item store.LockReturnItemsRow
	ReceiveQtyLine
}

// buildLines dựng dòng số lượng từ đầu vào của bên gọi, khi đã biết các dòng phiếu (đã khóa).
type buildLines func(items []store.LockReturnItemsRow) ([]ReceiveQtyLine, error)

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

// qtySplit ghép lines vào các dòng phiếu items (theo thứ tự của items).
func qtySplit(items []store.LockReturnItemsRow, lines []ReceiveQtyLine) ([]qtyLine, error) {
	by := make(map[uuid.UUID]ReceiveQtyLine, len(lines))
	for _, ln := range lines {
		by[ln.OrderItemID] = ln
	}
	out := make([]qtyLine, 0, len(items))
	for _, it := range items {
		out = append(out, qtyLine{item: it, ReceiveQtyLine: by[it.OrderItemID]})
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

// markReceived ghi tình trạng, tiền hoàn (thiếu → 0) từng dòng và phiếu requested → restocked; trả tổng tiền hoàn.
func markReceived(ctx context.Context, l lockedReturn, lines []qtyLine, a Actor, now time.Time) (int64, error) {
	var total int64
	for _, ln := range lines {
		refund := ln.item.RefundAmount
		if ln.Missing > 0 {
			refund = 0
		}
		if err := l.q.SetReturnItemReceived(ctx, store.SetReturnItemReceivedParams{
			Condition: ln.condition(), RefundAmount: refund, ID: ln.item.ID,
		}); err != nil {
			return 0, fmt.Errorf("ghi tình trạng dòng phiếu: %w", err)
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

// receiveCarrier nhả giữ hàng in_transit của dòng về kho (tốt, hỏng), chuyển phần hỏng sang ngăn hỏng, rồi đơn
// confirmed → returned.
func (w Writer) receiveCarrier(ctx context.Context, tx pgx.Tx, stock inventory.Writer, l lockedReturn,
	lines []qtyLine, full Order, who inventory.Actor, a Actor, now time.Time,
) error {
	var back []uuid.UUID
	type damaged struct {
		v   uuid.UUID
		qty int32
	}
	var hurt []damaged
	for _, ln := range lines {
		if ln.Missing > 0 {
			continue
		}
		if ln.Damaged > 0 {
			v, err := variantOf(full, ln.OrderItemID)
			if err != nil {
				return err
			}
			hurt = append(hurt, damaged{v: v, qty: ln.Damaged})
		}
		back = append(back, ln.OrderItemID)
	}
	if len(back) > 0 {
		if err := stock.ReleaseInTransitByItems(ctx, tx, back, who); err != nil {
			return fmt.Errorf("nhả hàng hoàn về kho: %w", err)
		}
	}
	slices.SortFunc(hurt, func(x, y damaged) int { return bytes.Compare(x.v[:], y.v[:]) })
	for _, d := range hurt {
		if err := stock.MoveToDamaged(ctx, tx, l.r.ID, d.v, full.WarehouseID, d.qty, who); err != nil {
			return fmt.Errorf("chuyển hàng hoàn hỏng: %w", err)
		}
	}
	cur := lockedOrder(l.order)
	if err := w.applyTransition(ctx, tx, &cur, state.Returned, "", reasonCarrierReturned, a, now); err != nil {
		return fmt.Errorf("đơn sang returned: %w", err)
	}
	return nil
}
