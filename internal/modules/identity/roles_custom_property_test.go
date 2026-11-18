package identity_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/testkit"
)

// Quy luật (rapid, 200 lượt; Task M1-56 nối bộ của Task M1-26): xen kẽ ngẫu nhiên gán, gỡ, khóa, mở, tạo vai trò,
// sửa vai trò, lưu trữ vai trò → (1) luôn còn ít nhất một Chủ hoạt động; (2) mỗi lần thành công, mọi quyền của mọi
// grant được thêm hay gỡ nằm trong quyền của người gán lúc gán, và mọi quyền được thêm vào hay gỡ khỏi vai trò nằm
// trong quyền phạm vi all của người sửa lúc sửa; (3) vai trò owner luôn đủ mọi quyền.
func TestRoleInvariantsProperty(t *testing.T) {
	e := &staffEnv{t: t, s: testkit.OpenStaff(t), ids: map[string]uuid.UUID{}}
	rapid.Check(t, func(rt *rapid.T) {
		run := newPropRun(e, rt)
		// "hr" được thêm quyền quản lý vai trò để có người sửa vai trò không phải Chủ.
		e.s.Exec(t, e.tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), `INSERT INTO identity.role_permissions (tenant_id, role_id, permission)
				SELECT tenant_id, id, 'identity.roles.manage' FROM identity.roles WHERE key = 'hr'`)
			return err //nolint:wrapcheck // test
		})
		run.refreshRoles()
		for range rapid.IntRange(1, 12).Draw(rt, "steps") {
			run.roleStep(rt)
		}
	})
}

func (r *propRun) refreshRoles() {
	e := r.e
	r.rolePerms = map[string][]identity.Permission{}
	r.roleIDs = map[string]identity.RoleRow{}
	e.s.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		rows, err := identity.ListRoles(e.t.Context(), tx)
		for _, row := range rows {
			r.rolePerms[row.Key] = row.Permissions
			r.roleIDs[row.Key] = row
		}
		return err //nolint:wrapcheck // test
	})
}

func genPerms() *rapid.Generator[[]identity.Permission] {
	keys := make([]identity.Permission, 0, len(identity.Permissions))
	for _, d := range identity.Permissions {
		keys = append(keys, d.Key)
	}
	return rapid.SliceOfNDistinct(rapid.SampledFrom(keys), 0, 6, func(p identity.Permission) identity.Permission { return p })
}

// roleStep: thao tác của Task M1-26 (step) hoặc tạo, sửa, lưu trữ vai trò của một người ngẫu nhiên, rồi kiểm quy luật.
func (r *propRun) roleStep(rt *rapid.T) {
	op := rapid.IntRange(0, 3).Draw(rt, "roleOp")
	if op == 0 {
		r.refreshRoles()
		r.step(rt)
		r.refreshRoles()
		r.checkOwnerFull(rt)
		return
	}
	e := r.e
	ai := rapid.IntRange(0, len(r.ids)-1).Draw(rt, "actor")
	actor, err := identity.LoadUser(e.t.Context(), e.s.App, r.emails[ai])
	if errors.Is(err, identity.ErrLocked) {
		return
	}
	if err != nil {
		rt.Fatalf("nạp người thao tác: %v", err)
	}
	keys := make([]string, 0, len(r.roleIDs))
	for k := range r.roleIDs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	target := r.roleIDs[rapid.SampledFrom(keys).Draw(rt, "role")]
	want := genPerms().Draw(rt, "perms")
	switch op {
	case 1:
		_, err = e.createRole(actor, "R "+uuid.NewString()[:8], permStrings(want)...)
		if err == nil {
			r.checkRoleAuthority(rt, actor, nil, want)
		}
	case 2:
		_, err = e.updateRole(actor, target.ID, target.Version, target.Name, permStrings(want)...)
		if err == nil {
			r.checkRoleAuthority(rt, actor, target.Permissions, want)
		}
	default:
		err = e.archiveRole(actor, target.ID, target.Version)
	}
	if err != nil && !isRoleRefusal(err) {
		rt.Fatalf("lỗi lạ: %v", err)
	}
	r.refreshRoles()
	r.checkOwnerFull(rt)
	if c := e.activeOwners(e.tenant); c < 1 {
		rt.Fatalf("không còn Chủ hoạt động")
	}
}

func permStrings(ps []identity.Permission) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, string(p))
	}
	return out
}

// checkRoleAuthority: mọi quyền được thêm vào hay gỡ khỏi vai trò nằm trong quyền phạm vi all của actor.
func (r *propRun) checkRoleAuthority(rt *rapid.T, actor identity.AuthUser, before, after []identity.Permission) {
	all := identity.Scope{Type: identity.ScopeAll}
	if !identity.Can(actor, "identity.roles.manage", nil) {
		rt.Fatalf("%s sửa vai trò mà không có identity.roles.manage", actor.Email)
	}
	for _, p := range append(slices.Clone(before), after...) {
		if slices.Contains(before, p) == slices.Contains(after, p) {
			continue
		}
		if !identity.Can(actor, p, &all) {
			rt.Fatalf("%s thêm/gỡ %s mà không có quyền đó ở toàn shop", actor.Email, p)
		}
	}
}

func (r *propRun) checkOwnerFull(rt *rapid.T) {
	if got := r.rolePerms["owner"]; len(got) != len(identity.Permissions) {
		rt.Fatalf("owner có %d quyền, mong %d", len(got), len(identity.Permissions))
	}
}

func isRoleRefusal(err error) bool {
	for _, want := range []error{
		identity.ErrGrantExceedsActor, identity.ErrOwnerRoleLocked, identity.ErrRoleInUse, identity.ErrRoleArchived,
		identity.ErrVersionConflict, identity.ErrLocked,
	} {
		if errors.Is(err, want) {
			return true
		}
	}
	return false
}
