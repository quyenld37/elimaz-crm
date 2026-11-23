package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/testkit"
)

// staffAPIEnv: máy chủ thật trên database thật; tenant có vai trò mẫu, Chủ A, Chủ B, Quản lý M (có thêm
// identity.users.manage) và Kho K (warehouse:w1).
type staffAPIEnv struct {
	t      *testing.T
	staff  *testkit.Staff
	keys   *cfaccesstest.AccessKeys
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
	e := &staffAPIEnv{t: t, staff: testkit.OpenStaff(t), keys: cfaccesstest.AccessKeyServer(t), ids: map[string]uuid.UUID{}}
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
	t.Parallel()
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
// Task 315 lượt 2: gán vai trò có quyền quản trị (owner) theo một kho không lỗi; thân trả lời và listStaff kèm cảnh
// báo "quyền quản trị chỉ hiệu lực khi gán toàn shop"; người được gán không quản lý được nhân viên. Gán toàn shop
// không cảnh báo.
func TestStaffAPI_AdminRoleScopedWarning(t *testing.T) {
	t.Parallel()
	e := newStaffAPIEnv(t)
	k := e.ids[staffK]
	rec := e.do(http.MethodPut, usersPath(k, "grants"), staffA, "w1", grantsBody(e.version(k), "owner@warehouse:w1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("A gán owner theo kho w1 cho K: %d %s", rec.Code, rec.Body.String())
	}
	type warning struct {
		Code, Message, RoleKey, ScopeType, ScopeID string
		Permissions                                []string
	}
	decode := func(raw []byte) []warning {
		var body struct {
			Warnings []struct {
				Code        string   `json:"code"`
				Message     string   `json:"message"`
				RoleKey     string   `json:"roleKey"`
				ScopeType   string   `json:"scopeType"`
				ScopeID     string   `json:"scopeId"`
				Permissions []string `json:"permissions"`
			} `json:"warnings"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		out := make([]warning, 0, len(body.Warnings))
		for _, w := range body.Warnings {
			out = append(out, warning{w.Code, w.Message, w.RoleKey, w.ScopeType, w.ScopeID, w.Permissions})
		}
		return out
	}
	check := func(name string, ws []warning) {
		t.Helper()
		if len(ws) != 1 {
			t.Fatalf("%s: mong 1 cảnh báo, có %+v", name, ws)
		}
		w := ws[0]
		if w.Code != "admin_needs_shop_scope" || w.Message != "Quyền quản trị chỉ hiệu lực khi gán toàn shop" ||
			w.RoleKey != "owner" || w.ScopeType != "warehouse" || w.ScopeID != "w1" ||
			!slices.Contains(w.Permissions, "identity.users.manage") || slices.Contains(w.Permissions, "catalog.read") {
			t.Fatalf("%s: cảnh báo sai %+v", name, w)
		}
	}
	check("thân replaceStaffGrants", decode(rec.Body.Bytes()))
	raw, err := json.Marshal(e.member(k))
	if err != nil {
		t.Fatal(err)
	}
	check("listStaff", decode(raw))
	expect(t, "K (owner theo kho) liệt kê nhân viên", e.do(http.MethodGet, "/api/v1/identity/users", staffK, "", ""),
		http.StatusForbidden, "forbidden")
	rec = e.do(http.MethodPut, usersPath(k, "grants"), staffA, "w2", grantsBody(e.version(k), "owner@all"))
	if rec.Code != http.StatusOK || len(decode(rec.Body.Bytes())) != 0 {
		t.Fatalf("gán owner toàn shop: mong 200 không cảnh báo, có %d %s", rec.Code, rec.Body.String())
	}
	expect(t, "K (owner toàn shop) liệt kê nhân viên", e.do(http.MethodGet, "/api/v1/identity/users", staffK, "", ""), http.StatusOK, "")
}

func TestStaffAPI_ReplaceGrantsIdempotent(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	for err, want := range map[error][2]any{
		identity.ErrSelfModify:        {http.StatusForbidden, "cannot_modify_self"},
		identity.ErrGrantExceedsActor: {http.StatusForbidden, "grant_exceeds_actor"},
		identity.ErrLocked:            {http.StatusForbidden, "forbidden"},
		identity.ErrLastOwner:         {http.StatusConflict, "last_owner"},
		identity.ErrEmailTaken:        {http.StatusConflict, "email_taken"},
		identity.ErrVersionConflict:   {http.StatusConflict, "version_conflict"},
		identity.ErrStaffNotFound:     {http.StatusNotFound, "not_found"},
		identity.ErrInvalidInput:      {http.StatusBadRequest, "invalid_input"},
		identity.ErrOwnerRoleLocked:   {http.StatusConflict, "owner_role_locked"},
		identity.ErrRoleInUse:         {http.StatusConflict, "role_in_use"},
		identity.ErrRoleArchived:      {http.StatusConflict, "role_archived"},
		identity.ErrRoleNameTaken:     {http.StatusConflict, "role_name_taken"},
		identity.ErrUnknownPermission: {http.StatusUnprocessableEntity, "unknown_permission"},
		identity.ErrRoleNotFound:      {http.StatusNotFound, "not_found"},
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

// Vai trò tự định nghĩa (kế hoạch M1, Task M1-56).

func roleBody(version int64, name string, perms ...string) string {
	b, _ := json.Marshal(map[string]any{"version": version, "name": name, "permissions": append([]string{}, perms...)}) //nolint:errchkjson // test
	return string(b)
}

func createRoleBody(name string, perms ...string) string {
	b, _ := json.Marshal(map[string]any{"name": name, "permissions": append([]string{}, perms...)}) //nolint:errchkjson // test
	return string(b)
}

func decodeObj(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("thân không phải JSON object: %s", rec.Body.String())
	}
	return m
}

func (e *staffAPIEnv) roleByKey(key string) map[string]any {
	e.t.Helper()
	rec := e.do(http.MethodGet, "/api/v1/identity/roles", staffA, "", "")
	var roles []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &roles); err != nil || rec.Code != http.StatusOK {
		e.t.Fatalf("listRoles: %d %s", rec.Code, rec.Body.String())
	}
	for _, r := range roles {
		if r["key"] == key {
			return r
		}
	}
	e.t.Fatalf("không có vai trò %s", key)
	return nil
}

func rolePath(id any, action string) string {
	p := fmt.Sprintf("/api/v1/identity/roles/%v", id)
	if action != "" {
		p += "/" + action
	}
	return p
}

func TestRolesAPI_CreateUpdateArchive(t *testing.T) {
	t.Parallel()
	e := newStaffAPIEnv(t)
	e.staff.Grant(t, e.tenant, e.ids[staffM], []string{"identity.roles.manage"}, "all", "")

	expect(t, "M tạo vai trò có platform.files.manage", e.do(http.MethodPost, "/api/v1/identity/roles", staffM, "r1",
		createRoleBody("Tệp", "platform.files.manage")), http.StatusForbidden, "grant_exceeds_actor")
	rec := e.do(http.MethodPost, "/api/v1/identity/roles", staffM, "r2", createRoleBody("Kho phụ", "catalog.read", "inventory.read", "inventory.stocktake"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("M tạo Kho phụ: %d %s", rec.Code, rec.Body.String())
	}
	r := decodeObj(t, rec)
	if !strings.HasPrefix(r["key"].(string), "custom_") || r["customized"] != true || r["version"] != float64(1) || //nolint:forcetypeassert // test
		r["status"] != "active" || len(r["permissions"].([]any)) != 3 || r["holderCount"] != float64(0) { //nolint:forcetypeassert // test
		t.Fatalf("vai trò mới: %v", r)
	}
	id := r["id"]
	expect(t, "K tạo vai trò", e.do(http.MethodPost, "/api/v1/identity/roles", staffK, "r3", createRoleBody("K", "catalog.read")), http.StatusForbidden, "forbidden")
	expect(t, "quyền lạ", e.do(http.MethodPost, "/api/v1/identity/roles", staffA, "r4", createRoleBody("Lạ", "foo.bar")), http.StatusUnprocessableEntity, "unknown_permission")
	expect(t, "trùng tên", e.do(http.MethodPost, "/api/v1/identity/roles", staffA, "r5", createRoleBody("kho phụ", "catalog.read")), http.StatusConflict, "role_name_taken")
	expect(t, "tên rỗng", e.do(http.MethodPost, "/api/v1/identity/roles", staffA, "r6", createRoleBody(" ", "catalog.read")), http.StatusBadRequest, "")

	owner := e.roleByKey("owner")
	expect(t, "sửa owner", e.do(http.MethodPut, rolePath(owner["id"], ""), staffA, "r7", roleBody(1, "Chủ", "catalog.read")), http.StatusConflict, "owner_role_locked")
	expect(t, "lưu trữ owner", e.do(http.MethodPost, rolePath(owner["id"], "archive"), staffA, "r8", `{"version":1}`), http.StatusConflict, "owner_role_locked")

	expect(t, "version cũ", e.do(http.MethodPut, rolePath(id, ""), staffA, "r9", roleBody(9, "Kho phụ", "catalog.read")), http.StatusConflict, "version_conflict")
	rec = e.do(http.MethodPut, rolePath(id, ""), staffM, "r10", roleBody(1, "Kho phụ", "catalog.read", "inventory.read"))
	if rec.Code != http.StatusOK || decodeObj(t, rec)["version"] != float64(2) {
		t.Fatalf("M gỡ inventory.stocktake: %d %s", rec.Code, rec.Body.String())
	}
	expect(t, "vai trò không có", e.do(http.MethodPut, rolePath(uuid.New(), ""), staffA, "r11", roleBody(1, "x", "catalog.read")), http.StatusNotFound, "not_found")

	// Gán cho K rồi lưu trữ → role_in_use; audit có dòng cho mỗi lần ghi vai trò.
	k := e.ids[staffK]
	if rec := e.do(http.MethodPut, usersPath(k, "grants"), staffA, "r12", grantsBody(e.version(k), "warehouse@warehouse:w1", r["key"].(string)+"@warehouse:w1")); rec.Code != http.StatusOK { //nolint:forcetypeassert // test
		t.Fatalf("gán Kho phụ cho K: %d %s", rec.Code, rec.Body.String())
	}
	if got := e.roleByKey(r["key"].(string)); got["holderCount"] != float64(1) { //nolint:forcetypeassert // test
		t.Fatalf("người giữ: %v", got)
	}
	expect(t, "lưu trữ khi còn người giữ", e.do(http.MethodPost, rolePath(id, "archive"), staffA, "r13", `{"version":2}`), http.StatusConflict, "role_in_use")
	if rec := e.do(http.MethodPut, usersPath(k, "grants"), staffA, "r14", grantsBody(e.version(k), "warehouse@warehouse:w1")); rec.Code != http.StatusOK {
		t.Fatalf("gỡ Kho phụ của K: %d %s", rec.Code, rec.Body.String())
	}
	rec = e.do(http.MethodPost, rolePath(id, "archive"), staffA, "r15", `{"version":2}`)
	if rec.Code != http.StatusOK || decodeObj(t, rec)["status"] != "archived" {
		t.Fatalf("lưu trữ: %d %s", rec.Code, rec.Body.String())
	}
	expect(t, "gán vai trò đã lưu trữ", e.do(http.MethodPut, usersPath(k, "grants"), staffA, "r16",
		grantsBody(e.version(k), "warehouse@warehouse:w1", r["key"].(string)+"@all")), http.StatusConflict, "role_archived") //nolint:forcetypeassert // test
	roleID, _ := uuid.Parse(id.(string)) //nolint:forcetypeassert // test
	if n := e.audits(roleID); n != 3 {
		t.Fatalf("audit của vai trò: %d dòng, mong 3 (tạo, sửa, lưu trữ)", n)
	}
}

// 50 lần cùng Idempotency-Key → một vai trò, một dòng audit.
func TestRolesAPI_CreateIdempotent(t *testing.T) {
	t.Parallel()
	e := newStaffAPIEnv(t)
	body := createRoleBody("Thời vụ Tết", "catalog.read")
	recs := make([]*httptest.ResponseRecorder, 50)
	var wg sync.WaitGroup
	for i := range recs {
		wg.Go(func() { recs[i] = e.do(http.MethodPost, "/api/v1/identity/roles", staffA, "same-role-key", body) })
	}
	wg.Wait()
	ok := 0
	for _, rec := range recs {
		switch rec.Code {
		case http.StatusCreated:
			ok++
		case http.StatusConflict:
			expect(t, "song song", rec, http.StatusConflict, "request_in_progress")
		default:
			t.Errorf("mã lạ %d %s", rec.Code, rec.Body.String())
		}
	}
	roles := e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM identity.roles WHERE name = 'Thời vụ Tết'")
	audits := e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM identity.audit_logs WHERE action = 'identity.role.create'")
	if ok == 0 || roles != 1 || audits != 1 {
		t.Fatalf("%d câu 201, %d vai trò, %d audit; mong ≥ 1, 1, 1", ok, roles, audits)
	}
}

// Danh sách quyền có nhãn; vai trò tự tạo gán cho nhân viên → nhân viên gọi được đúng operation của quyền trong vai trò.
func TestRolesAPI_PermissionsAndEffect(t *testing.T) {
	t.Parallel()
	e := newStaffAPIEnv(t)
	expect(t, "K đọc danh sách quyền", e.do(http.MethodGet, "/api/v1/identity/permissions", staffK, "", ""), http.StatusForbidden, "forbidden")
	rec := e.do(http.MethodGet, "/api/v1/identity/permissions", staffA, "", "")
	var list []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || rec.Code != http.StatusOK || len(list) != len(identity.Permissions) {
		t.Fatalf("listPermissions: %d %s", rec.Code, rec.Body.String())
	}
	for i, p := range list {
		if !identity.IsKnown(identity.Permission(p["key"].(string))) || p["label"] == "" || p["group"] == "" || p["scopeType"] == "" { //nolint:forcetypeassert // test
			t.Errorf("quyền %d: %v", i, p)
		}
	}
	rec = e.do(http.MethodPost, "/api/v1/identity/roles", staffA, "c1", createRoleBody("Soạn vai trò", "identity.roles.manage", "catalog.read"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("tạo vai trò: %d %s", rec.Code, rec.Body.String())
	}
	key := decodeObj(t, rec)["key"].(string) //nolint:forcetypeassert // test
	x := e.staff.User(t, e.tenant, "x@elimaz.test", "active")
	if rec := e.do(http.MethodPut, usersPath(x, "grants"), staffA, "c2", grantsBody(e.version(x), key+"@all")); rec.Code != http.StatusOK {
		t.Fatalf("gán vai trò mới cho X: %d %s", rec.Code, rec.Body.String())
	}
	expect(t, "X đọc danh sách quyền", e.do(http.MethodGet, "/api/v1/identity/permissions", "x@elimaz.test", "", ""), http.StatusOK, "")
	expect(t, "X liệt kê nhân viên", e.do(http.MethodGet, "/api/v1/identity/users", "x@elimaz.test", "", ""), http.StatusForbidden, "forbidden")
	// Lượt 2: X chỉ có identity.roles.manage (không có identity.users.manage) vẫn mở được danh sách vai trò của màn Vai
	// trò; đường của màn Nhân viên (listRoles) vẫn đòi identity.users.manage; K thiếu cả hai → 403.
	rec = e.do(http.MethodGet, "/api/v1/identity/roles/manage", "x@elimaz.test", "", "")
	var roles []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &roles); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("X liệt kê vai trò để quản lý: %d %s", rec.Code, rec.Body.String())
	}
	if !slices.ContainsFunc(roles, func(r map[string]any) bool { return r["key"] == key && r["holderCount"] == float64(1) }) ||
		!slices.ContainsFunc(roles, func(r map[string]any) bool { return r["key"] == "owner" }) {
		t.Fatalf("danh sách vai trò thiếu vai trò mới (1 người giữ) hoặc Chủ: %v", roles)
	}
	expect(t, "X liệt kê vai trò của màn Nhân viên", e.do(http.MethodGet, "/api/v1/identity/roles", "x@elimaz.test", "", ""), http.StatusForbidden, "forbidden")
	expect(t, "K liệt kê vai trò để quản lý", e.do(http.MethodGet, "/api/v1/identity/roles/manage", staffK, "", ""), http.StatusForbidden, "forbidden")
}
