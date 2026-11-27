package httpapi

// Bảng đổi lỗi của vùng fulfillment sang mã HTTP (Task 441: mỗi vùng một tệp errors_<vùng>.go; thêm lỗi cho vùng chỉ sửa tệp
// này và x-error-codes của openapi.yaml khi có mã mới; thứ tự kiểm giữa các vùng ở domainErrorMappers, errors.go).

import (
	"net/http"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// Mã lỗi chỉ vùng fulfillment trả.
const (
	codeShipmentExists            = "shipment_exists"
	codeCapabilityNotSupported    = "capability_not_supported"
	codeCarrierAccountUnavailable = "carrier_account_unavailable"
	codeOrderNotShippable         = "order_not_shippable"
	codeQuoteStale                = "quote_stale"
	codeAddressUnmapped           = "address_unmapped"
	codeCreateInProgress          = "create_in_progress"
	codeShipmentNotCancellable    = "shipment_not_cancellable"
	codeTrackingCodeConflict      = "tracking_code_conflict"
	codeCarrierNotOnWeb           = "carrier_not_on_web"
	codeLastWebCarrier            = "last_web_carrier"
	codeCorrectionBlocked         = "correction_blocked"
)

// fulfillmentError đổi lỗi nghiệp vụ của vận chuyển (và thiếu quyền ở kho của tài khoản hãng) sang mã HTTP.
// ErrOrderNotShippable (đơn không có, của người bán khác, chưa ở confirmed), ErrAccountNotFound, ErrNotFound: 404
// (không cho biết là có).
func fulfillmentError(err error) (status int, code string, ok bool) {
	return matchError(err, []errorMapping{
		{errForbiddenTarget, http.StatusForbidden, codeForbidden},
		{fulfillment.ErrShipmentExists, http.StatusConflict, codeShipmentExists},
		{fulfillment.ErrOrderNotShippable, http.StatusConflict, codeOrderNotShippable},
		{fulfillment.ErrOrderNotFound, http.StatusNotFound, codeNotFound},
		{fulfillment.ErrAccountNotFound, http.StatusNotFound, codeNotFound},
		{fulfillment.ErrNotFound, http.StatusNotFound, codeNotFound},
		// Cài đặt vận chuyển (M3, Task M3-04).
		{fulfillment.ErrSettingOutOfRange, http.StatusUnprocessableEntity, codeSettingOutOfRange},
		{fulfillment.ErrVersionConflict, http.StatusConflict, codeVersionConflict},
		// Tài khoản hãng (M3, Task M3-09; tệp task 100, Quyết định 6: chỉ mã có sẵn trong hợp đồng).
		{fulfillment.ErrCarrierNotSupported, http.StatusUnprocessableEntity, codeCapabilityNotSupported},
		{fulfillment.ErrAccountUnavailable, http.StatusConflict, codeCarrierAccountUnavailable},
		{fulfillment.ErrAccountTransition, http.StatusConflict, codeInvalidTransition},
		{fulfillment.ErrAccountTaken, http.StatusConflict, codeAlreadyExists},
		{fulfillment.ErrAccountInvalid, http.StatusBadRequest, codeInvalidInput},
		{fulfillment.ErrAccountTargetNotFound, http.StatusNotFound, codeNotFound},
		// Áp kết quả tra, lịch sử vận đơn (M3, Task M3-08).
		{fulfillment.ErrInvalidPage, http.StatusBadRequest, codeInvalidInput},
		{fulfillment.ErrInvalidTracking, http.StatusBadRequest, codeInvalidInput},
		{fulfillment.ErrStockShort, http.StatusConflict, codeStockShort},
		// Gửi hãng (M3, Task M3-12).
		{fulfillment.ErrQuoteStale, http.StatusConflict, codeQuoteStale},
		{fulfillment.ErrAddressUnmapped, http.StatusUnprocessableEntity, codeAddressUnmapped},
		{fulfillment.ErrCreateInProgress, http.StatusConflict, codeCreateInProgress},
		{fulfillment.ErrShipInvalid, http.StatusBadRequest, codeInvalidInput},
		{fulfillment.ErrTrackingCodeConflict, http.StatusConflict, codeTrackingCodeConflict},
		// Hủy vận đơn, đổi hãng (M3, Task M3-14).
		{fulfillment.ErrShipmentNotCancellable, http.StatusConflict, codeShipmentNotCancellable},
		// Giao một phần (M3, Task M3-26; tệp task 117, Quyết định 3: chỉ mã có sẵn trong hợp đồng).
		{fulfillment.ErrPartialNotAllowed, http.StatusConflict, codeInvalidTransition},
		{fulfillment.ErrPartialQty, http.StatusUnprocessableEntity, codeQtyMismatch},
		{fulfillment.ErrPartialCOD, http.StatusUnprocessableEntity, codePaymentExceedsTotal},
		// Nhập mã tay, cập nhật tay trạng thái vận đơn (M3, Task M3-15).
		{fulfillment.ErrManualReasonRequired, http.StatusUnprocessableEntity, codeInvalidInput},
		{fulfillment.ErrManualNotAllowed, http.StatusConflict, codeInvalidTransition},
		// Đảo "Thành công" (M3, Task M3-27): token khác → preview_stale; tiền đã về shop → correction_blocked.
		{fulfillment.ErrCorrectionInvalid, http.StatusUnprocessableEntity, codeInvalidInput},
		{fulfillment.ErrCorrectionNotAllowed, http.StatusConflict, codeInvalidTransition},
		{fulfillment.ErrCorrectionBlocked, http.StatusConflict, codeCorrectionBlocked},
		{fulfillment.ErrCorrectionStale, http.StatusConflict, codePreviewStale},
		// In nhãn (M3, Task M3-16).
		{fulfillment.ErrLabelBatchInvalid, http.StatusBadRequest, codeInvalidInput},
		// Đổi địa chỉ sau khi gửi hãng (M3, Task M3-17; chỉ mã có sẵn trong hợp đồng).
		{fulfillment.ErrAddressChangeOpen, http.StatusConflict, codeAlreadyExists},
		{fulfillment.ErrAddressChangeNotAllowed, http.StatusConflict, codeInvalidTransition},
		// Cần xem (M3, Task M3-20): việc không có / ngoài phạm vi → 404; đã đóng, in_doubt_create → 409.
		{fulfillment.ErrAnomalyNotFound, http.StatusNotFound, codeNotFound},
		{fulfillment.ErrAnomalyNotResolvable, http.StatusConflict, codeInvalidTransition},
		// Việc gọi lại (M3, Task M3-19; tệp task 110, Quyết định 4: chỉ mã có sẵn trong hợp đồng).
		{fulfillment.ErrCallbackNotFound, http.StatusNotFound, codeNotFound},
		{fulfillment.ErrCallbackClosed, http.StatusConflict, codeInvalidTransition},
		{fulfillment.ErrCallbackAssignee, http.StatusUnprocessableEntity, codeInvalidInput},
		// Hãng giao hàng trên website (M5, Task 198): không hãng nào báo phí được, không phí dự phòng → 503 retry_later.
		{fulfillment.ErrCarrierNotOnWeb, http.StatusUnprocessableEntity, codeCarrierNotOnWeb},
		{fulfillment.ErrLastWebCarrier, http.StatusUnprocessableEntity, codeLastWebCarrier},
		{fulfillment.ErrNoQuote, http.StatusServiceUnavailable, codeRetryLater},
	})
}
