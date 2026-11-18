package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/cfaccess"
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
	keys  *testkit.AccessKeys
	mux   *http.ServeMux
	logs  *bytes.Buffer
}

func newAuthEnv(t *testing.T, rl RateLimit) *authEnv {
	t.Helper()
	e := &authEnv{t: t, staff: testkit.OpenStaff(t), keys: testkit.AccessKeyServer(t), logs: &bytes.Buffer{}}
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
	return e.keys.Sign(testkit.AccessClaims{Email: email, AUD: testAUD, MFA: []string{"mfa"}})
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
	e := newAuthEnv(t, RateLimit{})
	a := e.staff.Tenant(t, "A")
	uid := e.staff.User(t, a, "lan@elimaz.test", "active")
	e.staff.Grant(t, a, uid, []string{"audit.read"}, "all", "")

	expect(t, "thẻ hợp lệ có MFA, grant all", e.get("/api/v1/_probe", e.token("lan@elimaz.test")), 200, "")
	k := e.keys
	c := testkit.AccessClaims{Email: "lan@elimaz.test", AUD: testAUD, MFA: []string{"mfa"}}
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
	// Không có realtime.subscribe → 403.
	other := e.staff.User(t, a, "khac@elimaz.test", "active")
	e.staff.Grant(t, a, other, []string{"audit.read"}, "all", "")
	req = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Cf-Access-Jwt-Assertion", e.token("khac@elimaz.test"))
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	expect(t, "thiếu realtime.subscribe", rec, 403, "forbidden")
}
