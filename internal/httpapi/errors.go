package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	ht "github.com/ogen-go/ogen/http"
	"github.com/ogen-go/ogen/ogenerrors"
	"github.com/ogen-go/ogen/validate"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/idempotency"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Mã lỗi trả cho client. Đây là chỗ duy nhất đổi lỗi sang mã HTTP (CLAUDE.md, mục "Lỗi và log").
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
	// Quản lý nhân viên (PQ07, kế hoạch M1 Task M1-26).
	codeCannotModifySelf  = "cannot_modify_self"
	codeGrantExceedsActor = "grant_exceeds_actor"
	codeLastOwner         = "last_owner"
	codeEmailTaken        = "email_taken"
	codeVersionConflict   = "version_conflict"
	codeInvalidInput      = "invalid_input"
	// Catalog (kế hoạch M1 Task M1-27).
	codeSKUTaken        = "sku_taken"
	codeBarcodeTaken    = "barcode_taken"
	codeDuplicateOption = "duplicate_option"
	codeCodeTaken       = "code_taken"
	codeNotForSale      = "not_for_sale"
	codeInvalidPrice    = "invalid_price"
	codeInvalidArticle  = "invalid_article"
	// Phân loại (kế hoạch M1 Task M1-34).
	codeCategoryCycle   = "category_cycle"
	codeCategoryTooDeep = "category_too_deep"
	codeCategoryInUse   = "category_in_use"
	codeAlreadyExists   = "already_exists"

	// Đơn (kế hoạch M1 Task M1-30).
	codeOutOfStock        = "out_of_stock"
	codeVariantNotForSale = "variant_not_for_sale"
	codeMixedSellers      = "mixed_sellers"
	codeTotalOverflow     = "total_overflow"
	codeShipmentExists    = "shipment_exists"

	// Kho tệp (kế hoạch M1 Task M1-41).
	codeFileTooLarge          = "file_too_large"
	codeFileTypeNotAllowed    = "file_type_not_allowed"
	codeFilePurposeNotAllowed = "file_purpose_not_allowed"
	codeUploadMissing         = "upload_missing"

	// Kho (kế hoạch M1 Task M1-29).
	codeInsufficientStock  = "insufficient_stock"
	codeDocumentNotDraft   = "document_not_draft"
	codeStocktakeOpen      = "stocktake_open"
	codeStocktakeNotOpen   = "stocktake_not_open"
	codeSupplierNameTaken  = "supplier_name_taken"
	codeSupplierArchived   = "supplier_archived"
	codeSupplierNotForKind = "supplier_not_for_kind"
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
	for _, mapErr := range []func(error) (int, string, bool){
		identityError, catalogError, filesError, orderError, inventoryError, queriesError, fulfillmentError,
	} {
		if status, code, ok := mapErr(err); ok {
			writeError(w, r, status, code)
			return true
		}
	}
	return false
}

// stockShortage: hết hàng khi lên đơn (out_of_stock, Task M1-30) hay thiếu hàng ở phiếu kho (insufficient_stock, Task
// M1-29), kèm biến thể đầu tiên không đủ và số còn làm được.
func stockShortage(err error) (code, variantID string, available int32, ok bool) {
	if oos, ok := errors.AsType[*order.OutOfStockError](err); ok {
		return codeOutOfStock, oos.VariantID.String(), oos.Available, true
	}
	if ie, ok := errors.AsType[*inventory.InsufficientError](err); ok {
		return codeInsufficientStock, ie.VariantID.String(), ie.Available, true
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

// identityError đổi lỗi nghiệp vụ của quản lý nhân viên (PQ07) sang mã HTTP. ErrLocked ở đây là người thao tác vừa bị
// khóa giữa lúc xác thực và lúc ghi: như bước xác thực, 403 forbidden. ErrStaffNotFound: không có hoặc của tenant
// khác, không cho biết là có.
func identityError(err error) (status int, code string, ok bool) {
	for _, m := range []struct {
		err    error
		status int
		code   string
	}{
		{identity.ErrSelfModify, http.StatusForbidden, codeCannotModifySelf},
		{identity.ErrGrantExceedsActor, http.StatusForbidden, codeGrantExceedsActor},
		{identity.ErrLocked, http.StatusForbidden, codeForbidden},
		{identity.ErrLastOwner, http.StatusConflict, codeLastOwner},
		{identity.ErrEmailTaken, http.StatusConflict, codeEmailTaken},
		{identity.ErrVersionConflict, http.StatusConflict, codeVersionConflict},
		{identity.ErrStaffNotFound, http.StatusNotFound, codeNotFound},
		{identity.ErrInvalidInput, http.StatusBadRequest, codeInvalidInput},
	} {
		if errors.Is(err, m.err) {
			return m.status, m.code, true
		}
	}
	return 0, "", false
}

// catalogError đổi lỗi nghiệp vụ của catalog sang mã HTTP. ErrNotFound: không có, của người bán hay tenant khác (không
// cho biết là có).
func catalogError(err error) (status int, code string, ok bool) {
	for _, m := range []struct {
		err    error
		status int
		code   string
	}{
		{catalog.ErrNotFound, http.StatusNotFound, codeNotFound},
		{catalog.ErrSKUTaken, http.StatusConflict, codeSKUTaken},
		{catalog.ErrBarcodeTaken, http.StatusConflict, codeBarcodeTaken},
		{catalog.ErrDuplicateOption, http.StatusConflict, codeDuplicateOption},
		{catalog.ErrCodeTaken, http.StatusConflict, codeCodeTaken},
		{catalog.ErrVersionConflict, http.StatusConflict, codeVersionConflict},
		{catalog.ErrNotForSale, http.StatusConflict, codeNotForSale},
		{catalog.ErrInvalidPrice, http.StatusUnprocessableEntity, codeInvalidPrice},
		{catalog.ErrInvalidArticle, http.StatusUnprocessableEntity, codeInvalidArticle},
		{catalog.ErrCategoryCycle, http.StatusUnprocessableEntity, codeCategoryCycle},
		{catalog.ErrCategoryTooDeep, http.StatusUnprocessableEntity, codeCategoryTooDeep},
		{catalog.ErrCategoryInUse, http.StatusConflict, codeCategoryInUse},
		{catalog.ErrAlreadyExists, http.StatusConflict, codeAlreadyExists},
		{catalog.ErrInvalidInput, http.StatusBadRequest, codeInvalidInput},
	} {
		if errors.Is(err, m.err) {
			return m.status, m.code, true
		}
	}
	return 0, "", false
}

// filesError đổi lỗi nghiệp vụ của kho tệp sang mã HTTP. ErrNotFound: không có, của tenant khác hay đã xóa, không cho
// biết là có (PQ08).
func filesError(err error) (status int, code string, ok bool) {
	for _, m := range []struct {
		err    error
		status int
		code   string
	}{
		{files.ErrTooLarge, http.StatusUnprocessableEntity, codeFileTooLarge},
		{files.ErrTypeNotAllowed, http.StatusUnprocessableEntity, codeFileTypeNotAllowed},
		{files.ErrUnknownPurpose, http.StatusUnprocessableEntity, codeFilePurposeNotAllowed},
		{files.ErrUploadNotAllowed, http.StatusUnprocessableEntity, codeFilePurposeNotAllowed},
		{files.ErrInvalidFile, http.StatusBadRequest, codeInvalidInput},
		{files.ErrUploadMissing, http.StatusConflict, codeUploadMissing},
		{files.ErrNotFound, http.StatusNotFound, codeNotFound},
	} {
		if errors.Is(err, m.err) {
			return m.status, m.code, true
		}
	}
	return 0, "", false
}

// inventoryError đổi lỗi nghiệp vụ của kho (và thiếu quyền ở kho của yêu cầu) sang mã HTTP. ErrNotFound: phiếu, nhà
// cung cấp, kho không có hoặc của tenant khác (không cho biết là có). InsufficientError có thân riêng ở
// writeHandlerError (kèm variantId, available).
func inventoryError(err error) (status int, code string, ok bool) {
	for _, m := range []struct {
		err    error
		status int
		code   string
	}{
		{errForbiddenTarget, http.StatusForbidden, codeForbidden},
		{inventory.ErrInsufficient, http.StatusConflict, codeInsufficientStock},
		{inventory.ErrNotFound, http.StatusNotFound, codeNotFound},
		{inventory.ErrVersionConflict, http.StatusConflict, codeVersionConflict},
		{inventory.ErrDocumentNotDraft, http.StatusConflict, codeDocumentNotDraft},
		{inventory.ErrStocktakeOpen, http.StatusConflict, codeStocktakeOpen},
		{inventory.ErrStocktakeNotOpen, http.StatusConflict, codeStocktakeNotOpen},
		{inventory.ErrSupplierNameTaken, http.StatusConflict, codeSupplierNameTaken},
		{inventory.ErrSupplierArchived, http.StatusUnprocessableEntity, codeSupplierArchived},
		{inventory.ErrSupplierNotForKind, http.StatusUnprocessableEntity, codeSupplierNotForKind},
		{inventory.ErrInvalidInput, http.StatusBadRequest, codeInvalidInput},
	} {
		if errors.Is(err, m.err) {
			return m.status, m.code, true
		}
	}
	return 0, "", false
}

// orderError đổi lỗi nghiệp vụ của lần tạo, đọc đơn (và khách tạo kèm) sang mã HTTP. OutOfStockError có thân riêng ở
// writeHandlerError (kèm variantId, available). ErrDuplicateOrder: khóa nghiệp vụ của đơn đã dùng — cùng nghĩa với khóa
// yêu cầu dùng lại. ErrNotFound: không có, của người bán hay tenant khác (không cho biết là có).
func orderError(err error) (status int, code string, ok bool) {
	for _, m := range []struct {
		err    error
		status int
		code   string
	}{
		{order.ErrOutOfStock, http.StatusConflict, codeOutOfStock},
		{order.ErrNotForSale, http.StatusUnprocessableEntity, codeVariantNotForSale},
		{order.ErrMixedSellers, http.StatusUnprocessableEntity, codeMixedSellers},
		{order.ErrTotalOverflow, http.StatusUnprocessableEntity, codeTotalOverflow},
		{order.ErrDuplicateOrder, http.StatusUnprocessableEntity, codeIdempotencyKeyReused},
		{order.ErrInvalidInput, http.StatusBadRequest, codeInvalidInput},
		{order.ErrNotFound, http.StatusNotFound, codeNotFound},
		{customer.ErrInvalidInput, http.StatusBadRequest, codeInvalidInput},
	} {
		if errors.Is(err, m.err) {
			return m.status, m.code, true
		}
	}
	return 0, "", false
}

// fulfillmentError đổi lỗi nghiệp vụ của vận chuyển (và thiếu quyền ở kho của tài khoản hãng) sang mã HTTP.
// ErrOrderNotShippable (đơn không có, của người bán khác, chưa ở confirmed), ErrAccountNotFound, ErrNotFound: 404
// (không cho biết là có).
func fulfillmentError(err error) (status int, code string, ok bool) {
	for _, m := range []struct {
		err    error
		status int
		code   string
	}{
		{errForbiddenTarget, http.StatusForbidden, codeForbidden},
		{fulfillment.ErrShipmentExists, http.StatusConflict, codeShipmentExists},
		{fulfillment.ErrOrderNotShippable, http.StatusNotFound, codeNotFound},
		{fulfillment.ErrAccountNotFound, http.StatusNotFound, codeNotFound},
		{fulfillment.ErrNotFound, http.StatusNotFound, codeNotFound},
	} {
		if errors.Is(err, m.err) {
			return m.status, m.code, true
		}
	}
	return 0, "", false
}
