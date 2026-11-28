package httpapi

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Báo cáo sẵn sàng dữ liệu trước đêm mở (Task M7-24 = 269): chỉ đọc, một transaction tenant, chỉ số đếm và mã.

// GetLaunchReadiness (GET /api/v1/reports/launch-readiness, reporting.launch.read) → 200 các mục ok, warn, fail.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetLaunchReadiness(ctx context.Context) (gen.GetLaunchReadinessRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var rep reporting.Readiness
	if err := tenancy.WithTenant(ctx, h.d.DB, u.TenantID, func(tx pgx.Tx) error {
		var err error
		rep, err = reporting.LaunchReadiness(ctx, tx, h.d.DB, h.now())
		return err //nolint:wrapcheck // bọc ngay dưới
	}, db.TxAPI); err != nil {
		return nil, fmt.Errorf("báo cáo sẵn sàng: %w", err)
	}
	out := &gen.LaunchReadiness{
		Items: make([]gen.ReadinessItem, 0, len(rep.Items)),
		Fail:  int32(rep.Fail), Warn: int32(rep.Warn), //nolint:gosec // G115: số mục của báo cáo (dưới 100)
		EvaluatedAt: rep.EvaluatedAt,
	}
	for _, it := range rep.Items {
		out.Items = append(out.Items, gen.ReadinessItem{
			Key: it.Key, Group: it.Group, Status: gen.ReadinessItemStatus(it.Status), Value: it.Value, Target: it.Target,
		})
	}
	return out, nil
}
