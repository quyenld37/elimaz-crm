package order

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order/internal/pricing"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
	"elimaz.com.vn/crm/internal/platform/events"
)

// Phiếu trả, đổi hàng, phiếu hoàn của hãng (kế hoạch M2, Task M2-22; 05 mục 2). M2 là chủ duy nhất của phiếu trả (Q-X2):
// M3 gọi OnShipmentReturning khi vận đơn bắt đầu hoàn, CarrierReturnFor rồi ReceiveReturn khi kho nhận kiện hoàn.
// Tạo phiếu không đổi tồn, không lùi trạng thái đơn gốc (WAT29, TD08); tồn chỉ đổi khi kho nhận (ReceiveReturn). Tiền
// hoàn chỉ là số trên phiếu: cash ghi tiền khi nghe order.partially_returned / order.returned (Q16).
//
// Thứ tự khóa (ADR-46 nhóm 7 → 10 → 11): đơn (hai đơn — gốc và thay thế — theo id tăng dần) → dòng đơn → phiếu → dòng
// phiếu → lượt mã (chỉ khi hủy đơn thay thế) → số dư tồn (inventory tự sắp theo biến thể, kho).

// Loại, trạng thái phiếu và tình trạng dòng khi kho nhận (CHECK của ord.returns, ord.return_items).
const (
	ReturnCustomer = "customer_return"
	ReturnExchange = "exchange"
	ReturnCarrier  = "carrier_return"
	// ReturnPartialDelivery: phiếu phần khách không nhận khi giao một phần (M3, TD09; Q-T91-4).
	ReturnPartialDelivery = "partial_delivery"

	ReturnRequested = "requested"
	// ReturnStatusReceived: kho đã nhận một phần hàng của phiếu (M3, nhận theo số lượng; Q-T91-4). Tên có Status vì
	// ReturnReceived là kiểu payload sự kiện của M2.
	ReturnStatusReceived = "received"
	ReturnRestocked      = "restocked"
	ReturnClosed         = "closed"
	ReturnCancelled      = "cancelled"

	ConditionSellable = "sellable"
	ConditionDamaged  = "damaged"
	ConditionMissing  = "missing"
	// ConditionMixed: dòng nhận có từ hai loại số (tốt, hỏng, thiếu) khác 0 (M3-23; CHECK của 0302).
	ConditionMixed = "mixed"
)

// Sự kiện khi kho nhận phiếu khách trả / đổi (payload ReturnReceived); phiếu hoàn của hãng: order.returned do
// applyTransition phát (đơn confirmed → returned).
const (
	TopicOrderReturned          = "order.returned"
	TopicOrderPartiallyReturned = "order.partially_returned"
)

const (
	maxReturnLines       = 100
	maxReturnQty         = 10000
	maxReturnReasonRunes = 500
	defaultReturnLimit   = 50
	maxReturnLimit       = 100
	// reasonCarrierReturned: lý do ở lịch sử khi kho nhận kiện hoàn của hãng (đơn confirmed → returned).
	reasonCarrierReturned = "carrier_returned"
)

// ReturnLineInput là một dòng của phiếu trả: dòng đơn và số lượng trả (1–10.000).
type ReturnLineInput struct {
	OrderItemID uuid.UUID
	Quantity    int
}

// CreateReturnInput là một phiếu khách trả (Kind customer_return) hoặc đổi hàng (exchange, Replacement bắt buộc: dòng
// mới, địa chỉ, phí của đơn thay thế; người bán, khách là của đơn gốc, không mã giảm giá). Reason tối đa 500 ký tự.
type CreateReturnInput struct {
	Kind        string
	Lines       []ReturnLineInput
	Reason      string
	Replacement *CreateInput
}

// ReceiveLine là tình trạng của một dòng phiếu khi kho nhận: sellable | damaged | missing.
type ReceiveLine struct {
	OrderItemID uuid.UUID
	Condition   string
}

// ReturnItem là một dòng phiếu trả. Condition chỉ có nghĩa sau khi kho nhận (ReceivedAt của phiếu khác nil).
type ReturnItem struct {
	ID, OrderItemID uuid.UUID
	Quantity        int
	Condition       string
	RefundAmount    int64
}

// Return là một phiếu trả kèm dòng (theo id). RefundAmount = tổng tiền hoàn của dòng.
type Return struct {
	ID, OrderID                    uuid.UUID
	Code, Kind, Status, Reason     string
	ShipmentID, ReplacementOrderID *uuid.UUID
	RefundAmount                   int64
	Items                          []ReturnItem
	ReceivedAt                     *time.Time
	CreatedAt                      time.Time
	Version                        int64
}

// ReturnReceived là payload của order.partially_returned / order.returned khi kho nhận một phiếu (khách trả, đổi; phiếu
// hoàn của hãng từ M3-23): phần Changed của đơn (version = seq) cộng mã phiếu, loại, tiền hoàn của phiếu, vận đơn của
// phiếu (chỉ phiếu hoàn của hãng, giao một phần) và tổng số món thiếu (chỉ mã và số, A16).
type ReturnReceived struct {
	Changed

	ReturnID     uuid.UUID  `json:"returnId"`
	Kind         string     `json:"kind"`
	RefundAmount int64      `json:"refundAmount"`
	ShipmentID   *uuid.UUID `json:"shipmentId,omitempty"`
	MissingQty   int32      `json:"missingQty"`
}

// CreateReturn tạo phiếu trả cho đơn orderID đã giao của người bán sellerID, trong tx của bên gọi: khóa đơn rồi dòng
// đơn; đơn không delivered → ErrReturnNotAllowed; mỗi dòng: số trả ≤ số mua − Σ số của các phiếu chưa hủy (TD07,
// WAT28) → không thì ErrReturnQtyExceeds; tiền hoàn từng dòng bằng pricing.RefundFor (đơn vị cuối nhận phần lẻ, WAT31).
// Phiếu requested, không đổi tồn, không đổi đơn gốc (AT14). exchange: tạo đơn thay thế bằng Create (OrderType
// exchange, ReplacedReturnID, cùng khách, không mã) trong cùng tx, ghi replacement_order_id. Đầu vào sai, dòng không
// thuộc đơn → ErrInvalidInput; không có đơn → ErrNotFound.
func (w Writer) CreateReturn(ctx context.Context, tx pgx.Tx, sellerID, orderID uuid.UUID, in CreateReturnInput,
	a Actor, now time.Time,
) (Return, error) {
	if err := w.wired(); err != nil {
		return Return{}, err
	}
	if err := checkReturnInput(in); err != nil {
		return Return{}, err
	}
	now = now.UTC()
	q := store.New(tx)
	o, err := lockOrder(ctx, q, sellerID, orderID)
	if err != nil {
		return Return{}, err
	}
	if o.Status != StatusDelivered {
		return Return{}, fmt.Errorf("%w: đơn %s chưa giao", ErrReturnNotAllowed, o.Status)
	}
	items, err := q.LockOrderItemsForReturn(ctx, store.LockOrderItemsForReturnParams{SellerID: sellerID, OrderID: orderID})
	if err != nil {
		return Return{}, fmt.Errorf("khóa dòng đơn: %w", err)
	}
	prior, err := q.ReturnedLinesOfOrder(ctx, orderID)
	if err != nil {
		return Return{}, fmt.Errorf("đọc phiếu trả của đơn: %w", err)
	}
	lines, total, err := priceReturnLines(items, prior, in.Lines)
	if err != nil {
		return Return{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Return{}, fmt.Errorf("sinh mã phiếu trả: %w", err)
	}
	code, err := nextReturnCode(ctx, tx)
	if err != nil {
		return Return{}, err
	}
	var replacement pgtype.UUID
	if in.Kind == ReturnExchange {
		rid, err := w.createReplacement(ctx, tx, sellerID, orderID, id, *in.Replacement, a, now)
		if err != nil {
			return Return{}, err
		}
		replacement = pgtype.UUID{Bytes: rid, Valid: true}
	}
	if err := q.InsertReturn(ctx, store.InsertReturnParams{
		ID: id, Code: code, OrderID: orderID, Kind: in.Kind, Reason: in.Reason, RefundAmount: total,
		ReplacementOrderID: replacement, CreatedBy: a.id(), Now: now,
	}); err != nil {
		return Return{}, fmt.Errorf("ghi phiếu trả: %w", err)
	}
	for _, l := range lines {
		if err := q.InsertReturnItem(ctx, store.InsertReturnItemParams{
			ReturnID: id, OrderItemID: l.item, Quantity: l.qty, RefundAmount: l.refund,
		}); err != nil {
			return Return{}, fmt.Errorf("ghi dòng phiếu trả: %w", err)
		}
	}
	if err := w.signal(ctx, tx, o.TenantID, lockedOrder(o)); err != nil {
		return Return{}, err
	}
	return GetReturn(ctx, tx, sellerID, id)
}

// checkReturnInput kiểm phần đầu vào không cần database.
func checkReturnInput(in CreateReturnInput) error {
	switch {
	case in.Kind != ReturnCustomer && in.Kind != ReturnExchange,
		(in.Kind == ReturnExchange) != (in.Replacement != nil),
		in.Replacement != nil && in.Replacement.CouponCode != "",
		len(in.Lines) == 0 || len(in.Lines) > maxReturnLines,
		!optText(in.Reason, maxReturnReasonRunes):
		return fmt.Errorf("%w: phiếu trả", ErrInvalidInput)
	}
	seen := make(map[uuid.UUID]bool, len(in.Lines))
	for _, l := range in.Lines {
		if l.OrderItemID == uuid.Nil || l.Quantity <= 0 || l.Quantity > maxReturnQty || seen[l.OrderItemID] {
			return fmt.Errorf("%w: dòng phiếu trả", ErrInvalidInput)
		}
		seen[l.OrderItemID] = true
	}
	return nil
}

// pricedLine là một dòng phiếu sắp ghi: dòng đơn, số lượng, tiền hoàn.
type pricedLine struct {
	item   uuid.UUID
	qty    int32
	refund int64
}

// priceReturnLines kiểm số trả của từng dòng với các phiếu chưa hủy (prior) và tính tiền hoàn bằng pricing.RefundFor.
// "Đã hoàn trước" của dòng phiếu missing (tiền hoàn đã về 0 khi kho nhận) là phần lẽ ra của nó, floor(net × q / qty),
// để đơn vị cuối không nhận tiền của món thiếu.
func priceReturnLines(items []store.LockOrderItemsForReturnRow, prior []store.ReturnedLinesOfOrderRow,
	in []ReturnLineInput,
) ([]pricedLine, int64, error) {
	byID := make(map[uuid.UUID]store.LockOrderItemsForReturnRow, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	out := make([]pricedLine, 0, len(in))
	var total int64
	for _, l := range in {
		it, ok := byID[l.OrderItemID]
		if !ok {
			return nil, 0, fmt.Errorf("%w: dòng %s không thuộc đơn", ErrInvalidInput, l.OrderItemID)
		}
		qty, now := int64(it.Quantity), int64(l.Quantity)
		var returned, refunded int64
		for _, p := range prior {
			if p.OrderItemID != it.ID {
				continue
			}
			returned += int64(p.Quantity)
			if p.Condition == ConditionMissing {
				refunded += pricing.RefundFor(it.NetAmount, qty, 0, int64(p.Quantity), 0)
			} else {
				refunded += p.RefundAmount
			}
		}
		if returned+now > qty {
			return nil, 0, fmt.Errorf("%w: dòng %s mua %d, đã trả %d, trả thêm %d", ErrReturnQtyExceeds, it.ID, qty,
				returned, now)
		}
		refund := pricing.RefundFor(it.NetAmount, qty, returned, now, refunded)
		out = append(out, pricedLine{item: it.ID, qty: int32(now), refund: refund}) //nolint:gosec // ≤ maxReturnQty
		total += refund
	}
	return out, total, nil
}

// createReplacement tạo đơn thay thế của phiếu đổi returnID (đơn gốc orderID đã khóa): người bán, khách của đơn gốc;
// kho, kênh, địa chỉ của đơn gốc khi in bỏ trống (mã giảm giá đã bị chặn ở checkReturnInput); ý định mặc định confirmed khi địa chỉ đủ tỉnh, xã, không thì new.
func (w Writer) createReplacement(ctx context.Context, tx pgx.Tx, sellerID, orderID, returnID uuid.UUID,
	in CreateInput, a Actor, now time.Time,
) (uuid.UUID, error) {
	orig, err := Get(ctx, tx, sellerID, orderID)
	if err != nil {
		return uuid.Nil, err
	}
	cust := orig.CustomerID
	in.SellerID, in.Customer = sellerID, customer.OrderCustomerInput{CustomerID: &cust}
	in.OrderType, in.ReplacedReturnID = OrderTypeExchange, &returnID
	if in.WarehouseID == uuid.Nil {
		in.WarehouseID = orig.WarehouseID
	}
	if in.Channel == "" {
		in.Channel = orig.Channel
	}
	if in.ShipAddress == nil {
		addr := orig.ShipAddress
		in.ShipAddress = &addr
	}
	if in.Intent == "" {
		in.Intent = IntentNew
		if in.ShipAddress.ProvinceCode != "" && in.ShipAddress.WardCode != "" {
			in.Intent = IntentConfirmed
		}
	}
	res, err := w.Create(ctx, tx, in, a, now)
	if err != nil {
		return uuid.Nil, fmt.Errorf("tạo đơn thay thế: %w", err)
	}
	if res.Replayed {
		return uuid.Nil, fmt.Errorf("đơn thay thế: %w", ErrDuplicateOrder)
	}
	return res.Order.ID, nil
}

// OnShipmentReturning là phía đơn khi vận đơn shipmentID của đơn orderID (người bán sellerID) bắt đầu hoàn, trong tx
// của fulfillment: khóa đơn; vận đơn không phải hiện hành (vận đơn cũ) hay đã có phiếu hoàn chưa hủy của vận đơn này →
// false, không đổi gì. Đơn confirmed → tạo phiếu carrier_return requested đủ mọi dòng (tiền hoàn 0); đơn vẫn
// confirmed, tồn chưa đổi (TD02). Đơn trạng thái khác → ErrInvalidTransition.
func (w Writer) OnShipmentReturning(ctx context.Context, tx pgx.Tx, sellerID, orderID, shipmentID uuid.UUID, a Actor,
	now time.Time,
) (applied bool, err error) {
	q, o, current, err := w.lockCurrentShipment(ctx, tx, sellerID, orderID, shipmentID)
	switch {
	case err != nil:
		return false, err
	case !current:
		return false, nil
	case o.Status != StatusConfirmed:
		return false, fmt.Errorf("%w: vận đơn hoàn của đơn %s", ErrInvalidTransition, o.Status)
	}
	items, err := q.LockOrderItemsForReturn(ctx, store.LockOrderItemsForReturnParams{SellerID: sellerID, OrderID: orderID})
	if err != nil {
		return false, fmt.Errorf("khóa dòng đơn: %w", err)
	}
	_, err = q.CarrierReturnOfShipment(ctx, store.CarrierReturnOfShipmentParams{ShipmentID: shipmentID, SellerID: sellerID})
	switch {
	case err == nil:
		return false, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return false, fmt.Errorf("đọc phiếu hoàn của vận đơn: %w", err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return false, fmt.Errorf("sinh mã phiếu trả: %w", err)
	}
	code, err := nextReturnCode(ctx, tx)
	if err != nil {
		return false, err
	}
	n, err := q.InsertCarrierReturn(ctx, store.InsertCarrierReturnParams{
		ID: id, Code: code, OrderID: orderID, ShipmentID: shipmentID, CreatedBy: a.id(), Now: now.UTC(),
	})
	if err != nil {
		return false, fmt.Errorf("ghi phiếu hoàn: %w", err)
	}
	if n == 0 {
		return false, nil
	}
	for _, it := range items {
		if err := q.InsertReturnItem(ctx, store.InsertReturnItemParams{
			ReturnID: id, OrderItemID: it.ID, Quantity: it.Quantity,
		}); err != nil {
			return false, fmt.Errorf("ghi dòng phiếu hoàn: %w", err)
		}
	}
	return true, w.signal(ctx, tx, o.TenantID, lockedOrder(o))
}

// CarrierReturnFor trả phiếu carrier_return chưa hủy của vận đơn shipmentID (đơn của người bán sellerID); không có →
// ErrNotFound.
func CarrierReturnFor(ctx context.Context, tx pgx.Tx, sellerID, shipmentID uuid.UUID) (Return, error) {
	id, err := store.New(tx).CarrierReturnOfShipment(ctx, store.CarrierReturnOfShipmentParams{
		ShipmentID: shipmentID, SellerID: sellerID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Return{}, ErrNotFound
	}
	if err != nil {
		return Return{}, fmt.Errorf("đọc phiếu hoàn của vận đơn: %w", err)
	}
	return GetReturn(ctx, tx, sellerID, id)
}

// lockedReturn là phiếu đã khóa cùng đơn gốc (và đơn thay thế khi được yêu cầu) đã khóa.
type lockedReturn struct {
	q           *store.Queries
	order       store.LockOrderRow
	replacement *store.LockOrderRow
	r           store.LockReturnRow
}

// lockReturn khóa đơn của phiếu returnID (withReplacement: cả đơn thay thế, hai đơn theo id tăng dần) rồi phiếu, và
// so version của phiếu. Không có (hay của người bán khác) → ErrNotFound; version khác → ErrVersionConflict.
func lockReturn(ctx context.Context, tx pgx.Tx, sellerID, returnID uuid.UUID, version int64, withReplacement bool,
) (lockedReturn, error) {
	l := lockedReturn{q: store.New(tx)}
	ref, err := l.q.ReturnOrderRef(ctx, store.ReturnOrderRefParams{ID: returnID, SellerID: sellerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return l, ErrNotFound
	}
	if err != nil {
		return l, fmt.Errorf("đọc đơn của phiếu trả: %w", err)
	}
	ids := []uuid.UUID{ref.OrderID}
	if withReplacement && ref.ReplacementOrderID.Valid {
		ids = append(ids, ref.ReplacementOrderID.Bytes)
		slices.SortFunc(ids, func(x, y uuid.UUID) int { return bytes.Compare(x[:], y[:]) })
	}
	for _, id := range ids {
		o, err := lockOrder(ctx, l.q, sellerID, id)
		if err != nil {
			return l, err
		}
		if id == ref.OrderID {
			l.order = o
		} else {
			l.replacement = &o
		}
	}
	if l.r, err = l.q.LockReturn(ctx, store.LockReturnParams{ID: returnID, OrderID: ref.OrderID}); errors.Is(err, pgx.ErrNoRows) {
		return l, ErrNotFound
	} else if err != nil {
		return l, fmt.Errorf("khóa phiếu trả: %w", err)
	}
	if l.r.Version != version {
		return l, ErrVersionConflict
	}
	return l, nil
}

// ReceiveReturn là kho nhận hàng của phiếu returnID (người bán sellerID) ở version, tình trạng cả dòng lines (mỗi
// dòng phiếu đúng một lần; sai → ErrInvalidInput), trong tx của bên gọi (lớp HTTP đã kiểm order.return.receive). Dựng
// dòng số lượng (cả số của dòng vào loại của tình trạng) rồi nhận như ReceiveReturnQty.
func (w Writer) ReceiveReturn(ctx context.Context, tx pgx.Tx, sellerID, returnID uuid.UUID, version int64,
	lines []ReceiveLine, a Actor, now time.Time,
) (Return, error) {
	return w.receiveQty(ctx, tx, sellerID, returnID, version, conditionLines(lines), a, now)
}

// variantOf trả biến thể của dòng đơn id trong đơn o.
func variantOf(o Order, id uuid.UUID) (uuid.UUID, error) {
	for _, it := range o.Items {
		if it.ID == id {
			return it.VariantID, nil
		}
	}
	return uuid.Nil, fmt.Errorf("%w: dòng phiếu trỏ tới dòng đơn %s không còn", ErrStatusConflict, id)
}

// receiveCustomer nhập kho hàng khách trả, tăng version đơn gốc và phát order.partially_returned / order.returned.
func (w Writer) receiveCustomer(ctx context.Context, tx pgx.Tx, stock inventory.Writer, l lockedReturn,
	lines []qtyLine, full Order, total int64, who inventory.Actor, a Actor, now time.Time,
) error {
	var in []inventory.ReturnLine
	for _, ln := range lines {
		if ln.Sellable+ln.Damaged == 0 {
			continue
		}
		v, err := variantOf(full, ln.OrderItemID)
		if err != nil {
			return err
		}
		if ln.Sellable > 0 {
			in = append(in, inventory.ReturnLine{VariantID: v, WarehouseID: full.WarehouseID, Qty: ln.Sellable})
		}
		if ln.Damaged > 0 {
			in = append(in, inventory.ReturnLine{
				VariantID: v, WarehouseID: full.WarehouseID, Qty: ln.Damaged, Damaged: true,
			})
		}
	}
	if len(in) > 0 {
		if err := stock.ReceiveCustomerReturn(ctx, tx, l.r.ID, in, who); err != nil {
			return fmt.Errorf("nhập kho hàng trả: %w", err)
		}
	}
	topic, err := receivedTopic(ctx, l.q, full)
	if err != nil {
		return err
	}
	version, err := l.q.BumpOrderVersion(ctx, store.BumpOrderVersionParams{
		Now: now, SellerID: full.SellerID, ID: full.ID, Version: l.order.Version,
	})
	if err != nil {
		return fmt.Errorf("ghi version đơn khi nhận phiếu trả: %w", err)
	}
	if _, _, err := w.Events.Emit(ctx, tx, w.Queue, events.Event{
		TenantID: a.TenantID, AggregateType: aggregateOrder, AggregateID: full.ID.String(), Seq: version, Topic: topic,
		Payload: ReturnReceived{
			Changed: Changed{
				OrderID: full.ID, SellerID: full.SellerID, Version: version, CustomerID: full.CustomerID, Status: full.Status,
			},
			ReturnID: l.r.ID, Kind: l.r.Kind, RefundAmount: total, ShipmentID: uuidPtr(l.r.ShipmentID),
			MissingQty: missingOf(lines),
		},
	}); err != nil {
		return fmt.Errorf("phát %s: %w", topic, err)
	}
	return nil
}

// receivedTopic: order.returned khi mọi dòng đơn o đã trả đủ số mua qua các phiếu đã nhận (restocked, closed), không
// thì order.partially_returned.
func receivedTopic(ctx context.Context, q *store.Queries, o Order) (string, error) {
	rows, err := q.ReturnedLinesOfOrder(ctx, o.ID)
	if err != nil {
		return "", fmt.Errorf("đọc phiếu trả của đơn: %w", err)
	}
	got := make(map[uuid.UUID]int32, len(o.Items))
	for _, r := range rows {
		if r.Status == ReturnRestocked || r.Status == ReturnClosed {
			got[r.OrderItemID] += r.Quantity
		}
	}
	for _, it := range o.Items {
		if got[it.ID] < it.Qty {
			return TopicOrderPartiallyReturned, nil
		}
	}
	return TopicOrderReturned, nil
}

// CloseReturn đóng phiếu returnID ở version (restocked → closed: cash báo đã hoàn tiền ở M3, hoặc nhân viên đóng khi
// không có tiền phải hoàn). Phiếu không restocked → ErrReturnNotAllowed.
func (w Writer) CloseReturn(ctx context.Context, tx pgx.Tx, sellerID, returnID uuid.UUID, version int64, _ Actor,
	now time.Time,
) (Return, error) {
	return w.changeReturn(ctx, tx, sellerID, returnID, version, ReturnRestocked, false, func(l lockedReturn) error {
		return setReturnStatus(ctx, l, ReturnClosed, now.UTC())
	})
}

// CancelReturn hủy phiếu returnID ở version (requested → cancelled; số đã trả của dòng mở lại). Phiếu exchange có đơn
// thay thế chưa gửi hãng (draft, new, confirmed không vận đơn hiện hành) thì hủy luôn đơn đó bằng Cancel (lý do other:
// nhả hàng, trả lượt); đơn thay thế đã hủy → chỉ hủy phiếu; đã gửi hãng, đã giao, đã trả → ErrReturnNotAllowed. Phiếu
// không requested → ErrReturnNotAllowed.
func (w Writer) CancelReturn(ctx context.Context, tx pgx.Tx, sellerID, returnID uuid.UUID, version int64, a Actor,
	now time.Time,
) (Return, error) {
	now = now.UTC()
	return w.changeReturn(ctx, tx, sellerID, returnID, version, ReturnRequested, true, func(l lockedReturn) error {
		repl := l.replacement
		cancelReplacement := false
		if repl != nil {
			switch {
			case repl.Status == StatusCancelled:
			case repl.CurrentShipmentID.Valid || !slices.Contains([]string{StatusDraft, StatusNew, StatusConfirmed}, repl.Status):
				return fmt.Errorf("%w: đơn thay thế đã gửi hãng hoặc đã xong (%s)", ErrReturnNotAllowed, repl.Status)
			default:
				cancelReplacement = true
			}
		}
		if err := setReturnStatus(ctx, l, ReturnCancelled, now); err != nil {
			return err
		}
		if !cancelReplacement {
			return nil
		}
		if _, err := w.Cancel(ctx, tx, sellerID, repl.ID, repl.Version, CancelOther, "phiếu đổi "+l.r.Code+" đã hủy", a,
			now); err != nil {
			return fmt.Errorf("hủy đơn thay thế: %w", err)
		}
		return nil
	})
}

// changeReturn là khung của ReceiveReturn, CloseReturn, CancelReturn: khóa đơn (withReplacement: cả đơn thay thế) rồi
// phiếu ở version (lockReturn); phiếu không ở from → ErrReturnNotAllowed; chạy change; báo hiệu đơn gốc; đọc lại phiếu.
func (w Writer) changeReturn(ctx context.Context, tx pgx.Tx, sellerID, returnID uuid.UUID, version int64, from string,
	withReplacement bool, change func(l lockedReturn) error,
) (Return, error) {
	if err := w.wired(); err != nil {
		return Return{}, err
	}
	l, err := lockReturn(ctx, tx, sellerID, returnID, version, withReplacement)
	if err != nil {
		return Return{}, err
	}
	if l.r.Status != from {
		return Return{}, fmt.Errorf("%w: phiếu %s đang %s", ErrReturnNotAllowed, l.r.Code, l.r.Status)
	}
	if err := change(l); err != nil {
		return Return{}, err
	}
	if err := w.signal(ctx, tx, l.order.TenantID, lockedOrder(l.order)); err != nil {
		return Return{}, err
	}
	return GetReturn(ctx, tx, sellerID, returnID)
}

// setReturnStatus đổi phiếu đã khóa (changeReturn đã kiểm trạng thái hiện tại) sang to.
func setReturnStatus(ctx context.Context, l lockedReturn, to string, now time.Time) error {
	_, err := l.q.SetReturnStatus(ctx, store.SetReturnStatusParams{
		ToStatus: to, Now: now, ID: l.r.ID, Version: l.r.Version, FromStatus: l.r.Status,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrVersionConflict
	}
	if err != nil {
		return fmt.Errorf("đổi trạng thái phiếu trả: %w", err)
	}
	return nil
}

// GetReturn trả phiếu id (đơn của người bán sellerID) kèm dòng; không có → ErrNotFound.
func GetReturn(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID) (Return, error) {
	q := store.New(tx)
	row, err := q.GetReturnRow(ctx, store.GetReturnRowParams{ID: id, SellerID: sellerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Return{}, ErrNotFound
	}
	if err != nil {
		return Return{}, fmt.Errorf("đọc phiếu trả: %w", err)
	}
	rs, err := withItems(ctx, q, []store.GetReturnRowRow{row})
	if err != nil {
		return Return{}, err
	}
	return rs[0], nil
}

// ReturnCursor là vị trí sau phiếu cuối của một trang (phiếu xếp theo id uuidv7 giảm dần: mới nhất trước).
type ReturnCursor struct {
	ID uuid.UUID
}

// String mã hóa con trỏ thành chuỗi mờ cho API (base64url của 16 byte mã phiếu).
func (c ReturnCursor) String() string { return base64.RawURLEncoding.EncodeToString(c.ID[:]) }

// ParseReturnCursor đọc chuỗi do ReturnCursor.String tạo; hỏng → ErrInvalidInput.
func ParseReturnCursor(s string) (ReturnCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(raw) != len(uuid.UUID{}) {
		return ReturnCursor{}, fmt.Errorf("đọc con trỏ phiếu trả: %w", ErrInvalidInput)
	}
	var c ReturnCursor
	copy(c.ID[:], raw)
	return c, nil
}

// ReturnFilter là bộ lọc của ListReturns: Status, Kind rỗng là mọi giá trị; After: trang sau; Limit ≤ 0 → 50, tối đa
// 100.
type ReturnFilter struct {
	Status, Kind string
	After        *ReturnCursor
	Limit        int
}

// ListReturns trả một trang phiếu trả của người bán sellerID (mới nhất trước) kèm dòng, và con trỏ trang sau (nil khi
// hết).
func ListReturns(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID, f ReturnFilter) ([]Return, *ReturnCursor, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultReturnLimit
	}
	limit = min(limit, maxReturnLimit)
	p := store.ListReturnRowsParams{
		SellerID: sellerID, Status: pgtype.Text{String: f.Status, Valid: f.Status != ""},
		Kind: pgtype.Text{String: f.Kind, Valid: f.Kind != ""}, Lim: int32(limit + 1),
	}
	if f.After != nil {
		p.AfterID = pgtype.UUID{Bytes: f.After.ID, Valid: true}
	}
	q := store.New(tx)
	rows, err := q.ListReturnRows(ctx, p)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc danh sách phiếu trả: %w", err)
	}
	var next *ReturnCursor
	if len(rows) > limit {
		rows = rows[:limit]
		next = &ReturnCursor{ID: rows[limit-1].ID}
	}
	base := make([]store.GetReturnRowRow, 0, len(rows))
	for _, r := range rows {
		base = append(base, store.GetReturnRowRow(r))
	}
	out, err := withItems(ctx, q, base)
	if err != nil {
		return nil, nil, err
	}
	return out, next, nil
}

// withItems chép các dòng phiếu sang Return và đọc dòng của chúng (một câu).
func withItems(ctx context.Context, q *store.Queries, rows []store.GetReturnRowRow) ([]Return, error) {
	out := make([]Return, 0, len(rows))
	ids := make([]uuid.UUID, 0, len(rows))
	pos := make(map[uuid.UUID]int, len(rows))
	for i, r := range rows {
		ids, pos[r.ID] = append(ids, r.ID), i
		out = append(out, Return{
			ID: r.ID, OrderID: r.OrderID, Code: r.Code, Kind: r.Kind, Status: r.Status, Reason: r.Reason,
			ShipmentID: uuidPtr(r.ShipmentID), ReplacementOrderID: uuidPtr(r.ReplacementOrderID),
			RefundAmount: r.RefundAmount, ReceivedAt: timePtr(r.ReceivedAt), CreatedAt: r.CreatedAt, Version: r.Version,
			Items: []ReturnItem{},
		})
	}
	if len(ids) == 0 {
		return out, nil
	}
	items, err := q.ListReturnItemsOf(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("đọc dòng phiếu trả: %w", err)
	}
	for _, it := range items {
		r := &out[pos[it.ReturnID]]
		r.Items = append(r.Items, ReturnItem{
			ID: it.ID, OrderItemID: it.OrderItemID, Quantity: int(it.Quantity), Condition: it.Condition,
			RefundAmount: it.RefundAmount,
		})
	}
	return out, nil
}
