package identity_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

func TestLoadUserGrants(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "A")
	uid := s.User(t, a, "Lan@Elimaz.test", "active")
	s.Grant(t, a, uid, []string{"audit.read", "realtime.subscribe"}, "all", "")
	s.Grant(t, a, uid, []string{"conversation.read"}, "page", "p1")

	u, err := identity.LoadUser(t.Context(), s.App, "lan@elimaz.test")
	if err != nil {
		t.Fatal(err)
	}
	if u.UserID != uid || u.TenantID != a || !strings.EqualFold(u.Email, "lan@elimaz.test") {
		t.Fatalf("AuthUser sai: %+v", u)
	}
	want := []identity.Grant{
		{Permission: "audit.read", Scope: identity.Scope{Type: "all"}},
		{Permission: "conversation.read", Scope: identity.Scope{Type: "page", ID: "p1"}},
		{Permission: "realtime.subscribe", Scope: identity.Scope{Type: "all"}},
	}
	got := slices.Clone(u.Grants)
	slices.SortFunc(got, func(x, y identity.Grant) int { return strings.Compare(string(x.Permission), string(y.Permission)) })
	if !slices.Equal(got, want) {
		t.Fatalf("grants = %v, mong %v", got, want)
	}
	// Email khác hoa thường vẫn tìm thấy (Cloudflare có thể gửi email giữ hoa thường như người dùng gõ).
	if _, err := identity.LoadUser(t.Context(), s.App, "LAN@elimaz.test"); err != nil {
		t.Fatalf("email khác hoa thường: %v", err)
	}
}

func TestLoadUserErrors(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "A")
	uid := s.User(t, a, "khoa@elimaz.test", "locked")
	s.Grant(t, a, uid, []string{"audit.read"}, "all", "")
	// Có dòng định tuyến nhưng không có dòng users (dữ liệu lệch) → user lạ.
	if _, err := s.Owner.Pool.Exec(t.Context(),
		"INSERT INTO platform.tenant_routes (kind, external_key, tenant_id) VALUES ('staff_email', 'mo@elimaz.test', $1)", a); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		email string
		want  error
	}{
		{"khong-co@elimaz.test", identity.ErrUnknownUser},
		{"mo@elimaz.test", identity.ErrUnknownUser},
		{"khoa@elimaz.test", identity.ErrLocked},
		{"", identity.ErrUnknownUser},
	}
	for _, c := range cases {
		if _, err := identity.LoadUser(t.Context(), s.App, c.email); !errors.Is(err, c.want) {
			t.Errorf("%q: lỗi %v, mong %v", c.email, err, c.want)
		}
	}
	// Mở khóa: đọc lại ở lần sau (không đệm).
	s.SetStatus(t, a, uid, "active")
	if _, err := identity.LoadUser(t.Context(), s.App, "khoa@elimaz.test"); err != nil {
		t.Fatalf("sau khi mở khóa: %v", err)
	}
}

// User của tenant B có cùng email với tenant A không lẫn: định tuyến quyết định tenant, grant chỉ của tenant đó.
func TestLoadUserTenantIsolation(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	a, b := s.Tenant(t, "A"), s.Tenant(t, "B")
	ua := s.User(t, a, "a@elimaz.test", "active")
	s.Grant(t, a, ua, []string{"audit.read"}, "all", "")
	// Tenant B có user cùng email nhưng không có định tuyến (định tuyến trỏ A).
	s.Exec(t, b, func(tx pgx.Tx) error {
		var id uuid.UUID
		err := tx.QueryRow(t.Context(), "INSERT INTO identity.users (tenant_id, email, display_name) VALUES ($1, 'a@elimaz.test', 'B') RETURNING id", b).Scan(&id)
		return err //nolint:wrapcheck // test
	})
	u, err := identity.LoadUser(t.Context(), s.App, "a@elimaz.test")
	if err != nil || u.TenantID != a || u.UserID != ua || len(u.Grants) != 1 {
		t.Fatalf("mong user của A với 1 grant, có %+v, %v", u, err)
	}
}

func TestGrantScopeCheck(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "A")
	uid := s.User(t, a, "x@elimaz.test", "active")
	bad := []struct{ typ, id string }{{"all", "x"}, {"page", ""}, {"tenant", "p1"}}
	for _, c := range bad {
		err := s.TryExec(t.Context(), a, func(tx pgx.Tx) error {
			var roleID uuid.UUID
			if err := tx.QueryRow(t.Context(), "INSERT INTO identity.roles (tenant_id, key, name) VALUES ($1, $2, 'r') RETURNING id",
				a, "r-"+c.typ+c.id).Scan(&roleID); err != nil {
				return err //nolint:wrapcheck // test
			}
			_, err := tx.Exec(t.Context(), "INSERT INTO identity.user_role_grants (tenant_id, user_id, role_id, scope_type, scope_id) VALUES ($1, $2, $3, $4, $5)",
				a, uid, roleID, c.typ, c.id)
			return err //nolint:wrapcheck // test
		})
		if err == nil || !strings.Contains(err.Error(), "check constraint") {
			t.Errorf("scope (%q, %q) phải vi phạm CHECK, có %v", c.typ, c.id, err)
		}
	}
	// users.status chỉ nhận active, locked.
	err := s.TryExec(t.Context(), a, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "INSERT INTO identity.users (tenant_id, email, display_name, status) VALUES ($1, 'y@elimaz.test', 'y', 'deleted')", a)
		return err //nolint:wrapcheck // test
	})
	if err == nil || !strings.Contains(err.Error(), "check constraint") {
		t.Fatalf("status lạ phải vi phạm CHECK, có %v", err)
	}
}

// crm_app không thấy dòng ngoài ngữ cảnh tenant, và từ M1 chỉ ghi được đúng phần quản lý nhân viên (Task M1-26) và vai
// trò tự định nghĩa (Task M1-56, migration 0128) cần: INSERT users, UPDATE (display_name, status, version) của users,
// INSERT/DELETE user_role_grants, INSERT roles, UPDATE (name, customized, status, version) của roles, INSERT/DELETE
// role_permissions. Mọi ghi khác vào bảng identity (đổi email, xóa user, đổi khóa hay tenant của vai trò, xóa vai trò,
// sửa dòng quyền) vẫn bị từ chối; RLS chặn ghi vai trò cho tenant khác.
func TestAppRolePrivilegesAndRLS(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "A")
	s.User(t, a, "r@elimaz.test", "active")
	var n int
	if err := s.App.Pool.QueryRow(t.Context(), "SELECT count(*) FROM identity.users").Scan(&n); err != nil || n != 0 {
		t.Fatalf("ngoài ngữ cảnh mong 0 dòng, có %d, %v", n, err)
	}
	for _, q := range []string{
		"UPDATE identity.users SET email = 'z@elimaz.test'",
		"UPDATE identity.users SET tenant_id = gen_random_uuid()",
		"DELETE FROM identity.users",
		"UPDATE identity.roles SET key = 'x'",
		"UPDATE identity.roles SET tenant_id = gen_random_uuid()",
		"DELETE FROM identity.roles",
		"UPDATE identity.role_permissions SET permission = 'audit.read'",
		"UPDATE identity.user_role_grants SET scope_type = 'all'",
	} {
		err := tenancy.WithTenant(t.Context(), s.App, a, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), q)
			return err //nolint:wrapcheck // test
		})
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("crm_app %q phải bị từ chối, có %v", q, err)
		}
	}
	for _, q := range []string{
		"UPDATE identity.users SET status = 'locked', version = version + 1",
		"INSERT INTO identity.roles (tenant_id, key, name, customized) VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, 'custom_x', 'x', true)",
		"UPDATE identity.roles SET name = 'y', customized = true, status = 'archived', version = version + 1 WHERE key = 'custom_x'",
		"INSERT INTO identity.role_permissions (tenant_id, role_id, permission) SELECT tenant_id, id, 'audit.read' FROM identity.roles WHERE key = 'custom_x'",
		"DELETE FROM identity.role_permissions WHERE permission = 'audit.read'",
		"SELECT id FROM identity.roles FOR UPDATE",
	} {
		if err := tenancy.WithTenant(t.Context(), s.App, a, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), q)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Errorf("crm_app %q phải được: %v", q, err)
		}
	}
	// RLS: trong ngữ cảnh tenant A không chèn được vai trò cho tenant B.
	b := s.Tenant(t, "B")
	err := tenancy.WithTenant(t.Context(), s.App, a, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "INSERT INTO identity.roles (tenant_id, key, name) VALUES ($1, 'custom_b', 'b')", b)
		return err //nolint:wrapcheck // test
	})
	if err == nil || !strings.Contains(err.Error(), "row-level security") {
		t.Errorf("chèn vai trò cho tenant khác phải bị RLS chặn, có %v", err)
	}
}

func TestSchemaRulesStillClean(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	got, err := tenancy.CheckSchemaRules(t.Context(), s.Owner.Pool, platformTables())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("CheckSchemaRules phải rỗng, có:\n%s", strings.Join(got, "\n"))
	}
}

// SeedOwner chạy lại được: lần hai không thêm dòng, user có mọi quyền với phạm vi all.
func TestSeedOwner(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "ELIMAZ")
	for range 2 {
		if err := identity.SeedOwner(t.Context(), s.Owner, a, "Chu@Elimaz.test"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Owner.Pool.Exec(t.Context(),
		"INSERT INTO platform.tenant_routes (kind, external_key, tenant_id) VALUES ('staff_email', 'chu@elimaz.test', $1)", a); err != nil {
		t.Fatal(err)
	}
	u, err := identity.LoadUser(t.Context(), s.App, "chu@elimaz.test")
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Grants) != len(identity.Permissions) {
		t.Fatalf("mong %d grant, có %d: %v", len(identity.Permissions), len(u.Grants), u.Grants)
	}
	for _, d := range identity.Permissions {
		if !identity.Can(u, d.Key, nil) {
			t.Errorf("owner thiếu quyền %s", d.Key)
		}
	}
}

// platformTables: bảng nền tảng không RLS có cột tenant_id của M0 (như test của tenancy).
func platformTables() []string { return []string{"platform.tenant_routes", "platform.jobs"} }
