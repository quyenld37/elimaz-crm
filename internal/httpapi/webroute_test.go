package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/testkit"
)

// Test của M5-05: chuỗi middleware tách đường khóa dịch vụ (x-auth: service_key) khỏi đường nhân viên, giới hạn thân
// theo route (x-body-limit) và cổng nhận vào (x-admission) trước bước chống trùng.

const (
	probeServiceKey = "ek_abcdefghijkl_secret"
	modeExempt      = "exempt" // chế độ chống trùng exempt (lớp HTTP chỉ cần biết required, no-replay)
)

// fakeAccess: bộ kiểm thẻ Access luôn nhận thẻ, đếm số lần gọi.
type fakeAccess struct{ calls atomic.Int64 }

func (f *fakeAccess) Verify(_ context.Context, _ string) (string, error) {
	f.calls.Add(1)
	return "a@elimaz.test", nil
}

// fakeKeys: bộ kiểm khóa dịch vụ thử: khóa đúng probeServiceKey → user; khóa khác → identity.ErrInvalidToken. Ghi
// khóa và IP đã nhận.
type fakeKeys struct {
	user   identity.AuthUser
	calls  atomic.Int64
	lastIP netip.Addr
	lastBy string
}

func (f *fakeKeys) Verify(_ context.Context, bearer string, ip netip.Addr) (identity.AuthUser, error) {
	f.calls.Add(1)
	f.lastIP, f.lastBy = ip, bearer
	if bearer != probeServiceKey {
		return identity.AuthUser{}, identity.ErrInvalidToken
	}
	return f.user, nil
}

// fakeWebLimit: giới hạn tần suất của khóa dịch vụ thử: từ chối khi deny, ghi chủ thể.
type fakeWebLimit struct {
	deny     bool
	subjects []string
}

func (f *fakeWebLimit) AllowWebKey(_ context.Context, subject string) (bool, time.Duration) {
	f.subjects = append(f.subjects, subject)
	if f.deny {
		return false, 1500 * time.Millisecond
	}
	return true, 0
}

// serviceUser: người gọi là khóa dịch vụ keyID của tenant (Task 189: UserID rỗng, ServiceKeyID là mã khóa).
func serviceUser(tenant, keyID uuid.UUID) identity.AuthUser {
	return identity.AuthUser{TenantID: tenant, Kind: identity.KindService, ServiceKeyID: keyID, Grants: []identity.Grant{
		{Permission: "catalog.read", Scope: identity.Scope{Type: identity.ScopeAll}},
	}}
}

// webRoute: route thử của website (service_key, quyền đã có trong danh sách quyền để guard cho qua).
func webRoute(mode string) Route {
	return Route{
		Method: http.MethodPost, Pattern: "POST /api/v1/web/_probe", OperationID: "webProbe",
		Permission: "catalog.read", Write: true, IdempotencyMode: mode, IdempotencyReason: "thử", Auth: authServiceKey,
	}
}

// staffWriteRoute: route ghi thử của nhân viên (access).
func staffWriteRoute() Route {
	return Route{
		Method: http.MethodPost, Pattern: "POST /api/v1/_probe", OperationID: "probeWrite",
		Permission: "catalog.read", Write: true, IdempotencyMode: modeRequired, Auth: authAccess,
	}
}

func post(t *testing.T, h http.Handler, path string, body io.Reader, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, body)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// Route service_key chỉ đọc Authorization: Bearer ek_…; thẻ Access hợp lệ không thay được khóa. Route access chỉ đọc
// thẻ Access; khóa dịch vụ trong Authorization bị bỏ qua.
func TestServiceKeyAuthSeparatedFromAccess(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	acc := &fakeAccess{}
	keys := &fakeKeys{user: serviceUser(uuid.New(), uuid.New())}
	d := unitDeps(&buf)
	d.Access = acc

	// Chưa nối bộ kiểm khóa (trước M5-06): mọi route service_key 401, kể cả có thẻ Access.
	h := chain(d, webRoute(modeExempt), okHandler())
	rec := post(t, h, "/api/v1/web/_probe", strings.NewReader("{}"), map[string]string{
		accessHeader: "the-access-hop-le", "Content-Type": "application/json",
	})
	expect(t, "service_key, chỉ có thẻ Access, chưa nối bộ kiểm", rec, http.StatusUnauthorized, codeUnauthenticated)

	d.ServiceKeys = keys
	h = chain(d, webRoute(modeExempt), okHandler())
	rec = post(t, h, "/api/v1/web/_probe", strings.NewReader("{}"), map[string]string{
		accessHeader: "the-access-hop-le", "Content-Type": "application/json",
	})
	expect(t, "service_key, chỉ có thẻ Access", rec, http.StatusUnauthorized, codeUnauthenticated)
	if acc.calls.Load() != 0 || keys.calls.Load() != 0 {
		t.Fatalf("route service_key không được kiểm thẻ Access (gọi %d) hay gọi bộ kiểm khi thiếu khóa (gọi %d)",
			acc.calls.Load(), keys.calls.Load())
	}

	for _, bad := range []string{"Basic " + probeServiceKey, "Bearer abc", "Bearer ", probeServiceKey, "bearer"} {
		rec = post(t, h, "/api/v1/web/_probe", strings.NewReader("{}"), map[string]string{"Authorization": bad})
		expect(t, "Authorization sai dạng "+bad, rec, http.StatusUnauthorized, codeUnauthenticated)
	}
	if keys.calls.Load() != 0 {
		t.Fatalf("header không phải Bearer ek_… không được tới bộ kiểm, gọi %d", keys.calls.Load())
	}

	rec = post(t, h, "/api/v1/web/_probe", strings.NewReader("{}"), map[string]string{"Authorization": "Bearer ek_x_y"})
	expect(t, "khóa sai", rec, http.StatusUnauthorized, codeUnauthenticated)

	rec = post(t, h, "/api/v1/web/_probe", strings.NewReader("{}"), map[string]string{
		"Authorization": "Bearer " + probeServiceKey, "Cf-Connecting-Ip": "10.1.2.3",
	})
	expect(t, "khóa đúng", rec, http.StatusOK, "")
	if keys.lastIP != netip.MustParseAddr("10.1.2.3") || keys.lastBy != probeServiceKey {
		t.Fatalf("bộ kiểm phải nhận khóa và IP của cf-connecting-ip, có %q %v", keys.lastBy, keys.lastIP)
	}
	rec = post(t, h, "/api/v1/web/_probe", strings.NewReader("{}"), map[string]string{
		"Authorization": "Bearer " + probeServiceKey, "Cf-Connecting-Ip": "khong-phai-ip",
	})
	expect(t, "khóa đúng, IP sai dạng (bộ kiểm quyết)", rec, http.StatusOK, "")
	if keys.lastIP.IsValid() {
		t.Fatalf("IP sai dạng phải truyền địa chỉ rỗng, có %v", keys.lastIP)
	}

	// Route access: khóa dịch vụ không thay được thẻ Access.
	staff := chain(d, Route{
		Method: http.MethodGet, Pattern: "GET /api/v1/_probe", OperationID: "probe",
		Permission: "catalog.read", Auth: authAccess,
	}, okHandler())
	before := keys.calls.Load()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/_probe", nil)
	req.Header.Set("Authorization", "Bearer ek_x_y")
	rec = httptest.NewRecorder()
	staff.ServeHTTP(rec, req)
	expect(t, "access, chỉ có khóa dịch vụ", rec, http.StatusUnauthorized, codeUnauthenticated)
	if keys.calls.Load() != before {
		t.Fatal("route access không được gọi bộ kiểm khóa dịch vụ")
	}
}

// Route service_key ghi không qua kiểm Origin (máy chủ gọi máy chủ); route access ghi vẫn 403 bad_origin như M0.
func TestOriginOnlyForNonServiceWrites(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	d := unitDeps(&buf)
	rec := post(t, chain(d, webRoute(modeRequired), okHandler()), "/api/v1/web/_probe", strings.NewReader("{}"), nil)
	expect(t, "service_key ghi không Origin (tới bước auth)", rec, http.StatusUnauthorized, codeUnauthenticated)
	rec = post(t, chain(d, staffWriteRoute(), okHandler()), "/api/v1/_probe", strings.NewReader("{}"),
		map[string]string{"Content-Type": "application/json"})
	expect(t, "access ghi không Origin", rec, http.StatusForbidden, codeBadOrigin)
}

// Cơ chế chống trùng của M0 vẫn đúng sau các bước mới: exempt không đòi khóa, required đòi.
func TestServiceKeyIdempotencyModes(t *testing.T) {
	t.Parallel()
	d, auth := keyDeps(t)
	rec := post(t, chain(d, webRoute(modeExempt), okHandler()), "/api/v1/web/_probe", strings.NewReader("{}"), auth)
	expect(t, "exempt không Idempotency-Key", rec, http.StatusOK, "")
	rec = post(t, chain(d, webRoute(modeRequired), okHandler()), "/api/v1/web/_probe", strings.NewReader("{}"), auth)
	expect(t, "required không Idempotency-Key", rec, http.StatusBadRequest, codeIdempotencyKeyRequired)
}

// keyDeps: Deps không database có bộ kiểm khóa thử, và header mang khóa đúng.
func keyDeps(t *testing.T) (Deps, map[string]string) {
	t.Helper()
	d := unitDeps(&bytes.Buffer{})
	d.ServiceKeys = &fakeKeys{user: serviceUser(uuid.New(), uuid.New())}
	return d, map[string]string{"Authorization": "Bearer " + probeServiceKey}
}

// readAll: handler đọc hết thân; vượt giới hạn → 413 qua writeHandlerError như handler ogen.
func readAll(d Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			writeHandlerError(d, w, r, err)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

// onlyReader ẩn kiểu của bytes.Reader để httptest không biết độ dài (Content-Length -1, thân "chunked").
type onlyReader struct{ io.Reader }

func TestBodyLimitPerRoute(t *testing.T) {
	t.Parallel()
	d, auth := keyDeps(t)
	big := webRoute(modeExempt)
	big.BodyLimit = 6 << 20
	twoMiB := bytes.Repeat([]byte("a"), 2<<20)
	sevenMiB := bytes.Repeat([]byte("a"), 7<<20)
	cases := []struct {
		name string
		rt   Route
		body io.Reader
		want int
	}{
		{"x-body-limit 6 MiB, thân 2 MiB", big, bytes.NewReader(twoMiB), http.StatusOK},
		{"x-body-limit 6 MiB, thân 2 MiB không khai độ dài", big, onlyReader{bytes.NewReader(twoMiB)}, http.StatusOK},
		{"x-body-limit 6 MiB, thân 7 MiB", big, bytes.NewReader(sevenMiB), http.StatusRequestEntityTooLarge},
		{"x-body-limit 6 MiB, thân 7 MiB không khai độ dài", big, onlyReader{bytes.NewReader(sevenMiB)}, http.StatusRequestEntityTooLarge},
		{"mặc định 1 MiB, thân 2 MiB", webRoute(modeExempt), bytes.NewReader(twoMiB), http.StatusRequestEntityTooLarge},
		{"mặc định 1 MiB, thân 2 MiB không khai độ dài", webRoute(modeExempt), onlyReader{bytes.NewReader(twoMiB)}, http.StatusRequestEntityTooLarge},
		{"mặc định 1 MiB, thân đúng 1 MiB", webRoute(modeExempt), bytes.NewReader(bytes.Repeat([]byte("a"), 1<<20)), http.StatusOK},
	}
	for _, c := range cases {
		rec := post(t, chain(d, c.rt, readAll(d)), "/api/v1/web/_probe", c.body, auth)
		code := ""
		if c.want == http.StatusRequestEntityTooLarge {
			code = codeBodyTooLarge
		}
		expect(t, c.name, rec, c.want, code)
	}
}

// Giới hạn tần suất của khóa dịch vụ: RateLimiter từ chối → 429 có Retry-After (làm tròn lên); chưa nối → bộ đếm
// trong tiến trình 600 yêu cầu / phút (không bao giờ bỏ giới hạn).
func TestServiceKeyRateLimit(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	user := serviceUser(uuid.New(), uuid.New())
	d := unitDeps(&buf)
	d.ServiceKeys = &fakeKeys{user: user}
	lim := &fakeWebLimit{}
	d.RateLimiter = lim
	auth := map[string]string{"Authorization": "Bearer " + probeServiceKey}
	h := chain(d, webRoute(modeExempt), okHandler())
	expect(t, "được phép", post(t, h, "/api/v1/web/_probe", strings.NewReader("{}"), auth), http.StatusOK, "")
	lim.deny = true
	rec := post(t, h, "/api/v1/web/_probe", strings.NewReader("{}"), auth)
	expect(t, "bị từ chối", rec, http.StatusTooManyRequests, codeRateLimited)
	if got := rec.Header().Get("Retry-After"); got != "2" {
		t.Errorf("Retry-After phải 2 (1,5 s làm tròn lên), có %q", got)
	}
	if len(lim.subjects) != 2 || lim.subjects[0] != "svc:"+user.ServiceKeyID.String() {
		t.Errorf("chủ thể giới hạn phải svc:<mã>, có %v", lim.subjects)
	}

	// Chưa nối RateLimiter: bộ đếm trong tiến trình.
	d.RateLimiter = nil
	h = chain(d, webRoute(modeExempt), okHandler())
	for i := range defaultServiceRateMax {
		if rec := post(t, h, "/api/v1/web/_probe", strings.NewReader("{}"), auth); rec.Code != http.StatusOK {
			t.Fatalf("yêu cầu %d trong ngưỡng phải 200, có %d", i+1, rec.Code)
		}
	}
	rec = post(t, h, "/api/v1/web/_probe", strings.NewReader("{}"), auth)
	expect(t, "vượt ngưỡng trong tiến trình", rec, http.StatusTooManyRequests, codeRateLimited)
	// Route của nhân viên dùng bộ đếm riêng: khóa dịch vụ hết lượt không làm nhân viên hết lượt (khác bản đồ).
	if d2 := prepare(d); d2.serviceLimiter == d2.limiter {
		t.Fatal("bộ đếm của khóa dịch vụ phải tách khỏi bộ đếm nhân viên")
	}
}

// Cổng nhận vào (x-admission) chạy trước bước chống trùng: cổng từ chối thì không có dòng idempotency_keys; cổng cho
// qua thì handler thấy khóa đã được nhận (thứ tự admission → idempotency → handler). Tên cổng chưa nối → 503.
func TestAdmissionBeforeIdempotency(t *testing.T) {
	t.Parallel()
	staff := testkit.OpenStaff(t)
	tenant := staff.Tenant(t, "A")
	user := staff.User(t, tenant, "svc@elimaz.test", "active")
	var buf bytes.Buffer
	d := testDeps(t, &buf)
	d.DB = staff.App
	d.ServiceKeys = &fakeKeys{user: serviceUser(tenant, user)}
	var sawClaimAtGate atomic.Bool
	reject := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "1")
			writeError(w, r, http.StatusServiceUnavailable, codeRetryLater)
		})
	}
	pass := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, has := claimFrom(r.Context())
			sawClaimAtGate.Store(has)
			next.ServeHTTP(w, r)
		})
	}
	rt := webRoute(modeRequired)
	rt.Admission = "flash"
	hdr := map[string]string{"Authorization": "Bearer " + probeServiceKey, "Idempotency-Key": uuid.NewString()}
	keyRows := func() int {
		var n int
		staff.Exec(t, tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(context.Background(), "SELECT count(*) FROM platform.idempotency_keys").Scan(&n)
		})
		return n
	}

	d.Admission = map[string]func(http.Handler) http.Handler{"flash": reject}
	var reached atomic.Bool
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, has := claimFrom(r.Context())
		reached.Store(has)
		w.WriteHeader(http.StatusOK)
	})
	rec := post(t, chain(d, rt, final), "/api/v1/web/_probe", strings.NewReader("{}"), hdr)
	expect(t, "cổng từ chối", rec, http.StatusServiceUnavailable, codeRetryLater)
	if n := keyRows(); n != 0 || reached.Load() {
		t.Fatalf("cổng từ chối: không được có dòng idempotency_keys (%d) hay tới handler", n)
	}

	d.Admission = map[string]func(http.Handler) http.Handler{"flash": pass}
	rec = post(t, chain(d, rt, final), "/api/v1/web/_probe", strings.NewReader("{}"), hdr)
	expect(t, "cổng cho qua", rec, http.StatusOK, "")
	if sawClaimAtGate.Load() || !reached.Load() {
		t.Fatalf("thứ tự phải cổng → chống trùng → handler: cổng thấy khóa %v, handler thấy khóa %v",
			sawClaimAtGate.Load(), reached.Load())
	}

	d.Admission = nil
	rec = post(t, chain(d, rt, final), "/api/v1/web/_probe", strings.NewReader("{}"), hdr)
	expect(t, "tên cổng chưa nối", rec, http.StatusServiceUnavailable, codeRetryLater)
	if rec.Header().Get("Retry-After") == "" {
		t.Error("503 retry_later phải có Retry-After")
	}
}

// Lỗi của bộ kiểm khóa không nhận ra (database…) đi đường lỗi chung: 500, không lộ khóa vào log.
func TestServiceKeyVerifierErrorNotLogged(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	d := unitDeps(&buf)
	d.ServiceKeys = verifierFunc(func(context.Context, string, netip.Addr) (identity.AuthUser, error) {
		return identity.AuthUser{}, errors.New("hỏng")
	})
	rec := post(t, chain(d, webRoute(modeExempt), okHandler()), "/api/v1/web/_probe", strings.NewReader("{}"),
		map[string]string{"Authorization": "Bearer " + probeServiceKey})
	expect(t, "lỗi bộ kiểm", rec, http.StatusInternalServerError, codeInternal)
	if strings.Contains(buf.String(), "secret") {
		t.Fatalf("log không được chứa khóa dịch vụ:\n%s", buf.String())
	}
}

type verifierFunc func(context.Context, string, netip.Addr) (identity.AuthUser, error)

func (f verifierFunc) Verify(ctx context.Context, bearer string, ip netip.Addr) (identity.AuthUser, error) {
	return f(ctx, bearer, ip)
}
