package identity_test

import (
	"slices"
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/testkit"
)

// m6Permissions là 5 quyền Task 225 (M6-02) thêm.
func m6Permissions() []identity.Permission {
	return []identity.Permission{
		"report.sales.read", "report.inventory.read", "report.export", "report.exports.manage", "customer.export",
	}
}

// checkPermissionScopes: mỗi quyền của want có trong identity.Permissions đúng loại phạm vi.
func checkPermissionScopes(t *testing.T, want map[identity.Permission]identity.ScopeKind) {
	t.Helper()
	for p, scope := range want {
		i := slices.IndexFunc(identity.Permissions, func(d identity.PermissionDef) bool { return d.Key == p })
		switch {
		case i < 0:
			t.Errorf("thiếu quyền %s", p)
		case identity.Permissions[i].Scope != scope:
			t.Errorf("%s: phạm vi %v, mong %v", p, identity.Permissions[i].Scope, scope)
		}
	}
}

// requireRolesHave: mỗi vai trò keys của r có đủ perms.
func requireRolesHave(t *testing.T, r map[string]identity.RoleRow, perms []identity.Permission, keys ...string) {
	t.Helper()
	for _, key := range keys {
		for _, p := range perms {
			if !slices.Contains(r[key].Permissions, p) {
				t.Errorf("%s thiếu %s", key, p)
			}
		}
	}
}

// Loại phạm vi của quyền M6 (Task 225, QĐ-223-6): hai quyền đọc theo đích (Page, kho), ba quyền nhạy cảm chỉ hiệu
// lực ở gán toàn shop (Q13, Q14).
func TestPermissions_M6Scopes(t *testing.T) {
	t.Parallel()
	want := map[identity.Permission]identity.ScopeKind{
		"report.sales.read":     identity.ScopeByTarget,
		"report.inventory.read": identity.ScopeByTarget,
		"report.export":         identity.ScopeShop,
		"report.exports.manage": identity.ScopeShop,
		"customer.export":       identity.ScopeShop,
	}
	checkPermissionScopes(t, want)
	// Đích của quyền theo đích trong danh mục quyền của màn Vai trò.
	for _, c := range identity.PermissionCatalog() {
		switch c.Key {
		case "report.sales.read":
			if c.ScopeType != identity.ScopePage {
				t.Errorf("report.sales.read: đích %q, mong page", c.ScopeType)
			}
		case "report.inventory.read":
			if c.ScopeType != identity.ScopeWarehouse {
				t.Errorf("report.inventory.read: đích %q, mong warehouse", c.ScopeType)
			}
		case "report.export", "report.exports.manage", "customer.export":
			if c.ScopeType != "shop" {
				t.Errorf("%s: đích %q, mong shop", c.Key, c.ScopeType)
			}
		}
	}
}

// Kế hoạch Task M6-02 Step 1: report.export gán theo Page không có hiệu lực, gán all thì có; report.sales.read gán
// theo Page p1 có quyền ở p1, không ở p2; report.inventory.read gán theo kho không lọt sang Page.
func TestCan_M6(t *testing.T) {
	t.Parallel()
	p1 := identity.Scope{Type: identity.ScopePage, ID: "p1"}
	p2 := identity.Scope{Type: identity.ScopePage, ID: "p2"}
	w1 := identity.Scope{Type: identity.ScopeWarehouse, ID: "w1"}
	page := identity.AuthUser{Grants: []identity.Grant{
		{Permission: "report.export", Scope: p1},
		{Permission: "customer.export", Scope: p1},
		{Permission: "report.exports.manage", Scope: p1},
		{Permission: "report.sales.read", Scope: p1},
	}}
	for _, p := range []identity.Permission{"report.export", "customer.export", "report.exports.manage"} {
		if identity.Can(page, p, nil) {
			t.Errorf("%s gán theo Page không được có hiệu lực (toàn shop)", p)
		}
	}
	if !identity.Can(page, "report.sales.read", &p1) {
		t.Error("report.sales.read gán theo p1 phải có quyền ở p1")
	}
	if identity.Can(page, "report.sales.read", &p2) {
		t.Error("report.sales.read gán theo p1 không được có quyền ở p2")
	}
	all := identity.AuthUser{Grants: []identity.Grant{
		{Permission: "report.export", Scope: identity.Scope{Type: identity.ScopeAll}},
		{Permission: "customer.export", Scope: identity.Scope{Type: identity.ScopeAll}},
	}}
	if !identity.Can(all, "report.export", nil) || !identity.Can(all, "customer.export", nil) {
		t.Error("report.export, customer.export gán all phải có hiệu lực")
	}
	wh := identity.AuthUser{Grants: []identity.Grant{{Permission: "report.inventory.read", Scope: w1}}}
	if !identity.Can(wh, "report.inventory.read", &w1) {
		t.Error("report.inventory.read gán theo kho w1 phải có quyền ở w1")
	}
	if identity.Can(wh, "report.sales.read", &p1) {
		t.Error("không có grant report.sales.read thì không có quyền")
	}
}

// Vai trò mẫu M6 (kế hoạch Task M6-02, G18): owner, manager đủ năm quyền; accountant report.sales.read,
// report.export; sale report.sales.read; warehouse report.inventory.read; customer.export chỉ owner, manager (H11).
func TestDefaultRoles_M6(t *testing.T) {
	t.Parallel()
	roles := map[string][]identity.Permission{}
	for _, r := range identity.DefaultRoles {
		roles[r.Key] = r.Permissions
	}
	want := map[string][]identity.Permission{
		"owner":      m6Permissions(),
		"manager":    m6Permissions(),
		"accountant": {"report.sales.read", "report.export"},
		"sale":       {"report.sales.read"},
		"warehouse":  {"report.inventory.read"},
	}
	for key, w := range want {
		var got []identity.Permission
		for _, p := range roles[key] {
			if slices.Contains(m6Permissions(), p) {
				got = append(got, p)
			}
		}
		slices.Sort(got)
		ws := slices.Clone(w)
		slices.Sort(ws)
		if !slices.Equal(got, ws) {
			t.Errorf("%s: quyền M6 %v, mong %v", key, got, ws)
		}
	}
}

// EnsureDefaultRoles với quyền M6: tenant mới có owner đủ năm quyền, sale có report.sales.read mà không có
// customer.export; chạy lần hai không đổi số dòng; sale đã sửa (customized) không nhận lại quyền M6, owner thì có.
func TestEnsureDefaultRoles_M6(t *testing.T) {
	t.Parallel()
	s, a := rolesTenant(t)
	r := rolesByKey(t, s, a)
	requireRolesHave(t, r, m6Permissions(), "owner")
	if !slices.Contains(r["sale"].Permissions, "report.sales.read") || slices.Contains(r["sale"].Permissions, "customer.export") {
		t.Errorf("sale (tenant mới): %v", r["sale"].Permissions)
	}
	roles, perms := countRoleRows(t, s, a)
	ensureRoles(t, s, a)
	if r2, p2 := countRoleRows(t, s, a); r2 != roles || p2 != perms {
		t.Errorf("chạy lần hai đổi số dòng: %d/%d → %d/%d", roles, perms, r2, p2)
	}
	execAll(t, s, a,
		"DELETE FROM identity.role_permissions WHERE permission LIKE 'report.%' OR permission = 'customer.export'",
		"UPDATE identity.roles SET customized = true WHERE key IN ('sale', 'owner')",
	)
	ensureRoles(t, s, a)
	r = rolesByKey(t, s, a)
	if slices.Contains(r["sale"].Permissions, "report.sales.read") {
		t.Error("sale đã sửa không được nhận lại report.sales.read")
	}
	requireRolesHave(t, r, m6Permissions(), "owner", "manager") // owner kể cả đã sửa; manager chưa sửa
}

// Tenant có sẵn trước M6 (vai trò mẫu thiếu quyền M6, giả lập thay cho migration tới 0599 — QĐ-223-6: quyền mới tới
// tenant cũ bằng SyncDefaultRoles của vai trò migrate, không bằng migration 0600): sau đồng bộ, vai trò mẫu chưa sửa
// và owner nhận quyền M6; quyền đã chỉnh tay của vai trò khác không bị bớt; sale đã sửa tay không nhận
// report.sales.read; chủ shop Can mọi quyền M6.
func TestSyncDefaultRoles_M6Existing(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "A")
	const email = "chu-m6@elimaz.test"
	s.User(t, a, email, "active")
	if err := identity.SeedOwner(t.Context(), s.Owner, a, email); err != nil {
		t.Fatal(err)
	}
	m6 := make([]string, 0, len(m6Permissions()))
	for _, p := range m6Permissions() {
		m6 = append(m6, "'"+string(p)+"'")
	}
	execAll(t, s, a,
		"DELETE FROM identity.role_permissions WHERE permission IN ("+strings.Join(m6, ", ")+")",
		// sale đã sửa tay: gỡ order.cancel (quyền có sẵn), đặt customized; warehouse chưa sửa.
		"DELETE FROM identity.role_permissions rp USING identity.roles r WHERE rp.role_id = r.id AND r.key = 'sale' AND rp.permission = 'order.cancel'",
		"UPDATE identity.roles SET customized = true WHERE key IN ('sale', 'owner')",
	)
	before := rolesByKey(t, s, a)
	r := syncOldTenant(t, s, a, email, "report.sales.read")
	checkM6Synced(t, before, r)
	u, err := identity.LoadUser(t.Context(), s.App, email)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range m6Permissions() {
		if !identity.Can(u, p, nil) {
			t.Errorf("chủ shop không có %s sau đồng bộ", p)
		}
	}
}

// checkM6Synced: sau đồng bộ, owner, manager đủ quyền M6; warehouse, accountant chưa sửa nhận quyền mẫu M6; sale đã
// sửa không đổi; không vai trò nào bị bớt quyền so với before.
func checkM6Synced(t *testing.T, before, r map[string]identity.RoleRow) {
	t.Helper()
	requireRolesHave(t, r, m6Permissions(), "owner", "manager")
	if !slices.Contains(r["warehouse"].Permissions, "report.inventory.read") {
		t.Error("warehouse chưa sửa thiếu report.inventory.read sau đồng bộ")
	}
	if got := r["accountant"].Permissions; !slices.Contains(got, "report.sales.read") || !slices.Contains(got, "report.export") {
		t.Errorf("accountant chưa sửa thiếu quyền M6: %v", got)
	}
	if slices.Contains(r["sale"].Permissions, "report.sales.read") || slices.Contains(r["sale"].Permissions, "order.cancel") {
		t.Errorf("sale đã sửa bị đổi quyền: %v", r["sale"].Permissions)
	}
	for key, row := range before {
		for _, p := range row.Permissions {
			if !slices.Contains(r[key].Permissions, p) {
				t.Errorf("%s bị bớt %s sau đồng bộ", key, p)
			}
		}
	}
}
