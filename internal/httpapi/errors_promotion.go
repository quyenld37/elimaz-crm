package httpapi

// Bảng đổi lỗi của vùng promotion sang mã HTTP (Task 441: mỗi vùng một tệp errors_<vùng>.go; thêm lỗi cho vùng chỉ sửa tệp
// này và x-error-codes của openapi.yaml khi có mã mới; thứ tự kiểm giữa các vùng ở domainErrorMappers, errors.go).

import (
	"net/http"

	"elimaz.com.vn/crm/internal/modules/promotion"
)

// Mã lỗi chỉ vùng promotion trả.
const (
	codeCouponRejected       = "coupon_rejected"
	codeCouponLockedAfterUse = "coupon_locked_after_use"
	codeMaxUsesBelowUsed     = "max_uses_below_used"
	codePreviewRequired      = "preview_required"
	codePercentTooHigh       = "percent_too_high"
	codeCampaignRunning      = "campaign_running"
	codeBatchConflict        = "batch_conflict"
)

// promotionError đổi lỗi nghiệp vụ của mã giảm giá (M2, Task M2-02, M2-14) và chiến dịch giảm giá web (M5, Task 452)
// sang mã HTTP. RejectError có thân riêng (kèm
// reason) ở detailedError.
func promotionError(err error) (status int, code string, ok bool) {
	return matchError(err, []errorMapping{
		{promotion.ErrCouponRejected, http.StatusUnprocessableEntity, codeCouponRejected},
		{promotion.ErrCouponLockedAfterUse, http.StatusConflict, codeCouponLockedAfterUse},
		{promotion.ErrMaxUsesBelowUsed, http.StatusUnprocessableEntity, codeMaxUsesBelowUsed},
		{promotion.ErrCodeTaken, http.StatusConflict, codeCodeTaken},
		{promotion.ErrNotFound, http.StatusNotFound, codeNotFound},
		{promotion.ErrVersionConflict, http.StatusConflict, codeVersionConflict},
		{promotion.ErrInvalidInput, http.StatusBadRequest, codeInvalidInput},
		{promotion.ErrPreviewRequired, http.StatusUnprocessableEntity, codePreviewRequired},
		{promotion.ErrPercentTooHigh, http.StatusUnprocessableEntity, codePercentTooHigh},
		{promotion.ErrCampaignRunning, http.StatusConflict, codeCampaignRunning},
		{promotion.ErrBatchConflict, http.StatusUnprocessableEntity, codeBatchConflict},
	})
}
