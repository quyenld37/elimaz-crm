package observability

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/metrics"
)

// deepMaxPendingAge: việc tới hạn chờ lâu hơn mức này thì /health/deep không đạt.
const deepMaxPendingAge = 120 * time.Second

// DeepHealthReport là câu trả lời của GET /health/deep (bộ kiểm từ ngoài, ADR-38): chỉ tuổi và số đếm, không dữ liệu
// khách. Con trỏ nil: chưa có (chưa có nhịp sống, chưa đẩy WAL, chưa sao lưu).
type DeepHealthReport struct {
	OK                  bool
	WorkerHeartbeatAgeS *int64
	IngestHeartbeatAgeS *int64
	OldestPendingAgeS   int64
	Dead, InDoubt       int64
	SpoolFiles          int64
	LastWalArchivedAgeS *int64
	LastBackupAgeH      *int64
}

// DeepHealth đọc báo cáo kiểm sâu. OK false khi nhịp sống của worker hay ingest cũ hơn 90 s (hay chưa có), hoặc việc
// tới hạn chờ lâu nhất quá 120 s.
func DeepHealth(ctx context.Context, d *db.DB) (DeepHealthReport, error) {
	q := store.New(d.Pool)
	now := time.Now()
	var r DeepHealthReport
	ages, err := q.HeartbeatAgesByRole(ctx, now)
	if err != nil {
		return r, fmt.Errorf("đọc nhịp sống: %w", err)
	}
	for _, a := range ages {
		v := int64(a.AgeS)
		switch a.Role {
		case "worker":
			r.WorkerHeartbeatAgeS = &v
		case "ingest":
			r.IngestHeartbeatAgeS = &v
		}
	}
	pending, err := q.OldestPendingAgeSeconds(ctx)
	if err != nil {
		return r, fmt.Errorf("đọc việc chờ: %w", err)
	}
	r.OldestPendingAgeS = int64(pending)
	open, err := q.OpenAttentionCounts(ctx)
	if err != nil {
		return r, fmt.Errorf("đếm việc dead, in_doubt: %w", err)
	}
	r.Dead, r.InDoubt = open.Dead, open.InDoubt
	spool, err := q.LatestGaugeSum(ctx, "spool_files")
	if err != nil {
		return r, fmt.Errorf("đọc số tệp tạm: %w", err)
	}
	r.SpoolFiles = int64(spool)
	if r.LastWalArchivedAgeS, err = walArchivedAge(ctx, d, now); err != nil {
		return r, err
	}
	backup, err := q.LastBackupAt(ctx, "base")
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return r, fmt.Errorf("đọc lần sao lưu: %w", err)
	default:
		h := int64(now.Sub(backup).Hours())
		r.LastBackupAgeH = &h
	}
	maxAge := int64(metrics.HeartbeatMaxAge.Seconds())
	r.OK = r.WorkerHeartbeatAgeS != nil && *r.WorkerHeartbeatAgeS <= maxAge &&
		r.IngestHeartbeatAgeS != nil && *r.IngestHeartbeatAgeS <= maxAge &&
		pending <= deepMaxPendingAge.Seconds()
	return r, nil
}

// walArchivedAge: tuổi (giây) lần đẩy WAL thành công cuối; nil khi archive tắt hay chưa đẩy lần nào.
func walArchivedAge(ctx context.Context, d *db.DB, now time.Time) (*int64, error) {
	s, err := readArchiver(ctx, d)
	if err != nil {
		return nil, err
	}
	if !s.archiveOn || s.lastArchived.IsZero() {
		return nil, nil
	}
	v := int64(now.Sub(s.lastArchived).Seconds())
	return &v, nil
}
