package httpapi

// Bảng đổi lỗi của vùng đơn website sang mã HTTP (Task 194; luật Task 441: thêm lỗi cho vùng chỉ sửa tệp này,
// x-error-codes của openapi.yaml và errors_snapshot_test.go).

import (
	"errors"
	"net/http"
	"time"

	"elimaz.com.vn/crm/internal/httpapi/web"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/queries/weborders"
)

// Mã lỗi chỉ vùng đơn website trả.
const (
	codeQuoteChanged    = "quote_changed"
	codeItemUnavailable = "item_unavailable"
	codeCouponInvalid   = "coupon_invalid"
	codeTooFast         = "too_fast"
	// Quá ngưỡng nhập mã giảm giá sai (Task 482).
	codeCouponAttemptsExceeded = "coupon_attempts_exceeded"
	codeWebOrdersPaused        = "web_orders_paused"
	// Yêu cầu về đơn hàng (Task 195).
	codeWindowClosed      = "window_closed"
	codeQtyExceeded       = "qty_exceeded"
	codePhotoRequired     = "photo_required"
	codeTooManyFiles      = "too_many_files"
	codeDuplicateOpen     = "duplicate_open"
	codeFileNotFound      = "file_not_found"
	codeRequestNotAllowed = "request_not_allowed"
	// Khách hủy đơn web (Task 196).
	codeCannotCancel = "cannot_cancel"
	// Hãng/dịch vụ khách chọn trên website (Task 490).
	codeShippingCarrierUnavailable = "shipping_carrier_unavailable"
	codeShippingFeeMismatch        = "shipping_fee_mismatch"
)

// webError đổi lỗi nghiệp vụ của vùng đơn website (không có thân chi tiết) sang mã HTTP (Task 456): shop đang tạm dừng
// nhận đơn web → 503 web_orders_paused (không Retry-After: thời gian mở lại không biết trước); tạm dừng không lý do →
// 422 invalid_input. Yêu cầu về đơn hàng (Task 195): luật của yêu cầu → 422 với mã riêng; quá 5 yêu cầu một đơn trong
// 24 giờ → 429 too_fast (không Retry-After); yêu cầu không có → 404. Khách hủy đơn đã giao, đã hoàn, đã hủy (Task 196)
// → 422 cannot_cancel; limit của luồng thay đổi sai → 400 bad_request (ogen đã chặn theo hợp đồng). Lựa chọn giao
// hàng của khách (Task 490): hãng không còn trên web → 422 shipping_carrier_unavailable, phí lệch báo phí → 422
// shipping_fee_mismatch, chưa kiểm được (hãng lỗi, bộ đệm bận) → 503 retry_later.
func webError(err error) (status int, code string, ok bool) {
	return matchError(err, []errorMapping{
		{order.ErrWebIntakePaused, http.StatusServiceUnavailable, codeWebOrdersPaused},
		{order.ErrShipCarrierUnavailable, http.StatusUnprocessableEntity, codeShippingCarrierUnavailable},
		{order.ErrShipFeeMismatch, http.StatusUnprocessableEntity, codeShippingFeeMismatch},
		{order.ErrShipUnverified, http.StatusServiceUnavailable, codeRetryLater},
		// Lượt mua flash sale (Task 201): đơn vị suất đang bị lượt khác khóa → thử lại, không lưu.
		{order.ErrFlashRetry, http.StatusServiceUnavailable, codeRetryLater},
		{order.ErrReasonRequired, http.StatusUnprocessableEntity, codeInvalidInput},
		{order.ErrWindowClosed, http.StatusUnprocessableEntity, codeWindowClosed},
		{order.ErrQtyExceeded, http.StatusUnprocessableEntity, codeQtyExceeded},
		{order.ErrPhotoRequired, http.StatusUnprocessableEntity, codePhotoRequired},
		{order.ErrTooManyFiles, http.StatusUnprocessableEntity, codeTooManyFiles},
		{order.ErrDuplicateOpen, http.StatusUnprocessableEntity, codeDuplicateOpen},
		{order.ErrFileRejected, http.StatusUnprocessableEntity, codeFileRejected},
		{order.ErrFileNotFound, http.StatusUnprocessableEntity, codeFileNotFound},
		{order.ErrRequestNotAllowed, http.StatusUnprocessableEntity, codeRequestNotAllowed},
		{order.ErrRequestNotFound, http.StatusNotFound, codeNotFound},
		{order.ErrRequestLimit, http.StatusTooManyRequests, codeTooFast},
		{order.ErrCannotCancel, http.StatusUnprocessableEntity, codeCannotCancel},
		{weborders.ErrBadLimit, http.StatusBadRequest, codeBadRequest},
		{weborders.ErrNotFound, http.StatusNotFound, codeNotFound},
	})
}

// tooFastError: SĐT người đặt đã quá ngưỡng đơn web (Deps.WebOrderLimiter; code rỗng → too_fast), hoặc khách đã quá
// ngưỡng nhập mã sai (Deps.CouponGuard; code coupon_attempts_exceeded, Task 482); retryAfter là thời gian chờ.
type tooFastError struct {
	retryAfter time.Duration
	code       string
}

func (e *tooFastError) Error() string {
	if e.code == codeCouponAttemptsExceeded {
		return "httpapi: quá nhiều lần nhập mã giảm giá sai"
	}
	return "httpapi: quá nhiều đơn web của một khách"
}

// webOrderDetail đổi ba kiểu lỗi chi tiết của order.CreateFromWeb sang thân lỗi (chỉ số tiền, mã biến thể, lý do —
// không dữ liệu cá nhân): báo giá đổi (kèm báo giá CRM), món không bán được (kèm variantIds), mã không dùng được (kèm
// reason). Đều 422.
func webOrderDetail(err error) (errorBody, int, bool) {
	if e, ok := errors.AsType[*order.QuoteChangedError](err); ok {
		raw, qerr := web.QuoteJSON(e.Quote)
		if qerr != nil {
			return errorBody{}, 0, false // không mã hóa được: lỗi nội bộ (500), không giả câu trả lời
		}
		return errorBody{Error: codeQuoteChanged, Quote: raw}, http.StatusUnprocessableEntity, true
	}
	if e, ok := errors.AsType[*order.ItemUnavailableError](err); ok {
		ids := make([]string, len(e.VariantIDs))
		for i, id := range e.VariantIDs {
			ids[i] = id.String()
		}
		return errorBody{Error: codeItemUnavailable, VariantIDs: ids}, http.StatusUnprocessableEntity, true
	}
	if e, ok := errors.AsType[*order.CouponInvalidError](err); ok {
		reason := e.Reason
		return errorBody{Error: codeCouponInvalid, Reason: &reason}, http.StatusUnprocessableEntity, true
	}
	return errorBody{}, 0, false
}

// webOrderRefusal: từ chối dứt khoát của tạo đơn web — kết quả cuối, được lưu theo khóa chống trùng (kế hoạch M5-11):
// ba lỗi của webOrderDetail, 409 out_of_stock (thân của M1: variantId, available), 422 mixed_sellers, 422
// shipping_carrier_unavailable, shipping_fee_mismatch (Task 490; như quote_changed). Lỗi khác (kể cả chưa kiểm được
// lựa chọn, 503): false.
func webOrderRefusal(err error) (errorBody, int, bool) {
	if body, status, ok := webOrderDetail(err); ok {
		return body, status, true
	}
	if oos, ok := errors.AsType[*order.OutOfStockError](err); ok {
		vid, avail := oos.VariantID.String(), oos.Available
		return errorBody{Error: codeOutOfStock, VariantID: &vid, Available: &avail}, http.StatusConflict, true
	}
	if errors.Is(err, order.ErrMixedSellers) {
		return errorBody{Error: codeMixedSellers}, http.StatusUnprocessableEntity, true
	}
	if errors.Is(err, order.ErrShipCarrierUnavailable) {
		return errorBody{Error: codeShippingCarrierUnavailable}, http.StatusUnprocessableEntity, true
	}
	if errors.Is(err, order.ErrShipFeeMismatch) {
		return errorBody{Error: codeShippingFeeMismatch}, http.StatusUnprocessableEntity, true
	}
	return errorBody{}, 0, false
}

// writeTooFast ghi 429 too_fast (hay mã riêng của tooFastError) kèm Retry-After khi err là tooFastError (không lưu theo
// khóa).
func writeTooFast(w http.ResponseWriter, r *http.Request, err error) bool {
	e, ok := errors.AsType[*tooFastError](err)
	if !ok {
		return false
	}
	code := e.code
	if code == "" {
		code = codeTooFast
	}
	w.Header().Set("Retry-After", retryAfterSeconds(e.retryAfter))
	writeError(w, r, http.StatusTooManyRequests, code)
	return true
}
