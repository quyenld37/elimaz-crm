package httpapi

// Bảng đổi lỗi của vùng đơn website sang mã HTTP (Task 194; luật Task 441: thêm lỗi cho vùng chỉ sửa tệp này,
// x-error-codes của openapi.yaml và errors_snapshot_test.go).

import (
	"errors"
	"net/http"
	"time"

	"elimaz.com.vn/crm/internal/httpapi/web"
	"elimaz.com.vn/crm/internal/modules/order"
)

// Mã lỗi chỉ vùng đơn website trả.
const (
	codeQuoteChanged    = "quote_changed"
	codeItemUnavailable = "item_unavailable"
	codeCouponInvalid   = "coupon_invalid"
	codeTooFast         = "too_fast"
	codeWebOrdersPaused = "web_orders_paused"
)

// webError đổi lỗi nghiệp vụ của vùng đơn website (không có thân chi tiết) sang mã HTTP (Task 456): shop đang tạm dừng
// nhận đơn web → 503 web_orders_paused (không Retry-After: thời gian mở lại không biết trước); tạm dừng không lý do →
// 422 invalid_input.
func webError(err error) (status int, code string, ok bool) {
	return matchError(err, []errorMapping{
		{order.ErrWebIntakePaused, http.StatusServiceUnavailable, codeWebOrdersPaused},
		{order.ErrReasonRequired, http.StatusUnprocessableEntity, codeInvalidInput},
	})
}

// tooFastError: SĐT người đặt đã quá ngưỡng đơn web (Deps.WebOrderLimiter); retryAfter là thời gian chờ.
type tooFastError struct {
	retryAfter time.Duration
}

func (e *tooFastError) Error() string { return "httpapi: quá nhiều đơn web của một khách" }

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
// ba lỗi của webOrderDetail, 409 out_of_stock (thân của M1: variantId, available), 422 mixed_sellers. Lỗi khác: false.
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
	return errorBody{}, 0, false
}

// writeTooFast ghi 429 too_fast kèm Retry-After khi err là tooFastError (không lưu theo khóa).
func writeTooFast(w http.ResponseWriter, r *http.Request, err error) bool {
	e, ok := errors.AsType[*tooFastError](err)
	if !ok {
		return false
	}
	w.Header().Set("Retry-After", retryAfterSeconds(e.retryAfter))
	writeError(w, r, http.StatusTooManyRequests, codeTooFast)
	return true
}
