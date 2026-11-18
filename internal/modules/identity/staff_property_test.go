package identity_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/testkit"
)

// Quy luật (rapid, 200 lượt, PQ07): chuỗi ngẫu nhiên các lần gán, gỡ, khóa, mở của 2–5 nhân viên →
// (1) luôn còn ít nhất một Chủ hoạt động; (2) mỗi lần thay grant thành công, mọi quyền của mọi grant được thêm hay
// gỡ đều nằm trong quyền của người gán lúc gán; (3) không ai sửa được chính mình.
func TestStaffInvariantsProperty(t *testing.T) {
	e := &staffEnv{t: t, s: testkit.OpenStaff(t), ids: map[string]uuid.UUID{}}
	rapid.Check(t, func(rt *rapid.T) {
		run := newPropRun(e, rt)
		for range rapid.IntRange(1, 12).Draw(rt, "steps") {
			run.step(rt)
		}
	})
}

// propRun là một lượt rapid: tenant mới, nhân viên 0 là Chủ, người khác có một vai trò ngẫu nhiên.
type propRun struct {
	e         *staffEnv
	emails    []string
	ids       []uuid.UUID
	rolePerms map[string][]identity.Permission
}

func genGrant() *rapid.Generator[identity.GrantInput] {
	return rapid.Custom(func(rt *rapid.T) identity.GrantInput {
		key := rapid.SampledFrom([]string{"owner", "manager", "sale", "warehouse", "accountant", "hr"}).Draw(rt, "role")
		scope := rapid.SampledFrom([]identity.Scope{
			{Type: identity.ScopeAll}, {Type: identity.ScopeWarehouse, ID: "w1"}, {Type: identity.ScopeWarehouse, ID: "w2"},
		}).Draw(rt, "scope")
		return identity.GrantInput{RoleKey: key, Scope: scope}
	})
}

func newPropRun(e *staffEnv, rt *rapid.T) *propRun {
	t, s := e.t, e.s
	e.tenant = s.Tenant(t, "P")
	e.seedTenant(e.tenant)
	// Vai trò "hr": được quản lý nhân viên nhưng ít quyền khác, để có người thao tác không phải Chủ.
	s.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `WITH r AS (INSERT INTO identity.roles (tenant_id, key, name) VALUES ($1, 'hr', 'HR') RETURNING id)
			INSERT INTO identity.role_permissions (tenant_id, role_id, permission)
			SELECT $1, r.id, p FROM r, unnest(ARRAY['identity.users.manage', 'catalog.read', 'realtime.subscribe']) AS p`, e.tenant)
		return err //nolint:wrapcheck // test
	})
	run := &propRun{e: e, rolePerms: map[string][]identity.Permission{}}
	for i := range rapid.IntRange(2, 5).Draw(rt, "people") {
		gr := identity.GrantInput{RoleKey: "owner", Scope: identity.Scope{Type: identity.ScopeAll}}
		if i > 0 {
			gr = genGrant().Draw(rt, "initial")
		}
		email := uuid.NewString() + "@elimaz.test"
		run.emails = append(run.emails, email)
		run.ids = append(run.ids, e.user(e.tenant, email, gr.RoleKey, gr.Scope.Type, gr.Scope.ID))
	}
	s.Exec(t, e.tenant, func(tx pgx.Tx) error {
		rows, err := identity.ListRoles(t.Context(), tx)
		for _, r := range rows {
			run.rolePerms[r.Key] = r.Permissions
		}
		return err //nolint:wrapcheck // test
	})
	return run
}

// step: một thao tác ngẫu nhiên (thay grant, khóa, mở) của một người lên một người, rồi kiểm ba quy luật.
func (r *propRun) step(rt *rapid.T) {
	e := r.e
	ai := rapid.IntRange(0, len(r.ids)-1).Draw(rt, "actor")
	ti := rapid.IntRange(0, len(r.ids)-1).Draw(rt, "target")
	actor, err := identity.LoadUser(e.t.Context(), e.s.App, r.emails[ai])
	if errors.Is(err, identity.ErrLocked) {
		return // lớp HTTP chặn người bị khóa trước khi tới module
	}
	if err != nil {
		rt.Fatalf("nạp người thao tác: %v", err)
	}
	before := e.staff(r.ids[ti])
	switch rapid.IntRange(0, 2).Draw(rt, "op") {
	case 0:
		grants := rapid.SliceOfN(genGrant(), 0, 3).Draw(rt, "grants")
		err = e.app(func(tx pgx.Tx) error {
			_, err := identity.ReplaceGrants(e.t.Context(), tx, actor, r.ids[ti], before.Version, grants)
			return err //nolint:wrapcheck // test
		})
		if err == nil {
			r.checkAuthority(rt, actor, before.Grants, e.staff(r.ids[ti]).Grants)
		}
	case 1:
		err = e.lock(actor, r.ids[ti], true)
	default:
		err = e.lock(actor, r.ids[ti], false)
	}
	switch {
	case err == nil && ai == ti:
		rt.Fatalf("%s tự sửa chính mình", r.emails[ai])
	case err != nil && !isRefusal(err):
		rt.Fatalf("lỗi lạ: %v", err)
	}
	if c := e.activeOwners(e.tenant); c < 1 {
		rt.Fatalf("không còn Chủ hoạt động sau bước của %s lên %s", r.emails[ai], r.emails[ti])
	}
}

// checkAuthority: mọi quyền của mọi grant được thêm hay gỡ nằm trong quyền của actor lúc gán.
func (r *propRun) checkAuthority(rt *rapid.T, actor identity.AuthUser, before, after []identity.GrantInput) {
	for _, gr := range identity.SymmetricDiff(before, after) {
		for _, p := range r.rolePerms[gr.RoleKey] {
			if !identity.Can(actor, p, &gr.Scope) {
				rt.Fatalf("%s gán/gỡ %v mà thiếu %s", actor.Email, gr, p)
			}
		}
	}
}

func isRefusal(err error) bool {
	for _, want := range []error{identity.ErrSelfModify, identity.ErrGrantExceedsActor, identity.ErrLastOwner, identity.ErrLocked} {
		if errors.Is(err, want) {
			return true
		}
	}
	return false
}
