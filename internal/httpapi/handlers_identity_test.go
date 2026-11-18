package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/testkit"
)

// staffAPIEnv: máy chủ thật trên database thật; tenant có vai trò mẫu, Chủ A, Chủ B, Quản lý M (có thêm
// identity.users.manage) và Kho K (warehouse:w1).
type staffAPIEnv struct {
	t      *testing.T
	staff  *testkit.Staff
	keys   *testkit.AccessKeys
	srv    http.Handler
	tenant uuid.UUID
	ids    map[string]uuid.UUID
}

const (
	staffA = "a@elimaz.test"
	staffB = "b@elimaz.test"
	staffM = "m@elimaz.test"
	staffK = "k@elimaz.test"
)

func newStaffAPIEnv(t *testing.T) *staffAPIEnv {
	t.Helper()
	e := &staffAPIEnv{t: t, staff: testkit.OpenStaff(t), keys: testkit.AccessKeyServer(t), ids: map[string]uuid.UUID{}}
	e.tenant = e.staff.Tenant(t, "A")
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error { return identity.EnsureDefaultRoles(t.Context(), tx, e.tenant) })
	for email, role := range map[string][3]string{
		staffA: {"owner", "all", ""}, staffB: {"owner", "all", ""}, staffM: {"manager", "all", ""}, staffK: {"warehouse", "warehouse", "w1"},
	} {
		id := e.staff.User(t, e.tenant, email, "active")
		e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), `INSERT INTO identity.user_role_grants (tenant_id, user_id, role_id, scope_type, scope_id)
				SELECT $1, $2, id, $4, $5 FROM identity.roles WHERE key = $3`, e.tenant, id, role[0], role[1], role[2])
			return err //nolint:wrapcheck // test
		})
		e.ids[email] = id
	}
	e.staff.Grant(t, e.tenant, e.ids[staffM], []string{"identity.users.manage"}, "all", "")
	e.srv = testServer(t, e.staff, e.keys, 0)
	return e
}

func (e *staffAPIEnv) do(method, path, email, key, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	return serveAs(e.srv, e.keys, method, path, email, key, body)
}

func (e *staffAPIEnv) member(id uuid.UUID) map[string]any {
	e.t.Helper()
	rec := e.do(http.MethodGet, "/api/v1/identity/users", staffA, "", "")
	if rec.Code != http.StatusOK {
		e.t.Fatalf("listStaff: %d %s", rec.Code, rec.Body.String())
	}
	var all []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		e.t.Fatal(err)
	}
	for _, m := range all {
		if m["id"] == id.String() {
			return m
		}
	}
	e.t.Fatalf("không thấy %s trong listStaff", id)
	return nil
}

func (e *staffAPIEnv) version(id uuid.UUID) int64 {
	return int64(e.member(id)["version"].(float64)) //nolint:forcetypeassert // test
}

func grantsBody(version int64, grants ...string) string {
	parts := make([]string, 0, len(grants))
	for _, g := range grants {
		role, scope, _ := strings.Cut(g, "@")
		typ, id, _ := strings.Cut(scope, ":")
		parts = append(parts, fmt.Sprintf(`{"roleKey":%q,"scopeType":%q,"scopeId":%q}`, role, typ, id))
	}
	return fmt.Sprintf(`{"version":%d,"grants":[%s]}`, version, strings.Join(parts, ","))
}

func (e *staffAPIEnv) audits(entity uuid.UUID) int {
	e.t.Helper()
	var n int
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM identity.audit_logs WHERE entity_id = $1", entity.String()).Scan(&n)
	})
	return n
}

func usersPath(id uuid.UUID, action string) string {
	return "/api/v1/identity/users/" + id.String() + "/" + action
}

func TestStaffAPI_ReplaceGrants(t *testing.T) {
	e := newStaffAPIEnv(t)
	k := e.ids[staffK]
	v := e.version(k)
	expect(t, "M gán owner cho K", e.do(http.MethodPut, usersPath(k, "grants"), staffM, "g1", grantsBody(v, "owner@all")),
		http.StatusForbidden, "grant_exceeds_actor")
	rec := e.do(http.MethodPut, usersPath(k, "grants"), staffM, "g2", grantsBody(v, "warehouse@warehouse:w1", "sale@warehouse:w1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("M gán warehouse cho K: %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["version"] != float64(v+1) || len(body["grants"].([]any)) != 2 { //nolint:forcetypeassert // test
		t.Fatalf("thân sau khi gán: %v", body)
	}
	if strings.Contains(rec.Body.String(), "request_id") {
		t.Fatalf("thân thành công lạ: %s", rec.Body.String())
	}
	expect(t, "version cũ", e.do(http.MethodPut, usersPath(k, "grants"), staffA, "g3", grantsBody(v)), http.StatusConflict, "version_conflict")
	expect(t, "vai trò lạ", e.do(http.MethodPut, usersPath(k, "grants"), staffA, "g4", grantsBody(v+1, "khong-co@all")),
		http.StatusBadRequest, "invalid_input")
	expect(t, "không có", e.do(http.MethodPut, usersPath(uuid.New(), "grants"), staffA, "g5", grantsBody(1)), http.StatusNotFound, "not_found")
	expect(t, "A sửa A", e.do(http.MethodPut, usersPath(e.ids[staffA], "grants"), staffA, "g6", grantsBody(1, "owner@all")),
		http.StatusForbidden, "cannot_modify_self")
	expect(t, "M gỡ owner của A", e.do(http.MethodPut, usersPath(e.ids[staffA], "grants"), staffM, "g7", grantsBody(e.version(e.ids[staffA]))),
		http.StatusForbidden, "grant_exceeds_actor")
	if n := e.audits(k); n != 1 {
		t.Fatalf("mong 1 dòng audit cho K (chỉ lần thành công), có %d", n)
	}
}

// 50 yêu cầu song song cùng Idempotency-Key → một lần ghi, một dòng audit; mỗi câu trả lời 200 hoặc 409
// request_in_progress.
func TestStaffAPI_ReplaceGrantsIdempotent(t *testing.T) {
	e := newStaffAPIEnv(t)
	k := e.ids[staffK]
	body := grantsBody(e.version(k), "sale@all")
	recs := make([]*httptest.ResponseRecorder, 50)
	var wg sync.WaitGroup
	for i := range recs {
		wg.Go(func() { recs[i] = e.do(http.MethodPut, usersPath(k, "grants"), staffA, "same-key", body) })
	}
	wg.Wait()
	ok := 0
	for _, rec := range recs {
		switch rec.Code {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			expect(t, "song song", rec, http.StatusConflict, "request_in_progress")
		default:
			t.Errorf("mã lạ %d %s", rec.Code, rec.Body.String())
		}
	}
	if ok == 0 || e.audits(k) != 1 {
		t.Fatalf("%d câu 200, %d dòng audit; mong ≥ 1 và đúng 1", ok, e.audits(k))
	}
}

func TestStaffAPI_LockAndCreate(t *testing.T) {
	e := newStaffAPIEnv(t)
	a, b := e.ids[staffA], e.ids[staffB]
	expect(t, "A khóa A", e.do(http.MethodPost, usersPath(a, "lock"), staffA, "l1", "{}"), http.StatusForbidden, "cannot_modify_self")
	expect(t, "M khóa A", e.do(http.MethodPost, usersPath(a, "lock"), staffM, "l2", "{}"), http.StatusForbidden, "grant_exceeds_actor")
	rec := e.do(http.MethodPost, usersPath(b, "lock"), staffA, "l3", "{}")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"locked"`) {
		t.Fatalf("A khóa B: %d %s", rec.Code, rec.Body.String())
	}
	expect(t, "B đã bị khóa", e.do(http.MethodGet, "/api/v1/identity/users", staffB, "", ""), http.StatusForbidden, "forbidden")
	if rec := e.do(http.MethodPost, usersPath(b, "unlock"), staffA, "l4", "{}"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"active"`) {
		t.Fatalf("A mở B: %d %s", rec.Code, rec.Body.String())
	}

	// Tạo nhân viên: 201; email đã có tuyến ở tenant khác → 409 email_taken.
	other := e.staff.Tenant(t, "B")
	e.staff.User(t, other, "taken@elimaz.test", "active")
	rec = e.do(http.MethodPost, "/api/v1/identity/users", staffA, "c1", `{"email":"Moi@Elimaz.test","displayName":"Mới"}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"email":"moi@elimaz.test"`) ||
		!strings.Contains(rec.Body.String(), `"grants":[]`) {
		t.Fatalf("createStaff: %d %s", rec.Code, rec.Body.String())
	}
	expect(t, "email trùng", e.do(http.MethodPost, "/api/v1/identity/users", staffA, "c2", `{"email":"taken@elimaz.test","displayName":"x"}`),
		http.StatusConflict, "email_taken")
	expect(t, "email sai", e.do(http.MethodPost, "/api/v1/identity/users", staffA, "c3", `{"email":"x","displayName":"x"}`),
		http.StatusBadRequest, "")
	// Nhân viên mới chưa có grant: /me 200 với grants [] (Task 315: chỉ cần đăng nhập), mọi route khác 403 (PQ03).
	if rec := e.do(http.MethodGet, "/api/v1/me", "moi@elimaz.test", "", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"grants":[]`) {
		t.Errorf("nhân viên mới /me: %d %s", rec.Code, rec.Body.String())
	}
	for _, p := range []string{"/api/v1/identity/users", "/api/v1/identity/roles", "/api/v1/platform/jobs"} {
		expect(t, "nhân viên mới "+p, e.do(http.MethodGet, p, "moi@elimaz.test", "", ""), http.StatusForbidden, "forbidden")
	}
}

func TestStaffAPI_Permissions(t *testing.T) {
	e := newStaffAPIEnv(t)
	k := e.ids[staffK]
	// K không có identity.users.manage: mọi operation 403.
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/identity/users", ""},
		{http.MethodGet, "/api/v1/identity/roles", ""},
		{http.MethodPost, "/api/v1/identity/users", `{"email":"z@elimaz.test","displayName":"z"}`},
		{http.MethodPut, usersPath(e.ids[staffM], "grants"), grantsBody(1)},
		{http.MethodPost, usersPath(e.ids[staffM], "lock"), "{}"},
		{http.MethodPost, usersPath(e.ids[staffM], "unlock"), "{}"},
	} {
		expect(t, "K "+c.method+" "+c.path, e.do(c.method, c.path, staffK, "p-"+uuid.NewString(), c.body), http.StatusForbidden, "forbidden")
	}
	rec := e.do(http.MethodGet, "/api/v1/identity/roles", staffA, "", "")
	var roles []map[string]any
	// Năm vai trò mẫu, cộng vai trò phụ testkit.Grant dựng để cấp identity.users.manage cho M.
	if err := json.Unmarshal(rec.Body.Bytes(), &roles); err != nil || rec.Code != http.StatusOK || len(roles) != len(identity.DefaultRoles)+1 {
		t.Fatalf("listRoles: %d %s", rec.Code, rec.Body.String())
	}
	for i, r := range identity.DefaultRoles {
		if roles[i]["key"] != r.Key || len(roles[i]["permissions"].([]any)) != len(r.Permissions) { //nolint:forcetypeassert // test
			t.Errorf("vai trò %d: %v, mong %s", i, roles[i], r.Key)
		}
	}
	m := e.member(k)
	if m["email"] != staffK || m["status"] != "active" {
		t.Fatalf("listStaff dòng K: %v", m)
	}
}

// Mã lỗi của các sentinel identity (một chỗ: errors.go), kể cả last_owner chỉ gặp khi hai Chủ tranh nhau.
func TestStaffErrorCodes(t *testing.T) {
	for err, want := range map[error][2]any{
		identity.ErrSelfModify:        {http.StatusForbidden, "cannot_modify_self"},
		identity.ErrGrantExceedsActor: {http.StatusForbidden, "grant_exceeds_actor"},
		identity.ErrLocked:            {http.StatusForbidden, "forbidden"},
		identity.ErrLastOwner:         {http.StatusConflict, "last_owner"},
		identity.ErrEmailTaken:        {http.StatusConflict, "email_taken"},
		identity.ErrVersionConflict:   {http.StatusConflict, "version_conflict"},
		identity.ErrStaffNotFound:     {http.StatusNotFound, "not_found"},
		identity.ErrInvalidInput:      {http.StatusBadRequest, "invalid_input"},
	} {
		var buf bytes.Buffer
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x", nil)
		requestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeHandlerError(unitDeps(&buf), w, r, fmt.Errorf("việc: %w", err))
		})).ServeHTTP(rec, req)
		expect(t, err.Error(), rec, want[0].(int), want[1].(string)) //nolint:forcetypeassert // test
	}
}
