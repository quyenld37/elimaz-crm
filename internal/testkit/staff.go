package testkit

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Staff là database thử đã migrate kèm hai pool: App (crm_app, như tiến trình api) và Owner (chủ bảng, để dựng dữ
// liệu nhân viên, vai trò, phạm vi mà crm_app không được ghi). Mọi dòng của bảng có RLS được ghi trong
// tenancy.WithTenant (FORCE RLS áp cả cho chủ bảng).
type Staff struct {
	App   *db.DB
	Owner *db.DB
}

// OpenStaff tạo database thử và mở hai pool; đóng khi test xong.
func OpenStaff(t testing.TB) *Staff {
	t.Helper()
	_, ownerURL, appURL := StartPostgres(t)
	s := &Staff{}
	for _, p := range []struct {
		dst **db.DB
		url string
	}{{&s.App, appURL}, {&s.Owner, ownerURL}} {
		d, err := db.Open(t.Context(), p.url, db.PoolOptions{MaxConns: 4})
		if err != nil {
			t.Fatalf("testkit: mở pool thử: %v", err)
		}
		t.Cleanup(d.Pool.Close)
		*p.dst = d
	}
	return s
}

// Tenant chèn một tenant và trả mã của nó.
func (s *Staff) Tenant(t testing.TB, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := s.Owner.Pool.QueryRow(t.Context(), "INSERT INTO platform.tenants (name) VALUES ($1) RETURNING id", name).Scan(&id); err != nil {
		t.Fatalf("testkit: chèn tenant: %v", err)
	}
	return id
}

// User chèn nhân viên (status active hoặc locked) và dòng định tuyến staff_email → tenant; trả mã user.
func (s *Staff) User(t testing.TB, tenantID uuid.UUID, email, status string) uuid.UUID {
	t.Helper()
	ctx := t.Context()
	if _, err := s.Owner.Pool.Exec(ctx,
		"INSERT INTO platform.tenant_routes (kind, external_key, tenant_id) VALUES ('staff_email', $1, $2)",
		strings.ToLower(email), tenantID); err != nil {
		t.Fatalf("testkit: chèn định tuyến email: %v", err)
	}
	var id uuid.UUID
	s.Exec(t, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			"INSERT INTO identity.users (tenant_id, email, display_name, status) VALUES ($1, $2, $3, $4) RETURNING id",
			tenantID, email, email, status).Scan(&id)
	})
	return id
}

// Grant tạo một vai trò mới có các quyền perms rồi gán cho user với phạm vi (scopeType, scopeID).
func (s *Staff) Grant(t testing.TB, tenantID, userID uuid.UUID, perms []string, scopeType, scopeID string) {
	t.Helper()
	ctx := t.Context()
	s.Exec(t, tenantID, func(tx pgx.Tx) error {
		var roleID uuid.UUID
		if err := tx.QueryRow(ctx, "INSERT INTO identity.roles (tenant_id, key, name) VALUES ($1, $2, $2) RETURNING id",
			tenantID, "role-"+uuid.NewString()).Scan(&roleID); err != nil {
			return fmt.Errorf("chèn vai trò: %w", err)
		}
		for _, p := range perms {
			if _, err := tx.Exec(ctx, "INSERT INTO identity.role_permissions (tenant_id, role_id, permission) VALUES ($1, $2, $3)",
				tenantID, roleID, p); err != nil {
				return fmt.Errorf("chèn quyền: %w", err)
			}
		}
		_, err := tx.Exec(ctx, "INSERT INTO identity.user_role_grants (tenant_id, user_id, role_id, scope_type, scope_id) VALUES ($1, $2, $3, $4, $5)",
			tenantID, userID, roleID, scopeType, scopeID)
		return err //nolint:wrapcheck // Exec báo lỗi kèm ngữ cảnh
	})
}

// SetStatus đổi trạng thái user (active, locked).
func (s *Staff) SetStatus(t testing.TB, tenantID, userID uuid.UUID, status string) {
	t.Helper()
	s.Exec(t, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE identity.users SET status = $2 WHERE id = $1", userID, status)
		return err //nolint:wrapcheck // Exec báo lỗi kèm ngữ cảnh
	})
}

// Exec chạy fn bằng chủ bảng trong ngữ cảnh tenant; lỗi làm test đỏ.
func (s *Staff) Exec(t testing.TB, tenantID uuid.UUID, fn func(pgx.Tx) error) {
	t.Helper()
	if err := s.TryExec(t.Context(), tenantID, fn); err != nil {
		t.Fatalf("testkit: ghi dữ liệu thử: %v", err)
	}
}

// TryExec như Exec nhưng trả lỗi (test mong lỗi, ví dụ vi phạm CHECK).
func (s *Staff) TryExec(ctx context.Context, tenantID uuid.UUID, fn func(pgx.Tx) error) error {
	return tenancy.WithTenant(ctx, s.Owner, tenantID, fn) //nolint:wrapcheck // trả nguyên cho test
}
