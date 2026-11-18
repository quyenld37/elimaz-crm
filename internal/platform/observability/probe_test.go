package observability_test

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/observability"
)

func portOf(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	_, p, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCheckLive(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health/live" {
			http.NotFound(w, r)
		}
	}))
	defer ok.Close()
	if err := observability.CheckLive(t.Context(), portOf(t, ok), time.Second); err != nil {
		t.Fatalf("tiến trình sống: %v", err)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer bad.Close()
	if err := observability.CheckLive(t.Context(), portOf(t, bad), time.Second); !errors.Is(err, observability.ErrNotLive) {
		t.Fatalf("503 phải là ErrNotLive, nhận %v", err)
	}

	port := portOf(t, bad)
	bad.Close()
	if err := observability.CheckLive(t.Context(), port, time.Second); err == nil {
		t.Fatal("không có tiến trình phải lỗi")
	}
}
