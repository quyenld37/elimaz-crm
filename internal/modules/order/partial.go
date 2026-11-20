package order

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
)

// DeliveredLine là một dòng khách nhận khi giao một phần: dòng đơn và số cái nhận (> 0, ≤ số đang đi giao).
type DeliveredLine struct {
	OrderItemID uuid.UUID
	Qty         int32
}

// MarkDeliveredPartial ghi lần hãng giao một phần của vận đơn shipmentID trên đơn orderID của người bán sellerID (TD09,
// Q-15; dùng ở Task M3-26), trong tx của bên gọi. Như OnShipmentEvent(delivered) của M2 (beginShipmentStep: khóa đơn
// trước, chỉ vận đơn hiện hành, đơn đã delivered → applied=false; giữ hàng còn held → in_transit trước), nhưng trừ tồn
// theo lines bằng inventory.ConsumePartial (dòng không có trong lines vẫn in_transit, chờ đường hoàn); đơn
// cancel_requested về confirmed rồi confirmed → delivered; order.delivered có partial: true (seq = version mới).
//
// Lỗi (bên gọi lùi cả transaction): không dòng, dòng trùng, Qty ≤ 0, dòng không thuộc đơn → ErrInvalidInput; không có
// đơn → ErrNotFound; đơn không ở confirmed/cancel_requested → ErrStatusConflict; lỗi của inventory trả nguyên (Qty lớn
// hơn số đang giữ → inventory.ErrStateConflict).
func (w Writer) MarkDeliveredPartial(ctx context.Context, tx pgx.Tx, sellerID, orderID, shipmentID uuid.UUID,
	lines []DeliveredLine, a Actor, now time.Time,
) (applied bool, err error) {
	if len(lines) == 0 {
		return false, fmt.Errorf("%w: giao một phần không dòng", ErrInvalidInput)
	}
	d, ok, err := w.beginShipmentStep(ctx, tx, sellerID, orderID, shipmentID, StepDelivered, a)
	if err != nil || !ok {
		return false, err
	}
	partial := make([]inventory.PartialLine, len(lines))
	for i, l := range lines {
		if l.Qty <= 0 || !slices.Contains(d.items, l.OrderItemID) ||
			slices.ContainsFunc(lines[:i], func(p DeliveredLine) bool { return p.OrderItemID == l.OrderItemID }) {
			return false, fmt.Errorf("%w: dòng giao một phần %d sai (số, trùng, không thuộc đơn)", ErrInvalidInput, i)
		}
		partial[i] = inventory.PartialLine{OrderItemID: l.OrderItemID, Qty: l.Qty}
	}
	if err := d.stock.ConsumePartial(ctx, tx, partial, d.who); err != nil {
		return false, fmt.Errorf("trừ tồn khi giao một phần: %w", err)
	}
	if err := w.finishDelivered(ctx, tx, d, transitionOpt{partial: true}, a, now); err != nil {
		return false, err
	}
	return true, nil
}
