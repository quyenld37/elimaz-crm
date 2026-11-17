package observability_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/observability"
)

type fakeDB struct {
	err   error
	delay time.Duration
}

func (f fakeDB) Ping(ctx context.Context) error {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return fmt.Errorf("ping: %w", ctx.Err())
		}
	}
	return f.err
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
	return rec
}

func mux(db observability.Pinger) *http.ServeMux {
	m := http.NewServeMux()
	observability.NewHealth(db).Register(m)
	return m
}

func TestHealthLive(t *testing.T) {
	rec := get(t, mux(fakeDB{err: errors.New("down")}), "/health/live")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"status":"ok"}` {
		t.Fatalf("live = %d %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q", ct)
	}
}

func TestHealthReadyUp(t *testing.T) {
	rec := get(t, mux(fakeDB{}), "/health/ready")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"status":"ok"}` {
		t.Fatalf("ready = %d %s", rec.Code, rec.Body)
	}
}

func TestHealthReadyDown(t *testing.T) {
	rec := get(t, mux(fakeDB{err: errors.New("connection refused 10.0.0.1")}), "/health/ready")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready = %d", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"status":"unavailable","checks":{"db":"down"}}` {
		t.Fatalf("body = %s", got)
	}
}

func TestHealthReadyTimesOutAfterOneSecond(t *testing.T) {
	start := time.Now()
	rec := get(t, mux(fakeDB{delay: 5 * time.Second}), "/health/ready")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready = %d", rec.Code)
	}
	if d := time.Since(start); d < 900*time.Millisecond || d > 2*time.Second {
		t.Fatalf("thời gian = %v, muốn khoảng 1 s", d)
	}
}

func TestHealthOnlyGET(t *testing.T) {
	m := mux(fakeDB{})
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/health/live", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d", rec.Code)
	}
}
