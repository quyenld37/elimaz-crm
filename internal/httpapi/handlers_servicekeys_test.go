package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Ba operation quản lý khóa dịch vụ (Task 189) và đường khóa dịch vụ thật (bộ kiểm khóa, báo hiệu PostgreSQL thật,
// giới hạn theo khóa, actor chống trùng) trên máy chủ thật, database thật.

var secretPattern = regexp.MustCompile(`^ek_([a-z2-7]{12})_([a-z2-7]{52})$`)

type keyAPIEnv struct {
	*staffAPIEnv

	d   Deps
	hub *realtime.PGHub
}

func newKeyAPIEnv(t *testing.T) *keyAPIEnv {
	t.Helper()
	e := &keyAPIEnv{staffAPIEnv: newStaffAPIEnv(t)}
	e.hub = realtime.NewPGHub(realtime.PGHubOptions{URL: e.staff.App.Pool.Config().ConnString()})
	ctx, cancel := context.WithCancel(context.Background())
	hubDone := make(chan struct{})
	go func() { e.hub.Run(ctx); close(hubDone) }()
	// CacheTTL dài: thu hồi có hiệu lực nhanh chỉ nhờ báo hiệu.
	v := NewServiceKeyVerifier(e.staff.App, e.hub, ServiceKeyOptions{CacheTTL: time.Hour})
	vDone := make(chan struct{})
	go func() { v.Run(ctx); close(vDone) }()
	t.Cleanup(func() { cancel(); <-hubDone; <-vDone })
	deadline := time.Now().Add(10 * time.Second)
	for listening, _ := e.hub.Status(); !listening; listening, _ = e.hub.Status() {
		if time.Now().After(deadline) {
			t.Fatal("hub chưa nghe được")
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.d = testServerDeps(t, e.staff, e.keys, 0)
	e.d.Realtime, e.d.ServiceKeys = e.hub, v
	e.srv = NewServer(e.d)
	return e
}

// create gọi createServiceKey bằng Chủ A; trả mã, thân.
func (e *keyAPIEnv) create(key, body string) (int, map[string]any) {
	e.t.Helper()
	rec := e.do(http.MethodPost, "/api/v1/identity/service-keys", staffA, key, body)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func (e *keyAPIEnv) mustCreate(body string) (id, full string, version float64) {
	e.t.Helper()
	code, out := e.create(uuid.NewString(), body)
	if code != http.StatusCreated {
		e.t.Fatalf("tạo khóa: %d %v", code, out)
	}
	return out["id"].(string), out["secret"].(string), out["version"].(float64) //nolint:forcetypeassert // test
}

// probe gửi yêu cầu tới route thử của website (service_key) cần quyền perm, bằng khóa full, IP ip ("" bỏ header).
func (e *keyAPIEnv) probe(h http.Handler, method, full, ip string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, "/api/v1/web/_probe", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+full)
	if ip != "" {
		req.Header.Set(clientIPHeader, ip)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func webReadRoute(perm string) Route {
	return Route{
		Method: http.MethodGet, Pattern: "GET /api/v1/web/_probe", OperationID: "webProbeRead", Permission: perm, Auth: authServiceKey,
	}
}

// Tạo khóa → 201 kèm khóa đầy đủ một lần; danh sách không có bí mật; gửi lại cùng Idempotency-Key → 409
// already_processed, không bí mật, vẫn một khóa, response_body NULL; bí mật không nằm ở cột nào của bốn bảng. Quyền:
// Quản lý 403; quyền nhân viên cho khóa → 422; tenant B không thấy khóa của A; thu hồi version cũ → 409.
func TestServiceKeysAPI_CreateListRevoke(t *testing.T) {
	t.Parallel()
	e := newKeyAPIEnv(t)
	body := `{"name":"Website","permissions":["web.catalog.read","web.orders.create"],"allowedCidrs":["10.0.0.0/8"]}`
	idem := uuid.NewString()
	code, out := e.create(idem, body)
	if code != http.StatusCreated {
		t.Fatalf("tạo khóa: %d %v", code, out)
	}
	full, _ := out["secret"].(string)
	m := secretPattern.FindStringSubmatch(full)
	if m == nil || out["keyPrefix"] != m[1] || out["name"] != "Website" || out["version"] != float64(1) ||
		fmt.Sprint(out["allowedCidrs"]) != "[10.0.0.0/8]" || fmt.Sprint(out["permissions"]) != "[web.catalog.read web.orders.create]" {
		t.Fatalf("câu trả lời tạo khóa sai: %v", out)
	}
	secret := m[2]

	code, again := e.create(idem, body)
	if code != http.StatusConflict || again["error"] != codeAlreadyProcessed || strings.Contains(fmt.Sprint(again), secret) {
		t.Fatalf("gửi lại cùng khóa chống trùng: %d %v", code, again)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.service_keys`); n != 1 {
		t.Fatalf("gửi lại không được tạo khóa thứ hai: %d", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.idempotency_keys
		WHERE route = 'POST /api/v1/identity/service-keys' AND status = 'done' AND response_body IS NULL`); n != 1 {
		t.Fatalf("dòng chống trùng phải done, response_body NULL: %d", n)
	}
	e.assertSecretNowhere(secret, m[1])

	rec := e.do(http.MethodGet, "/api/v1/identity/service-keys", staffA, "", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), secret) || strings.Contains(rec.Body.String(), `"secret"`) ||
		!strings.Contains(rec.Body.String(), m[1]) {
		t.Fatalf("danh sách: %d %s", rec.Code, rec.Body.String())
	}

	expect(t, "Quản lý không có quyền", e.do(http.MethodGet, "/api/v1/identity/service-keys", staffM, "", ""), http.StatusForbidden, codeForbidden)
	code, out = e.create(uuid.NewString(), `{"name":"X","permissions":["identity.users.manage"]}`)
	if code != http.StatusUnprocessableEntity || out["error"] != codePermissionNotForKey {
		t.Fatalf("quyền nhân viên cho khóa: %d %v", code, out)
	}
	code, out = e.create(uuid.NewString(), `{"name":"X","permissions":["web.catalog.read"],"allowedCidrs":["không phải CIDR"]}`)
	if code != http.StatusBadRequest || out["error"] != codeInvalidInput {
		t.Fatalf("CIDR sai: %d %v", code, out)
	}

	// Tenant B: Chủ của B không thấy khóa của A, không thu hồi được (404).
	b := e.staff.Tenant(t, "B")
	e.staff.Exec(t, b, func(tx pgx.Tx) error { return identity.EnsureDefaultRoles(t.Context(), tx, b) })
	ownerB := e.staff.User(t, b, "chu-b@elimaz.test", "active")
	e.staff.Grant(t, b, ownerB, []string{"identity.service_keys.manage"}, "all", "")
	rec = e.do(http.MethodGet, "/api/v1/identity/service-keys", "chu-b@elimaz.test", "", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("tenant B thấy khóa của A: %d %s", rec.Code, rec.Body.String())
	}
	keyID := e.keyIDByPrefix(m[1])
	expect(t, "tenant B thu hồi khóa của A", e.do(http.MethodPost, "/api/v1/identity/service-keys/"+keyID+"/revoke",
		"chu-b@elimaz.test", uuid.NewString(), `{"version":1}`), http.StatusNotFound, codeNotFound)
	expect(t, "thu hồi version cũ", e.do(http.MethodPost, "/api/v1/identity/service-keys/"+keyID+"/revoke",
		staffA, uuid.NewString(), `{"version":7}`), http.StatusConflict, codeVersionConflict)
	rec = e.do(http.MethodPost, "/api/v1/identity/service-keys/"+keyID+"/revoke", staffA, uuid.NewString(), `{"version":1}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"revokedAt"`) || !strings.Contains(rec.Body.String(), `"version":2`) {
		t.Fatalf("thu hồi: %d %s", rec.Code, rec.Body.String())
	}
}

// assertSecretNowhere quét mọi cột (dòng ép sang text) của bốn bảng: không dòng nào chứa phần bí mật; đối chứng: cùng
// câu quét thấy tiền tố (không bí mật) ở bảng khóa, tuyến và audit.
func (e *keyAPIEnv) assertSecretNowhere(secret, prefix string) {
	e.t.Helper()
	const scan = `SELECT count(*) FROM %s AS t WHERE t::text LIKE '%%' || $1 || '%%'`
	for _, tbl := range []string{"identity.service_keys", "identity.audit_logs", "platform.idempotency_keys", "platform.tenant_routes"} {
		if n := e.staff.CountIn(e.t, e.tenant, fmt.Sprintf(scan, tbl), secret); n != 0 {
			e.t.Errorf("%s có %d dòng chứa bí mật của khóa", tbl, n)
		}
	}
	for _, tbl := range []string{"identity.service_keys", "identity.audit_logs", "platform.tenant_routes"} {
		if n := e.staff.CountIn(e.t, e.tenant, fmt.Sprintf(scan, tbl), prefix); n == 0 {
			e.t.Errorf("câu quét không thấy tiền tố trong %s: câu quét hỏng", tbl)
		}
	}
}

func (e *keyAPIEnv) keyIDByPrefix(prefix string) string {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT id FROM identity.service_keys WHERE key_prefix = $1`, prefix).Scan(&id)
	})
	return id.String()
}

// Đường khóa dịch vụ thật: khóa đúng 200; sai một ký tự, tiền tố lạ 401; quyền không cấp 403; CIDR: IP ngoài 403
// ip_not_allowed, IP trong 200; thu hồi qua API → yêu cầu kế tiếp 401 trong ≤ 1 s (báo hiệu PostgreSQL thật, đệm 1 giờ).
func TestServiceKeyRoute_VerifyAndRevoke(t *testing.T) {
	// Không t.Parallel (Task 474): tiêu chí giờ thật (thu hồi ≤ 1 s), máy bận vì test song song làm đỏ sai.
	e := newKeyAPIEnv(t)
	id, full, version := e.mustCreate(`{"name":"Web","permissions":["web.catalog.read"]}`)
	read := chain(e.d, webReadRoute("web.catalog.read"), okHandler())
	expect(t, "khóa đúng", e.probe(read, http.MethodGet, full, "", nil), http.StatusOK, "")
	wrong := []byte(full)
	wrong[len(wrong)-1] ^= 1
	expect(t, "sai một ký tự", e.probe(read, http.MethodGet, string(wrong), "", nil), http.StatusUnauthorized, codeUnauthenticated)
	expect(t, "tiền tố lạ", e.probe(read, http.MethodGet, "ek_aaaaaaaaaaaa_"+full[16:], "", nil), http.StatusUnauthorized, codeUnauthenticated)
	needCreate := chain(e.d, webReadRoute("web.orders.create"), okHandler())
	expect(t, "quyền không cấp", e.probe(needCreate, http.MethodGet, full, "", nil), http.StatusForbidden, codeForbidden)

	_, cidrKey, _ := e.mustCreate(`{"name":"Web IP","permissions":["web.catalog.read"],"allowedCidrs":["10.0.0.0/8"]}`)
	expect(t, "IP ngoài CIDR", e.probe(read, http.MethodGet, cidrKey, "1.2.3.4", nil), http.StatusForbidden, codeIPNotAllowed)
	expect(t, "thiếu IP", e.probe(read, http.MethodGet, cidrKey, "", nil), http.StatusForbidden, codeIPNotAllowed)
	expect(t, "IP trong CIDR", e.probe(read, http.MethodGet, cidrKey, "10.1.2.3", nil), http.StatusOK, "")

	rec := e.do(http.MethodPost, "/api/v1/identity/service-keys/"+id+"/revoke", staffA, uuid.NewString(), fmt.Sprintf(`{"version":%v}`, version))
	if rec.Code != http.StatusOK {
		t.Fatalf("thu hồi: %d %s", rec.Code, rec.Body.String())
	}
	start := time.Now()
	for e.probe(read, http.MethodGet, full, "", nil).Code != http.StatusUnauthorized {
		if time.Since(start) > time.Second {
			t.Fatal("thu hồi phải có hiệu lực trong ≤ 1 s nhờ báo hiệu")
		}
		time.Sleep(10 * time.Millisecond)
	}
	expect(t, "khóa đã thu hồi", e.probe(read, http.MethodGet, full, "", nil), http.StatusUnauthorized, codeUnauthenticated)
	expect(t, "khóa khác không bị ảnh hưởng", e.probe(read, http.MethodGet, cidrKey, "10.1.2.3", nil), http.StatusOK, "")
}

// Giới hạn theo khóa qua cache.RateLimiter thật (Valkey không nối → bộ đếm trong tiến trình, ngưỡng thử 5/s): yêu cầu
// 6 → 429 có Retry-After; khóa khác vẫn 200 (chủ thể svc:<ServiceKeyID>).
func TestServiceKeyRoute_RateLimitPerKey(t *testing.T) {
	t.Parallel()
	e := newKeyAPIEnv(t)
	_, k1, _ := e.mustCreate(`{"name":"Web 1","permissions":["web.catalog.read"]}`)
	_, k2, _ := e.mustCreate(`{"name":"Web 2","permissions":["web.catalog.read"]}`)
	clk := newFakeClock()
	d := e.d
	d.RateLimiter = NewWebKeyLimiter(cache.NewRateLimiter(nil, clk.Now, nil),
		cache.Limit{Name: "web_key", Max: 5, Window: time.Second, FallbackMax: 5})
	h := chain(d, webReadRoute("web.catalog.read"), okHandler())
	for i := range 5 {
		expect(t, fmt.Sprintf("yêu cầu %d", i+1), e.probe(h, http.MethodGet, k1, "", nil), http.StatusOK, "")
	}
	rec := e.probe(h, http.MethodGet, k1, "", nil)
	expect(t, "yêu cầu 6", rec, http.StatusTooManyRequests, codeRateLimited)
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("429 phải có Retry-After")
	}
	expect(t, "khóa khác", e.probe(h, http.MethodGet, k2, "", nil), http.StatusOK, "")
}

// Actor của khóa chống trùng là svc:<ServiceKeyID> (nhân viên: user:<UserID>): cùng Idempotency-Key của khóa A, khóa B
// là hai dòng độc lập, lần gửi lại của cùng khóa phát lại.
func TestServiceKeyRoute_IdempotencyActor(t *testing.T) {
	t.Parallel()
	e := newKeyAPIEnv(t)
	idA, kA, _ := e.mustCreate(`{"name":"Web A","permissions":["web.orders.create"]}`)
	idB, kB, _ := e.mustCreate(`{"name":"Web B","permissions":["web.orders.create"]}`)
	rt := Route{
		Method: http.MethodPost, Pattern: "POST /api/v1/web/_probe", OperationID: "webProbeWrite", Permission: "web.orders.create",
		Write: true, IdempotencyMode: modeRequired, Auth: authServiceKey,
	}
	calls := 0
	h := chain(e.d, rt, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := authUser(r.Context())
		calls++
		if err := WriteTx(r.Context(), e.d, u.TenantID, func(pgx.Tx) (int, any, bool, error) {
			return http.StatusOK, map[string]string{"ok": "1"}, true, nil
		}); err != nil {
			writeHandlerError(e.d, w, r, err)
		}
	}))
	key := uuid.NewString()
	for _, full := range []string{kA, kB, kA} {
		rec := e.probe(h, http.MethodPost, full, "", map[string]string{"Idempotency-Key": key, "Content-Type": "application/json"})
		if rec.Code != http.StatusOK {
			t.Fatalf("ghi thử: %d %s", rec.Code, rec.Body.String())
		}
	}
	if calls != 2 {
		t.Fatalf("khóa A gửi lại phải phát lại (handler chạy 2 lần), chạy %d", calls)
	}
	for _, id := range []string{idA, idB} {
		if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.idempotency_keys WHERE actor = $1 AND key = $2`, "svc:"+id, key); n != 1 {
			t.Errorf("actor svc:%s: %d dòng", id, n)
		}
	}
	if got := idempotencyActor(identity.AuthUser{UserID: e.ids[staffA], Kind: identity.KindStaff}); got != "user:"+e.ids[staffA].String() {
		t.Errorf("actor nhân viên: %q", got)
	}
}

// Lệnh tạo khóa ghi tuyến service_key qua tenancy.PutRoute: Resolve tìm ra đúng tenant.
func TestServiceKeysAPI_RouteResolves(t *testing.T) {
	t.Parallel()
	e := newKeyAPIEnv(t)
	_, full, _ := e.mustCreate(`{"name":"Web","permissions":["web.catalog.read"]}`)
	prefix, _ := identity.SplitServiceKey(full)
	got, ok, err := tenancy.Resolve(t.Context(), e.staff.App, identity.RouteKindServiceKey, prefix)
	if err != nil || !ok || got != e.tenant {
		t.Fatalf("tuyến: %v %v %v", got, ok, err)
	}
}
