package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// recomputeCommand là lệnh vận hành `crm recompute-customer-stats <mã khách>` (Task 434; RUNBOOK mục "Chỉ số khách
// sai"): tính lại chỉ số và áp quy tắc nhãn cho gốc hiện tại của một khách khi không có việc event.customer_stats lỗi
// nào để thử lại.
const recomputeCommand = "recompute-customer-stats"

var errCustomerUnknown = errors.New("không có khách với mã này ở tenant nào")

// recomputeResult là kết quả của lệnh: tenant của khách, gốc hiện tại và chỉ số mới của gốc.
type recomputeResult struct {
	TenantID, RootID uuid.UUID
	Stats            customer.Stats
}

// recomputeCustomerStats tìm tenant của customerID (thử lần lượt mọi tenant: RLS ẩn khách của tenant khác, không có →
// customer.ErrNotFound → transaction lùi, thử tenant kế), rồi trong một transaction tenant làm đúng hai bước của consumer
// customer_stats: tính lại chỉ số cả nhóm từ ord.orders_v, ord.returns_v (RecomputeStats; gốc lead có đơn sang
// customer) và áp quy tắc nhãn (ApplyTagRules). Chạy lại cho cùng kết quả. Không có ở tenant nào → errCustomerUnknown.
func recomputeCustomerStats(ctx context.Context, d *db.DB, bus realtime.Bus, customerID uuid.UUID, now time.Time,
) (recomputeResult, error) {
	tenants, err := tenancy.ListTenantIDs(ctx, d)
	if err != nil {
		return recomputeResult{}, err //nolint:wrapcheck // đã bọc trong tenancy
	}
	w := customer.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: bus}
	for _, tenantID := range tenants {
		var res recomputeResult
		err := tenancy.WithTenant(ctx, d, tenantID, func(tx pgx.Tx) error {
			if err := w.RecomputeStats(ctx, tx, customerID, now); err != nil {
				return err //nolint:wrapcheck // lỗi của customer đã bọc
			}
			if err := w.ApplyTagRules(ctx, tx, customerID, now); err != nil {
				return err //nolint:wrapcheck // như trên
			}
			root, err := customer.RootOf(ctx, tx, customerID)
			if err != nil {
				return err //nolint:wrapcheck // như trên
			}
			s, err := customer.StatsFor(ctx, tx, root)
			res = recomputeResult{TenantID: tenantID, RootID: root, Stats: s}
			return err //nolint:wrapcheck // như trên
		})
		if errors.Is(err, customer.ErrNotFound) {
			continue
		}
		if err != nil {
			return recomputeResult{}, fmt.Errorf("tính lại chỉ số khách ở tenant %s: %w", tenantID, err)
		}
		return res, nil
	}
	return recomputeResult{}, fmt.Errorf("%s: %w", customerID, errCustomerUnknown)
}

// openCommandDB mở pool nhỏ (2 kết nối) bằng DATABASE_URL (crm_app, RLS vẫn áp) cho lệnh vận hành một lần.
func openCommandDB(ctx context.Context, cfg config.Config) (*db.DB, error) {
	opts := poolOptions(cfg)
	opts.MaxConns = 2
	d, err := db.Open(ctx, cfg.DatabaseURL, opts)
	if err != nil {
		return nil, fmt.Errorf("mở database: %w", err)
	}
	return d, nil
}

// runRecompute mở pool bằng DATABASE_URL (crm_app, RLS vẫn áp) của vai trò đang chạy, tính lại cho customerID và in một
// dòng kết quả (chỉ mã và số, không dữ liệu cá nhân). Báo hiệu customer.changed đi qua pg_notify trong transaction.
func runRecompute(ctx context.Context, cfg config.Config, customerID uuid.UUID, stdout io.Writer, logger *slog.Logger,
) error {
	d, err := openCommandDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer d.Pool.Close()
	hub := realtime.NewPGHub(realtime.PGHubOptions{URL: cfg.DatabaseURL, Logger: logger})
	res, err := recomputeCustomerStats(ctx, d, hub, customerID, time.Now())
	if err != nil {
		return err
	}
	s := res.Stats
	last := "chưa có"
	if s.LastOrderAt != nil {
		last = s.LastOrderAt.UTC().Format(time.RFC3339)
	}
	_, _ = fmt.Fprintf(stdout, "tenant %s, gốc %s: %d đơn, %d đã giao, %d hoàn qua hãng, tổng chi %d đồng, "+
		"tỷ lệ hoàn %d/10000, đơn cuối %s\n", res.TenantID, res.RootID, s.OrderCount, s.DeliveredCount, s.ReturnedCount,
		s.TotalSpent, s.ReturnRateBP, last)
	logger.Info("đã tính lại chỉ số khách", slog.String("tenant_id", res.TenantID.String()),
		slog.String("root_id", res.RootID.String()))
	return nil
}
