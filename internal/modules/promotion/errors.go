// Package promotion là mã giảm giá, đợt mã và lượt dùng mã (spec 05 mục 6; kế hoạch M2, Task M2-06, M2-14). Lượt mã
// chỉ đổi qua public API của gói này, trong transaction của bên gọi (đơn).
//
// Khi nào không dùng: module khác không đọc bảng promotion.* (đọc view promotion.coupons_v); không ghi SĐT gắn mã vào
// log, lỗi, sự kiện.
package promotion

import (
	"errors"
	"fmt"
)

// Lỗi nghiệp vụ của promotion (kế hoạch M2, Task M2-02 khai một lần; task sau chỉ dùng). Lớp HTTP đổi sang mã ở
// internal/httpapi/errors.go.
var (
	// ErrCouponRejected: mã không dùng được cho đơn này; lỗi cụ thể là *RejectError (kèm lý do).
	ErrCouponRejected = errors.New("promotion: mã không dùng được")
	// ErrCouponLockedAfterUse: mã đã có lượt dùng, chỉ được đổi tên, hạn cuối, số lượt tối đa, trạng thái.
	ErrCouponLockedAfterUse = errors.New("promotion: mã đã dùng, không đổi được điều kiện")
	// ErrMaxUsesBelowUsed: số lượt tối đa mới nhỏ hơn số lượt đã dùng.
	ErrMaxUsesBelowUsed = errors.New("promotion: số lượt tối đa nhỏ hơn số đã dùng")
	// ErrCodeTaken: đã có mã cùng chữ (không phân biệt hoa thường) trong shop.
	ErrCodeTaken = errors.New("promotion: mã đã có")
	// ErrNotFound: không có mã hay đợt mã này trong shop (Task M2-14, Q-T71-2).
	ErrNotFound = errors.New("promotion: không có")
	// ErrVersionConflict: mã đã bị người khác sửa (version gửi lên đã cũ).
	ErrVersionConflict = errors.New("promotion: version đã đổi")
	// ErrInvalidInput: điều khoản, mã, đợt mã hay khách gắn mã không hợp lệ.
	ErrInvalidInput = errors.New("promotion: đầu vào không hợp lệ")
	// ErrBatchConflict: website xin lô (purpose, tên) đã có với số lượng, điều khoản hay SĐT khác (Task 197).
	ErrBatchConflict = errors.New("promotion: đã có lô cùng tên với điều khoản khác")
	// ErrRunInProgress: một lượt phát quà sinh nhật khác của shop đang chạy (Task 468).
	ErrRunInProgress = errors.New("promotion: lượt phát quà sinh nhật khác đang chạy")
	// ErrInvalidGiftSettings: cài đặt quà sinh nhật ngoài giới hạn (mệnh giá, đơn tối thiểu, số ngày; Task 468).
	ErrInvalidGiftSettings = errors.New("promotion: cài đặt quà sinh nhật không hợp lệ")
)

// Reason là lý do mã bị từ chối (CouponRejectReason của hợp đồng API).
type Reason string

// Lý do từ chối mã (kế hoạch M2, Task M2-06).
const (
	ReasonNotFound      Reason = "not_found"
	ReasonDisabled      Reason = "disabled"
	ReasonNotStarted    Reason = "not_started"
	ReasonExpired       Reason = "expired"
	ReasonExhausted     Reason = "exhausted"
	ReasonMinSubtotal   Reason = "min_subtotal"
	ReasonChannel       Reason = "channel"
	ReasonBoundOther    Reason = "bound_other"
	ReasonCustomerLimit Reason = "customer_limit"
	ReasonSeller        Reason = "seller"
	// ReasonZeroDiscount: mã đạt mọi luật nhưng phần giảm tính ra 0 đồng (Task 433, C-05): không áp, không tiêu lượt.
	ReasonZeroDiscount Reason = "zero_discount"
)

// RejectError là chi tiết của ErrCouponRejected.
type RejectError struct {
	Reason Reason
}

func (e *RejectError) Error() string {
	return fmt.Sprintf("promotion: mã bị từ chối (%s)", e.Reason)
}

// Is cho errors.Is(err, ErrCouponRejected).
func (e *RejectError) Is(target error) bool { return target == ErrCouponRejected }
