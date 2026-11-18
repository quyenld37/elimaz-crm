package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// seedTenantName là tenant của máy dev (kế hoạch M0, Task 7).
const seedTenantName = "ELIMAZ"

var errSeedRole = errors.New("seed-dev chỉ chạy với CRM_ROLE=migrate (cần DATABASE_OWNER_URL: crm_app chỉ đọc platform.tenant_routes)")

// openOwner mở pool bằng chuỗi chủ bảng (DATABASE_OWNER_URL) của vai trò migrate; bên gọi đóng pool.
func openOwner(ctx context.Context, cfg config.Config) (*db.DB, error) {
	owner, err := db.Open(ctx, cfg.Migrate.OwnerURL, db.PoolOptions{MaxConns: cfg.DBMaxConns})
	if err != nil {
		return nil, fmt.Errorf("mở database bằng chủ bảng: %w", err)
	}
	return owner, nil
}

// seedDev (`crm seed-dev <email>`) dựng dữ liệu dev trên database đã migrate, bằng chuỗi chủ bảng: tenant "ELIMAZ",
// năm vai trò mẫu (identity.EnsureDefaultRoles; owner có mọi quyền), user theo email được gán owner phạm vi all,
// dòng tenant_routes('staff_email', email). Chạy lại được.
func seedDev(ctx context.Context, cfg config.Config, email string, logger *slog.Logger) error {
	if cfg.Role != config.RoleMigrate || cfg.Migrate == nil {
		return errSeedRole
	}
	owner, err := openOwner(ctx, cfg)
	if err != nil {
		return err
	}
	defer owner.Pool.Close()
	tenantID, err := tenancy.SeedTenant(ctx, owner, seedTenantName)
	if err != nil {
		return err //nolint:wrapcheck // đã bọc trong tenancy
	}
	if err := identity.SeedOwner(ctx, owner, tenantID, email); err != nil {
		return err //nolint:wrapcheck // đã bọc trong identity
	}
	if err := tenancy.AddRoute(ctx, owner, identity.RouteKindStaffEmail, identity.NormalizeEmail(email), tenantID); err != nil {
		return err //nolint:wrapcheck // đã bọc trong tenancy
	}
	logger.Info("seed-dev xong", slog.String("tenant_id", tenantID.String()))
	return nil
}
