package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	ht "github.com/ogen-go/ogen/http"
	"github.com/ogen-go/ogen/ogenerrors"
	"github.com/ogen-go/ogen/validate"
)

// Mã lỗi trả cho client. Đây là chỗ duy nhất đổi lỗi sang mã HTTP (CLAUDE.md, mục "Lỗi và log").
const (
	codeBadRequest       = "bad_request"
	codeBadOrigin        = "bad_origin"
	codeBodyTooLarge     = "body_too_large"
	codeInternal         = "internal"
	codeNotFound         = "not_found"
	codeNotImplemented   = "not_implemented"
	codeUnauthenticated  = "unauthenticated"
	codeUnsupportedMedia = "unsupported_media_type"
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
	if r.Context().Err() == nil { // client đã đóng kết nối thì không phải lỗi của máy chủ
		d.Logger.LogAttrs(r.Context(), slog.LevelError, "lỗi xử lý yêu cầu",
			slog.String("request_id", RequestID(r.Context())),
			slog.String("route", r.Pattern),
			slog.String("error", err.Error()))
	}
	writeError(w, r, http.StatusInternalServerError, codeInternal)
}
