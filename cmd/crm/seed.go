package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/fakecarrier"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// seedTenantName là tenant của máy dev (kế hoạch M0, Task 7).
const seedTenantName = "ELIMAZ"

// seedCarrierAccount là mã tài khoản hãng giả của máy dev (Task M1-31), cũng là khóa tuyến webhook carrier_account.
const seedCarrierAccount = "fake-1"

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
// dòng tenant_routes('staff_email', email), người bán ELIMAZ (catalog), kho MAIN "Kho chính" do ELIMAZ vận hành
// (inventory), tài khoản hãng giả fake-1 ở kho MAIN và tuyến carrier_account:fake-1 (fulfillment). Chạy lại được.
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
	// Người bán của chính shop (ADR-29, kế hoạch M1 Task M1-27): đúng một người bán ELIMAZ, kind own; kho MAIN do
	// người bán đó vận hành (Task M1-28).
	if err := tenancy.WithTenant(ctx, owner, tenantID, func(tx pgx.Tx) error {
		sellerID, err := catalog.EnsureDefaultSeller(ctx, tx, tenantID)
		if err != nil {
			return err //nolint:wrapcheck // đã bọc trong catalog
		}
		warehouseID, err := inventory.EnsureDefaultWarehouse(ctx, tx, tenantID, sellerID)
		if err != nil {
			return err //nolint:wrapcheck // đã bọc trong inventory
		}
		// Tài khoản hãng giả fake-1 ở kho MAIN (Task M1-31); chỉ dùng khi worker có FAKECARRIER_URL.
		_, err = fulfillment.EnsureCarrierAccount(ctx, tx, fulfillment.AccountInput{
			SellerID: sellerID, WarehouseID: warehouseID, CarrierKey: fakecarrier.CarrierKey, Name: "Hãng giả",
			ExternalAccountRef: seedCarrierAccount,
		})
		return err //nolint:wrapcheck // đã bọc trong fulfillment
	}); err != nil {
		return fmt.Errorf("seed người bán, kho, tài khoản hãng: %w", err)
	}
	if err := tenancy.AddRoute(ctx, owner, fulfillment.RouteKindCarrierAccount, seedCarrierAccount, tenantID); err != nil {
		return err //nolint:wrapcheck // đã bọc trong tenancy
	}
	logger.Info("seed-dev xong", slog.String("tenant_id", tenantID.String()))
	return nil
}
