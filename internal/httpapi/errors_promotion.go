package httpapi

// Bảng đổi lỗi của vùng promotion sang mã HTTP (Task 441: mỗi vùng một tệp errors_<vùng>.go; thêm lỗi cho vùng chỉ sửa tệp
// này và x-error-codes của openapi.yaml khi có mã mới; thứ tự kiểm giữa các vùng ở domainErrorMappers, errors.go).

import (
	"errors"
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
	codeRunInProgress        = "another_run_in_progress"
	codeInvalidGiftSettings  = "invalid_gift_settings"
	codeNotEditable          = "not_editable"
	codeNotADiscount         = "not_a_discount"
	codeInvalidFlashWindow   = "invalid_flash_window"
	codeFlashOverlap         = "flash_overlap"
	codeFlashSaleEmpty       = "flash_sale_empty"
	codeFlashNotActive       = "flash_not_active"
	codeFlashEnded           = "flash_ended"
)

// promotionError đổi lỗi nghiệp vụ của mã giảm giá (M2, Task M2-02, M2-14) và chiến dịch giảm giá web (M5, Task 452), quà sinh nhật (Task 468), đợt flash sale (Task 199, 200)
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
		{promotion.ErrRunInProgress, http.StatusConflict, codeRunInProgress},
		{promotion.ErrInvalidGiftSettings, http.StatusUnprocessableEntity, codeInvalidGiftSettings},
		{promotion.ErrNotEditable, http.StatusUnprocessableEntity, codeNotEditable},
		{promotion.ErrNotADiscount, http.StatusUnprocessableEntity, codeNotADiscount},
		{promotion.ErrFlashItemExists, http.StatusUnprocessableEntity, codeAlreadyExists},
		{promotion.ErrFlashWindow, http.StatusUnprocessableEntity, codeInvalidFlashWindow},
		{promotion.ErrVariantNotForSale, http.StatusUnprocessableEntity, codeVariantNotForSale},
		{promotion.ErrFlashOverlap, http.StatusUnprocessableEntity, codeFlashOverlap},
		{promotion.ErrFlashEmpty, http.StatusUnprocessableEntity, codeFlashSaleEmpty},
		{promotion.ErrFlashNotActive, http.StatusConflict, codeFlashNotActive},
		{promotion.ErrFlashEnded, http.StatusUnprocessableEntity, codeFlashEnded},
		{promotion.ErrFlashInsufficient, http.StatusConflict, codeInsufficientStock},
	})
}

// flashShortageDetail: thiếu hàng khi lên lịch, thêm suất đợt flash sale (Task 200) → 409 insufficient_stock kèm mọi
// món thiếu.
func flashShortageDetail(err error) (errorBody, int, bool) {
	e, ok := errors.AsType[*promotion.FlashShortageError](err)
	if !ok {
		return errorBody{}, 0, false
	}
	items := make([]flashShortageBody, 0, len(e.Items))
	for _, it := range e.Items {
		items = append(items, flashShortageBody{
			VariantID: it.VariantID.String(), SKU: it.SKU, Need: it.Need, Sellable: it.Sellable,
		})
	}
	return errorBody{Error: codeInsufficientStock, Shortages: items}, http.StatusConflict, true
}

// flashShortageBody là FlashShortage của hợp đồng.
type flashShortageBody struct {
	VariantID string `json:"variantId"`
	SKU       string `json:"sku"`
	Need      int    `json:"need"`
	Sellable  int    `json:"sellable"`
}
