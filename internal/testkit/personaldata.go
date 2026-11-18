package testkit

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Dữ liệu cá nhân giả cho test "không lọt qua nội dung lỗi" (Task 319): SĐT, địa chỉ, token.
const (
	FakePhone   = "0912345678"
	FakeAddress = "12 Nguyễn Trãi, Thanh Xuân, Hà Nội"
	FakeToken   = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjMifQ.c2lnbmF0dXJlMTIz" //nolint:gosec // G101: token giả cho test
)

// personalFragments: các mẩu không được xuất hiện ở đâu sau khi lọc (cả mẩu con của địa chỉ, của token).
func personalFragments() []string {
	return []string{FakePhone, "Nguyễn Trãi", "Thanh Xuân", "Hà Nội", FakeToken, "eyJhbGciOiJIUzI1NiJ9"}
}

// ErrPersonalBase là lỗi gốc mà PersonalDataError bọc (để test kiểm errors.Is vẫn đúng sau khi lọc).
var ErrPersonalBase = errors.New("hỏng")

// PersonalDataError trả một lỗi bọc hai lớp có SĐT, địa chỉ, token trong nội dung, như lỗi do mã đưa dữ liệu khách vào.
func PersonalDataError() error {
	return fmt.Errorf("lưu khách: %w", fmt.Errorf("thẻ %s, giao tới %s, gọi %s: %w", FakeToken, FakeAddress, FakePhone, ErrPersonalBase))
}

// AssertNoPersonalData báo lỗi khi s (một dòng log, nội dung bảng, văn bản span) còn mẩu dữ liệu cá nhân giả nào.
func AssertNoPersonalData(t testing.TB, where, s string) {
	t.Helper()
	for _, f := range personalFragments() {
		if strings.Contains(s, f) {
			t.Errorf("%s còn %q: %s", where, f, s)
		}
	}
}

// SpanText gộp mọi thứ một span mang ra ngoài (tên, trạng thái, thuộc tính, sự kiện và thuộc tính sự kiện) thành
// văn bản để quét dữ liệu cá nhân.
func SpanText(spans []sdktrace.ReadOnlySpan) string {
	var b strings.Builder
	for _, s := range spans {
		fmt.Fprintf(&b, "%s|%s|", s.Name(), s.Status().Description)
		for _, a := range s.Attributes() {
			fmt.Fprintf(&b, "%s=%s;", a.Key, a.Value.String())
		}
		for _, e := range s.Events() {
			fmt.Fprintf(&b, "event %s:", e.Name)
			for _, a := range e.Attributes {
				fmt.Fprintf(&b, "%s=%s;", a.Key, a.Value.String())
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// CaptureSpans đặt TracerProvider toàn cục ghi vào bộ nhớ cho tới hết test (trả lại provider cũ khi dọn); test dùng nó
// không được chạy song song.
func CaptureSpans(t testing.TB) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return exp
}

// SyncBuffer là bytes.Buffer an toàn khi nhiều goroutine cùng ghi (đích của logger trong test).
type SyncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *SyncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p) //nolint:wrapcheck // chuyển thẳng của bytes.Buffer
}

func (s *SyncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
