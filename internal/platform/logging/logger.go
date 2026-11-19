// Package logging dựng logger JSON của CRM: che sâu dữ liệu cá nhân và bí mật ở mọi cấp, thêm trace_id/span_id
// từ ctx, và khi có LOG_DIR thì ghi thêm vào tệp theo ngày.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"regexp"

	"go.opentelemetry.io/otel/trace"
)

// NewLogger tạo logger JSON ghi ra stdout, và vào DailyFile(logDir, role) khi logDir khác rỗng. Closer đóng tệp log.
// Nhận giá trị rời thay vì config.Config để gói này không nhập config (tệp chạy imager dùng logging, Task 414).
func NewLogger(logDir, role string, stdout io.Writer) (*slog.Logger, io.Closer, error) {
	var (
		w      = stdout
		closer io.Closer
	)
	if logDir != "" {
		f := NewDailyFile(logDir, role)
		if err := f.Check(); err != nil {
			return nil, nil, fmt.Errorf("mở thư mục log: %w", err)
		}
		w = io.MultiWriter(stdout, f)
		closer = f
	} else {
		closer = nopCloser{}
	}
	inner := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(&redactHandler{inner: inner}), closer, nil
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

// redactHandler che mọi thuộc tính trước khi chuyển cho handler JSON, và thêm trace_id, span_id.
type redactHandler struct {
	inner slog.Handler
	// redactAll đúng khi đang ở trong một nhóm có tên nhạy cảm: mọi giá trị bên dưới đều bị che.
	redactAll bool
}

func (h *redactHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *redactHandler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.redactAttr(a))
		return true
	})
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		// Đặt ở cấp gốc dù đang trong nhóm: nối log với dấu vết cần khóa cố định.
		return h.withTrace(sc).Handle(ctx, out) //nolint:wrapcheck // handler chuẩn của slog, không bọc thêm
	}
	return h.inner.Handle(ctx, out) //nolint:wrapcheck // handler chuẩn của slog, không bọc thêm
}

// withTrace trả handler gốc có thêm trace_id, span_id. Khi logger đã có nhóm (WithGroup), hai khóa này nằm trong nhóm
// đó; CRM không dùng WithGroup cho logger gốc nên thực tế chúng ở cấp gốc.
func (h *redactHandler) withTrace(sc trace.SpanContext) slog.Handler {
	return h.inner.WithAttrs([]slog.Attr{
		slog.String("trace_id", sc.TraceID().String()),
		slog.String("span_id", sc.SpanID().String()),
	})
}

func (h *redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	red := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		red[i] = h.redactAttr(a)
	}
	return &redactHandler{inner: h.inner.WithAttrs(red), redactAll: h.redactAll}
}

func (h *redactHandler) WithGroup(name string) slog.Handler {
	return &redactHandler{inner: h.inner.WithGroup(name), redactAll: h.redactAll || SensitiveKey(name)}
}

// errorKey: khóa log mang nội dung lỗi (error, db_error, err…); giá trị chuỗi dưới khóa này qua ScrubText, phòng
// khi mã ghi err.Error() thay cho Err(err).
var errorKey = regexp.MustCompile(`(?i)err`)

func (h *redactHandler) redactAttr(a slog.Attr) slog.Attr {
	if h.redactAll || SensitiveKey(a.Key) {
		return slog.String(a.Key, redacted)
	}
	return redactAttrValue(a, DefaultDepth)
}

func redactAttrValue(a slog.Attr, depth int) slog.Attr {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindGroup:
		group := v.Group()
		out := make([]any, 0, len(group))
		for _, g := range group {
			if SensitiveKey(g.Key) {
				out = append(out, slog.String(g.Key, redacted))
				continue
			}
			out = append(out, redactAttrValue(g, depth-1))
		}
		return slog.Group(a.Key, out...)
	case slog.KindAny:
		return slog.Any(a.Key, Redact(v.Any(), depth))
	case slog.KindString:
		if errorKey.MatchString(a.Key) {
			return slog.String(a.Key, ScrubText(v.String()))
		}
		return slog.Attr{Key: a.Key, Value: v}
	default:
		return slog.Attr{Key: a.Key, Value: v}
	}
}
