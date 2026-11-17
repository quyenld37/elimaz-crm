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
