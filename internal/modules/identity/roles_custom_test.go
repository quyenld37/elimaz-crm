package identity_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
)

// Vai trò tự định nghĩa (kế hoạch M1, Task M1-56; Q35). Bộ dữ liệu như Task M1-26: Chủ A, Chủ B, Quản lý M (phạm vi
// all) và Kho K (warehouse:w1); M được cấp thêm identity.roles.manage phạm vi all ở các ca cần.

func perms(ps ...string) []identity.Permission {
	out := make([]identity.Permission, 0, len(ps))
	for _, p := range ps {
		out = append(out, identity.Permission(p))
	}
	return out
}

func (e *staffEnv) createRole(actor identity.AuthUser, name string, ps ...string) (identity.RoleRow, error) {
	var r identity.RoleRow
	err := e.app(func(tx pgx.Tx) error {
		var err error
		r, err = identity.CreateRole(context.Background(), tx, actor, identity.RoleInput{Name: name, Permissions: perms(ps...)})
		return err //nolint:wrapcheck // test
	})
	return r, err
}

func (e *staffEnv) updateRole(actor identity.AuthUser, id uuid.UUID, version int64, name string, ps ...string) (identity.RoleChange, error) {
	var c identity.RoleChange
	err := e.app(func(tx pgx.Tx) error {
		var err error
		c, err = identity.UpdateRole(context.Background(), tx, actor, id, version, identity.RoleInput{Name: name, Permissions: perms(ps...)})
		return err //nolint:wrapcheck // test
	})
	return c, err
}

func (e *staffEnv) archiveRole(actor identity.AuthUser, id uuid.UUID, version int64) error {
	return e.app(func(tx pgx.Tx) error {
		_, err := identity.ArchiveRole(context.Background(), tx, actor, id, version)
		return err //nolint:wrapcheck // test
	})
}

// roleByKey đọc vai trò theo khóa (crm_app, ListRoles).
func (e *staffEnv) roleByKey(key string) identity.RoleRow {
	e.t.Helper()
	var rows []identity.RoleRow
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		rows, err = identity.ListRoles(context.Background(), tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	i := slices.IndexFunc(rows, func(r identity.RoleRow) bool { return r.Key == key })
	if i < 0 {
		e.t.Fatalf("không có vai trò %s", key)
	}
	return rows[i]
}

func (e *staffEnv) withRolesManager() identity.AuthUser {
	e.t.Helper()
	e.s.Grant(e.t, e.tenant, e.ids[emailM], []string{"identity.roles.manage"}, identity.ScopeAll, "")
	return e.actor(emailM)
}

func TestCustomRole_CannotExceedActor(t *testing.T) {
	e := newStaffEnv(t)
	m := e.withRolesManager()

	_, err := e.createRole(m, "Tệp", "platform.files.manage")
	wantErr(t, "M tạo vai trò có platform.files.manage", err, identity.ErrGrantExceedsActor)
	r, err := e.createRole(m, "Kho phụ", "catalog.read", "inventory.read", "inventory.stocktake")
	if err != nil {
		t.Fatalf("M tạo Kho phụ: %v", err)
	}
	if !strings.HasPrefix(r.Key, "custom_") || len(r.Key) != len("custom_")+8 || !r.Customized || r.Status != "active" || r.Version != 1 {
		t.Fatalf("vai trò mới: %+v", r)
	}
	if !slices.Equal(r.Permissions, perms("catalog.read", "inventory.read", "inventory.stocktake")) {
		t.Fatalf("quyền của vai trò mới: %v", r.Permissions)
	}

	// M đang giữ Kho phụ: thêm quyền M không có → từ chối (không tự nâng quyền qua vai trò mình giữ).
	e.grantRole(e.tenant, e.ids[emailM], r.Key, identity.ScopeAll, "")
	m = e.actor(emailM)
	_, err = e.updateRole(m, r.ID, r.Version, "Kho phụ", "catalog.read", "inventory.read", "inventory.stocktake", "identity.users.manage")
	wantErr(t, "M thêm identity.users.manage vào Kho phụ", err, identity.ErrGrantExceedsActor)
	if got := e.roleByKey(r.Key); len(got.Permissions) != 3 || got.Version != 1 {
		t.Fatalf("Kho phụ bị đổi: %+v", got)
	}
	c, err := e.updateRole(m, r.ID, r.Version, "Kho phụ", "catalog.read", "inventory.read")
	if err != nil {
		t.Fatalf("M gỡ inventory.stocktake khỏi Kho phụ: %v", err)
	}
	if c.After.Version != 2 || !slices.Equal(c.After.Permissions, perms("catalog.read", "inventory.read")) ||
		!slices.Equal(c.Before.Permissions, perms("catalog.read", "inventory.read", "inventory.stocktake")) {
		t.Fatalf("thay đổi: %+v", c)
	}
	// Gỡ quyền M không có cũng bị chặn: A tạo "Sức khỏe" có platform.health.read (M không giữ), M gỡ ra → từ chối;
	// lưu trữ cũng là gỡ mọi quyền nên cũng bị chặn.
	r2, err := e.createRole(e.actor(emailA), "Sức khỏe", "catalog.read", "platform.health.read")
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.updateRole(m, r2.ID, r2.Version, "Sức khỏe", "catalog.read")
	wantErr(t, "M gỡ platform.health.read", err, identity.ErrGrantExceedsActor)
	wantErr(t, "M lưu trữ Sức khỏe", e.archiveRole(m, r2.ID, r2.Version), identity.ErrGrantExceedsActor)

	// Quyền theo kho chỉ ở một kho không đủ: K' có inventory.documents.post ở w1 và identity.roles.manage.
	e.s.Grant(t, e.tenant, e.ids[emailK], []string{"identity.roles.manage"}, identity.ScopeAll, "")
	e.s.Grant(t, e.tenant, e.ids[emailK], []string{"inventory.documents.post"}, identity.ScopeWarehouse, "w1")
	_, err = e.createRole(e.actor(emailK), "Ghi sổ", "inventory.documents.post")
	wantErr(t, "K chỉ có ghi sổ ở w1", err, identity.ErrGrantExceedsActor)

	// Không có identity.roles.manage: module tự từ chối (không chỉ guard).
	_, err = e.createRole(e.actor(emailK), "x", "catalog.read")
	if err != nil {
		t.Fatalf("K có roles.manage + catalog.read: %v", err)
	}
	e.s.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `DELETE FROM identity.user_role_grants g USING identity.role_permissions rp
			WHERE rp.role_id = g.role_id AND rp.permission = 'identity.roles.manage' AND g.user_id = $1`, e.ids[emailK])
		return err //nolint:wrapcheck // test
	})
	_, err = e.createRole(e.actor(emailK), "y", "catalog.read")
	wantErr(t, "K không có identity.roles.manage", err, identity.ErrGrantExceedsActor)
}

func TestCustomRole_OwnerLocked(t *testing.T) {
	e := newStaffEnv(t)
	a := e.actor(emailA)
	owner := e.roleByKey("owner")
	_, err := e.updateRole(a, owner.ID, owner.Version, "Chủ shop", "catalog.read")
	wantErr(t, "A sửa owner", err, identity.ErrOwnerRoleLocked)
	wantErr(t, "A lưu trữ owner", e.archiveRole(a, owner.ID, owner.Version), identity.ErrOwnerRoleLocked)
	if got := e.roleByKey("owner"); got.Name != "Chủ" || len(got.Permissions) != len(identity.Permissions) || got.Status != "active" {
		t.Fatalf("owner bị đổi: %+v", got)
	}
}

// Sửa vai trò mẫu warehouse bỏ inventory.stocktake → customized; EnsureDefaultRoles không thêm lại; owner vẫn đủ.
func TestCustomRole_DefaultRoleCustomized(t *testing.T) {
	e := newStaffEnv(t)
	a := e.actor(emailA)
	w := e.roleByKey("warehouse")
	var keep []string
	for _, p := range w.Permissions {
		if p != "inventory.stocktake" {
			keep = append(keep, string(p))
		}
	}
	c, err := e.updateRole(a, w.ID, w.Version, w.Name, keep...)
	if err != nil {
		t.Fatal(err)
	}
	if !c.After.Customized || slices.Contains(c.After.Permissions, "inventory.stocktake") || c.Before.Customized {
		t.Fatalf("sau khi sửa: %+v", c)
	}
	e.seedTenant(e.tenant)
	if got := e.roleByKey("warehouse"); slices.Contains(got.Permissions, "inventory.stocktake") || !got.Customized {
		t.Fatalf("EnsureDefaultRoles thêm lại quyền đã gỡ: %+v", got)
	}
	if got := e.roleByKey("owner"); len(got.Permissions) != len(identity.Permissions) {
		t.Fatalf("owner không đủ quyền: %d", len(got.Permissions))
	}
	// K (đang giữ Kho) mất quyền ngay ở yêu cầu kế tiếp.
	if identity.Can(e.actor(emailK), "inventory.stocktake", &identity.Scope{Type: identity.ScopeWarehouse, ID: "w1"}) {
		t.Fatal("K vẫn còn inventory.stocktake")
	}
}

func TestCustomRole_ArchiveAndInput(t *testing.T) {
	e := newStaffEnv(t)
	a := e.actor(emailA)
	r, err := e.createRole(a, "Kho phụ", "catalog.read", "inventory.read")
	if err != nil {
		t.Fatal(err)
	}
	k := e.ids[emailK]
	if err := e.replace(a, k, g("warehouse", identity.ScopeWarehouse, "w1"), g(r.Key, identity.ScopeWarehouse, "w1")); err != nil {
		t.Fatal(err)
	}
	if got := e.roleByKey(r.Key); got.HolderCount != 1 {
		t.Fatalf("người giữ = %d, mong 1", got.HolderCount)
	}
	wantErr(t, "lưu trữ khi K active còn giữ", e.archiveRole(a, r.ID, r.Version), identity.ErrRoleInUse)

	// Lỗi đầu vào.
	_, err = e.createRole(a, "x", "foo.bar")
	wantErr(t, "quyền lạ", err, identity.ErrUnknownPermission)
	_, err = e.createRole(a, "  kho PHỤ ", "catalog.read")
	wantErr(t, "trùng tên (hoa thường, khoảng trắng)", err, identity.ErrRoleNameTaken)
	_, err = e.createRole(a, "Kho", "catalog.read")
	wantErr(t, "trùng tên vai trò mẫu", err, identity.ErrRoleNameTaken)
	_, err = e.createRole(a, "   ", "catalog.read")
	wantErr(t, "tên rỗng", err, identity.ErrInvalidInput)
	_, err = e.createRole(a, strings.Repeat("a", 101), "catalog.read")
	wantErr(t, "tên quá dài", err, identity.ErrInvalidInput)
	_, err = e.createRole(a, "a\x07b", "catalog.read")
	wantErr(t, "ký tự điều khiển", err, identity.ErrInvalidInput)
	_, err = e.updateRole(a, r.ID, r.Version+5, "Kho phụ", "catalog.read")
	wantErr(t, "version cũ", err, identity.ErrVersionConflict)
	_, err = e.updateRole(a, uuid.New(), 1, "Kho phụ", "catalog.read")
	wantErr(t, "không có vai trò", err, identity.ErrRoleNotFound)
	_, err = e.updateRole(a, r.ID, r.Version, "Bán hàng", "catalog.read")
	wantErr(t, "đổi sang tên đã có", err, identity.ErrRoleNameTaken)
	// Giữ nguyên tên chính nó (khác hoa thường) được.
	if _, err := e.updateRole(a, r.ID, r.Version, "KHO PHỤ", "catalog.read", "inventory.read"); err != nil {
		t.Fatalf("đổi hoa thường tên của chính nó: %v", err)
	}

	// Khóa K: không còn người active giữ → lưu trữ được.
	if err := e.lock(a, k, true); err != nil {
		t.Fatal(err)
	}
	cur := e.roleByKey(r.Key)
	wantErr(t, "lưu trữ version cũ", e.archiveRole(a, r.ID, cur.Version-1), identity.ErrVersionConflict)
	if err := e.archiveRole(a, r.ID, cur.Version); err != nil {
		t.Fatalf("lưu trữ: %v", err)
	}
	if got := e.roleByKey(r.Key); got.Status != "archived" || got.Version != cur.Version+1 {
		t.Fatalf("sau lưu trữ: %+v", got)
	}
	_, err = e.updateRole(a, r.ID, cur.Version+1, "Kho phụ", "catalog.read")
	wantErr(t, "sửa vai trò đã lưu trữ", err, identity.ErrRoleArchived)
	// Mở lại K: quyền của vai trò đã lưu trữ không còn hiệu lực (K vẫn có inventory.read từ vai trò Kho).
	if err := e.lock(a, k, false); err != nil {
		t.Fatal(err)
	}
	if n := e.s.CountIn(t, e.tenant, `SELECT count(*) FROM identity.user_role_grants g JOIN identity.roles r ON r.id = g.role_id
		WHERE g.user_id = $1 AND r.status = 'archived'`, k); n != 1 {
		t.Fatalf("grant cũ của vai trò đã lưu trữ: %d", n)
	}
	// Gán mới vai trò đã lưu trữ → ErrRoleArchived; giữ grant cũ (không thêm) thì vẫn sửa được nhân viên.
	wantErr(t, "gán vai trò đã lưu trữ cho M", e.replace(a, e.ids[emailM], g("manager", identity.ScopeAll, ""), g(r.Key, identity.ScopeAll, "")), identity.ErrRoleArchived)
	if err := e.replace(a, k, g("warehouse", identity.ScopeWarehouse, "w1"), g(r.Key, identity.ScopeWarehouse, "w1"), g("sale", identity.ScopeWarehouse, "w1")); err != nil {
		t.Fatalf("giữ grant cũ của vai trò đã lưu trữ: %v", err)
	}
	if err := e.replace(a, k, g("warehouse", identity.ScopeWarehouse, "w1")); err != nil {
		t.Fatalf("gỡ grant của vai trò đã lưu trữ: %v", err)
	}
}

// Vai trò đã lưu trữ không cho quyền: người bị khóa còn giữ nó, được mở lại thì không lấy lại quyền.
func TestCustomRole_ArchivedGivesNothing(t *testing.T) {
	e := newStaffEnv(t)
	a := e.actor(emailA)
	r, err := e.createRole(a, "Nhật ký", "audit.read")
	if err != nil {
		t.Fatal(err)
	}
	x := e.user(e.tenant, "x@elimaz.test", r.Key, identity.ScopeAll, "")
	if !identity.Can(e.actor("x@elimaz.test"), "audit.read", nil) {
		t.Fatal("X phải có audit.read trước khi lưu trữ")
	}
	e.s.SetStatus(t, e.tenant, x, "locked")
	if err := e.archiveRole(a, r.ID, r.Version); err != nil {
		t.Fatal(err)
	}
	e.s.SetStatus(t, e.tenant, x, "active")
	if identity.Can(e.actor("x@elimaz.test"), "audit.read", nil) {
		t.Fatal("vai trò đã lưu trữ vẫn cho quyền")
	}
}

// Song song (50 lượt): lưu trữ vai trò R và gán R cho K cùng lúc → hoặc lưu trữ thành công và gán nhận
// ErrRoleArchived, hoặc gán thành công và lưu trữ nhận ErrRoleInUse; không deadlock.
func TestCustomRole_ArchiveVsAssignConcurrent(t *testing.T) {
	e := newStaffEnv(t)
	a, b := e.actor(emailA), e.actor(emailB)
	k := e.ids[emailK]
	before := e.deadlocks()
	for round := range 50 {
		r, err := e.createRole(a, "R"+uuid.NewString()[:8], "catalog.read")
		if err != nil {
			t.Fatal(err)
		}
		v := e.staff(k).Version
		var wg sync.WaitGroup
		start := make(chan struct{})
		var errArchive, errAssign error
		wg.Go(func() { <-start; errArchive = e.archiveRole(a, r.ID, r.Version) })
		wg.Go(func() {
			<-start
			errAssign = e.app(func(tx pgx.Tx) error {
				_, err := identity.ReplaceGrants(context.Background(), tx, b, k, v,
					[]identity.GrantInput{g("warehouse", identity.ScopeWarehouse, "w1"), g(r.Key, identity.ScopeWarehouse, "w1")})
				return err //nolint:wrapcheck // test
			})
		})
		close(start)
		wg.Wait()
		switch {
		case errArchive == nil && errors.Is(errAssign, identity.ErrRoleArchived):
		case errAssign == nil && errors.Is(errArchive, identity.ErrRoleInUse):
			// Gỡ để lượt sau bắt đầu sạch.
			if err := e.replace(a, k, g("warehouse", identity.ScopeWarehouse, "w1")); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("lượt %d: lưu trữ %v, gán %v", round, errArchive, errAssign)
		}
	}
	if after := e.deadlocks(); after != before {
		t.Fatalf("deadlock tăng: %d → %d", before, after)
	}
}

// Hai lần tạo cùng tên cùng lúc (50 lượt): đúng một lần thành công, lần kia ErrRoleNameTaken.
func TestCustomRole_NameTakenConcurrent(t *testing.T) {
	e := newStaffEnv(t)
	a, b := e.actor(emailA), e.actor(emailB)
	for round := range 50 {
		name := "Tên " + uuid.NewString()[:8]
		errs := make([]error, 2)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i, actor := range []identity.AuthUser{a, b} {
			wg.Go(func() { <-start; _, errs[i] = e.createRole(actor, name, "catalog.read") })
		}
		close(start)
		wg.Wait()
		ok := 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case !errors.Is(err, identity.ErrRoleNameTaken):
				t.Fatalf("lượt %d: lỗi lạ %v", round, err)
			}
		}
		if ok != 1 {
			t.Fatalf("lượt %d: %d lần tạo thành công (%v)", round, ok, errs)
		}
	}
}

func (e *staffEnv) deadlocks() int64 {
	e.t.Helper()
	var n int64
	ctx := e.t.Context()
	if _, err := e.s.Owner.Pool.Exec(ctx, "SELECT pg_stat_clear_snapshot()"); err != nil {
		e.t.Fatal(err)
	}
	if err := e.s.Owner.Pool.QueryRow(ctx, "SELECT deadlocks FROM pg_stat_database WHERE datname = current_database()").Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// Mọi quyền có đúng một dòng với nhãn tiếng Việt, nhóm và loại phạm vi; dòng xếp liền theo nhóm.
func TestPermissionCatalog_Complete(t *testing.T) {
	cat := identity.PermissionCatalog()
	if len(cat) != len(identity.Permissions) {
		t.Fatalf("danh mục có %d quyền, mong %d", len(cat), len(identity.Permissions))
	}
	byKey := map[identity.Permission]identity.PermissionInfo{}
	var groups []string
	for _, c := range cat {
		byKey[c.Key] = c
		if len(groups) == 0 || groups[len(groups)-1] != c.Group {
			if slices.Contains(groups, c.Group) {
				t.Errorf("nhóm %q không liền nhau", c.Group)
			}
			groups = append(groups, c.Group)
		}
	}
	for _, d := range identity.Permissions {
		c, ok := byKey[d.Key]
		if !ok || strings.TrimSpace(c.Label) == "" || strings.TrimSpace(c.Group) == "" {
			t.Errorf("quyền %s: %+v", d.Key, c)
		}
		want := map[identity.ScopeKind][]string{
			identity.ScopeNone: {"none"}, identity.ScopeShop: {"shop"}, identity.ScopeByTarget: {"warehouse", "page"},
		}[d.Scope]
		if !slices.Contains(want, c.ScopeType) {
			t.Errorf("quyền %s: loại phạm vi %q, mong một trong %v", d.Key, c.ScopeType, want)
		}
	}
}
