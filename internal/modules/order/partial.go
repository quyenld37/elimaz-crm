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
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
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

// CreatePartialReturn tạo phiếu partial_delivery requested cho phần khách chưa nhận khi vận đơn shipmentID giao một
// phần đơn orderID của người bán sellerID (TD09; Task M3-26, gọi ngay sau MarkDeliveredPartial trong cùng tx): khóa đơn
// rồi dòng đơn; phiếu mang vận đơn, tiền hoàn 0 (tiền phần chưa giao chưa thu), mã TH… theo sequence của M2, lý do
// rỗng (lý do của nhân viên ở audit của bên gọi). Không đổi tồn (phần còn lại vẫn in_transit, chờ kho nhận qua
// ReceiveReturnQty), không đổi đơn. Dòng: như phiếu khách trả (1–100 dòng, số 1–10.000, mỗi dòng đơn một lần, thuộc
// đơn; sai → ErrInvalidInput), số ≤ số mua − Σ phiếu chưa hủy (ErrReturnQtyExceeds).
//
// Lỗi: không có đơn → ErrNotFound; đơn không delivered, hay đã có phiếu partial_delivery chưa hủy → ErrReturnNotAllowed;
// shipmentID không phải vận đơn hiện hành của đơn → ErrInvalidTransition.
func (w Writer) CreatePartialReturn(ctx context.Context, tx pgx.Tx, sellerID, orderID, shipmentID uuid.UUID,
	lines []ReturnLineInput, a Actor, now time.Time,
) (Return, error) {
	if err := w.wired(); err != nil {
		return Return{}, err
	}
	if err := checkReturnLines(lines); err != nil {
		return Return{}, err
	}
	q := store.New(tx)
	b, err := lockForNewReturn(ctx, q, sellerID, orderID)
	switch {
	case err != nil:
		return Return{}, err
	case !b.order.CurrentShipmentID.Valid || uuid.UUID(b.order.CurrentShipmentID.Bytes) != shipmentID:
		return Return{}, fmt.Errorf("%w: vận đơn không phải vận đơn hiện hành của đơn", ErrInvalidTransition)
	case slices.ContainsFunc(b.prior, func(p store.ReturnedLinesOfOrderRow) bool { return p.Kind == ReturnPartialDelivery }):
		return Return{}, fmt.Errorf("%w: đơn đã có phiếu giao một phần", ErrReturnNotAllowed)
	}
	priced, _, err := priceReturnLines(b.items, b.prior, lines)
	if err != nil {
		return Return{}, err
	}
	for i := range priced {
		priced[i].refund = 0 // tiền phần chưa giao chưa thu: không hoàn
	}
	id, code, err := newReturnCode(ctx, tx)
	if err != nil {
		return Return{}, err
	}
	if err := q.InsertReturn(ctx, store.InsertReturnParams{
		ID: id, Code: code, OrderID: orderID, ShipmentID: pgtype.UUID{Bytes: shipmentID, Valid: true},
		Kind: ReturnPartialDelivery, CreatedBy: a.id(), Now: now.UTC(),
	}); err != nil {
		return Return{}, fmt.Errorf("ghi phiếu giao một phần: %w", err)
	}
	return w.finishNewReturn(ctx, tx, q, b.order, id, priced)
}
