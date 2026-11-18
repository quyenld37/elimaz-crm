package tenancy

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/platform/internal/store"
)

// ErrRouteTaken báo khóa định tuyến đã thuộc một tenant khác (PutRoute không ghi đè).
var ErrRouteTaken = errors.New("tenancy: khóa định tuyến đã thuộc tenant khác")

// PutRoute ghi tuyến kind/key → tenant đang trong ngữ cảnh của tx (tx là transaction WithTenant của crm_app) qua hàm
// platform.put_tenant_route (migration 0290). kind chỉ nhận carrier_account (M3), fb_page (M4), service_key (M5).
// Đã có dòng của chính tenant này → không làm gì; của tenant khác → ErrRouteTaken. Thiếu ngữ cảnh tenant, kind khác,
// khóa rỗng → lỗi. Mọi lỗi (kể cả ErrRouteTaken) làm transaction của PostgreSQL hỏng: bên gọi trả lỗi lên, không chạy
// tiếp câu nào trong tx.
func PutRoute(ctx context.Context, tx pgx.Tx, kind, key string) error {
	err := store.New(tx).PutTenantRoute(ctx, store.PutTenantRouteParams{Kind: kind, ExternalKey: key})
	if pe, ok := errors.AsType[*pgconn.PgError](err); ok && pe.Code == "23505" {
		return fmt.Errorf("%w: %s", ErrRouteTaken, kind)
	}
	if err != nil {
		return fmt.Errorf("ghi định tuyến %s: %w", kind, err)
	}
	return nil
}
