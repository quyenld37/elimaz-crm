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

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/testkit"
)

// testServer: máy chủ thật (NewServer, bảng Routes) trên database của staff, kiểm thẻ bằng máy chủ khóa keys.
// lockTimeout > 0 thay lock_timeout của WriteTx.
func testServer(t *testing.T, staff *testkit.Staff, keys *cfaccesstest.AccessKeys, lockTimeout time.Duration) http.Handler {
	t.Helper()
	return NewServer(testServerDeps(t, staff, keys, lockTimeout))
}

// testServerDeps là Deps mà testServer dùng (database của staff, kiểm thẻ bằng keys), cho test dựng chain riêng.
func testServerDeps(t *testing.T, staff *testkit.Staff, keys *cfaccesstest.AccessKeys, lockTimeout time.Duration) Deps {
	t.Helper()
	authE := &authEnv{t: t, keys: keys}
	return Deps{
		Config: config.Config{ConsoleOrigins: []string{testOrigin}}, DB: staff.App,
		Logger: slog.New(slog.NewJSONHandler(&lockedBuffer{buf: &bytes.Buffer{}}, nil)), Access: authE.verifier(),
		WriteLockTimeout: lockTimeout,
	}
}

// serveAs gửi yêu cầu tới srv với thẻ Access hợp lệ của email (có bước hai); yêu cầu ghi có Origin, Content-Type
// JSON; key khác rỗng thì đặt Idempotency-Key.
func serveAs(srv http.Handler, keys *cfaccesstest.AccessKeys, method, path, email, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	req.Header.Set("Cf-Access-Jwt-Assertion", keys.Sign(cfaccesstest.AccessClaims{Email: email, AUD: testAUD, MFA: []string{"mfa"}}))
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
