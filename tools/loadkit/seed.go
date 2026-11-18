package main

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/selftest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// loadTenantName là tenant riêng của bộ thử tải (không lẫn với tenant "ELIMAZ" của seed-dev).
const loadTenantName = "Thử tải"

// seedStaff (chủ bảng) dựng tenant thử, n nhân viên giả staffEmail(1..n) với vai trò owner phạm vi all
// (identity.SeedOwner, như crm seed-dev), định tuyến email → tenant và ("selftest", tenantKey) → tenant. Chạy lại được.
func seedStaff(ctx context.Context, owner *db.DB, n int, tenantKey string) (uuid.UUID, error) {
	id, err := tenancy.SeedTenant(ctx, owner, loadTenantName)
	if err != nil {
		return uuid.Nil, fmt.Errorf("seed tenant thử: %w", err)
	}
	for i := 1; i <= n; i++ {
		email := staffEmail(i)
		if err := identity.SeedOwner(ctx, owner, id, email); err != nil {
			return uuid.Nil, fmt.Errorf("seed nhân viên %d: %w", i, err)
		}
		if err := tenancy.AddRoute(ctx, owner, identity.RouteKindStaffEmail, identity.NormalizeEmail(email), id); err != nil {
			return uuid.Nil, fmt.Errorf("định tuyến nhân viên %d: %w", i, err)
		}
	}
	if err := tenancy.AddRoute(ctx, owner, selftest.Key, tenantKey, id); err != nil {
		return uuid.Nil, fmt.Errorf("định tuyến selftest: %w", err)
	}
	return id, nil
}
