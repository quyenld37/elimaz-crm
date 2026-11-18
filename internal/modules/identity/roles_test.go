package identity_test

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/testkit"
)

// Năm vai trò mẫu đúng kế hoạch Task M1-26: owner đủ mọi quyền; manager mọi quyền trừ identity.users.manage,
// identity.roles.manage, platform.*; ba vai trò còn lại đúng danh sách.
func TestDefaultRoles_Content(t *testing.T) {
	all := make([]identity.Permission, 0, len(identity.Permissions))
	for _, d := range identity.Permissions {
		all = append(all, d.Key)
	}
	var manager []identity.Permission
	for _, p := range all {
		if p != "identity.users.manage" && p != "identity.roles.manage" && (len(p) < 9 || p[:9] != "platform.") {
			manager = append(manager, p)
		}
	}
	want := map[string][]identity.Permission{
		"owner":   all,
		"manager": manager,
		"sale":    {"catalog.read", "order.create", "order.read", "inventory.read", "fulfillment.read", "fulfillment.ship", "realtime.subscribe"},
		"warehouse": {
			"catalog.read", "inventory.read", "inventory.documents.create", "inventory.stocktake", "fulfillment.read",
			"fulfillment.ship", "inventory.suppliers.manage", "realtime.subscribe",
		},
		"accountant": {"catalog.read", "inventory.read", "order.read", "fulfillment.read", "realtime.subscribe"},
	}
	names := map[string]string{"owner": "Chủ", "manager": "Quản lý", "sale": "Bán hàng", "warehouse": "Kho", "accountant": "Kế toán"}
	if len(identity.DefaultRoles) != len(want) {
		t.Fatalf("mong %d vai trò mẫu, có %d", len(want), len(identity.DefaultRoles))
	}
	for _, r := range identity.DefaultRoles {
		w, ok := want[r.Key]
		if !ok || r.Name != names[r.Key] {
			t.Errorf("vai trò lạ hoặc sai tên: %s %q", r.Key, r.Name)
			continue
		}
		got := slices.Sorted(slices.Values(r.Permissions))
		if !slices.Equal(got, slices.Sorted(slices.Values(w))) {
			t.Errorf("quyền của %s = %v, mong %v", r.Key, got, w)
		}
		for _, p := range r.Permissions {
			if !identity.IsKnown(p) {
				t.Errorf("%s có quyền lạ %s", r.Key, p)
			}
		}
	}
}

// rolesTenant: database thử có một tenant đã chạy EnsureDefaultRoles một lần.
func rolesTenant(t *testing.T) (*testkit.Staff, uuid.UUID) {
	t.Helper()
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "A")
	ensureRoles(t, s, a)
	return s, a
}

func ensureRoles(t *testing.T, s *testkit.Staff, a uuid.UUID) {
	t.Helper()
	s.Exec(t, a, func(tx pgx.Tx) error { return identity.EnsureDefaultRoles(t.Context(), tx, a) })
}

func listRoles(t *testing.T, s *testkit.Staff, a uuid.UUID) []identity.RoleRow {
	t.Helper()
	var rows []identity.RoleRow
	s.Exec(t, a, func(tx pgx.Tx) error {
		var err error
		rows, err = identity.ListRoles(t.Context(), tx)
		return err //nolint:wrapcheck // test
	})
	return rows
}

func countRoleRows(t *testing.T, s *testkit.Staff, a uuid.UUID) (roles, perms int) {
	t.Helper()
	for _, r := range listRoles(t, s, a) {
		roles++
		perms += len(r.Permissions)
	}
	return roles, perms
}

// EnsureDefaultRoles chạy hai lần không đổi gì (số dòng roles, role_permissions giữ nguyên).
func TestEnsureDefaultRoles_Idempotent(t *testing.T) {
	s, a := rolesTenant(t)
	r1, p1 := countRoleRows(t, s, a)
	wantPerms := 0
	for _, d := range identity.DefaultRoles {
		wantPerms += len(d.Permissions)
	}
	if r1 != len(identity.DefaultRoles) || p1 != wantPerms {
		t.Fatalf("lần 1: %d vai trò, %d quyền; mong %d, %d", r1, p1, len(identity.DefaultRoles), wantPerms)
	}
	ensureRoles(t, s, a)
	if r2, p2 := countRoleRows(t, s, a); r2 != r1 || p2 != p1 {
		t.Fatalf("lần 2 đổi số dòng: %d/%d → %d/%d", r1, p1, r2, p2)
	}
	for _, r := range listRoles(t, s, a) {
		if r.Status != "active" || r.Customized {
			t.Errorf("vai trò %s: trạng thái %s, customized %v", r.Key, r.Status, r.Customized)
		}
	}
}

// Chủ shop đã sửa vai trò sale (bỏ order.create) và owner (bỏ audit.read): sale giữ nguyên, owner được thêm lại.
func TestEnsureDefaultRoles_CustomizedKept(t *testing.T) {
	s, a := rolesTenant(t)
	s.Exec(t, a, func(tx pgx.Tx) error {
		for _, q := range []string{
			"UPDATE identity.roles SET customized = true WHERE key IN ('sale', 'owner')",
			"DELETE FROM identity.role_permissions WHERE permission = 'order.create' AND role_id = (SELECT id FROM identity.roles WHERE key = 'sale')",
			"DELETE FROM identity.role_permissions WHERE permission = 'audit.read' AND role_id = (SELECT id FROM identity.roles WHERE key = 'owner')",
		} {
			if _, err := tx.Exec(t.Context(), q); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	ensureRoles(t, s, a)
	rows := listRoles(t, s, a)
	idx := slices.IndexFunc(rows, func(r identity.RoleRow) bool { return r.Key == "sale" })
	if idx < 0 || slices.Contains(rows[idx].Permissions, "order.create") || !rows[idx].Customized {
		t.Errorf("sale đã sửa bị thêm lại quyền: %+v", rows)
	}
	idx = slices.IndexFunc(rows, func(r identity.RoleRow) bool { return r.Key == "owner" })
	if idx < 0 || len(rows[idx].Permissions) != len(identity.Permissions) {
		t.Errorf("owner phải đủ %d quyền: %+v", len(identity.Permissions), rows)
	}
}

// Bước đồng bộ sau migrate (Task 315): shop chưa có vai trò nhận đủ vai trò mẫu; quyền mới của bảng quyền (giả lập
// bằng cách xóa dòng role_permissions như database có trước khi quyền đó được thêm vào mã) vào vai trò mẫu chưa sửa;
// vai trò mẫu đã sửa (customized) và vai trò tự định nghĩa không đổi; chạy lần hai không đổi thêm.
func TestSyncDefaultRoles(t *testing.T) {
	s, a := rolesTenant(t)
	b := s.Tenant(t, "B") // shop mới, chưa có vai trò nào
	s.Exec(t, a, func(tx pgx.Tx) error {
		for _, q := range []string{
			"UPDATE identity.roles SET customized = true WHERE key = 'accountant'",
			"DELETE FROM identity.role_permissions WHERE permission = 'order.read' AND role_id IN (SELECT id FROM identity.roles WHERE key IN ('sale', 'accountant'))",
			"INSERT INTO identity.roles (tenant_id, key, name) VALUES (current_setting('app.tenant_id')::uuid, 'tu-dat', 'Tự đặt')",
		} {
			if _, err := tx.Exec(t.Context(), q); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	n, err := identity.SyncDefaultRoles(t.Context(), s.Owner)
	if err != nil || n != 2 {
		t.Fatalf("SyncDefaultRoles = %d, %v; mong 2 tenant", n, err)
	}
	byKey := func(tenant uuid.UUID) map[string]identity.RoleRow {
		m := map[string]identity.RoleRow{}
		for _, r := range listRoles(t, s, tenant) {
			m[r.Key] = r
		}
		return m
	}
	ra := byKey(a)
	if !slices.Contains(ra["sale"].Permissions, "order.read") {
		t.Errorf("sale chưa sửa phải nhận lại order.read: %v", ra["sale"].Permissions)
	}
	if slices.Contains(ra["accountant"].Permissions, "order.read") || !ra["accountant"].Customized {
		t.Errorf("accountant đã sửa không được đổi: %+v", ra["accountant"])
	}
	if len(ra["tu-dat"].Permissions) != 0 {
		t.Errorf("vai trò tự định nghĩa không được thêm quyền: %v", ra["tu-dat"].Permissions)
	}
	rb := byKey(b)
	for _, d := range identity.DefaultRoles {
		if r, ok := rb[d.Key]; !ok || len(r.Permissions) != len(d.Permissions) {
			t.Errorf("shop mới thiếu vai trò mẫu %s hoặc thiếu quyền: %+v", d.Key, r)
		}
	}
	ra1, pa1 := countRoleRows(t, s, a)
	rb1, pb1 := countRoleRows(t, s, b)
	if _, err := identity.SyncDefaultRoles(t.Context(), s.Owner); err != nil {
		t.Fatal(err)
	}
	ra2, pa2 := countRoleRows(t, s, a)
	rb2, pb2 := countRoleRows(t, s, b)
	if ra1 != ra2 || pa1 != pa2 || rb1 != rb2 || pb1 != pb2 {
		t.Fatalf("lần hai đổi số dòng: A %d/%d → %d/%d, B %d/%d → %d/%d", ra1, pa1, ra2, pa2, rb1, pb1, rb2, pb2)
	}
}
