package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/testkit"
)

const testOrigin = "https://crm.elimaz.test"

// testDeps: Deps với database thật (crm_app) và logger ghi vào buf.
func testDeps(t *testing.T, buf *bytes.Buffer) Deps {
	t.Helper()
	return Deps{
		Config: config.Config{ConsoleOrigins: []string{testOrigin, "https://khac.elimaz.test"}},
		DB:     testkit.OpenAppDB(t, db.PoolOptions{MaxConns: 2}),
		Logger: slog.New(slog.NewJSONHandler(buf, nil)),
	}
}

// unitDeps: Deps không database, cho test chỉ chạy chain().
func unitDeps(buf *bytes.Buffer) Deps {
	return Deps{
		Config: config.Config{ConsoleOrigins: []string{testOrigin}},
		Logger: slog.New(slog.NewJSONHandler(buf, nil)),
	}
}

type errBody struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id"`
}

// decodeErr đọc body lỗi; body phải đúng hai khóa error, request_id.
func decodeErr(t *testing.T, rec *httptest.ResponseRecorder) errBody {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("body lỗi không phải JSON: %v\n%s", err, rec.Body.String())
	}
	if len(raw) != 2 {
		t.Fatalf("body lỗi chỉ được có error và request_id, có: %s", rec.Body.String())
	}
	var b errBody
	_ = json.Unmarshal(rec.Body.Bytes(), &b)
	if b.Error == "" || b.RequestID == "" {
		t.Fatalf("body lỗi thiếu error hoặc request_id: %s", rec.Body.String())
	}
	if got := rec.Header().Get("X-Request-Id"); got != b.RequestID {
		t.Fatalf("request_id trong body %q khác header x-request-id %q", b.RequestID, got)
	}
	return b
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
}

func writeRoute() Route {
	return Route{Method: http.MethodPost, Pattern: "POST /api/v1/x", OperationID: "x", Public: true, Write: true}
}

func TestOriginRejectsWrites(t *testing.T) {
	var buf bytes.Buffer
	h := chain(unitDeps(&buf), writeRoute(), okHandler())
	cases := []struct {
		name, origin, ctype string
		want                int
	}{
		{"thiếu Origin", "", "application/json", http.StatusForbidden},
		{"Origin lạ", "https://evil.example", "application/json", http.StatusForbidden},
		{"Origin gần giống", testOrigin + ".evil.example", "application/json", http.StatusForbidden},
		{"content-type text/plain", testOrigin, "text/plain", http.StatusForbidden},
		{"thiếu content-type", testOrigin, "", http.StatusForbidden},
		{"đúng", testOrigin, "application/json", http.StatusOK},
		{"đúng, có charset", testOrigin, "application/json; charset=utf-8", http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/x", strings.NewReader("{}"))
			if c.origin != "" {
				req.Header.Set("Origin", c.origin)
			}
			if c.ctype != "" {
				req.Header.Set("Content-Type", c.ctype)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("mong %d, có %d %s", c.want, rec.Code, rec.Body.String())
			}
			if c.want == http.StatusForbidden {
				if b := decodeErr(t, rec); b.Error != "bad_origin" {
					t.Fatalf("mã lỗi phải bad_origin, có %q", b.Error)
				}
			}
		})
	}
}

func TestOriginSkipsReads(t *testing.T) {
	var buf bytes.Buffer
	read := Route{Method: http.MethodGet, Pattern: "GET /api/v1/x", OperationID: "x", Public: true}
	rec := httptest.NewRecorder()
	chain(unitDeps(&buf), read, okHandler()).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/x", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET không cần Origin, có %d", rec.Code)
	}
}

func TestPanicReturns500WithoutTrace(t *testing.T) {
	var buf bytes.Buffer
	read := Route{Method: http.MethodGet, Pattern: "GET /api/v1/boom", OperationID: "boom", Public: true}
	h := chain(unitDeps(&buf), read, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom 0912345678")
	}))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/boom", nil)
	req.Header.Set("X-Request-Id", "req-abc_123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic phải ra 500, có %d", rec.Code)
	}
	b := decodeErr(t, rec)
	if b.Error != "internal" || b.RequestID != "req-abc_123" {
		t.Fatalf("mong internal và request_id gửi lên, có %+v", b)
	}
	for _, leak := range []string{"boom", "0912345678", "goroutine", ".go:"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Fatalf("body lộ %q: %s", leak, rec.Body.String())
		}
		if leak == "0912345678" && strings.Contains(buf.String(), leak) {
			t.Fatalf("log lộ giá trị panic: %s", buf.String())
		}
	}
	if !strings.Contains(buf.String(), `"request_id":"req-abc_123"`) {
		t.Fatalf("log phải có request_id: %s", buf.String())
	}
}

func TestRequestIDGeneratedWhenInvalid(t *testing.T) {
	var buf bytes.Buffer
	read := Route{Method: http.MethodGet, Pattern: "GET /x", OperationID: "x", Public: true}
	var seen string
	h := chain(unitDeps(&buf), read, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	for _, bad := range []string{"", "có dấu", "a\nb", strings.Repeat("a", 129)} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
		if bad != "" {
			req.Header.Set("X-Request-Id", bad)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		got := rec.Header().Get("X-Request-Id")
		if got == "" || got == bad || got != seen || !requestIDPattern.MatchString(got) {
			t.Fatalf("x-request-id %q phải được thay bằng mã mới khớp mẫu; header %q, ctx %q", bad, got, seen)
		}
	}
}

func TestBodyLimit(t *testing.T) {
	var buf bytes.Buffer
	d := unitDeps(&buf)
	reader := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			writeHandlerError(d, w, r, err)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	h := chain(d, writeRoute(), reader)
	send := func(n int, chunked bool) *httptest.ResponseRecorder {
		var body io.Reader = bytes.NewReader(make([]byte, n))
		if chunked {
			body = io.MultiReader(body) // không khai độ dài
		}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/x", body)
		if chunked {
			req.ContentLength = -1
		}
		req.Header.Set("Origin", testOrigin)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	for _, chunked := range []bool{false, true} {
		if rec := send(maxBodyBytes, chunked); rec.Code != http.StatusOK {
			t.Fatalf("đúng 1 MiB (chunked=%v) phải qua, có %d", chunked, rec.Code)
		}
		rec := send(maxBodyBytes+1, chunked)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("1 MiB + 1 (chunked=%v) phải 413, có %d", chunked, rec.Code)
		}
		if b := decodeErr(t, rec); b.Error != "body_too_large" {
			t.Fatalf("mã lỗi phải body_too_large, có %q", b.Error)
		}
	}
}

// Qua NewServer: 1 MiB + 1 byte bị từ chối 413 trước cả origin và auth.
func TestServerBodyTooLarge(t *testing.T) {
	var buf bytes.Buffer
	srv := NewServer(testDeps(t, &buf))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/platform/jobs/0195f3a0-0000-7000-8000-000000000001/retry",
		bytes.NewReader(make([]byte, maxBodyBytes+1)))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("mong 413, có %d %s", rec.Code, rec.Body.String())
	}
	decodeErr(t, rec)
}

func TestServerRouting(t *testing.T) {
	var buf bytes.Buffer
	srv := NewServer(testDeps(t, &buf))
	cases := []struct {
		method, path string
		want         int
		code         string // mã lỗi mong đợi; rỗng khi không phải lỗi
	}{
		{http.MethodGet, "/health/live", http.StatusOK, ""},
		{http.MethodGet, "/health/ready", http.StatusOK, ""},
		{http.MethodGet, "/health/deep", http.StatusUnauthorized, "unauthenticated"}, // thiếu thẻ HEALTH_TOKEN (Task 19)
		{http.MethodGet, "/khong-co", http.StatusNotFound, "not_found"},
		{http.MethodGet, "/api/v1/khong-co", http.StatusNotFound, "not_found"},
		{http.MethodDelete, "/health/live", http.StatusNotFound, "not_found"},
		// Mặc định từ chối: route không công khai trả 401 cho tới khi Task 7 có auth.
		{http.MethodGet, "/api/v1/me", http.StatusUnauthorized, "unauthenticated"},
		{http.MethodGet, "/api/v1/platform/jobs", http.StatusUnauthorized, "unauthenticated"},
	}
	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), c.method, c.path, nil))
			if rec.Code != c.want {
				t.Fatalf("mong %d, có %d %s", c.want, rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control phải no-store, có %q", got)
			}
			if c.code != "" {
				if b := decodeErr(t, rec); b.Error != c.code {
					t.Fatalf("mã lỗi mong %q, có %q", c.code, b.Error)
				}
			}
		})
	}
	// Ghi không công khai với Origin đúng vẫn bị auth chặn (401), không tới handler.
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/platform/jobs/0195f3a0-0000-7000-8000-000000000001/abandon",
		strings.NewReader("{}"))
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST không công khai phải 401, có %d", rec.Code)
	}
}

func TestServerRegistersEveryRoute(t *testing.T) {
	var buf bytes.Buffer
	_, n := newMux(testDeps(t, &buf))
	if n != len(Routes) {
		t.Fatalf("NewServer đăng ký %d route, bảng Routes có %d", n, len(Routes))
	}
}

func TestHealthReadyDown(t *testing.T) {
	var buf bytes.Buffer
	d := testDeps(t, &buf)
	d.DB.Pool.Close()
	rec := httptest.NewRecorder()
	NewServer(d).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health/ready", nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"unavailable"`) {
		t.Fatalf("database đóng: mong 503 unavailable, có %d %s", rec.Code, rec.Body.String())
	}
}
