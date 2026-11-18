package observability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
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

var (
	longDigitsPattern = regexp.MustCompile(`[0-9]{8,}`)
	emailPattern      = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
)

// ScrubMessage lọc dữ liệu cá nhân khỏi nội dung lỗi trước khi lưu: chuỗi từ 8 chữ số liền trở lên → "#", email →
// "@", rồi cắt còn tối đa 300 ký tự (không cắt giữa ký tự UTF-8). Chỉ lọc hai dạng này; tên, địa chỉ trong nội dung
// tự do không được lọc, nên mã không đưa dữ liệu khách vào chuỗi lỗi.
func ScrubMessage(m string) string {
	m = strings.ToValidUTF8(m, "?")
	m = emailPattern.ReplaceAllString(m, "@")
	m = longDigitsPattern.ReplaceAllString(m, "#")
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

// NewPanicError gói giá trị v của recover(); phải gọi ngay trong hàm defer đã recover (vết gọi lấy tại đó).
func NewPanicError(v any) *PanicError {
	return &PanicError{Value: v, Frames: PanicFrames()}
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
			slog.String("error", fmt.Errorf("ghi lỗi ứng dụng: %w", werr).Error()))
		return fingerprint, false
	}
	return fingerprint, isNew
}
