package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/testkit"
)

// Task 529 (phát hiện của Task 220 bước e): PostgreSQL ngừng giữa tải → route web (khóa dịch vụ, chống trùng) trả 503
// retry_later kèm Retry-After: 1, không 500 internal; không log lỗi, không ghi error_events (không ghi được). Hai chỗ chạm database của
// đường mua flash: ghi nhận khóa chống trùng (idempotency.Acquire) và transaction của handler (WriteTx). Pool của api
// đi qua proxy cắt kết nối như khi dừng PostgreSQL.
func TestDBUnavailable_WebRouteChain(t *testing.T) {
	t.Parallel()
	st := testkit.OpenStaff(t)
	tenant := st.Tenant(t, "t529")
	proxy, u := testkit.StartSwitchProxy(t, st.App.Pool.Config().ConnString())
	app, err := db.Open(t.Context(), u, db.PoolOptions{MaxConns: 4})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Pool.Close)

	newDeps := func(buf *bytes.Buffer) Deps {
		d := unitDeps(buf)
		d.DB = app
		d.ServiceKeys = &fakeKeys{user: serviceUser(tenant, uuid.New())}
		return d
	}
	hdr := func(key string) map[string]string {
		return map[string]string{"Authorization": "Bearer " + probeServiceKey, headerIdempotencyKey: key}
	}
	// check: 503 retry_later, Retry-After: 1; logged: lỗi có được ghi như lỗi ứng dụng (database còn sống) hay không
	// (không tới được database).
	check := func(t *testing.T, name string, rec *httptest.ResponseRecorder, buf *bytes.Buffer, logged bool) {
		t.Helper()
		expect(t, name, rec, http.StatusServiceUnavailable, codeRetryLater)
		if rec.Header().Get("Retry-After") != "1" {
			t.Errorf("%s: Retry-After %q, mong 1", name, rec.Header().Get("Retry-After"))
		}
		if got := strings.Contains(buf.String(), "lỗi xử lý yêu cầu"); got != logged {
			t.Errorf("%s: ghi lỗi %v, mong %v:\n%s", name, got, logged, buf.String())
		}
	}

	// Quá hạn câu lệnh (57014) trong transaction của handler khi database còn sống: 503 và vẫn ghi lỗi (câu chậm phải
	// có cảnh báo); khóa được trả lại ngay nên lần gửi lại cùng khóa theo Retry-After chạy được (không 409
	// request_in_progress tới 60 s).
	var buf bytes.Buffer
	d := newDeps(&buf)
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		err := WriteTx(ctx, d, tenant, func(tx pgx.Tx) (int, any, bool, error) {
			_, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '10ms'; SELECT pg_sleep(1)")
			return http.StatusOK, nil, true, err //nolint:wrapcheck // lỗi thử
		})
		if err != nil {
			writeHandlerError(d, w, r, err)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	rec := post(t, chain(d, webRoute(modeRequired), slow), "/api/v1/web/_probe", strings.NewReader("{}"), hdr("k529-s"))
	check(t, "57014 trong transaction", rec, &buf, true)
	rec = post(t, chain(d, webRoute(modeRequired), okHandler()), "/api/v1/web/_probe", strings.NewReader("{}"), hdr("k529-s"))
	expect(t, "gửi lại cùng khóa sau 57014", rec, http.StatusOK, "")

	// Database ngừng khi handler đang chạy: khóa đã ghi nhận, transaction của handler mất kết nối.
	buf.Reset()
	inTx := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		proxy.SetDown(true)
		err := WriteTx(ctx, d, tenant, func(tx pgx.Tx) (int, any, bool, error) {
			_, err := tx.Exec(ctx, "SELECT 1")
			return http.StatusOK, nil, true, err //nolint:wrapcheck // lỗi thử
		})
		if err != nil {
			writeHandlerError(d, w, r, err)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	rec = post(t, chain(d, webRoute(modeRequired), inTx), "/api/v1/web/_probe", strings.NewReader("{}"), hdr("k529-a"))
	check(t, "ngừng giữa transaction", rec, &buf, false)

	// Database đang ngừng: bước ghi nhận khóa chống trùng không nối được.
	buf.Reset()
	rec = post(t, chain(d, webRoute(modeRequired), okHandler()), "/api/v1/web/_probe", strings.NewReader("{}"), hdr("k529-b"))
	check(t, "ngừng trước khóa chống trùng", rec, &buf, false)

	// Database trở lại: cùng chuỗi chạy bình thường.
	proxy.SetDown(false)
	buf.Reset()
	rec = post(t, chain(d, webRoute(modeRequired), okHandler()), "/api/v1/web/_probe", strings.NewReader("{}"), hdr("k529-c"))
	expect(t, "database trở lại", rec, http.StatusOK, "")
}

// Bộ kiểm khóa dịch vụ (đọc database khi bản đệm hết hạn) gặp database ngừng: 503 retry_later có Retry-After, như
// ErrRetryLater của bước xác thực (trước đây thiếu Retry-After).
func TestDBUnavailable_AuthStep(t *testing.T) {
	t.Parallel()
	for _, e := range []error{
		fmt.Errorf("kiểm khóa dịch vụ: %w", &pgconn.PgError{Code: "57P01"}),
		fmt.Errorf("kiểm khóa dịch vụ: %w", db.ErrRetryLater),
	} {
		var buf bytes.Buffer
		d := unitDeps(&buf)
		d.ServiceKeys = verifierFunc(func(context.Context, string, netip.Addr) (identity.AuthUser, error) {
			return identity.AuthUser{}, e
		})
		rec := post(t, chain(d, webRoute(modeExempt), okHandler()), "/api/v1/web/_probe", strings.NewReader("{}"),
			map[string]string{"Authorization": "Bearer " + probeServiceKey})
		expect(t, e.Error(), rec, http.StatusServiceUnavailable, codeRetryLater)
		if rec.Header().Get("Retry-After") != "1" {
			t.Errorf("%v: Retry-After %q, mong 1", e, rec.Header().Get("Retry-After"))
		}
	}
}

// writeHandlerError: từng loại lỗi database ngừng/quá hạn → 503 retry_later + Retry-After; chỉ nhóm không tới được
// database (refused, 57P01, 53300) không ghi lỗi, nhóm database còn sống (57014, 53100, hạn ctx) vẫn ghi. Lỗi nghiệp
// vụ, lỗi câu lệnh, lỗi lạ giữ nguyên mã (không biến thành 503).
func TestDBUnavailable_HandlerErrorMapping(t *testing.T) {
	t.Parallel()
	_, refused := pgx.Connect(t.Context(), "postgres://crm_app:x@127.0.0.1:1/crm?sslmode=disable&connect_timeout=1")
	if refused == nil {
		t.Fatal("cổng 1 phải từ chối kết nối")
	}
	cases := []struct {
		name   string
		err    error
		status int
		code   string
		logged bool
	}{
		{"connect refused", refused, http.StatusServiceUnavailable, codeRetryLater, false},
		{"57P01 admin shutdown", &pgconn.PgError{Code: "57P01"}, http.StatusServiceUnavailable, codeRetryLater, false},
		{"53300 too many connections", &pgconn.PgError{Code: "53300"}, http.StatusServiceUnavailable, codeRetryLater, false},
		{"57014 statement timeout", &pgconn.PgError{Code: "57014"}, http.StatusServiceUnavailable, codeRetryLater, true},
		{"53100 disk full", &pgconn.PgError{Code: "53100"}, http.StatusServiceUnavailable, codeRetryLater, true},
		{"deadline", context.DeadlineExceeded, http.StatusServiceUnavailable, codeRetryLater, true},
		{"retry later", db.ErrRetryLater, http.StatusServiceUnavailable, codeRetryLater, false},
		{"hold expired", order.ErrHoldExpired, http.StatusUnprocessableEntity, codeHoldExpired, false},
		{"flash retry", order.ErrFlashRetry, http.StatusServiceUnavailable, codeRetryLater, false},
		{"unique violation", &pgconn.PgError{Code: "23505"}, http.StatusInternalServerError, codeInternal, true},
		{"unknown", errors.New("hỏng"), http.StatusInternalServerError, codeInternal, true},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		d := unitDeps(&buf)
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/web/_probe", nil)
		requestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeHandlerError(d, w, r, fmt.Errorf("lượt mua flash sale: %w", c.err))
		})).ServeHTTP(rec, req)
		expect(t, c.name, rec, c.status, c.code)
		if got := strings.Contains(buf.String(), "lỗi xử lý yêu cầu"); got != c.logged {
			t.Errorf("%s: ghi lỗi %v, mong %v", c.name, got, c.logged)
		}
		if c.code == codeRetryLater && errors.Is(c.err, order.ErrFlashRetry) {
			continue // Retry-After của ErrFlashRetry do retryAfterWriter của cổng đặt
		}
		if got := rec.Header().Get("Retry-After"); (c.code == codeRetryLater) != (got == "1") {
			t.Errorf("%s: Retry-After %q", c.name, got)
		}
	}
}
