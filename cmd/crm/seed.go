package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// seedTenantName là tenant của máy dev (kế hoạch M0, Task 7).
const seedTenantName = "ELIMAZ"

var errSeedRole = errors.New("seed-dev chỉ chạy với CRM_ROLE=migrate (cần DATABASE_OWNER_URL: crm_app chỉ đọc platform.tenant_routes)")

// seedDev (`crm seed-dev <email>`) dựng dữ liệu dev trên database đã migrate, bằng chuỗi chủ bảng: tenant "ELIMAZ",
// năm vai trò mẫu (identity.EnsureDefaultRoles; owner có mọi quyền), user theo email được gán owner phạm vi all,
// dòng tenant_routes('staff_email', email), người bán ELIMAZ (catalog). Chạy lại được.
func seedDev(ctx context.Context, cfg config.Config, email string, logger *slog.Logger) error {
	if cfg.Role != config.RoleMigrate || cfg.Migrate == nil {
		return errSeedRole
	}
	owner, err := db.Open(ctx, cfg.Migrate.OwnerURL, db.PoolOptions{MaxConns: cfg.DBMaxConns})
	if err != nil {
		return fmt.Errorf("mở database bằng chủ bảng: %w", err)
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
	// Người bán của chính shop (ADR-29, kế hoạch M1 Task M1-27): đúng một người bán ELIMAZ, kind own.
	if err := tenancy.WithTenant(ctx, owner, tenantID, func(tx pgx.Tx) error {
		_, err := catalog.EnsureDefaultSeller(ctx, tx, tenantID)
		return err //nolint:wrapcheck // đã bọc trong catalog
	}); err != nil {
		return fmt.Errorf("seed người bán: %w", err)
	}
	logger.Info("seed-dev xong", slog.String("tenant_id", tenantID.String()))
	return nil
}
