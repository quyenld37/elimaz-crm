package shoptest

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/catalogtest"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// SeedSellers tạo (nếu chưa có) hai người bán của tenantID bằng d (chủ bảng: crm_app không ghi được catalog.sellers):
// ELIMAZ (kind own, người bán mặc định của shop) và PARTNER1 (kind partner) — dữ liệu thử hai người bán của KH14,
// ADR-29. Trả mã của hai người theo thứ tự đó. Lỗi làm test dừng (chỉ gọi từ goroutine của test).
func SeedSellers(t testing.TB, d *db.DB, tenantID uuid.UUID) (elimaz, partner uuid.UUID) {
	t.Helper()
	if err := tenancy.WithTenant(t.Context(), d, tenantID, func(tx pgx.Tx) error {
		elimaz, partner = catalogtest.Sellers(t, tx, tenantID)
		return nil
	}); err != nil {
		t.Fatalf("shoptest: tạo hai người bán: %v", err)
	}
	return elimaz, partner
}
