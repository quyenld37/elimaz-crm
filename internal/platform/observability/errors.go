package observability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/metrics"
)

const (
	// maxMessageRunes: message của error_events tối đa chừng này ký tự (sau khi lọc).
	maxMessageRunes = 300
	// fingerprintFrames: số khung gọi đầu tiên tính vào dấu vân tay.
	fingerprintFrames = 3
	// maxStackDepth: số khung đọc tối đa khi lấy vết gọi.
	maxStackDepth = 32
	// recordTimeout: hạn của lần ghi error_events, tách khỏi việc hủy của ctx gọi.
	recordTimeout = 2 * time.Second
	// MetricErrors là counter của mọi lần RecordError, nhãn là vai trò tiến trình (luật error-spike đọc nó).
	MetricErrors = "errors"
)

// ScrubMessage là nội dung lỗi lưu vào error_events: qua logging.ScrubText (bộ che văn bản duy nhất của CRM: SĐT,
// email, địa chỉ, token thành "[Redacted]"), rồi cắt còn tối đa 300 ký tự (không cắt giữa ký tự UTF-8).
func ScrubMessage(m string) string {
	m = logging.ScrubText(m)
	if r := []rune(m); len(r) > maxMessageRunes {
		m = string(r[:maxMessageRunes])
	}
	return m
}

// PanicError là giá trị panic đã bắt bằng recover, kèm vết gọi tại chỗ panic. Error() đã qua ScrubMessage để có thể
// lưu vào last_error của việc nền.
type PanicError struct {
	Value  any
	Frames []runtime.Frame
}

// NewPanicError gói giá trị v của recover(); phải gọi ngay trong hàm defer đã recover (vết gọi lấy tại đó). v đã là
// *PanicError (lớp ScrubPanics bên trong đã gói rồi panic lại) thì trả nguyên, giữ vết của chỗ panic gốc.
func NewPanicError(v any) *PanicError {
	if pe, ok := v.(*PanicError); ok {
		return pe
	}
	return &PanicError{Value: v, Frames: PanicFrames()}
}

// RecoverPanics là lớp recover ngoài cùng của một máy chủ HTTP (api, ingest): panic trong next được gói thành
// *PanicError (NewPanicError: vết tại chỗ panic gốc) rồi giao cho onPanic cùng ctx của yêu cầu và wrote (đã gửi header
// chưa: chỉ trả 500 khi còn trả được). onPanic ghi lỗi, log và trả lời theo dạng lỗi của máy chủ đó; không log giá
// trị panic. http.ErrAbortHandler được ném tiếp như net/http mong đợi.
func RecoverPanics(next http.Handler, onPanic func(ctx context.Context, w http.ResponseWriter, r *http.Request, pe *PanicError, wrote bool)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hw := &headerWriter{ResponseWriter: w}
		ctx := r.Context()
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler { //nolint:errorlint,err113 // so đúng giá trị mà net/http dùng
				panic(v)
			}
			onPanic(ctx, hw, r, NewPanicError(v), hw.wrote)
		}()
		next.ServeHTTP(hw, r)
	})
}

// headerWriter nhớ đã gửi header chưa, để RecoverPanics báo onPanic có còn trả lời được không.
type headerWriter struct {
	http.ResponseWriter
	wrote bool
}

func (w *headerWriter) WriteHeader(code int) {
	w.wrote = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *headerWriter) Write(b []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(b) //nolint:wrapcheck // chuyển thẳng của ResponseWriter
}

// Unwrap cho http.ResponseController (Flush của SSE) đi tới writer gốc.
func (w *headerWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// ScrubPanics bọc next: panic trong next được gói thành *PanicError (Error() đã lọc dữ liệu cá nhân) rồi panic lại,
// để lớp dấu vết bọc ngoài (otelhttp: span ghi giá trị panic vào sự kiện exception) chỉ thấy nội dung đã lọc. Đặt
// ngay bên trong otelhttp; lớp recover ngoài cùng bắt *PanicError và ghi lỗi như cũ. http.ErrAbortHandler đi qua
// nguyên vẹn.
func ScrubPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler { //nolint:errorlint,err113 // so đúng giá trị mà net/http dùng
				panic(v)
			}
			panic(NewPanicError(v))
		}()
		next.ServeHTTP(w, r)
	})
}

func (p *PanicError) Error() string { return "panic: " + ScrubMessage(fmt.Sprint(p.Value)) }

// Unwrap trả lỗi gốc khi panic bằng một error.
func (p *PanicError) Unwrap() error {
	err, _ := p.Value.(error)
	return err
}

// ErrorContext mô tả nơi lỗi xảy ra cho RecordError.
type ErrorContext struct {
	Role      string    // vai trò tiến trình: api, worker, ingest
	RequestID string    // mã yêu cầu HTTP; rỗng ngoài yêu cầu
	JobID     uuid.UUID // mã việc nền; uuid.Nil ngoài việc nền
	// Frames là vết gọi tính dấu vân tay. nil: vết của *PanicError nếu err là panic, không thì vết tại chỗ gọi
	// RecordError.
	Frames  []runtime.Frame
	Metrics *metrics.Registry // nil: không đếm
	Logger  *slog.Logger      // nil: slog.Default()
}

// StackFrames trả vết gọi của hàm gọi StackFrames (bỏ skip khung phía trên nó), không gồm khung của runtime.
func StackFrames(skip int) []runtime.Frame {
	pcs := make([]uintptr, maxStackDepth)
	n := runtime.Callers(skip+2, pcs) // +2: runtime.Callers và chính StackFrames
	return userFrames(pcs[:n])
}

// PanicFrames trả vết gọi bắt đầu ở hàm đã panic; gọi trong hàm defer đã recover. Không thấy runtime.gopanic (không
// đang panic) thì như StackFrames(1).
func PanicFrames() []runtime.Frame {
	pcs := make([]uintptr, maxStackDepth)
	n := runtime.Callers(2, pcs)
	all := framesOf(pcs[:n])
	for i, f := range all {
		if f.Function == "runtime.gopanic" {
			return dropRuntime(all[i+1:])
		}
	}
	return dropRuntime(all)
}

func userFrames(pcs []uintptr) []runtime.Frame { return dropRuntime(framesOf(pcs)) }

func framesOf(pcs []uintptr) []runtime.Frame {
	out := make([]runtime.Frame, 0, len(pcs))
	frames := runtime.CallersFrames(pcs)
	for {
		f, more := frames.Next()
		out = append(out, f)
		if !more {
			return out
		}
	}
}

func dropRuntime(fs []runtime.Frame) []runtime.Frame {
	out := fs[:0:0]
	for _, f := range fs {
		if f.Function == "" || strings.HasPrefix(f.Function, "runtime.") {
			continue
		}
		out = append(out, f)
	}
	return out
}

// errorName là tên loại lỗi: "panic(<kiểu giá trị>)" với panic, không thì kiểu của lỗi trong cùng của chuỗi bọc
// (dừng ở errors.Join).
func errorName(err error) string {
	if pe, ok := errors.AsType[*PanicError](err); ok {
		return "panic(" + reflect.TypeOf(pe.Value).String() + ")"
	}
	for {
		next := errors.Unwrap(err)
		if next == nil {
			return reflect.TypeOf(err).String()
		}
		err = next
	}
}

// Fingerprint là sha256 (hex) của kiểu lỗi và 3 khung gọi đầu (tên hàm và tên tệp, bỏ số dòng và thư mục dựng).
func Fingerprint(err error, frames []runtime.Frame) string {
	h := sha256.New()
	_, _ = h.Write([]byte(errorName(err)))
	for _, f := range frames[:min(len(frames), fingerprintFrames)] {
		_, _ = fmt.Fprintf(h, "\n%s %s", f.Function, path.Base(f.File))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func topFrames(frames []runtime.Frame) string {
	lines := make([]string, 0, fingerprintFrames)
	for _, f := range frames[:min(len(frames), fingerprintFrames)] {
		lines = append(lines, f.Function+" ("+path.Base(f.File)+":"+strconv.Itoa(f.Line)+")")
	}
	return strings.Join(lines, "\n")
}

// RecordError ghi một lần lỗi err vào platform.error_events (gộp theo Fingerprint: dòng mới hoặc count + 1) và cộng
// counter errors{Role}. Không bao giờ trả lỗi: ghi thất bại chỉ log một dòng (không kèm nội dung lỗi gốc). Trả dấu vân
// tay và isNew khi đây là lần đầu gặp loại lỗi này. Ghi trong transaction riêng, hạn 2 s, không theo việc hủy của
// ctx (client đóng kết nối vẫn ghi); không gọi trong transaction nghiệp vụ.
func RecordError(ctx context.Context, d *db.DB, err error, c ErrorContext) (fingerprint string, isNew bool) {
	if err == nil {
		return "", false
	}
	frames := c.Frames
	if frames == nil {
		if pe, ok := errors.AsType[*PanicError](err); ok {
			frames = pe.Frames
		} else {
			frames = StackFrames(1)
		}
	}
	fingerprint = Fingerprint(err, frames)
	if c.Metrics != nil {
		c.Metrics.Inc(MetricErrors, c.Role, 1)
	}
	if d == nil {
		return fingerprint, false
	}
	p := store.UpsertErrorEventParams{
		Fingerprint: fingerprint, Role: c.Role, Name: errorName(err), Message: ScrubMessage(err.Error()),
		TopFrames: topFrames(frames),
	}
	if c.RequestID != "" {
		p.SampleRequestID = pgtype.Text{String: c.RequestID, Valid: true}
	}
	if c.JobID != uuid.Nil {
		p.SampleJobID = pgtype.UUID{Bytes: c.JobID, Valid: true}
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()
	werr := d.RetryTx(wctx, db.TxOptions{}, func(tx pgx.Tx) error {
		var qerr error
		isNew, qerr = store.New(tx).UpsertErrorEvent(wctx, p)
		return qerr //nolint:wrapcheck // bọc một lần ở dưới
	})
	if werr != nil {
		logger := c.Logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.LogAttrs(ctx, slog.LevelError, "ghi error_events thất bại",
			slog.String("fingerprint", fingerprint), slog.String("role", c.Role),
			logging.Err(fmt.Errorf("ghi lỗi ứng dụng: %w", werr)))
		return fingerprint, false
	}
	return fingerprint, isNew
}
