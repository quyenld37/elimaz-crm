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
	CustomerRootID uuid.UUID     // hồ sơ gốc của khách (sau gộp)
	Phone          logging.Phone // SĐT nhận hàng của đơn, dạng nào cũng được (so sau khi chuẩn hóa)
	SellerID       uuid.UUID     // người bán của đơn
	Now            time.Time
}

// Evaluate tính phần giảm của mã c cho đơn in; thuần (không đọc database). Kiểm theo thứ tự: disabled, not_started
// (Now < ValidFrom), expired (Now ≥ ValidTo), channel, seller, bound_other, min_subtotal (Base < MinSubtotal); mã bị từ
// chối trả (0, lý do). Phần trăm: floor(Base × Value / 10000), chặn bởi MaxDiscount; cố định: min(Value, Base). Không
// kiểm lượt (việc của Redeem, Check). Mã đạt trả (phần giảm, "").
func Evaluate(c Coupon, in EvalInput) (discount int64, reason Reason) {
	if r := rejectReason(c, in); r != "" {
		return 0, r
	}
	base := max(in.Base, 0)
	if c.DiscountKind == KindFixed {
		return min(c.Value, base), ""
	}
	d := percentOf(base, c.Value)
	if c.MaxDiscount != nil {
		d = min(d, *c.MaxDiscount)
	}
	return d, ""
}

// rejectReason trả lý do đầu tiên mã c không dùng được cho in (rỗng: dùng được), theo thứ tự của Evaluate.
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
	case in.Base < c.MinSubtotal:
		return ReasonMinSubtotal
	}
	return ""
}

// boundMatches: mã không gắn ai → đúng; mã gắn khách và/hoặc SĐT → đúng khi đơn khớp ít nhất một thứ đã gắn.
func boundMatches(c Coupon, in EvalInput) bool {
	if c.BoundCustomerID == uuid.Nil && c.BoundPhone == "" {
		return true
	}
	if c.BoundCustomerID != uuid.Nil && c.BoundCustomerID == in.CustomerRootID {
		return true
	}
	if c.BoundPhone == "" || in.Phone == "" {
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
