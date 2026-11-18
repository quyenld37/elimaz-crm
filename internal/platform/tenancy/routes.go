package tenancy

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
)

// Resolve tìm tenant của khóa ngoài (kind: fb_page, staff_email, carrier_account, selftest) trong
// platform.tenant_routes. Khóa không có: uuid.Nil, false, nil. Đọc ngoài ngữ cảnh tenant (bảng không RLS).
func Resolve(ctx context.Context, d *db.DB, kind, externalKey string) (uuid.UUID, bool, error) {
	id, err := store.New(d.Pool).ResolveTenantRoute(ctx, store.ResolveTenantRouteParams{Kind: kind, ExternalKey: externalKey})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("tìm tenant theo %s: %w", kind, err)
	}
	return id, true, nil
}

// ListTenantIDs trả mã mọi tenant, tăng dần (việc định kỳ tạo một việc cho từng tenant, ADR-29).
func ListTenantIDs(ctx context.Context, d *db.DB) ([]uuid.UUID, error) {
	ids, err := store.New(d.Pool).ListTenantIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách tenant: %w", err)
	}
	return ids, nil
}

// ErrRouteConflict báo khóa ngoài đã trỏ tới một tenant khác (AddRoute không ghi đè).
var ErrRouteConflict = errors.New("tenancy: khóa định tuyến đã thuộc tenant khác")

// SeedTenant (lệnh crm seed-dev, chủ bảng) trả tenant cũ nhất có tên name, chưa có thì tạo. Chạy lại không tạo thêm.
func SeedTenant(ctx context.Context, owner *db.DB, name string) (uuid.UUID, error) {
	var id uuid.UUID
	err := owner.RetryTx(ctx, db.TxOptions{}, func(tx pgx.Tx) error {
		q := store.New(tx)
		found, err := q.FindTenantByName(ctx, name)
		if err == nil {
			id = found
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("tìm tenant: %w", err)
		}
		t, err := q.CreateTenant(ctx, name)
		if err != nil {
			return fmt.Errorf("tạo tenant: %w", err)
		}
		id = t.ID
		return nil
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("seed tenant: %w", err)
	}
	return id, nil
}

// AddRoute (chủ bảng; crm_app chỉ đọc platform.tenant_routes) thêm định tuyến kind/externalKey → tenantID. Đã có
// dòng trỏ đúng tenant thì không làm gì; trỏ tenant khác thì ErrRouteConflict (không ghi đè).
func AddRoute(ctx context.Context, owner *db.DB, kind, externalKey string, tenantID uuid.UUID) error {
	got, err := store.New(owner.Pool).AddTenantRoute(ctx, store.AddTenantRouteParams{Kind: kind, ExternalKey: externalKey, TenantID: tenantID})
	if err != nil {
		return fmt.Errorf("thêm định tuyến %s: %w", kind, err)
	}
	if got != tenantID {
		return fmt.Errorf("%w: %s", ErrRouteConflict, kind)
	}
	return nil
}
