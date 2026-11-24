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

func wh(id string) identity.Scope { return identity.Scope{Type: identity.ScopeWarehouse, ID: id} }

func ptr(s identity.Scope) *identity.Scope { return &s }

func user(grants ...identity.Grant) identity.AuthUser { return identity.AuthUser{Grants: grants} }

func TestCanTable(t *testing.T) {
	t.Parallel()
	p1, p2 := page("p1"), page("p2")
	cases := []struct {
		name   string
		u      identity.AuthUser
		p      identity.Permission
		target *identity.Scope
		want   bool
	}{
		{"quyền không theo đích, phạm vi all", user(identity.Grant{Permission: "catalog.read", Scope: scopeAll()}), "catalog.read", nil, true},
		// Task 315: quyền không theo đích có hiệu lực từ grant ở bất kỳ phạm vi nào (phạm vi chỉ thu hẹp quyền theo đích).
		{"quyền không theo đích chỉ có trên page:p1", user(identity.Grant{Permission: "realtime.subscribe", Scope: p1}), "realtime.subscribe", nil, true},
		{"quyền không theo đích có đích khác: đích bỏ qua", user(identity.Grant{Permission: "catalog.read", Scope: p1}), "catalog.read", &p2, true},
		// Task 315 lượt 2: quyền quản trị (toàn shop) chỉ hiệu lực từ grant all.
		{"quản trị, phạm vi all", user(identity.Grant{Permission: "identity.users.manage", Scope: scopeAll()}), "identity.users.manage", nil, true},
		{"quản trị chỉ trên kho w1", user(identity.Grant{Permission: "identity.users.manage", Scope: wh("w1")}), "identity.users.manage", nil, false},
		{"quản trị chỉ trên kho w1, đích w1", user(identity.Grant{Permission: "platform.jobs.manage", Scope: wh("w1")}), "platform.jobs.manage", ptr(wh("w1")), false},
		{"audit.read chỉ trên page:p1", user(identity.Grant{Permission: "audit.read", Scope: p1}), "audit.read", nil, false},
		{"quản trị all, có đích", user(identity.Grant{Permission: "audit.read", Scope: scopeAll()}), "audit.read", &p2, true},
		{"catalog.read từ grant theo kho w1", user(identity.Grant{Permission: "catalog.read", Scope: wh("w1")}), "catalog.read", nil, true},
		{"phiếu kho w1 từ grant w1", user(identity.Grant{Permission: "inventory.documents.create", Scope: wh("w1")}), "inventory.documents.create", ptr(wh("w1")), true},
		{"phiếu kho w2 từ grant w1", user(identity.Grant{Permission: "inventory.documents.create", Scope: wh("w1")}), "inventory.documents.create", ptr(wh("w2")), false},
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
	t.Parallel()
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
	t.Parallel()
	want := map[identity.Permission]identity.ScopeKind{
		"realtime.subscribe": identity.ScopeNone, "audit.read": identity.ScopeShop,
		"platform.jobs.manage": identity.ScopeShop, "platform.health.read": identity.ScopeShop,
		"identity.users.manage": identity.ScopeShop, "conversation.read": identity.ScopeByTarget,
		// M1 (kế hoạch Task M1-26).
		"catalog.read": identity.ScopeNone, "catalog.manage": identity.ScopeNone, "catalog.import": identity.ScopeNone,
		"order.create": identity.ScopeNone, "order.read": identity.ScopeNone, "files.upload": identity.ScopeNone,
		"platform.files.manage": identity.ScopeShop, "inventory.suppliers.manage": identity.ScopeNone,
		"identity.roles.manage": identity.ScopeShop,
		"inventory.read":        identity.ScopeByTarget, "inventory.documents.create": identity.ScopeByTarget,
		"inventory.documents.post": identity.ScopeByTarget, "inventory.stocktake": identity.ScopeByTarget,
		"inventory.stocktake.apply": identity.ScopeByTarget, "fulfillment.read": identity.ScopeByTarget,
		"fulfillment.ship": identity.ScopeByTarget,
		// M2 (kế hoạch Task M2-01; hai quyền ghi chính sách toàn shop là ScopeShop theo luật docs/quyen.md).
		"customer.read": identity.ScopeNone, "customer.write": identity.ScopeNone, "customer.merge": identity.ScopeNone,
		"customer.import": identity.ScopeNone, "customer.tags.manage": identity.ScopeNone,
		"order.update": identity.ScopeNone, "order.confirm": identity.ScopeNone, "order.cancel": identity.ScopeNone,
		"order.discount.approve": identity.ScopeNone, "order.return": identity.ScopeNone,
		"order.return.receive": identity.ScopeNone, "order.duplicates.resolve": identity.ScopeNone,
		"order.settings.manage": identity.ScopeShop, "promotion.read": identity.ScopeNone,
		"promotion.manage": identity.ScopeShop,
		// M3 (kế hoạch M3 Task M3-04; kết nối hãng và cài đặt vận chuyển là chính sách toàn shop: ScopeShop).
		"fulfillment.carrier_accounts.read": identity.ScopeNone, "fulfillment.carrier_accounts.manage": identity.ScopeShop,
		"fulfillment.settings.manage": identity.ScopeShop, "fulfillment.quote": identity.ScopeByTarget,
		"fulfillment.cancel": identity.ScopeByTarget, "fulfillment.print": identity.ScopeByTarget,
		"fulfillment.pack": identity.ScopeByTarget, "fulfillment.manual_status": identity.ScopeByTarget,
		"fulfillment.correct": identity.ScopeNone, "fulfillment.callbacks.work": identity.ScopeNone,
		"inventory.aged.read": identity.ScopeByTarget, "cash.cod.read": identity.ScopeNone,
		"cash.statements.import": identity.ScopeNone, "cash.statements.confirm": identity.ScopeNone,
		"cash.adjust": identity.ScopeNone, "cash.payments.record": identity.ScopeNone,
		"cash.refunds.record": identity.ScopeNone, "cash.refunds.waive": identity.ScopeNone,
		// M5 (Task 189): quản lý khóa dịch vụ là quản trị.
		"identity.service_keys.manage": identity.ScopeShop,
		// M5 (Task 456): tạm dừng nhận đơn web là chính sách toàn shop.
		"order.web_intake.manage": identity.ScopeShop,
		// M5 (Task 195): yêu cầu về đơn hàng.
		"order.requests.read": identity.ScopeNone, "order.requests.manage": identity.ScopeNone,
		// M5 (Task 468): quà sinh nhật.
		"promotion.birthday.manage": identity.ScopeShop,
	}
	seen := map[identity.Permission]bool{}
	for _, d := range identity.Permissions {
		if seen[d.Key] {
			t.Errorf("quyền %q khai hai lần", d.Key)
		}
		seen[d.Key] = true
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
// (quyền theo đích) đúng target, hoặc (theo đích, không target) ít nhất một grant, hoặc (quyền không theo đích, Task
// 315) ít nhất một grant ở bất kỳ phạm vi nào. Quyền toàn shop (Task 315 lượt 2) chỉ đúng với grant all. Grants rỗng
// → luôn sai; grant của quyền khác không bao giờ làm Can đúng.
// permissionKeys là khóa của mọi quyền trong identity.Permissions.
func permissionKeys() []identity.Permission {
	keys := make([]identity.Permission, 0, len(identity.Permissions))
	for _, d := range identity.Permissions {
		keys = append(keys, d.Key)
	}
	return keys
}

// scopeGen sinh phạm vi all, hoặc page/warehouse với mã lấy từ ids.
func scopeGen(ids ...string) *rapid.Generator[identity.Scope] {
	return rapid.Custom(func(t *rapid.T) identity.Scope {
		typ := rapid.SampledFrom([]string{identity.ScopeAll, identity.ScopePage, identity.ScopeWarehouse}).Draw(t, "type")
		if typ == identity.ScopeAll {
			return scopeAll()
		}
		return identity.Scope{Type: typ, ID: rapid.SampledFrom(ids).Draw(t, "id")}
	})
}

// grantsGen sinh 0–6 grant của quyền trong keys, phạm vi từ scopes.
func grantsGen(keys []identity.Permission, scopes *rapid.Generator[identity.Scope]) *rapid.Generator[[]identity.Grant] {
	return rapid.SliceOfN(rapid.Custom(func(t *rapid.T) identity.Grant {
		return identity.Grant{Permission: rapid.SampledFrom(keys).Draw(t, "perm"), Scope: scopes.Draw(t, "scope")}
	}), 0, 6)
}

func TestCanProperty(t *testing.T) {
	t.Parallel()
	keys := permissionKeys()
	genScope := scopeGen("p1", "p2", "w1")
	rapid.Check(t, func(t *rapid.T) {
		grants := grantsGen(keys, genScope).Draw(t, "grants")
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
	kind := identity.KindOf(p)
	for _, g := range grants {
		if g.Permission != p {
			continue
		}
		switch {
		case g.Scope == scopeAll():
			return true
		case kind == identity.ScopeShop:
			continue
		case kind == identity.ScopeNone || target == nil || g.Scope == *target:
			return true
		}
	}
	return false
}

// Effective: grant của quyền toàn shop chỉ có hiệu lực ở phạm vi all; quyền khác luôn có hiệu lực.
func TestEffective(t *testing.T) {
	t.Parallel()
	cases := []struct {
		g    identity.Grant
		want bool
	}{
		{identity.Grant{Permission: "identity.users.manage", Scope: scopeAll()}, true},
		{identity.Grant{Permission: "identity.users.manage", Scope: wh("w1")}, false},
		{identity.Grant{Permission: "audit.read", Scope: page("p1")}, false},
		{identity.Grant{Permission: "catalog.read", Scope: wh("w1")}, true},
		{identity.Grant{Permission: "inventory.read", Scope: wh("w1")}, true},
	}
	for _, c := range cases {
		if got := identity.Effective(c.g); got != c.want {
			t.Errorf("Effective(%v) = %v, mong %v", c.g, got, c.want)
		}
	}
}

// ShopOnlyWarnings: mỗi gán vai trò ở phạm vi hẹp hơn toàn shop mà vai trò có quyền toàn shop → một cảnh báo kèm các
// quyền đó (đã sắp); gán all, vai trò không có quyền quản trị, vai trò lạ → không cảnh báo.
func TestShopOnlyWarnings(t *testing.T) {
	t.Parallel()
	roles := []identity.RoleRow{
		{Key: "owner", Permissions: []identity.Permission{"platform.jobs.manage", "catalog.read", "identity.users.manage"}},
		{Key: "warehouse", Permissions: []identity.Permission{"catalog.read", "inventory.read"}},
	}
	got := identity.ShopOnlyWarnings(roles, []identity.GrantInput{
		{RoleKey: "owner", Scope: scopeAll()},
		{RoleKey: "owner", Scope: wh("w1")},
		{RoleKey: "warehouse", Scope: wh("w1")},
		{RoleKey: "khong-co", Scope: wh("w1")},
	})
	want := []identity.ScopeWarning{{
		RoleKey: "owner", Scope: wh("w1"),
		Permissions: []identity.Permission{"identity.users.manage", "platform.jobs.manage"},
	}}
	if !slices.EqualFunc(got, want, func(a, b identity.ScopeWarning) bool {
		return a.RoleKey == b.RoleKey && a.Scope == b.Scope && slices.Equal(a.Permissions, b.Permissions)
	}) {
		t.Fatalf("ShopOnlyWarnings = %+v, mong %+v", got, want)
	}
	if w := identity.ShopOnlyWarnings(roles, nil); w == nil || len(w) != 0 {
		t.Fatalf("không gán: mong rỗng khác nil, có %#v", w)
	}
}

func TestCheckMFA(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

// Quyền quản trị (ScopeShop): grant phạm vi hẹp đứng trước không chặn grant phạm vi all đứng sau; grant thiếu mã đích
// (Type khác all, ID rỗng) không được coi là all (mutation testing Task 23: isAll còn sống).
func TestCanShopScopeGrantOrderAndEmptyID(t *testing.T) {
	t.Parallel()
	wh := identity.Scope{Type: identity.ScopeWarehouse, ID: "w1"}
	u := identity.AuthUser{Grants: []identity.Grant{
		{Permission: "platform.health.read", Scope: wh},
		{Permission: "platform.health.read", Scope: scopeAll()},
	}}
	if !identity.Can(u, "platform.health.read", nil) {
		t.Error("grant all đứng sau grant kho phải cho quyền quản trị")
	}
	for _, s := range []identity.Scope{{Type: identity.ScopeWarehouse}, {Type: identity.ScopeAll, ID: "w1"}} {
		u := identity.AuthUser{Grants: []identity.Grant{{Permission: "platform.health.read", Scope: s}}}
		if identity.Can(u, "platform.health.read", nil) {
			t.Errorf("grant %+v không phải phạm vi all mà vẫn cho quyền quản trị", s)
		}
	}
}

// Quyền theo kho của M3 (Task M3-04): fulfillment.ship gán theo kho W1 không cho kho W2; ScopesFor trả [W1]; quyền kho
// M3 mới (in tem) cũng chỉ ở đúng kho được gán.
func TestCan_M3WarehouseScope(t *testing.T) {
	t.Parallel()
	w1 := identity.Scope{Type: identity.ScopeWarehouse, ID: "W1"}
	w2 := identity.Scope{Type: identity.ScopeWarehouse, ID: "W2"}
	u := identity.AuthUser{Grants: []identity.Grant{
		{Permission: "fulfillment.ship", Scope: w1}, {Permission: "fulfillment.print", Scope: w1},
	}}
	if identity.Can(u, "fulfillment.ship", &w2) || identity.Can(u, "fulfillment.print", &w2) {
		t.Error("gán theo W1 không được có quyền ở W2")
	}
	if !identity.Can(u, "fulfillment.ship", &w1) || !identity.Can(u, "fulfillment.print", &w1) {
		t.Error("gán theo W1 phải có quyền ở W1")
	}
	if all, ids := identity.ScopesFor(u, "fulfillment.ship"); all || len(ids) != 1 || ids[0] != "W1" {
		t.Errorf("ScopesFor: mong [W1], có all=%v %v", all, ids)
	}
	if identity.Can(u, "fulfillment.quote", &w1) {
		t.Error("không có grant fulfillment.quote thì không có quyền")
	}
}
