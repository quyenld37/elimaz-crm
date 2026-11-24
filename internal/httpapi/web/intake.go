package web

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Trạng thái nhận đơn web cho website (kế hoạch M5, Task 456): chỉ {paused}, không lý do, người, giờ. Bộ đệm
// SpaceWebIntake 5 s; sự kiện order.web_intake.changed làm mới sớm hơn (webcache). Chỉ để hiện: tạo đơn kiểm lại ở
// PostgreSQL (order.CreateFromWeb).
const (
	intakeTTL = 5 * time.Second
	intakeKey = "state"
)

// IntakeStatus trả shop có đang tạm dừng nhận đơn web không.
//
//nolint:ireturn // kiểu trả của operation do ogen sinh (gen.Handler)
func (c *Catalog) IntakeStatus(ctx context.Context, tenant uuid.UUID) (gen.WebIntakeStatusRes, error) {
	raw, _, err := c.rc.GetOrLoad(ctx, tenant, cache.SpaceWebIntake, intakeKey, intakeTTL, func(ctx context.Context) ([]byte, error) {
		var out gen.WebIntakeStatus
		err := tenancy.WithTenant(ctx, c.db, tenant, func(tx pgx.Tx) error {
			wi, err := order.GetWebIntake(ctx, tx)
			out.Paused = wi.Paused
			return err //nolint:wrapcheck // order đã bọc
		}, db.TxAPI)
		if err != nil {
			return nil, fmt.Errorf("đọc trạng thái nhận đơn web cho website: %w", err)
		}
		return out.MarshalJSON()
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // ReadCache đã bọc; ErrBusy giữ nguyên để đổi 503
	}
	var out gen.WebIntakeStatus
	if err := out.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("đọc trạng thái nhận đơn web trong bộ đệm: %w", err)
	}
	return &out, nil
}
