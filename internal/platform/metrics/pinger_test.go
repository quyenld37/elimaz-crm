package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/platform/metrics"
)

func TestPingEmptyURLDoesNothing(t *testing.T) {
	if err := (&metrics.Pinger{}).Ping(t.Context()); err != nil {
		t.Fatalf("URL rỗng phải không làm gì: %v", err)
	}
	var p *metrics.Pinger
	if err := p.Ping(t.Context()); err != nil {
		t.Fatalf("Pinger nil phải không làm gì: %v", err)
	}
}

// Lỗi không chứa URL (URL nhịp sống thường mang mã bí mật).
func TestPingErrorHidesURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	err := (&metrics.Pinger{URL: srv.URL + "/secret-token-123", Client: srv.Client()}).Ping(t.Context())
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("trả 500 phải lỗi nêu 500: %v", err)
	}
	if strings.Contains(err.Error(), "secret-token-123") {
		t.Fatalf("lỗi để lộ URL: %v", err)
	}

	srv.Close()
	err = (&metrics.Pinger{URL: srv.URL + "/secret-token-123"}).Ping(t.Context())
	if err == nil || strings.Contains(err.Error(), "secret-token-123") || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("máy không nối được phải lỗi, không lộ URL: %v", err)
	}
}
