package identity_test

import (
	"slices"
	"testing"

	"elimaz.com.vn/crm/internal/modules/identity"
)

// m4Permissions là 5 quyền Task 143 (M4-03) thêm; conversation.read đã có từ M0.
func m4Permissions() []identity.Permission {
	return []identity.Permission{
		"channel.manage", "conversation.reply", "conversation.assign", "conversation.settings", "live.operate",
	}
}

// Loại phạm vi của quyền M4 (Task 143): bốn quyền hội thoại, live theo đích Page; channel.manage (kết nối kênh, chế
// độ gửi) là chính sách toàn shop (luật Task 315, docs/quyen.md).
func TestPermissions_M4Scopes(t *testing.T) {
	t.Parallel()
	want := map[identity.Permission]identity.ScopeKind{
		"channel.manage":        identity.ScopeShop,
		"conversation.read":     identity.ScopeByTarget,
		"conversation.reply":    identity.ScopeByTarget,
		"conversation.assign":   identity.ScopeByTarget,
		"conversation.settings": identity.ScopeByTarget,
		"live.operate":          identity.ScopeByTarget,
	}
	for p, scope := range want {
		i := slices.IndexFunc(identity.Permissions, func(d identity.PermissionDef) bool { return d.Key == p })
		if i < 0 {
			t.Errorf("thiếu quyền %s", p)
			continue
		}
		if identity.Permissions[i].Scope != scope {
			t.Errorf("%s: phạm vi %v, mong %v", p, identity.Permissions[i].Scope, scope)
		}
	}
}

// PQ04 nền (Task 143): grant conversation.reply trên page:a có quyền ở Page a, không có ở Page b; ScopesFor trả [a];
// grant all có quyền ở mọi Page; channel.manage gán theo Page không có hiệu lực (toàn shop).
func TestCan_M4PageScope(t *testing.T) {
	t.Parallel()
	pa := identity.Scope{Type: identity.ScopePage, ID: "a"}
	pb := identity.Scope{Type: identity.ScopePage, ID: "b"}
	u := identity.AuthUser{Grants: []identity.Grant{
		{Permission: "conversation.reply", Scope: pa},
		{Permission: "live.operate", Scope: pa},
		{Permission: "channel.manage", Scope: pa},
	}}
	if !identity.Can(u, "conversation.reply", &pa) || !identity.Can(u, "live.operate", &pa) {
		t.Error("gán theo Page a phải có quyền ở Page a")
	}
	if identity.Can(u, "conversation.reply", &pb) || identity.Can(u, "live.operate", &pb) {
		t.Error("gán theo Page a không được có quyền ở Page b")
	}
	if all, ids := identity.ScopesFor(u, "conversation.reply"); all || len(ids) != 1 || ids[0] != "a" {
		t.Errorf("ScopesFor: mong [a], có all=%v %v", all, ids)
	}
	if identity.Can(u, "conversation.assign", &pa) {
		t.Error("không có grant conversation.assign thì không có quyền")
	}
	if identity.Can(u, "channel.manage", nil) {
		t.Error("channel.manage gán theo Page không được có hiệu lực (toàn shop)")
	}
	all := identity.AuthUser{Grants: []identity.Grant{{Permission: "conversation.reply", Scope: identity.Scope{Type: identity.ScopeAll}}}}
	if !identity.Can(all, "conversation.reply", &pb) {
		t.Error("grant all phải có quyền ở mọi Page")
	}
}

// Vai trò mẫu M4 (Task 143, A9): sale có đúng conversation.read, conversation.reply, live.operate (ba quyền M4 làm
// việc hằng ngày) và files.upload (gửi ảnh); manager, owner đủ 5 quyền mới; Kho, Kế toán không có quyền hội thoại.
func TestDefaultRoles_M4(t *testing.T) {
	t.Parallel()
	roles := map[string][]identity.Permission{}
	for _, r := range identity.DefaultRoles {
		roles[r.Key] = r.Permissions
	}
	chat := append(m4Permissions(), "conversation.read")
	var saleChat []identity.Permission
	for _, p := range roles["sale"] {
		if slices.Contains(chat, p) {
			saleChat = append(saleChat, p)
		}
	}
	slices.Sort(saleChat)
	if want := []identity.Permission{"conversation.read", "conversation.reply", "live.operate"}; !slices.Equal(saleChat, want) {
		t.Errorf("sale: quyền hội thoại %v, mong %v", saleChat, want)
	}
	if !slices.Contains(roles["sale"], "files.upload") {
		t.Error("sale phải có files.upload (gửi ảnh cho khách)")
	}
	for _, key := range []string{"owner", "manager"} {
		for _, p := range chat {
			if !slices.Contains(roles[key], p) {
				t.Errorf("%s thiếu %s", key, p)
			}
		}
	}
	for _, key := range []string{"warehouse", "accountant"} {
		for _, p := range chat {
			if slices.Contains(roles[key], p) {
				t.Errorf("%s không được có %s", key, p)
			}
		}
	}
}

// EnsureDefaultRoles với quyền M4 (Task 143): tenant mới có quyền M4 ở sale, manager, owner; chạy lần hai không đổi số
// dòng (TestEnsureDefaultRoles_Idempotent phủ chung); sale đã sửa (customized) không được thêm quyền M4, owner thì có.
func TestEnsureDefaultRoles_M4(t *testing.T) {
	t.Parallel()
	s, a := rolesTenant(t)
	r := rolesByKey(t, s, a)
	for _, p := range []identity.Permission{"conversation.reply", "live.operate", "files.upload"} {
		if !slices.Contains(r["sale"].Permissions, p) {
			t.Errorf("sale (tenant mới) thiếu %s", p)
		}
	}
	execAll(t, s, a,
		"DELETE FROM identity.role_permissions WHERE permission IN ('conversation.reply', 'live.operate', 'channel.manage')",
		"UPDATE identity.roles SET customized = true WHERE key IN ('sale', 'owner')",
	)
	ensureRoles(t, s, a)
	r = rolesByKey(t, s, a)
	for _, p := range []identity.Permission{"conversation.reply", "live.operate"} {
		if slices.Contains(r["sale"].Permissions, p) {
			t.Errorf("sale đã sửa không được nhận lại %s", p)
		}
	}
	for _, p := range m4Permissions() {
		if !slices.Contains(r["owner"].Permissions, p) {
			t.Errorf("owner (kể cả đã sửa) thiếu %s", p)
		}
		if !slices.Contains(r["manager"].Permissions, p) {
			t.Errorf("manager chưa sửa thiếu %s", p)
		}
	}
}
