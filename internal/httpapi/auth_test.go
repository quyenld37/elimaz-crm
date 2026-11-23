package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/cfaccess"
	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/testkit"
)

const testAUD = "aud-crm"

// authEnv: database thật có nhân viên, máy chủ khóa Access thử, và mux có hai route thử của kế hoạch:
// GET /api/v1/_probe (audit.read) và GET /api/v1/_probe/page/{id} (conversation.read, kiểm đích page:{id}).
type authEnv struct {
	t     *testing.T
	staff *testkit.Staff
	keys  *cfaccesstest.AccessKeys
	mux   *http.ServeMux
	logs  *bytes.Buffer
}

func newAuthEnv(t *testing.T, rl RateLimit) *authEnv {
	t.Helper()
	e := &authEnv{t: t, staff: testkit.OpenStaff(t), keys: cfaccesstest.AccessKeyServer(t), logs: &bytes.Buffer{}}
	v := e.verifier()
	d := prepare(Deps{
		Config: config.Config{ConsoleOrigins: []string{testOrigin}}, DB: e.staff.App,
		Logger: slog.New(slog.NewJSONHandler(e.logs, nil)), Access: v, RateLimit: rl,
	})
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	e.mux = http.NewServeMux()
	e.mux.Handle("GET /api/v1/_probe", chain(d, Route{Method: "GET", Pattern: "GET /api/v1/_probe", OperationID: "probe", Permission: "audit.read"}, ok))
	e.mux.Handle("GET /api/v1/_probe/page/{id}", chain(d,
		Route{Method: "GET", Pattern: "GET /api/v1/_probe/page/{id}", OperationID: "probePage", Permission: "conversation.read"},
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, _ := authUser(r.Context())
			if !identity.Can(u, "conversation.read", &identity.Scope{Type: identity.ScopePage, ID: r.PathValue("id")}) {
				writeError(w, r, http.StatusForbidden, codeForbidden)
				return
			}
			w.WriteHeader(http.StatusOK)
		})))
	// Task 315: quyền không theo đích (catalog.read) và quyền theo kho (inventory.documents.create, kiểm đích ở handler).
	e.mux.Handle("GET /api/v1/_probe/catalog", chain(d, Route{Method: "GET", Pattern: "GET /api/v1/_probe/catalog", OperationID: "probeCatalog", Permission: "catalog.read"}, ok))
	e.mux.Handle("GET /api/v1/_probe/warehouse/{id}", chain(d,
		Route{Method: "GET", Pattern: "GET /api/v1/_probe/warehouse/{id}", OperationID: "probeWarehouse", Permission: "inventory.documents.create"},
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, _ := authUser(r.Context())
			if !identity.Can(u, "inventory.documents.create", &identity.Scope{Type: identity.ScopeWarehouse, ID: r.PathValue("id")}) {
				writeError(w, r, http.StatusForbidden, codeForbidden)
				return
			}
			w.WriteHeader(http.StatusOK)
		})))
	e.mux.Handle("GET /api/v1/_probe/stream", chain(d,
		Route{Method: "GET", Pattern: "GET /api/v1/_probe/stream", OperationID: "probeStream", Permission: "audit.read", Stream: true}, ok))
	return e
}

// verifier: bộ kiểm thẻ thật (cfaccess) trỏ tới máy chủ khóa thử.
func (e *authEnv) verifier() *cfaccess.Verifier {
	e.t.Helper()
	return e.keys.Verifier(e.t, testAUD)
}

// get gửi GET path với thẻ tok trong header cf-access-jwt-assertion (rỗng: không header).
func (e *authEnv) get(path, tok string, mod ...func(*http.Request)) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequestWithContext(e.t.Context(), http.MethodGet, path, nil)
	if tok != "" {
		req.Header.Set("Cf-Access-Jwt-Assertion", tok)
	}
	for _, m := range mod {
		m(req)
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	return rec
}

func (e *authEnv) token(email string) string {
	return e.keys.Sign(cfaccesstest.AccessClaims{Email: email, AUD: testAUD, MFA: []string{"mfa"}})
}

func expect(t *testing.T, name string, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Errorf("%s: mã %d, mong %d (%s)", name, rec.Code, status, rec.Body.String())
		return
	}
	if code != "" {
		if b := decodeErr(t, rec); b.Error != code {
			t.Errorf("%s: lỗi %q, mong %q", name, b.Error, code)
		}
	}
}

func TestAuthTokens(t *testing.T) {
	t.Parallel()
	e := newAuthEnv(t, RateLimit{})
	a := e.staff.Tenant(t, "A")
	uid := e.staff.User(t, a, "lan@elimaz.test", "active")
	e.staff.Grant(t, a, uid, []string{"audit.read"}, "all", "")

	expect(t, "thẻ hợp lệ có MFA, grant all", e.get("/api/v1/_probe", e.token("lan@elimaz.test")), 200, "")
	k := e.keys
	c := cfaccesstest.AccessClaims{Email: "lan@elimaz.test", AUD: testAUD, MFA: []string{"mfa"}}
	wrongAud, wrongIss, expired := c, c, c
	wrongAud.AUD = "khac"
	wrongIss.Iss = "https://khac.cloudflareaccess.com"
	expired.Exp = -time.Minute
	for name, tok := range map[string]string{
		"không thẻ": "", "sai aud": k.Sign(wrongAud), "sai iss": k.Sign(wrongIss),
		"hết hạn": k.Sign(expired), "khóa khác": k.SignForeign(c),
	} {
		expect(t, name, e.get("/api/v1/_probe", tok), 401, "unauthenticated")
	}
	noMFA, badMFA := c, c
	noMFA.MFA = nil
	badMFA.MFA = []string{"pwd"}
	expect(t, "thiếu MFA", e.get("/api/v1/_probe", k.Sign(noMFA)), 401, "mfa_required")
	expect(t, "MFA không cho phép", e.get("/api/v1/_probe", k.Sign(badMFA)), 401, "mfa_required")

	// Thẻ hợp lệ chỉ nằm trong cookie CF_Authorization → 401.
	expect(t, "chỉ cookie", e.get("/api/v1/_probe", "", func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: "CF_Authorization", Value: e.token("lan@elimaz.test"), Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	}), 401, "unauthenticated")
	// Thẻ trong Authorization: Bearer cũng không được đọc.
	expect(t, "chỉ Authorization", e.get("/api/v1/_probe", "", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+e.token("lan@elimaz.test"))
	}), 401, "unauthenticated")

	// Log không có thẻ, không có email.
	if strings.Contains(e.logs.String(), "lan@elimaz.test") || strings.Contains(e.logs.String(), "eyJ") {
		t.Fatalf("log chứa email hoặc thẻ: %s", e.logs.String())
	}
}

func TestAuthUsersAndGrants(t *testing.T) {
	t.Parallel()
	e := newAuthEnv(t, RateLimit{})
	a := e.staff.Tenant(t, "A")
	lan := e.staff.User(t, a, "lan@elimaz.test", "active")
	e.staff.Grant(t, a, lan, []string{"audit.read"}, "all", "")
	khoa := e.staff.User(t, a, "khoa@elimaz.test", "locked")
	e.staff.Grant(t, a, khoa, []string{"audit.read"}, "all", "")
	page := e.staff.User(t, a, "page@elimaz.test", "active")
	e.staff.Grant(t, a, page, []string{"audit.read", "conversation.read"}, "page", "p1")
	e.staff.User(t, a, "moi@elimaz.test", "active") // nhân viên mới, chưa có dòng user_role_grants

	expect(t, "email không có định tuyến", e.get("/api/v1/_probe", e.token("la@elimaz.test")), 403, "forbidden")
	expect(t, "user locked", e.get("/api/v1/_probe", e.token("khoa@elimaz.test")), 403, "forbidden")
	// Task 315 lượt 2: audit.read là quyền quản trị (toàn shop), grant theo page không có hiệu lực.
	expect(t, "audit.read chỉ trên page:p1", e.get("/api/v1/_probe", e.token("page@elimaz.test")), 403, "forbidden")
	for _, p := range []string{"/api/v1/_probe", "/api/v1/_probe/page/p1", "/api/v1/_probe/stream"} {
		expect(t, "không grant "+p, e.get(p, e.token("moi@elimaz.test")), 403, "forbidden")
	}
	expect(t, "conversation.read page:p1 → /page/p1", e.get("/api/v1/_probe/page/p1", e.token("page@elimaz.test")), 200, "")
	expect(t, "conversation.read page:p1 → /page/p2", e.get("/api/v1/_probe/page/p2", e.token("page@elimaz.test")), 403, "forbidden")

	// Khóa user giữa hai yêu cầu: yêu cầu thứ hai 403 (trạng thái đọc ở mỗi yêu cầu).
	expect(t, "trước khi khóa", e.get("/api/v1/_probe", e.token("lan@elimaz.test")), 200, "")
	e.staff.SetStatus(t, a, lan, "locked")
	expect(t, "sau khi khóa", e.get("/api/v1/_probe", e.token("lan@elimaz.test")), 403, "forbidden")
}

// Deps không có Access (quên nối) → mọi route không công khai 401, không mở cửa.
func TestAuthWithoutVerifierDenies(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	h := chain(unitDeps(&buf), Route{Method: "GET", Pattern: "GET /api/v1/x", OperationID: "x", Permission: "audit.read"}, okHandler())
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/x", nil)
	req.Header.Set("Cf-Access-Jwt-Assertion", "abc")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	expect(t, "không Access", rec, 401, "unauthenticated")
}

// GET /api/v1/me qua NewServer: hợp đồng → handler mỏng → identity.
func TestGetMe(t *testing.T) {
	t.Parallel()
	e := newAuthEnv(t, RateLimit{})
	a := e.staff.Tenant(t, "A")
	uid := e.staff.User(t, a, "me@elimaz.test", "active")
	e.staff.Grant(t, a, uid, []string{"realtime.subscribe"}, "all", "")
	e.staff.Grant(t, a, uid, []string{"conversation.read"}, "page", "p1")
	v := e.verifier()
	srv := NewServer(Deps{Config: config.Config{}, DB: e.staff.App, Logger: slog.New(slog.NewJSONHandler(e.logs, nil)), Access: v})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Cf-Access-Jwt-Assertion", e.token("me@elimaz.test"))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/me: %d %s", rec.Code, rec.Body.String())
	}
	var me struct {
		UserID string `json:"userId"`
		Email  string `json:"email"`
		Grants []struct {
			Permission string `json:"permission"`
			Scope      struct{ Type, ID string }
		} `json:"grants"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.UserID != uid.String() || me.Email != "me@elimaz.test" || len(me.Grants) != 2 {
		t.Fatalf("Me sai: %s", rec.Body.String())
	}
	// Task 315: /me chỉ cần đăng nhập và tài khoản không bị khóa.
	other := e.staff.User(t, a, "khac@elimaz.test", "active")
	e.staff.Grant(t, a, other, []string{"audit.read"}, "all", "")
	e.staff.User(t, a, "moi@elimaz.test", "active") // chưa có grant
	locked := e.staff.User(t, a, "khoa@elimaz.test", "locked")
	e.staff.Grant(t, a, locked, []string{"realtime.subscribe"}, "all", "")
	get := func(email string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/me", nil)
		req.Header.Set("Cf-Access-Jwt-Assertion", e.token(email))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}
	expect(t, "thiếu realtime.subscribe", get("khac@elimaz.test"), 200, "")
	if rec := get("moi@elimaz.test"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"grants":[]`) {
		t.Errorf("chưa có grant: mong 200 grants [], có %d %s", rec.Code, rec.Body.String())
	}
	expect(t, "tài khoản bị khóa", get("khoa@elimaz.test"), 403, "forbidden")
	expect(t, "email không có định tuyến", get("la@elimaz.test"), 403, "forbidden")
}

// newAuthEnvDefaultRoles như newAuthEnv (không giới hạn tần suất), thêm tenant A có đủ vai trò mẫu; trả env và tenant.
func newAuthEnvDefaultRoles(t *testing.T) (*authEnv, uuid.UUID) {
	t.Helper()
	e := newAuthEnv(t, RateLimit{})
	a := e.staff.Tenant(t, "A")
	e.staff.Exec(t, a, func(tx pgx.Tx) error { return identity.EnsureDefaultRoles(t.Context(), tx, a) })
	return e, a
}

// Task 315: nhân viên chỉ có vai trò mẫu Kho gán theo kho A dùng được quyền không theo đích của vai trò (đọc sản phẩm,
// /me) và quyền theo kho ở đúng kho A; kho B 403. Người không có gán nào: mọi route có quyền 403.
func TestWarehouseScopedRole(t *testing.T) {
	t.Parallel()
	e, a := newAuthEnvDefaultRoles(t)
	k := e.staff.User(t, a, "kho@elimaz.test", "active")
	e.staff.Exec(t, a, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO identity.user_role_grants (tenant_id, user_id, role_id, scope_type, scope_id)
			SELECT $1, $2, id, 'warehouse', 'kho-a' FROM identity.roles WHERE key = 'warehouse'`, a, k)
		return err //nolint:wrapcheck // test
	})
	e.staff.User(t, a, "moi@elimaz.test", "active")
	srv := NewServer(Deps{Config: config.Config{}, DB: e.staff.App, Logger: slog.New(slog.NewJSONHandler(e.logs, nil)), Access: e.verifier()})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Cf-Access-Jwt-Assertion", e.token("kho@elimaz.test"))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	expect(t, "Kho theo kho A → /me", rec, 200, "")
	kho := e.token("kho@elimaz.test")
	expect(t, "Kho theo kho A → đọc sản phẩm", e.get("/api/v1/_probe/catalog", kho), 200, "")
	expect(t, "Kho theo kho A → phiếu kho A", e.get("/api/v1/_probe/warehouse/kho-a", kho), 200, "")
	expect(t, "Kho theo kho A → phiếu kho B", e.get("/api/v1/_probe/warehouse/kho-b", kho), 403, "forbidden")
	expect(t, "Kho không có audit.read", e.get("/api/v1/_probe", kho), 403, "forbidden")
	moi := e.token("moi@elimaz.test")
	for _, p := range []string{"/api/v1/_probe", "/api/v1/_probe/catalog", "/api/v1/_probe/warehouse/kho-a"} {
		expect(t, "không gán "+p, e.get(p, moi), 403, "forbidden")
	}
}

// Task 315 lượt 2: quyền quản trị chỉ hiệu lực từ gán toàn shop. Chủ gán theo kho A: không quản lý được nhân viên
// (403), không xem nhật ký (403), vẫn đọc sản phẩm và ghi phiếu kho A (200); /me không liệt kê quyền quản trị không
// hiệu lực mà trả kèm cảnh báo. Chủ gán toàn shop quản lý được (200), /me không cảnh báo.
func TestOwnerScopedToWarehouse(t *testing.T) {
	t.Parallel()
	e, a := newAuthEnvDefaultRoles(t)
	for email, scope := range map[string][2]string{"chukho@elimaz.test": {"warehouse", "kho-a"}, "chu@elimaz.test": {"all", ""}} {
		uid := e.staff.User(t, a, email, "active")
		e.staff.Exec(t, a, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), `INSERT INTO identity.user_role_grants (tenant_id, user_id, role_id, scope_type, scope_id)
				SELECT $1, $2, id, $3, $4 FROM identity.roles WHERE key = 'owner'`, a, uid, scope[0], scope[1])
			return err //nolint:wrapcheck // test
		})
	}
	srv := NewServer(Deps{Config: config.Config{}, DB: e.staff.App, Logger: slog.New(slog.NewJSONHandler(e.logs, nil)), Access: e.verifier()})
	serve := func(path, email string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		req.Header.Set("Cf-Access-Jwt-Assertion", e.token(email))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}
	chuKho, chu := e.token("chukho@elimaz.test"), e.token("chu@elimaz.test")
	expect(t, "Chủ theo kho A → quản lý nhân viên", serve("/api/v1/identity/users", "chukho@elimaz.test"), 403, "forbidden")
	expect(t, "Chủ theo kho A → việc nền", serve("/api/v1/platform/jobs", "chukho@elimaz.test"), 403, "forbidden")
	expect(t, "Chủ theo kho A → nhật ký", e.get("/api/v1/_probe", chuKho), 403, "forbidden")
	expect(t, "Chủ theo kho A → đọc sản phẩm", e.get("/api/v1/_probe/catalog", chuKho), 200, "")
	expect(t, "Chủ theo kho A → phiếu kho A", e.get("/api/v1/_probe/warehouse/kho-a", chuKho), 200, "")
	expect(t, "Chủ theo kho A → phiếu kho B", e.get("/api/v1/_probe/warehouse/kho-b", chuKho), 403, "forbidden")
	expect(t, "Chủ toàn shop → quản lý nhân viên", serve("/api/v1/identity/users", "chu@elimaz.test"), 200, "")
	expect(t, "Chủ toàn shop → nhật ký", e.get("/api/v1/_probe", chu), 200, "")

	type grantJSON struct {
		Permission string `json:"permission"`
	}
	type meBody struct {
		Grants   []grantJSON `json:"grants"`
		Warnings []struct {
			Code        string   `json:"code"`
			Message     string   `json:"message"`
			ScopeType   string   `json:"scopeType"`
			ScopeID     string   `json:"scopeId"`
			Permissions []string `json:"permissions"`
		} `json:"warnings"`
	}
	me := func(email string) meBody {
		rec := serve("/api/v1/me", email)
		if rec.Code != http.StatusOK {
			t.Fatalf("/me của %s: %d %s", email, rec.Code, rec.Body.String())
		}
		var b meBody
		if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
			t.Fatal(err)
		}
		return b
	}
	mk := me("chukho@elimaz.test")
	for _, g := range mk.Grants {
		if k := identity.KindOf(identity.Permission(g.Permission)); k == identity.ScopeShop {
			t.Errorf("/me của Chủ theo kho liệt kê quyền quản trị không hiệu lực %s", g.Permission)
		}
	}
	if len(mk.Grants) == 0 || len(mk.Warnings) != 1 {
		t.Fatalf("/me của Chủ theo kho: mong grant thường và đúng 1 cảnh báo, có %+v", mk)
	}
	w := mk.Warnings[0]
	if w.Code != "admin_needs_shop_scope" || w.Message != "Quyền quản trị chỉ hiệu lực khi gán toàn shop" ||
		w.ScopeType != "warehouse" || w.ScopeID != "kho-a" || !slices.Contains(w.Permissions, "identity.users.manage") ||
		slices.Contains(w.Permissions, "catalog.read") {
		t.Fatalf("cảnh báo sai: %+v", w)
	}
	if mc := me("chu@elimaz.test"); len(mc.Warnings) != 0 || !slices.Contains(mc.Grants, grantJSON{"identity.users.manage"}) {
		t.Fatalf("/me của Chủ toàn shop: mong có identity.users.manage, không cảnh báo, có %+v", mc)
	}
}
