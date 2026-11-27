package promotion

import (
	"math/bits"
	"slices"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// percentScale: Value của mã phần trăm tính theo phần vạn (1000 = 10%).
const percentScale = 10000

// EvalInput là phần của đơn mà luật mã cần.
type EvalInput struct {
	Base           int64         // tạm tính sau giảm dòng và giảm tay cấp đơn
	Channel        string        // kênh của đơn (facebook, web, manual)
	CustomerID     uuid.UUID     // hồ sơ khách của đơn (Redeem ghi lượt mỗi khách theo hồ sơ này; Check không cần)
	CustomerRootID uuid.UUID     // hồ sơ gốc hiện tại của khách (sau gộp)
	Phone          logging.Phone // SĐT của đơn (người nhận, hay SĐT nhập cho khách lúc tạo), dạng nào cũng được
	SellerID       uuid.UUID     // người bán của đơn
	Now            time.Time

	// phoneOwned: SĐT gắn mã là danh tính SĐT của nhóm khách của đơn (Redeem, Check đọc bằng customer.HasPhone trước
	// Evaluate; Task 433, C-03).
	phoneOwned bool
}

// Evaluate tính phần giảm của mã c cho đơn in; thuần (không đọc database). Kiểm theo thứ tự: disabled, not_started
// (Now < ValidFrom), expired (Now ≥ ValidTo), channel, seller, bound_other, min_subtotal (Base < MinSubtotal), rồi
// zero_discount (phần giảm tính ra 0: không áp, không tiêu lượt); mã bị từ chối trả (0, lý do). Phần giảm: amountOf.
// Không kiểm lượt (việc của Redeem, Check). Mã đạt trả (phần giảm > 0, "").
func Evaluate(c Coupon, in EvalInput) (discount int64, reason Reason) {
	if r := rejectReason(c, in); r != "" {
		return 0, r
	}
	return amountOf(c, in.Base)
}

// amountOf là luật phụ thuộc tiền của mã c trên tạm tính base: base < MinSubtotal → min_subtotal; phần trăm:
// floor(base × Value / 10000), chặn bởi MaxDiscount; cố định: min(Value, base); phần giảm 0 → zero_discount. Reprice
// (sửa đơn giữ mã) chỉ dùng luật này.
func amountOf(c Coupon, base int64) (int64, Reason) {
	if base < c.MinSubtotal {
		return 0, ReasonMinSubtotal
	}
	base = max(base, 0)
	var d int64
	if c.DiscountKind == KindFixed {
		d = min(c.Value, base)
	} else {
		d = percentOf(base, c.Value)
		if c.MaxDiscount != nil {
			d = min(d, *c.MaxDiscount)
		}
	}
	if d <= 0 {
		return 0, ReasonZeroDiscount
	}
	return d, ""
}

// rejectReason trả lý do đầu tiên mã c không dùng được cho in theo luật không phụ thuộc tiền (rỗng: dùng được), theo
// thứ tự của Evaluate.
func rejectReason(c Coupon, in EvalInput) Reason {
	switch {
	case c.Status != StatusActive:
		return ReasonDisabled
	case in.Now.Before(c.ValidFrom):
		return ReasonNotStarted
	case !in.Now.Before(c.ValidTo):
		return ReasonExpired
	case len(c.Channels) > 0 && !slices.Contains(c.Channels, in.Channel):
		return ReasonChannel
	case c.SellerID != uuid.Nil && c.SellerID != in.SellerID:
		return ReasonSeller
	case !boundMatches(c, in):
		return ReasonBoundOther
	}
	return ""
}

// boundMatches: mã không gắn ai → đúng; mã gắn khách và/hoặc SĐT → đúng khi đơn khớp ít nhất một thứ đã gắn. Khách
// gắn mã so theo gốc: bên gọi có database (Redeem, Check) đã đổi c.BoundCustomerID về gốc hiện tại. SĐT gắn mã khớp khi
// trùng SĐT của đơn, hoặc là danh tính SĐT của nhóm khách (in.phoneOwned) — cùng một luật lúc tạo và lúc sửa đơn.
func boundMatches(c Coupon, in EvalInput) bool {
	if c.BoundCustomerID == uuid.Nil && c.BoundPhone == "" {
		return true
	}
	if c.BoundCustomerID != uuid.Nil && c.BoundCustomerID == in.CustomerRootID {
		return true
	}
	if c.BoundPhone == "" {
		return false
	}
	if in.phoneOwned {
		return true
	}
	if in.Phone == "" {
		return false
	}
	p, err := customer.NormalizePhone(string(in.Phone))
	return err == nil && p == c.BoundPhone
}

// percentOf trả floor(base × value / 10000), tích tính 128 bit. Điều kiện: base ≥ 0, 1 ≤ value ≤ 10000 (CHECK của
// coupons) nên kết quả ≤ base, vừa int64.
func percentOf(base, value int64) int64 {
	if value <= 0 {
		return 0
	}
	value = min(value, percentScale)
	hi, lo := bits.Mul64(uint64(base), uint64(value)) //nolint:gosec // G115: base ≥ 0, value trong [1, 10000]
	q, _ := bits.Div64(hi, lo, percentScale)
	return int64(q) //nolint:gosec // G115: q ≤ base ≤ MaxInt64
}
