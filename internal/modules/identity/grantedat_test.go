package identity_test

import (
	"slices"
	"testing"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/identity"
)

// GrantedAt (Task 411): grant của p ở phạm vi all hoặc đúng đích, bất kể loại phạm vi của p (order.create loại none
// vẫn kiểm theo kho giữ hàng); quyền lạ, grant theo đích khác, grant quyền khác → sai.
func TestGrantedAtTable(t *testing.T) {
	cases := []struct {
		name   string
		u      identity.AuthUser
		p      identity.Permission
		target identity.Scope
		want   bool
	}{
		{"quyền none, grant all", user(identity.Grant{Permission: "order.create", Scope: scopeAll()}), "order.create", wh("w1"), true},
		{"quyền none, grant đúng kho", user(identity.Grant{Permission: "order.create", Scope: wh("w1")}), "order.create", wh("w1"), true},
		{"quyền none, grant kho khác", user(identity.Grant{Permission: "order.create", Scope: wh("w2")}), "order.create", wh("w1"), false},
		{"quyền none, grant theo Page", user(identity.Grant{Permission: "order.create", Scope: page("p1")}), "order.create", wh("w1"), false},
		{"kho đúng nhưng của quyền khác", user(identity.Grant{Permission: "inventory.read", Scope: wh("w1")}), "order.create", wh("w1"), false},
		{"nhiều grant, một grant đúng kho", user(
			identity.Grant{Permission: "order.create", Scope: page("p1")},
			identity.Grant{Permission: "order.create", Scope: wh("w1")},
		), "order.create", wh("w1"), true},
		{"không grant", user(), "order.create", wh("w1"), false},
		{"quyền theo đích, đúng kho", user(identity.Grant{Permission: "inventory.read", Scope: wh("w1")}), "inventory.read", wh("w1"), true},
		{"quyền quản trị chỉ ở kho: không hiệu lực", user(identity.Grant{Permission: "audit.read", Scope: wh("w1")}), "audit.read", wh("w1"), false},
		{"quyền quản trị all", user(identity.Grant{Permission: "audit.read", Scope: scopeAll()}), "audit.read", wh("w1"), true},
		{"quyền lạ", user(identity.Grant{Permission: "khong.co", Scope: scopeAll()}), "khong.co", wh("w1"), false},
	}
	for _, c := range cases {
		if got := identity.GrantedAt(c.u, c.p, c.target); got != c.want {
			t.Errorf("%s: GrantedAt = %v, mong %v", c.name, got, c.want)
		}
	}
}

// Quy luật: GrantedAt đúng ⇔ có grant hiệu lực của p ở all hoặc đúng đích; GrantedAt đúng thì Can(p, đích) đúng
// (GrantedAt không bao giờ rộng hơn Can).
func TestGrantedAtProperty(t *testing.T) {
	keys := permissionKeys()
	scopes := scopeGen("p1", "w1", "w2")
	rapid.Check(t, func(t *rapid.T) {
		grants := grantsGen(keys, scopes).Draw(t, "grants")
		p := rapid.SampledFrom(keys).Draw(t, "p")
		target := identity.Scope{Type: identity.ScopeWarehouse, ID: rapid.SampledFrom([]string{"w1", "w2"}).Draw(t, "target")}
		want := slices.ContainsFunc(grants, func(g identity.Grant) bool {
			return g.Permission == p && identity.Effective(g) && (g.Scope == scopeAll() || g.Scope == target)
		})
		got := identity.GrantedAt(user(grants...), p, target)
		if got != want {
			t.Fatalf("GrantedAt(%v, %s, %v) = %v, mong %v", grants, p, target, got, want)
		}
		if got && !identity.Can(user(grants...), p, &target) {
			t.Fatalf("GrantedAt rộng hơn Can: %v %s %v", grants, p, target)
		}
	})
}
