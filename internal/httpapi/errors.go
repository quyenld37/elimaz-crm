package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	ht "github.com/ogen-go/ogen/http"
	"github.com/ogen-go/ogen/ogenerrors"
	"github.com/ogen-go/ogen/validate"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/idempotency"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Mã lỗi chung trả cho client: mã của nền tảng (xác thực, quyền, chống trùng, giới hạn) và mã nhiều vùng cùng dùng.
// Mã chỉ một vùng trả nằm trong errors_<vùng>.go của vùng đó (Task 441). Đổi lỗi sang mã HTTP chỉ ở nhóm tệp
// errors*.go của gói này (CLAUDE.md, mục "Lỗi và log").
const (
	codeAlreadyProcessed = "already_processed"
	codeBadRequest       = "bad_request"
	codeBadOrigin        = "bad_origin"
	codeBodyTooLarge     = "body_too_large"
	codeForbidden        = "forbidden"
	codeInternal         = "internal"
	codeJobStateConflict = "job_state_conflict"
	// Khóa chống trùng (ADR-41).
	codeIdempotencyKeyInvalid  = "idempotency_key_invalid"
	codeIdempotencyKeyRequired = "idempotency_key_required"
	codeIdempotencyKeyReused   = "idempotency_key_reused"
	codeRequestInProgress      = "request_in_progress"
	codeMFARequired            = "mfa_required"
	codeNotFound               = "not_found"
	codeNotImplemented         = "not_implemented"
	codeRateLimited            = "rate_limited"
	codeRetryLater             = "retry_later"
	codeStreamUnavailable      = "stream_unavailable"
	codeUnauthenticated        = "unauthenticated"
	codeUnsupportedMedia       = "unsupported_media_type"
	// Nhiều vùng cùng dùng.
	codeVersionConflict     = "version_conflict"
	codeInvalidInput        = "invalid_input"
	codeCodeTaken           = "code_taken"
	codeAlreadyExists       = "already_exists"
	codeInvalidTransition   = "invalid_transition"
	codeSettingOutOfRange   = "setting_out_of_range"
	codeQtyMismatch         = "qty_mismatch"
	codePaymentExceedsTotal = "payment_exceeds_total"
	codeFileNotReady        = "file_not_ready"
	codeStockShort          = "stock_short" // Task 406: lấy hàng, giao bị chặn vì kiểm kho ghi thấp hơn phần giữ
)

// errForbiddenTarget: người gọi có quyền (guard đã cho qua) nhưng không ở đích của yêu cầu (kho của phiếu): 403.
var errForbiddenTarget = errors.New("httpapi: không có quyền trên đích")

type errorBody struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id"`
	// Chỉ có ở insufficient_stock (Task M1-29) và out_of_stock (Task M1-30): biến thể đầu tiên không đủ và số còn
	// làm được (có thể bán).
	VariantID *string `json:"variantId,omitempty"`
	Available *int32  `json:"available,omitempty"`
	// Chi tiết của lỗi M2 (Task M2-02), chỉ mã, không dữ liệu cá nhân: identity_taken (gốc của khách đang giữ danh
	// tính), merge_not_latest (lần gộp phải hoàn tác trước), coupon_rejected (lý do).
	OwnerID         *string `json:"ownerId,omitempty"`
	BlockingMergeID *string `json:"blockingMergeId,omitempty"`
	Reason          *string `json:"reason,omitempty"`
	// Chỉ có ở setting_out_of_range (Task M2-15): tên trường cài đặt ngoài khoảng.
	Field *string `json:"field,omitempty"`
	// Chỉ có ở statement_duplicate (Task M3-31): bảng kê COD đã nhập trước với cùng tệp.
	StatementID *string `json:"statementId,omitempty"`
}

// writeError ghi lỗi theo dạng duy nhất {"error": "<mã>", "request_id": "<id>"}; không bao giờ có vết lỗi.
func writeError(w http.ResponseWriter, r *http.Request, status int, code string) {
	writeErrorBody(w, status, errorBody{Error: code, RequestID: RequestID(r.Context())})
}

// writeErrorBody ghi thân lỗi body (mã, request_id, chi tiết tùy chọn không bao giờ là vết lỗi).
func writeErrorBody(w http.ResponseWriter, status int, body errorBody) {
	b, _ := json.Marshal(body) //nolint:errchkjson // chuỗi và số, không lỗi
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}

// writeHandlerError đổi lỗi của handler (hoặc của mã ogen khi đọc yêu cầu) sang câu trả lời. Lỗi không nhận ra là
// lỗi nội bộ: ghi log một lần ở đây (mép HTTP) rồi trả 500 không kèm chi tiết.
func writeHandlerError(d Deps, w http.ResponseWriter, r *http.Request, err error) {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		writeError(w, r, http.StatusRequestEntityTooLarge, codeBodyTooLarge)
		return
	}
	if errors.Is(err, ht.ErrNotImplemented) {
		writeError(w, r, http.StatusNotImplemented, codeNotImplemented)
		return
	}
	if _, ok := errors.AsType[*validate.InvalidContentTypeError](err); ok {
		writeError(w, r, http.StatusUnsupportedMediaType, codeUnsupportedMedia)
		return
	}
	if oe, ok := errors.AsType[ogenerrors.Error](err); ok && oe.Code() >= 400 && oe.Code() < 500 {
		writeError(w, r, oe.Code(), codeBadRequest)
		return
	}
	if writeDomainError(w, r, err) {
		return
	}
	switch {
	case errors.Is(err, idempotency.ErrInProgress), errors.Is(err, idempotency.ErrClaimLost):
		// Cùng khóa đang chạy, hoặc khóa đã bị yêu cầu khác nhận lại: bên gọi thử lại với đúng khóa đó.
		writeError(w, r, http.StatusConflict, codeRequestInProgress)
		return
	case errors.Is(err, queue.ErrJobNotFound):
		// Việc không có hoặc của tenant khác: không cho biết là có.
		writeError(w, r, http.StatusNotFound, codeNotFound)
		return
	case errors.Is(err, queue.ErrJobState):
		writeError(w, r, http.StatusConflict, codeJobStateConflict)
		return
	case errors.Is(err, idempotency.ErrKeyReused):
		writeError(w, r, http.StatusUnprocessableEntity, codeIdempotencyKeyReused)
		return
	case errors.Is(err, db.ErrRetryLater):
		// Lỗi tạm hết lượt chạy lại (ADR-46): không lưu, bên gọi gửi lại sau 1 s với cùng khóa.
		w.Header().Set("Retry-After", "1")
		writeError(w, r, http.StatusServiceUnavailable, codeRetryLater)
		return
	}
	if r.Context().Err() == nil { // client đã đóng kết nối thì không phải lỗi của máy chủ
		fp := recordError(r.Context(), d, err, nil)
		d.Logger.LogAttrs(r.Context(), slog.LevelError, "lỗi xử lý yêu cầu",
			slog.String("request_id", RequestID(r.Context())),
			slog.String("route", r.Pattern),
			slog.String("fingerprint", fp),
			logging.Err(err))
	}
	writeError(w, r, http.StatusInternalServerError, codeInternal)
}

// writeDomainError ghi lỗi nghiệp vụ của các module: thiếu hàng có thân chi tiết (variantId, available), rồi bảng đổi
// lỗi của từng module. Trả false khi err không phải lỗi nghiệp vụ nào.
func writeDomainError(w http.ResponseWriter, r *http.Request, err error) bool {
	if code, vid, avail, ok := stockShortage(err); ok {
		writeErrorBody(w, http.StatusConflict, errorBody{
			Error: code, RequestID: RequestID(r.Context()), VariantID: &vid, Available: &avail,
		})
		return true
	}
	if body, status, ok := detailedError(err); ok {
		body.RequestID = RequestID(r.Context())
		writeErrorBody(w, status, body)
		return true
	}
	for _, mapErr := range domainErrorMappers() {
		if status, code, ok := mapErr(err); ok {
			writeError(w, r, status, code)
			return true
		}
	}
	return false
}

// domainErrorMappers: hàm đổi lỗi của từng vùng (errors_<vùng>.go), theo thứ tự kiểm cố định — lỗi khớp nhiều vùng
// (errForbiddenTarget, ErrStockShort cùng giá trị) lấy kết quả của vùng đứng trước. Vùng mới thêm một hàm vào đây; lỗi
// mới của vùng đã có chỉ sửa tệp của vùng đó.
func domainErrorMappers() []func(error) (int, string, bool) {
	return []func(error) (int, string, bool){
		identityError, catalogError, filesError, orderError, customerError, promotionError, inventoryError,
		queriesError, fulfillmentError, cashError,
	}
}

// errorMapping: một dòng của bảng đổi lỗi: lỗi khớp err (errors.Is) thì trả status và mã code.
type errorMapping struct {
	err    error
	status int
	code   string
}

// matchError trả dòng đầu tiên của table mà err khớp (errors.Is, cả khi đã bọc); thứ tự dòng là thứ tự kiểm.
func matchError(err error, table []errorMapping) (status int, code string, ok bool) {
	for _, m := range table {
		if errors.Is(err, m.err) {
			return m.status, m.code, true
		}
	}
	return 0, "", false
}

// detailedError: lỗi M2 có trường chi tiết trong thân (Task M2-02; setting_out_of_range kèm field, Task M2-15).
func detailedError(err error) (errorBody, int, bool) {
	if e, ok := errors.AsType[*customer.IdentityTakenError](err); ok {
		id := e.OwnerRootID.String()
		return errorBody{Error: codeIdentityTaken, OwnerID: &id}, http.StatusConflict, true
	}
	if e, ok := errors.AsType[*customer.MergeNotLatestError](err); ok {
		id := e.BlockingMergeID.String()
		return errorBody{Error: codeMergeNotLatest, BlockingMergeID: &id}, http.StatusConflict, true
	}
	if e, ok := errors.AsType[*promotion.RejectError](err); ok {
		reason := string(e.Reason)
		return errorBody{Error: codeCouponRejected, Reason: &reason}, http.StatusUnprocessableEntity, true
	}
	if e, ok := errors.AsType[*cash.DuplicateStatementError](err); ok {
		id := e.StatementID.String()
		return errorBody{Error: codeStatementDuplicate, StatementID: &id}, http.StatusConflict, true
	}
	if e, ok := errors.AsType[*order.SettingOutOfRangeError](err); ok {
		field := string(e.Field)
		return errorBody{Error: codeSettingOutOfRange, Field: &field}, http.StatusUnprocessableEntity, true
	}
	return errorBody{}, 0, false
}

// stockShortage: hết hàng khi lên đơn (out_of_stock, Task M1-30), thiếu hàng ở phiếu kho (insufficient_stock, Task
// M1-29) hay tồn thực không đủ cho giữ hàng khi lấy hàng, giao (stock_short, Task 406), kèm biến thể đầu tiên không đủ
// và số còn làm được (stock_short: phần trên kệ còn cho dòng giữ hàng).
func stockShortage(err error) (code, variantID string, available int32, ok bool) {
	if oos, ok := errors.AsType[*order.OutOfStockError](err); ok {
		return codeOutOfStock, oos.VariantID.String(), oos.Available, true
	}
	if ie, ok := errors.AsType[*inventory.InsufficientError](err); ok {
		return codeInsufficientStock, ie.VariantID.String(), ie.Available, true
	}
	if se, ok := errors.AsType[*inventory.StockShortError](err); ok {
		return codeStockShort, se.VariantID.String(), se.OnShelf, true
	}
	return "", "", 0, false
}

// writeAuthError đổi lỗi của bước xác thực (kiểm thẻ, nạp nhân viên) sang câu trả lời: thẻ sai 401 unauthenticated,
// thiếu bước hai 401 mfa_required, nhân viên lạ hoặc bị khóa 403 forbidden (không cho biết email có tồn tại), lỗi
// tạm của database 503 retry_later. Lỗi khác: log một lần (không có email, không có thẻ) rồi 500.
func writeAuthError(d Deps, w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrMFARequired):
		writeError(w, r, http.StatusUnauthorized, codeMFARequired)
	case errors.Is(err, identity.ErrInvalidToken):
		writeError(w, r, http.StatusUnauthorized, codeUnauthenticated)
	case errors.Is(err, identity.ErrUnknownUser), errors.Is(err, identity.ErrLocked):
		writeError(w, r, http.StatusForbidden, codeForbidden)
	case errors.Is(err, db.ErrRetryLater):
		writeError(w, r, http.StatusServiceUnavailable, codeRetryLater)
	default:
		writeHandlerError(d, w, r, err)
	}
}
