package order

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/customer"
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

// CreateManual lên đơn nhập tay của M1 (chữ ký giữ nguyên, Q30): lớp bọc Create với kênh manual, khách có sẵn
// in.CustomerID, địa chỉ kiểu M1, dòng không giảm giá, ý định rỗng (tạo thẳng confirmed với luật địa chỉ M1), giờ lấy
// từ database (now() của transaction). Lỗi như M1: biến thể không có trong shop cũng là ErrMixedSellers (M1 không phân
// biệt); khóa nghiệp vụ đã có đơn khác nội dung → ErrDuplicateOrder, cùng nội dung → trả đơn đó. Audit do bên gọi
// ghi (AuditFields).
func (w Writer) CreateManual(ctx context.Context, tx pgx.Tx, in CreateManualInput, actor Actor) (Order, error) {
	if in.SellerID == uuid.Nil || in.CustomerID == uuid.Nil {
		return Order{}, ErrInvalidInput
	}
	now, err := store.New(tx).TxNow(ctx)
	if err != nil {
		return Order{}, fmt.Errorf("đọc giờ của transaction: %w", err)
	}
	lines := make([]LineInput, len(in.Items))
	for i, it := range in.Items {
		lines[i] = LineInput{VariantID: it.VariantID, Quantity: it.Qty}
	}
	addr := in.ShipAddress
	res, err := w.Create(ctx, tx, CreateInput{
		IdempotencyKey: in.IdempotencyKey, SellerID: in.SellerID, WarehouseID: in.WarehouseID, Channel: ChannelManual,
		Customer: customer.OrderCustomerInput{CustomerID: &in.CustomerID}, ShipAddress: &addr, Lines: lines,
		ShippingFee: in.ShippingFee,
	}, actor, now)
	if errors.Is(err, ErrVariantNotFound) {
		return Order{}, ErrMixedSellers
	}
	return res.Order, err
}

// normalizedShipPhone trả SĐT giao dạng chuẩn +84… và chữ số đảo ngược (cột ship_phone, ship_phone_reversed: gợi ý
// đơn trùng, tìm theo đuôi số). SĐT không chuẩn hóa được (số nước ngoài, số máy bàn ngắn) → hai chuỗi rỗng.
func normalizedShipPhone(p logging.Phone) (logging.Phone, string) {
	n, err := customer.NormalizePhone(string(p))
	if err != nil {
		return "", ""
	}
	return n, customer.ReverseDigits(n)
}

// mergeLines gộp các dòng cùng biến thể thành một dòng (một biến thể một dòng, ràng buộc order_items_variant_key của
// M2-05): cộng số lượng và giảm dòng, giữ vị trí lần xuất hiện đầu. Tổng số lượng một biến thể vượt int32, hay tổng
// giảm dòng vượt giới hạn tiền → ErrInvalidInput.
func mergeLines(items []LineInput) ([]LineInput, error) {
	out := make([]LineInput, 0, len(items))
	at := make(map[uuid.UUID]int, len(items))
	for _, it := range items {
		i, seen := at[it.VariantID]
		if !seen {
			at[it.VariantID] = len(out)
			out = append(out, it)
			continue
		}
		discount, ok := addMoney(out[i].LineDiscount, it.LineDiscount)
		if it.Quantity > math.MaxInt32-out[i].Quantity || !ok { // số lượng đã kiểm > 0 ở prepare
			return nil, ErrInvalidInput
		}
		out[i].Quantity += it.Quantity
		out[i].LineDiscount = discount
	}
	return out, nil
}

func validText(s string, limit int) bool {
	n := utf8.RuneCountInString(s)
	return n > 0 && n <= limit && utf8.ValidString(s) &&
		!strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || (r >= 0x7f && r <= 0x9f) })
}

// addMoney cộng hai số tiền trong [0, maxMoney]; vượt maxMoney → false (không cắt).
func addMoney(a, b int64) (int64, bool) {
	if a < 0 || b < 0 || a > maxMoney-b {
		return 0, false
	}
	return a + b, true
}
