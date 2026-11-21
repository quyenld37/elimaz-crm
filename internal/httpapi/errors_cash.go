package httpapi

// Bảng đổi lỗi của vùng cash sang mã HTTP (Task 441: mỗi vùng một tệp errors_<vùng>.go; thêm lỗi cho vùng chỉ sửa tệp
// này và x-error-codes của openapi.yaml khi có mã mới; thứ tự kiểm giữa các vùng ở domainErrorMappers, errors.go).

import (
	"net/http"

	"elimaz.com.vn/crm/internal/modules/cash"
)

// Mã lỗi chỉ vùng cash trả.
const (
	codeStatementDuplicate  = "statement_duplicate"
	codeStatementChanged    = "statement_changed"
	codeCODLockedByShipment = "cod_locked_by_shipment"
	codeRefundExceeds       = "refund_exceeds_remaining"
	codePreviewStale        = "preview_stale"
)

// cashError đổi lỗi nghiệp vụ của tiền, bảng kê COD (M3, Task M3-31) sang mã HTTP, rồi lỗi phiếu thu, nghĩa vụ hoàn
// (paymentsError). Tài khoản hãng, bảng kê không có hay của tenant khác: 404 (không cho biết là có).
// DuplicateStatementError có thân riêng ở detailedError.
func cashError(err error) (status int, code string, ok bool) {
	if s, c, ok := matchError(err, []errorMapping{
		{cash.ErrCarrierAccountNotFound, http.StatusNotFound, codeNotFound},
		{cash.ErrStatementNotFound, http.StatusNotFound, codeNotFound},
		{cash.ErrStatementChanged, http.StatusConflict, codeStatementChanged},
		{cash.ErrShipmentNotFound, http.StatusNotFound, codeNotFound},
		{cash.ErrUnknownStatementFormat, http.StatusUnprocessableEntity, codeInvalidInput},
		{cash.ErrStatementFileNotReady, http.StatusConflict, codeFileNotReady},
		{cash.ErrInvalidInput, http.StatusBadRequest, codeInvalidInput},
		// Điều chỉnh COD (Task M3-33): ErrPreviewStale đứng trước ErrAdjustmentBlocked, ErrAdjustTargetNotFound (lúc ghi
		// hai lỗi đó bọc thêm ErrPreviewStale).
		{cash.ErrPreviewStale, http.StatusConflict, codePreviewStale},
		{cash.ErrAdjustReasonRequired, http.StatusUnprocessableEntity, codeInvalidInput},
		{cash.ErrAdjustmentInvalid, http.StatusUnprocessableEntity, codeInvalidInput},
		{cash.ErrAdjustTargetNotFound, http.StatusNotFound, codeNotFound},
		{cash.ErrAdjustmentBlocked, http.StatusConflict, codeInvalidTransition},
		{cash.ErrAlreadyReversed, http.StatusConflict, codeInvalidTransition},
		{cash.ErrReconcileRunNotFound, http.StatusNotFound, codeNotFound},
	}); ok {
		return s, c, true
	}
	return paymentsError(err)
}

// paymentsError đổi lỗi của phiếu thu, nghĩa vụ hoàn (M3, Task M3-30) sang mã HTTP; cashError gọi khi không khớp lỗi
// bảng kê. Đơn, nghĩa vụ không có hay của tenant khác: 404.
func paymentsError(err error) (status int, code string, ok bool) {
	return matchError(err, []errorMapping{
		{cash.ErrOrderNotFound, http.StatusNotFound, codeNotFound},
		{cash.ErrObligationNotFound, http.StatusNotFound, codeNotFound},
		{cash.ErrCODLockedByShipment, http.StatusConflict, codeCODLockedByShipment},
		{cash.ErrPaymentNotAllowed, http.StatusConflict, codeInvalidTransition},
		{cash.ErrObligationClosed, http.StatusConflict, codeInvalidTransition},
		{cash.ErrPaymentExceedsTotal, http.StatusConflict, codePaymentExceedsTotal},
		{cash.ErrRefundExceedsRemaining, http.StatusConflict, codeRefundExceeds},
		{cash.ErrReasonRequired, http.StatusUnprocessableEntity, codeInvalidInput},
	})
}
