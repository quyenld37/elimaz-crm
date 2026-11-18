package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	ht "github.com/ogen-go/ogen/http"
	"github.com/ogen-go/ogen/ogenerrors"
	"github.com/ogen-go/ogen/validate"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/idempotency"
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
	// Kho tệp (kế hoạch M1 Task M1-41).
	codeFileTooLarge          = "file_too_large"
	codeFileTypeNotAllowed    = "file_type_not_allowed"
	codeFilePurposeNotAllowed = "file_purpose_not_allowed"
	codeUploadMissing         = "upload_missing"
)

type errorBody struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id"`
}

// writeError ghi lỗi theo dạng duy nhất {"error": "<mã>", "request_id": "<id>"}; không bao giờ có vết lỗi.
func writeError(w http.ResponseWriter, r *http.Request, status int, code string) {
	b, _ := json.Marshal(errorBody{Error: code, RequestID: RequestID(r.Context())}) //nolint:errchkjson // hai chuỗi, không lỗi
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
	if status, code, ok := identityError(err); ok {
		writeError(w, r, status, code)
		return
	}
	if status, code, ok := filesError(err); ok {
		writeError(w, r, status, code)
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
		d.Logger.LogAttrs(r.Context(), slog.LevelError, "lỗi xử lý yêu cầu",
			slog.String("request_id", RequestID(r.Context())),
			slog.String("route", r.Pattern),
			slog.String("error", err.Error()))
	}
	writeError(w, r, http.StatusInternalServerError, codeInternal)
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
