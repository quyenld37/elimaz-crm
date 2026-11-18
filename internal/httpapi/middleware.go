package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/platform/observability"
)

// maxBodyBytes: thân yêu cầu tối đa 1 MiB (kế hoạch M0, Task 6).
const maxBodyBytes = 1 << 20

// requestIDPattern: x-request-id gửi lên chỉ được nhận khi khớp mẫu này (không chèn được ký tự lạ vào log).
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type requestIDKey struct{}

// RequestID trả mã yêu cầu mà middleware requestID đặt vào ctx; rỗng khi ngoài yêu cầu HTTP.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// requestID lấy x-request-id gửi lên nếu khớp mẫu, không thì sinh mã mới; đặt vào ctx và header trả về. Mọi câu
// trả lời của API mang Cache-Control: no-store (dữ liệu nhân viên không được đệm ở Cloudflare hay trình duyệt).
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if !requestIDPattern.MatchString(id) {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

// recoverPanic: handler panic → ghi error_events (observability.RecordError: giá trị panic đã lọc, vết gọi tại chỗ
// panic), log một lần (kiểu giá trị panic, dấu vân tay và stack, không có giá trị panic vì có thể chứa dữ liệu cá
// nhân) rồi 500 {"error":"internal"} nếu chưa trả lời. Bắt panic và http.ErrAbortHandler: observability.RecoverPanics.
func recoverPanic(d Deps, rt Route, next http.Handler) http.Handler {
	return observability.RecoverPanics(next, func(ctx context.Context, w http.ResponseWriter, r *http.Request,
		pe *observability.PanicError, wrote bool,
	) {
		fp := recordError(ctx, d, pe, nil)
		d.Logger.LogAttrs(ctx, slog.LevelError, "handler panic",
			slog.String("request_id", RequestID(ctx)),
			slog.String("operation_id", rt.OperationID),
			slog.String("panic_type", fmt.Sprintf("%T", pe.Value)),
			slog.String("fingerprint", fp),
			slog.String("stack", string(debug.Stack())))
		if !wrote {
			writeError(w, r, http.StatusInternalServerError, codeInternal)
		}
	})
}

// bodyLimit: Content-Length quá 1 MiB → 413 ngay; thân không khai độ dài bị cắt khi đọc (lỗi *http.MaxBytesError,
// writeHandlerError đổi thành 413).
func bodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxBodyBytes {
			writeError(w, r, http.StatusRequestEntityTooLarge, codeBodyTooLarge)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// httpapiFramePrefix: khung gọi của chính lớp HTTP (đường trả lỗi, middleware) không tính vào dấu vân tay của lỗi
// trả về; mã ogen sinh (httpapi/gen) vẫn tính, nên lỗi của mỗi operation có khung riêng.
const httpapiFramePrefix = "elimaz.com.vn/crm/internal/httpapi."

// recordError ghi lỗi 500 vào error_events với vai trò tiến trình và mã yêu cầu; frames nil: vết của panic (err là
// *observability.PanicError) hoặc vết gọi hiện tại bỏ khung của lớp HTTP. Trả dấu vân tay để log kèm.
func recordError(ctx context.Context, d Deps, err error, frames []runtime.Frame) string {
	if _, ok := errors.AsType[*observability.PanicError](err); !ok && frames == nil {
		frames = slices.DeleteFunc(observability.StackFrames(1), func(f runtime.Frame) bool {
			return strings.HasPrefix(f.Function, httpapiFramePrefix)
		})
	}
	fp, _ := observability.RecordError(ctx, d.DB, err, observability.ErrorContext{
		Role: string(d.Config.Role), RequestID: RequestID(ctx), Frames: frames, Metrics: d.Metrics, Logger: d.Logger,
	})
	return fp
}
