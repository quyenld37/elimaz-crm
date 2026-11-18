package identity_test

import (
	"errors"
	"slices"
	"testing"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/identity"
)

func page(id string) identity.Scope { return identity.Scope{Type: identity.ScopePage, ID: id} }

func scopeAll() identity.Scope { return identity.Scope{Type: identity.ScopeAll} }

func user(grants ...identity.Grant) identity.AuthUser { return identity.AuthUser{Grants: grants} }

func TestCanTable(t *testing.T) {
	p1, p2 := page("p1"), page("p2")
	cases := []struct {
		name   string
		u      identity.AuthUser
		p      identity.Permission
		target *identity.Scope
		want   bool
	}{
		{"quyền không theo đích, phạm vi all", user(identity.Grant{Permission: "audit.read", Scope: scopeAll()}), "audit.read", nil, true},
		{"quyền không theo đích chỉ có trên page:p1", user(identity.Grant{Permission: "audit.read", Scope: p1}), "audit.read", nil, false},
		{"quyền không theo đích có đích: vẫn cần all", user(identity.Grant{Permission: "audit.read", Scope: p1}), "audit.read", &p1, false},
		{"không grant", user(), "audit.read", nil, false},
		{"theo đích, đúng page", user(identity.Grant{Permission: "conversation.read", Scope: p1}), "conversation.read", &p1, true},
		{"theo đích, page khác", user(identity.Grant{Permission: "conversation.read", Scope: p1}), "conversation.read", &p2, false},
		{"theo đích, all", user(identity.Grant{Permission: "conversation.read", Scope: scopeAll()}), "conversation.read", &p2, true},
		{"theo đích, không đích, có một grant", user(identity.Grant{Permission: "conversation.read", Scope: p1}), "conversation.read", nil, true},
		{"quyền khác trên cùng đích", user(identity.Grant{Permission: "audit.read", Scope: p1}), "conversation.read", &p1, false},
		{"quyền lạ", user(identity.Grant{Permission: "khong.co", Scope: scopeAll()}), "khong.co", nil, false},
		{"đích cùng ID khác loại", user(identity.Grant{Permission: "conversation.read", Scope: identity.Scope{Type: identity.ScopeWarehouse, ID: "p1"}}), "conversation.read", &p1, false},
	}
	for _, c := range cases {
		if got := identity.Can(c.u, c.p, c.target); got != c.want {
			t.Errorf("%s: Can = %v, mong %v", c.name, got, c.want)
		}
	}
}

func TestScopesFor(t *testing.T) {
	u := user(
		identity.Grant{Permission: "conversation.read", Scope: page("p2")},
		identity.Grant{Permission: "conversation.read", Scope: page("p1")},
		identity.Grant{Permission: "conversation.read", Scope: page("p1")},
		identity.Grant{Permission: "audit.read", Scope: page("p9")},
	)
	all, ids := identity.ScopesFor(u, "conversation.read")
	if all || !slices.Equal(ids, []string{"p1", "p2"}) {
		t.Fatalf("ScopesFor = %v %v, mong false [p1 p2]", all, ids)
	}
	u.Grants = append(u.Grants, identity.Grant{Permission: "conversation.read", Scope: scopeAll()})
	if all, ids = identity.ScopesFor(u, "conversation.read"); !all || ids != nil {
		t.Fatalf("có all: ScopesFor = %v %v, mong true nil", all, ids)
	}
	if all, ids = identity.ScopesFor(user(), "conversation.read"); all || len(ids) != 0 {
		t.Fatalf("không grant: ScopesFor = %v %v", all, ids)
	}
}

func TestPermissionsList(t *testing.T) {
	want := map[identity.Permission]identity.ScopeKind{
		"realtime.subscribe": identity.ScopeNone, "audit.read": identity.ScopeNone,
		"platform.jobs.manage": identity.ScopeNone, "platform.health.read": identity.ScopeNone,
		"identity.users.manage": identity.ScopeNone, "conversation.read": identity.ScopeByTarget,
		// M1 (kế hoạch Task M1-26).
		"catalog.read": identity.ScopeNone, "catalog.manage": identity.ScopeNone, "catalog.import": identity.ScopeNone,
		"order.create": identity.ScopeNone, "order.read": identity.ScopeNone, "files.upload": identity.ScopeNone,
		"platform.files.manage": identity.ScopeNone, "inventory.suppliers.manage": identity.ScopeNone,
		"identity.roles.manage": identity.ScopeNone,
		"inventory.read":        identity.ScopeByTarget, "inventory.documents.create": identity.ScopeByTarget,
		"inventory.documents.post": identity.ScopeByTarget, "inventory.stocktake": identity.ScopeByTarget,
		"inventory.stocktake.apply": identity.ScopeByTarget, "fulfillment.read": identity.ScopeByTarget,
		"fulfillment.ship": identity.ScopeByTarget,
	}
	if len(identity.Permissions) != len(want) {
		t.Fatalf("mong %d quyền, có %d", len(want), len(identity.Permissions))
	}
	for _, d := range identity.Permissions {
		if k, ok := want[d.Key]; !ok || k != d.Scope {
			t.Errorf("quyền %q phạm vi %v không đúng kế hoạch", d.Key, d.Scope)
		}
		if !identity.IsKnown(d.Key) {
			t.Errorf("IsKnown(%q) = false", d.Key)
		}
	}
	if identity.IsKnown("khong.co") {
		t.Fatal("IsKnown của quyền lạ phải false")
	}
}

// Quy luật (rapid, 200 lượt): Can(u, p, target) đúng khi và chỉ khi có grant đúng quyền p với phạm vi all, hoặc
// (quyền theo đích) đúng target, hoặc (theo đích, không target) ít nhất một grant. Grants rỗng → luôn sai; grant
// của quyền khác không bao giờ làm Can đúng.
func TestCanProperty(t *testing.T) {
	keys := make([]identity.Permission, 0, len(identity.Permissions))
	for _, d := range identity.Permissions {
		keys = append(keys, d.Key)
	}
	genScope := rapid.Custom(func(t *rapid.T) identity.Scope {
		typ := rapid.SampledFrom([]string{identity.ScopeAll, identity.ScopePage, identity.ScopeWarehouse}).Draw(t, "type")
		if typ == identity.ScopeAll {
			return scopeAll()
		}
		return identity.Scope{Type: typ, ID: rapid.SampledFrom([]string{"p1", "p2", "w1"}).Draw(t, "id")}
	})
	rapid.Check(t, func(t *rapid.T) {
		grants := rapid.SliceOfN(rapid.Custom(func(t *rapid.T) identity.Grant {
			return identity.Grant{Permission: rapid.SampledFrom(keys).Draw(t, "perm"), Scope: genScope.Draw(t, "scope")}
		}), 0, 6).Draw(t, "grants")
		u := user(grants...)
		p := rapid.SampledFrom(keys).Draw(t, "p")
		var target *identity.Scope
		if rapid.Bool().Draw(t, "hasTarget") {
			s := genScope.Draw(t, "target")
			if s.Type != identity.ScopeAll {
				target = &s
			}
		}
		want := canModel(grants, p, target)
		if got := identity.Can(u, p, target); got != want {
			t.Fatalf("Can(%v, %s, %v) = %v, mong %v", grants, p, target, got, want)
		}
		// Bỏ mọi grant của p: không còn gì làm Can đúng.
		others := slices.DeleteFunc(slices.Clone(grants), func(g identity.Grant) bool { return g.Permission == p })
		if identity.Can(user(others...), p, target) {
			t.Fatalf("grant của quyền khác làm Can đúng: %v %s %v", others, p, target)
		}
	})
}

// canModel là định nghĩa của Can viết thẳng từ kế hoạch, để so trong TestCanProperty.
func canModel(grants []identity.Grant, p identity.Permission, target *identity.Scope) bool {
	byTarget := identity.KindOf(p) == identity.ScopeByTarget
	for _, g := range grants {
		if g.Permission != p {
			continue
		}
		if g.Scope == scopeAll() || (byTarget && (target == nil || g.Scope == *target)) {
			return true
		}
	}
	return false
}

func TestCheckMFA(t *testing.T) {
	allowed := []string{"mfa", "otp"}
	cases := []struct {
		name  string
		claim any
		ok    bool
	}{
		{"mảng có giá trị cho phép", []any{"pwd", "mfa"}, true},
		{"chuỗi cho phép", "otp", true},
		{"mảng chuỗi Go", []string{"otp"}, true},
		{"thiếu trường", nil, false},
		{"mảng không có giá trị cho phép", []any{"pwd"}, false},
		{"mảng rỗng", []any{}, false},
		{"chuỗi khác", "pwd", false},
		{"kiểu lạ", true, false},
		{"số", 1.0, false},
		{"mảng phần tử không phải chuỗi", []any{1.0, true}, false},
		{"khác hoa thường", "MFA", false},
	}
	for _, c := range cases {
		err := identity.CheckMFA(c.claim, allowed)
		if c.ok != (err == nil) || (err != nil && !errors.Is(err, identity.ErrMFARequired)) {
			t.Errorf("%s: CheckMFA = %v", c.name, err)
		}
	}
	if err := identity.CheckMFA("mfa", nil); !errors.Is(err, identity.ErrMFARequired) {
		t.Fatalf("danh sách cho phép rỗng phải từ chối, có %v", err)
	}
}

// Quy luật: CheckMFA qua khi và chỉ khi giá trị (chuỗi hoặc mảng) có ít nhất một phần tử nằm trong allowed.
func TestCheckMFAProperty(t *testing.T) {
	vals := []string{"mfa", "otp", "pwd", "hwk", ""}
	rapid.Check(t, func(t *rapid.T) {
		allowed := rapid.SliceOfDistinct(rapid.SampledFrom(vals), rapid.ID).Draw(t, "allowed")
		claim := rapid.SliceOfN(rapid.SampledFrom(vals), 0, 4).Draw(t, "claim")
		arr := make([]any, len(claim))
		for i, c := range claim {
			arr[i] = c
		}
		want := slices.ContainsFunc(claim, func(c string) bool { return slices.Contains(allowed, c) })
		if got := identity.CheckMFA(arr, allowed) == nil; got != want {
			t.Fatalf("CheckMFA(%v, %v) = %v, mong %v", claim, allowed, got, want)
		}
	})
}
