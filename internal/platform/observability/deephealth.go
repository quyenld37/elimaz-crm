package observability

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/cache"
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
	// LastFileSyncAgeM: tuổi (phút) lần chép tốt cũ nhất trong ba bucket chép sang kho ngoài; nil khi còn bucket chưa
	// có lần chép tốt nào (Task M1-45). Không tính vào OK: cảnh báo file-sync-age lo việc này.
	LastFileSyncAgeM *int64
	// Valkey: trạng thái hai tiến trình Valkey của tiến trình api (kế hoạch M5 Task 184). Không tính vào OK: Valkey
	// ngừng là chậm đi, không phải hỏng (ADR-39); cảnh báo valkey-down:<tên> lo việc này.
	Valkey ValkeyHealth
}

// Valkey là hai kết nối Valkey của tiến trình gọi DeepHealth; Conn nil là không cấu hình (báo off).
type Valkey struct{ Cache, Gate *cache.Conn }

// ValkeyHealth là trạng thái ok|down|off của valkey-cache và valkey-gate.
type ValkeyHealth struct{ Cache, Gate cache.Health }

// DeepHealth đọc báo cáo kiểm sâu. OK false khi nhịp sống của worker hay ingest cũ hơn 90 s (hay chưa có), hoặc việc
// tới hạn chờ lâu nhất quá 120 s.
//
// Mỗi tuổi so với đồng hồ của bên ghi mốc (Task 445): sao lưu (scripts/backup ghi bằng now()), WAL (pg_stat_archiver) và
// việc chờ tính bằng now() của PostgreSQL trong SQL; nhịp sống (metrics ghi bằng time.Now()) và lần chép tệp
// (scripts/filesync, date +%s) so với now của tiến trình. Valkey: Ping từng kết nối của vk (hạn 50 ms mỗi lần, qua cầu
// dao), không đổi OK.
func DeepHealth(ctx context.Context, d *db.DB, vk Valkey) (DeepHealthReport, error) {
	return deepHealth(ctx, d, vk, time.Now())
}

// deepHealth là DeepHealth với đồng hồ tiến trình now (test giả lệch đồng hồ qua DeepHealthAt).
func deepHealth(ctx context.Context, d *db.DB, vk Valkey, now time.Time) (DeepHealthReport, error) {
	q := store.New(d.Pool)
	r := DeepHealthReport{Valkey: ValkeyHealth{Cache: vk.Cache.Health(ctx), Gate: vk.Gate.Health(ctx)}}
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
	if r.LastWalArchivedAgeS, err = walArchivedAge(ctx, d); err != nil {
		return r, err
	}
	backupS, err := q.LastBackupAgeSeconds(ctx, "base")
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return r, fmt.Errorf("đọc lần sao lưu: %w", err)
	default:
		h := int64(backupS / 3600)
		r.LastBackupAgeH = &h
	}
	if r.LastFileSyncAgeM, err = fileSyncAge(ctx, q, now); err != nil {
		return r, err
	}
	maxAge := int64(metrics.HeartbeatMaxAge.Seconds())
	r.OK = r.WorkerHeartbeatAgeS != nil && *r.WorkerHeartbeatAgeS <= maxAge &&
		r.IngestHeartbeatAgeS != nil && *r.IngestHeartbeatAgeS <= maxAge &&
		pending <= deepMaxPendingAge.Seconds()
	return r, nil
}

// walArchivedAge: tuổi (giây) lần đẩy WAL thành công cuối, theo đồng hồ PostgreSQL; nil khi archive tắt hay chưa đẩy
// lần nào.
func walArchivedAge(ctx context.Context, d *db.DB) (*int64, error) {
	s, err := readArchiver(ctx, d)
	if err != nil {
		return nil, err
	}
	if !s.archiveOn {
		return nil, nil
	}
	return s.lastArchivedAgeS, nil
}

// fileSyncAge: tuổi (phút) của lần copy ok gần nhất cũ nhất trong syncedBuckets; nil khi một bucket chưa có.
func fileSyncAge(ctx context.Context, q *store.Queries, now time.Time) (*int64, error) {
	rows, err := q.FileSyncStatus(ctx, syncedBuckets())
	if err != nil {
		return nil, fmt.Errorf("đọc lần chép sang kho ngoài: %w", err)
	}
	var oldest *int64
	for _, r := range rows {
		if !r.HasOk {
			return nil, nil
		}
		m := int64(now.Sub(r.LastOkAt).Minutes())
		if oldest == nil || m > *oldest {
			oldest = &m
		}
	}
	return oldest, nil
}
