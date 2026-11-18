package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// maxMoney là số tiền lớn nhất (2^53 − 1 đồng), như CHECK của mọi cột tiền.
const maxMoney = catalog.MaxMoney

const (
	maxItems        = 100
	maxNameRunes    = 120
	maxLineRunes    = 500
	pgUniqueViolate = "23505"
	idempotencyIdx  = "orders_idempotency_key_key"
)

var phoneRe = regexp.MustCompile(`^\+?[0-9]{8,15}$`)

// CreateManual lên đơn nhập tay trong tx của bên gọi (04 mục 3 quy tắc 3: một transaction): kiểm biến thể bằng
// catalog.VariantsForSale (không thuộc người bán của đơn → ErrMixedSellers; ngừng bán → ErrNotForSale), tính tiền int64
// có kiểm tràn (ErrTotalOverflow), lấy mã đơn, chèn đơn ở confirmed (01 mục 2) với cod_amount = grand_total, dòng đơn
// theo thứ tự nhập, một dòng lịch sử, giữ hàng (inventory.Reserve; thiếu → *OutOfStockError), sự kiện order.created
// (seq 1) và order.confirmed (seq 2). Audit do bên gọi ghi (AuditFields; order không nhập identity, 04 mục 3). Khóa nghiệp vụ đã có đơn → ErrDuplicateOrder. Mọi lỗi: bên gọi
// lùi cả transaction (không còn đơn, dòng, giữ hàng nào).
func (w Writer) CreateManual(ctx context.Context, tx pgx.Tx, in CreateManualInput, actor Actor) (Order, error) {
	stock, err := w.stock()
	if err != nil {
		return Order{}, err
	}
	d, err := draft(ctx, tx, in)
	if err != nil {
		return Order{}, err
	}
	o, err := insertOrder(ctx, tx, in, d, actor)
	if err != nil {
		return Order{}, err
	}
	reserve, err := insertItems(ctx, tx, &o, d)
	if err != nil {
		return Order{}, err
	}
	if _, err := stock.Reserve(ctx, tx, reserve, inventory.Actor{Type: inventory.ActorStaff, ID: actor.UserID}); err != nil {
		if ie, ok := errors.AsType[*inventory.InsufficientError](err); ok {
			return Order{}, &OutOfStockError{VariantID: ie.VariantID, Available: ie.Available}
		}
		return Order{}, fmt.Errorf("giữ hàng: %w", err)
	}
	for seq, topic := range []string{TopicOrderCreated, TopicOrderConfirmed} {
		if err := w.emit(ctx, tx, actor.TenantID, o, topic, int64(seq+1)); err != nil {
			return Order{}, err
		}
	}
	return o, nil
}

// drafted là phần đã kiểm và tính của một đơn nhập tay, trước khi ghi.
type drafted struct {
	addr            ShipAddress
	lines           []line
	subtotal, grand int64
	warehouse       uuid.UUID
}

// draft kiểm đầu vào, biến thể, tính tiền có kiểm tràn và chọn kho giữ hàng (chưa ghi gì).
func draft(ctx context.Context, tx pgx.Tx, in CreateManualInput) (drafted, error) {
	addr, err := validate(in)
	if err != nil {
		return drafted{}, err
	}
	lines, err := saleLines(ctx, tx, in)
	if err != nil {
		return drafted{}, err
	}
	subtotal, err := sumLines(lines)
	if err != nil {
		return drafted{}, err
	}
	grand, ok := addMoney(subtotal, in.ShippingFee)
	if !ok {
		return drafted{}, ErrTotalOverflow
	}
	warehouse := in.WarehouseID
	if warehouse == uuid.Nil {
		if warehouse, err = inventory.DefaultWarehouse(ctx, tx); err != nil {
			return drafted{}, fmt.Errorf("chọn kho giữ hàng: %w", err)
		}
	}
	return drafted{addr: addr, lines: lines, subtotal: subtotal, grand: grand, warehouse: warehouse}, nil
}

// insertOrder lấy mã đơn, chèn đơn confirmed và dòng lịch sử tạo đơn.
func insertOrder(ctx context.Context, tx pgx.Tx, in CreateManualInput, d drafted, actor Actor) (Order, error) {
	code, err := nextCode(ctx, tx)
	if err != nil {
		return Order{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Order{}, fmt.Errorf("sinh mã đơn: %w", err)
	}
	addrJSON, err := json.Marshal(d.addr)
	if err != nil {
		return Order{}, fmt.Errorf("ghi địa chỉ giao: %w", err)
	}
	q := store.New(tx)
	staff := pgtype.UUID{Bytes: actor.UserID, Valid: actor.UserID != uuid.Nil}
	// version 2: hai sự kiện của lần tạo (created seq 1, confirmed seq 2); version luôn bằng seq của sự kiện cuối.
	createdAt, err := q.InsertOrder(ctx, store.InsertOrderParams{
		ID: id, SellerID: in.SellerID, Code: code, CustomerID: in.CustomerID, Channel: channelManual,
		IdempotencyKey: in.IdempotencyKey, Status: StatusConfirmed, ShipAddress: addrJSON, Subtotal: d.subtotal,
		DiscountTotal: 0, ShippingFeeCharged: in.ShippingFee, CodAmount: d.grand, GrandTotal: d.grand,
		CreatedBy: staff, Version: 2,
	})
	if pe, ok := errors.AsType[*pgconn.PgError](err); ok && pe.Code == pgUniqueViolate && pe.ConstraintName == idempotencyIdx {
		return Order{}, ErrDuplicateOrder
	}
	if err != nil {
		return Order{}, fmt.Errorf("ghi đơn: %w", err)
	}
	if err := q.InsertStatusHistory(ctx, store.InsertStatusHistoryParams{
		OrderID: id, ToStatus: StatusConfirmed, ActorType: inventory.ActorStaff, ActorID: staff, OccurredAt: createdAt,
	}); err != nil {
		return Order{}, fmt.Errorf("ghi lịch sử đơn: %w", err)
	}
	return Order{
		ID: id, SellerID: in.SellerID, CustomerID: in.CustomerID, Code: code, Status: StatusConfirmed,
		Channel: channelManual, ShipAddress: d.addr, Subtotal: d.subtotal, ShippingFee: in.ShippingFee,
		CODAmount: d.grand, GrandTotal: d.grand, Version: 2, CreatedAt: createdAt,
	}, nil
}

// insertItems chèn dòng đơn theo thứ tự nhập (position 1, 2, …) vào o và trả các dòng giữ hàng tương ứng.
func insertItems(ctx context.Context, tx pgx.Tx, o *Order, d drafted) ([]inventory.ReserveLine, error) {
	q := store.New(tx)
	reserve := make([]inventory.ReserveLine, len(d.lines))
	for i, l := range d.lines {
		id, err := uuid.NewV7()
		if err != nil {
			return nil, fmt.Errorf("sinh mã dòng đơn: %w", err)
		}
		it := Item{
			ID: id, VariantID: l.variant.ID, Position: int32(i + 1), Qty: l.qty, SKU: l.variant.SKU,
			Name: l.variant.Name, UnitPrice: l.variant.Price, NetAmount: l.net,
		}
		if err := q.InsertOrderItem(ctx, store.InsertOrderItemParams{
			ID: it.ID, SellerID: o.SellerID, OrderID: o.ID, Position: it.Position, VariantID: it.VariantID,
			SkuSnapshot: it.SKU, NameSnapshot: it.Name, Quantity: it.Qty, UnitPrice: it.UnitPrice, NetAmount: it.NetAmount,
		}); err != nil {
			return nil, fmt.Errorf("ghi dòng đơn: %w", err)
		}
		o.Items = append(o.Items, it)
		reserve[i] = inventory.ReserveLine{OrderItemID: it.ID, VariantID: it.VariantID, WarehouseID: d.warehouse, Qty: it.Qty}
	}
	return reserve, nil
}

// line là một dòng đơn đã kiểm: biến thể bán được, số lượng, thành tiền.
type line struct {
	variant catalog.SaleVariant
	qty     int32
	net     int64
}

// validate kiểm phần đầu vào không cần database và trả địa chỉ giao đã chuẩn hóa.
func validate(in CreateManualInput) (ShipAddress, error) {
	if in.IdempotencyKey == uuid.Nil || in.SellerID == uuid.Nil || in.CustomerID == uuid.Nil ||
		len(in.Items) == 0 || len(in.Items) > maxItems || in.ShippingFee < 0 || in.ShippingFee > maxMoney {
		return ShipAddress{}, ErrInvalidInput
	}
	for _, it := range in.Items {
		if it.VariantID == uuid.Nil || it.Qty <= 0 {
			return ShipAddress{}, ErrInvalidInput
		}
	}
	a := ShipAddress{
		Name:  strings.TrimSpace(in.ShipAddress.Name),
		Phone: logging.Phone(strings.TrimSpace(string(in.ShipAddress.Phone))),
		Line:  logging.Address(strings.TrimSpace(string(in.ShipAddress.Line))),
	}
	if !validText(a.Name, maxNameRunes) || !validText(string(a.Line), maxLineRunes) || !phoneRe.MatchString(string(a.Phone)) {
		return ShipAddress{}, ErrInvalidInput
	}
	return a, nil
}

func validText(s string, limit int) bool {
	n := utf8.RuneCountInString(s)
	return n > 0 && n <= limit && utf8.ValidString(s) &&
		!strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || (r >= 0x7f && r <= 0x9f) })
}

// saleLines đọc các biến thể của đơn trong một câu (catalog.VariantsForSale); biến thể thiếu trong kết quả được hỏi
// lại từng cái để phân biệt ngừng bán (ErrNotForSale) với không thuộc người bán của đơn (ErrMixedSellers). Kiểm theo
// thứ tự nhập: lỗi của dòng đầu tiên sai được trả.
func saleLines(ctx context.Context, tx pgx.Tx, in CreateManualInput) ([]line, error) {
	ids := make([]uuid.UUID, len(in.Items))
	for i, it := range in.Items {
		ids[i] = it.VariantID
	}
	sale, err := catalog.VariantsForSale(ctx, tx, in.SellerID, ids)
	if err != nil {
		return nil, fmt.Errorf("kiểm biến thể: %w", err)
	}
	out := make([]line, len(in.Items))
	for i, it := range in.Items {
		v, ok := sale[it.VariantID]
		if !ok {
			_, err := catalog.VariantForSale(ctx, tx, in.SellerID, it.VariantID)
			switch {
			case errors.Is(err, catalog.ErrNotForSale):
				return nil, ErrNotForSale
			case errors.Is(err, catalog.ErrNotFound):
				return nil, ErrMixedSellers
			case err != nil:
				return nil, fmt.Errorf("kiểm biến thể: %w", err)
			}
			// Vừa đổi sang bán được giữa hai câu: coi như ngừng bán lúc kiểm; nhân viên gửi lại.
			return nil, ErrNotForSale
		}
		net, ok := mulMoney(v.Price, it.Qty)
		if !ok {
			return nil, ErrTotalOverflow
		}
		out[i] = line{variant: v, qty: it.Qty, net: net}
	}
	return out, nil
}

func sumLines(lines []line) (int64, error) {
	var sum int64
	for _, l := range lines {
		var ok bool
		if sum, ok = addMoney(sum, l.net); !ok {
			return 0, ErrTotalOverflow
		}
	}
	return sum, nil
}

// addMoney cộng hai số tiền trong [0, maxMoney]; vượt maxMoney → false (không cắt).
func addMoney(a, b int64) (int64, bool) {
	if a < 0 || b < 0 || a > maxMoney-b {
		return 0, false
	}
	return a + b, true
}

// mulMoney nhân giá với số lượng (> 0); vượt maxMoney → false.
func mulMoney(price int64, qty int32) (int64, bool) {
	if price < 0 || qty <= 0 || price > maxMoney/int64(qty) {
		return 0, false
	}
	return price * int64(qty), true
}
