package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/logging"
)

func TestRedactNested(t *testing.T) {
	in := map[string]any{
		"a": map[string]any{
			"b": []any{map[string]any{"customerPhone": "0912345678", "note": "ok"}},
		},
		"headers":     map[string]any{"authorization": "Bearer x"},
		"messageText": "xin chào",
	}
	out, err := json.Marshal(logging.Redact(in, 8))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, leak := range []string{"0912345678", "Bearer x", "xin chào"} {
		if strings.Contains(s, leak) {
			t.Errorf("còn %q: %s", leak, s)
		}
	}
	if !strings.Contains(s, `"ok"`) || !strings.Contains(s, "[Redacted]") {
		t.Errorf("phải giữ ok và có [Redacted]: %s", s)
	}
}

type inner struct {
	BankAccount string
	City        string
}

type order struct {
	ShipAddress string
	Items       []inner
	Ptr         *inner
	Tag         string `json:"recipient_phone"`
	private     string
}

type marshaler struct{}

func (marshaler) MarshalJSON() ([]byte, error) {
	return []byte(`{"phone":"0987654321","ok":1}`), nil
}

func TestRedactStructsPointersMarshalers(t *testing.T) {
	o := order{
		ShipAddress: "12 Lê Lợi",
		Items:       []inner{{BankAccount: "9999", City: "Huế"}},
		Ptr:         &inner{BankAccount: "8888", City: "Đà Nẵng"},
		Tag:         "0900000000",
		private:     "hidden",
	}
	out, err := json.Marshal(logging.Redact(map[string]any{"o": o, "m": marshaler{}}, 8))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, leak := range []string{"Lê Lợi", "9999", "8888", "0900000000", "hidden", "0987654321"} {
		if strings.Contains(s, leak) {
			t.Errorf("còn %q: %s", leak, s)
		}
	}
	for _, keep := range []string{"Huế", "Đà Nẵng"} {
		if !strings.Contains(s, keep) {
			t.Errorf("mất %q: %s", keep, s)
		}
	}
}

func TestRedactDepthLimitNeverPrintsRaw(t *testing.T) {
	var v any = map[string]any{"phone": "0911111111"}
	for range 12 {
		v = map[string]any{"x": v}
	}
	out, _ := json.Marshal(logging.Redact(v, 8))
	if strings.Contains(string(out), "0911111111") {
		t.Fatalf("quá độ sâu vẫn lộ: %s", out)
	}
	if !strings.Contains(string(out), "[Truncated]") {
		t.Fatalf("quá độ sâu phải ghi [Truncated]: %s", out)
	}
}

func TestSensitiveKey(t *testing.T) {
	for _, k := range []string{
		"phone", "customerPhone", "ADDRESS", "access_token", "client_secret", "password",
		"Authorization", "cookie", "body", "messageText", "id_number", "bank_account",
	} {
		if !logging.SensitiveKey(k) {
			t.Errorf("%q phải bị che", k)
		}
	}
	for _, k := range []string{"note", "status", "trace_id", "request_id", "city"} {
		if logging.SensitiveKey(k) {
			t.Errorf("%q không được che", k)
		}
	}
}

func newTestLogger(t *testing.T) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	l, closer, err := logging.NewLogger(config.Config{Role: config.RoleAPI}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closer.Close() })
	return l, &buf
}

func TestLoggerRedactsStructAndTypes(t *testing.T) {
	l, buf := newTestLogger(t)
	l.Info("đơn", slog.Any("order", struct{ ShipAddress string }{"12 Lê Lợi"}))
	l.Info("sđt", slog.Any("p", logging.Phone("0911")))
	l.Info("chuỗi", slog.String("password", "hunter2"), slog.Group("req", slog.String("cookie", "sid=1"), slog.Int("n", 3)))
	l.With(slog.String("token", "tk-1")).WithGroup("body").Info("nhóm", slog.String("anything", "nội dung"))
	l.Info("kiểu", slog.Any("a", logging.Address("1 Trần Phú")), slog.Any("s", logging.Secret("s3cr3t")))
	s := buf.String()
	for _, leak := range []string{"Lê Lợi", "0911", "hunter2", "sid=1", "tk-1", "nội dung", "Trần Phú", "s3cr3t"} {
		if strings.Contains(s, leak) {
			t.Errorf("log còn %q:\n%s", leak, s)
		}
	}
	if !strings.Contains(s, "[Redacted]") || !strings.Contains(s, `"n":3`) {
		t.Errorf("log phải có [Redacted] và giữ n:\n%s", s)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(s), "\n") {
		if !json.Valid([]byte(line)) {
			t.Errorf("dòng không phải JSON: %s", line)
		}
	}
}

func TestLoggerAddsTraceID(t *testing.T) {
	l, buf := newTestLogger(t)
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(tracetest.NewInMemoryExporter()))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	ctx, span := tp.Tracer("t").Start(t.Context(), "op")
	l.InfoContext(ctx, "trong span")
	span.End()
	l.InfoContext(t.Context(), "ngoài span")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	var first, second map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatal(err)
	}
	if first["trace_id"] != span.SpanContext().TraceID().String() || first["span_id"] != span.SpanContext().SpanID().String() {
		t.Fatalf("trace_id/span_id sai: %v (muốn %s)", first, span.SpanContext().TraceID())
	}
	if _, ok := second["trace_id"]; ok {
		t.Fatalf("ngoài span không được có trace_id: %v", second)
	}
}
