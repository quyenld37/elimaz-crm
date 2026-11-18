package testkit

import (
	"io"
	"net/http"
	"testing"
	"time"
)

// httpTimeout là hạn của một yêu cầu HTTP trong test.
const httpTimeout = 30 * time.Second

// Do gửi req bằng http.Client hạn 30 s, đọc hết thân rồi đóng. Lỗi mạng hay lỗi đọc thân làm test đỏ.
func Do(t testing.TB, req *http.Request) (status int, header http.Header, body []byte) {
	t.Helper()
	resp, err := (&http.Client{Timeout: httpTimeout}).Do(req) //nolint:gosec // G704: chỉ dùng trong test, URL do test dựng
	if err != nil {
		t.Fatalf("testkit: gửi %s %s: %v", req.Method, req.URL.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("testkit: đọc thân %s %s: %v", req.Method, req.URL.Path, err)
	}
	return resp.StatusCode, resp.Header, b
}
