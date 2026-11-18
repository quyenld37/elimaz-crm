package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/testkit"
)

// testServer: máy chủ thật (NewServer, bảng Routes) trên database của staff, kiểm thẻ bằng máy chủ khóa keys.
// lockTimeout > 0 thay lock_timeout của WriteTx.
func testServer(t *testing.T, staff *testkit.Staff, keys *testkit.AccessKeys, lockTimeout time.Duration) http.Handler {
	t.Helper()
	authE := &authEnv{t: t, keys: keys}
	return NewServer(Deps{
		Config: config.Config{ConsoleOrigins: []string{testOrigin}}, DB: staff.App,
		Logger: slog.New(slog.NewJSONHandler(&lockedBuffer{buf: &bytes.Buffer{}}, nil)), Access: authE.verifier(),
		WriteLockTimeout: lockTimeout,
	})
}

// serveAs gửi yêu cầu tới srv với thẻ Access hợp lệ của email (có bước hai); yêu cầu ghi có Origin, Content-Type
// JSON; key khác rỗng thì đặt Idempotency-Key.
func serveAs(srv http.Handler, keys *testkit.AccessKeys, method, path, email, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	req.Header.Set("Cf-Access-Jwt-Assertion", keys.Sign(testkit.AccessClaims{Email: email, AUD: testAUD, MFA: []string{"mfa"}}))
	if method != http.MethodGet {
		req.Header.Set("Origin", testOrigin)
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}
