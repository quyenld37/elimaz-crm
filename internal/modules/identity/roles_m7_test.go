package identity_test

import (
	"errors"
	"slices"
	"testing"

	"elimaz.com.vn/crm/internal/modules/identity"
)

// m7Permissions là 10 quyền nhân viên Task 260 (M7-15) thêm cho phần khởi động.
func m7Permissions() []identity.Permission {
	return []identity.Permission{
		"catalog.launch.publish", "inventory.stocktake.opening", "inventory.legacy_return.create",
		"fulfillment.unmatched.read", "conversation.t2.read", "channel.golive", "order.fallback.import",
		"reporting.launch.read", "reporting.launch.run", "reporting.launch.decide",
	}
}

// Đích của quyền M7 theo đích trong danh mục quyền của màn Vai trò (loại phạm vi của mọi quyền kiểm ở
// TestPermissionsList; QĐ-260-2: lượt chạy và quyết đêm mở là toàn shop).
func TestPermissions_M7Targets(t *testing.T) {
	t.Parallel()
	target := map[identity.Permission]string{
		"inventory.stocktake.opening": identity.ScopeWarehouse, "inventory.legacy_return.create": identity.ScopeWarehouse,
		"conversation.t2.read": identity.ScopePage, "channel.golive": identity.ScopePage,
		"reporting.launch.run": "shop", "reporting.launch.decide": "shop", "reporting.launch.read": "none",
	}
	for _, c := range identity.PermissionCatalog() {
		if want, ok := target[c.Key]; ok && c.ScopeType != want {
			t.Errorf("%s: đích %q, mong %q", c.Key, c.ScopeType, want)
		}
	}
}

// Vai trò mẫu (kế hoạch Task M7-15, G03): owner mọi quyền M7; manager mọi quyền M7 trừ reporting.launch.decide;
// warehouse hai quyền kho; sale conversation.t2.read; accountant fulfillment.unmatched.read, reporting.launch.read.
func TestDefaultRoles_M7(t *testing.T) {
	t.Parallel()
	notDecide := slices.DeleteFunc(m7Permissions(), func(p identity.Permission) bool { return p == "reporting.launch.decide" })
	want := map[string][]identity.Permission{
		"owner":      m7Permissions(),
		"manager":    notDecide,
		"warehouse":  {"inventory.stocktake.opening", "inventory.legacy_return.create"},
		"sale":       {"conversation.t2.read"},
		"accountant": {"fulfillment.unmatched.read", "reporting.launch.read"},
	}
	for _, r := range identity.DefaultRoles {
		var got []identity.Permission
		for _, p := range r.Permissions {
			if slices.Contains(m7Permissions(), p) {
				got = append(got, p)
			}
		}
		if w := want[r.Key]; !slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(w))) {
			t.Errorf("%s: quyền M7 %v, mong %v", r.Key, got, w)
		}
	}
	if rs := rolesWith("reporting.launch.decide"); !slices.Equal(rs, []string{"owner"}) {
		t.Errorf("reporting.launch.decide chỉ owner, có %v", rs)
	}
}

// Quyền toàn shop của đêm mở gán theo Page không có hiệu lực, gán all thì có; channel.golive gán theo Page p1 chỉ có
// hiệu lực ở p1; inventory.stocktake.opening gán theo kho w1 không lọt sang kho w2.
func TestCan_M7(t *testing.T) {
	t.Parallel()
	page := func(id string) identity.Scope { return identity.Scope{Type: identity.ScopePage, ID: id} }
	wh := func(id string) identity.Scope { return identity.Scope{Type: identity.ScopeWarehouse, ID: id} }
	p1, p2, w1, w2 := page("p1"), page("p2"), wh("w1"), wh("w2")
	u := identity.AuthUser{Grants: []identity.Grant{
		{Permission: "reporting.launch.decide", Scope: p1},
		{Permission: "reporting.launch.run", Scope: w1},
		{Permission: "reporting.launch.read", Scope: w1},
		{Permission: "channel.golive", Scope: p1},
		{Permission: "inventory.stocktake.opening", Scope: w1},
	}}
	all := identity.AuthUser{Grants: []identity.Grant{{Permission: "reporting.launch.decide", Scope: identity.Scope{Type: identity.ScopeAll}}}}
	for _, c := range []struct {
		name   string
		u      identity.AuthUser
		p      identity.Permission
		target *identity.Scope
		want   bool
	}{
		{"decide gán theo Page", u, "reporting.launch.decide", nil, false},
		{"run gán theo kho", u, "reporting.launch.run", nil, false},
		{"read (none) gán theo kho", u, "reporting.launch.read", nil, true},
		{"golive ở p1", u, "channel.golive", &p1, true},
		{"golive ở p2", u, "channel.golive", &p2, false},
		{"opening ở w1", u, "inventory.stocktake.opening", &w1, true},
		{"opening ở w2", u, "inventory.stocktake.opening", &w2, false},
		{"decide gán all", all, "reporting.launch.decide", nil, true},
	} {
		if got := identity.Can(c.u, c.p, c.target); got != c.want {
			t.Errorf("%s: Can = %v, mong %v", c.name, got, c.want)
		}
	}
}

// Tenant mới: owner, manager nhận quyền M7 (manager không có reporting.launch.decide); vai trò tự định nghĩa không nhận
// web.catalog.preview (ErrUnknownPermission, như mọi quyền web.*).
func TestEnsureDefaultRoles_M7(t *testing.T) {
	t.Parallel()
	s, a := rolesTenant(t)
	r := rolesByKey(t, s, a)
	requireRolesHave(t, r, m7Permissions(), "owner")
	if slices.Contains(r["manager"].Permissions, "reporting.launch.decide") {
		t.Error("manager không được có reporting.launch.decide")
	}
	requireRolesHave(t, r, []identity.Permission{"inventory.stocktake.opening", "inventory.legacy_return.create"}, "warehouse")
	e := newStaffEnv(t)
	if _, err := e.createRole(e.actor(emailA), "Web thử", "web.catalog.preview"); !errors.Is(err, identity.ErrUnknownPermission) {
		t.Fatalf("vai trò tự định nghĩa với web.catalog.preview: mong ErrUnknownPermission, có %v", err)
	}
}
