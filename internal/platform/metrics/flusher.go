package metrics

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Nhịp của Flusher.Run và các giới hạn.
const (
	flushEvery = 60 * time.Second
	beatEvery  = 15 * time.Second
	// finalFlushTimeout: lần ghi cuối khi tắt êm (tổng thời gian tắt dưới 25 s).
	finalFlushTimeout = 5 * time.Second
	// maxPending: số dòng tối đa giữ lại khi ghi lỗi liên tục; quá thì bỏ dòng cũ nhất.
	maxPending = 10_000
)

// HeartbeatMaxAge là tuổi tối đa của nhịp sống: nhịp sống cũ hơn mức này thì healthcheck của worker đỏ (kế hoạch M0, Task 3 và 10).
const HeartbeatMaxAge = 90 * time.Second

// ErrNoHeartbeat báo tiến trình chưa ghi nhịp sống lần nào.
var ErrNoHeartbeat = errors.New("chưa có nhịp sống")

// Flusher ghi chỉ số của Reg vào platform.metrics_minute và nhịp sống vào platform.process_heartbeats. Pinger nil
// (vai trò không gửi nhịp sống ra ngoài) hay URL rỗng thì không gửi. Log nil dùng slog.Default().
//
// Không gọi Flush, Beat, Run đồng thời trên cùng một Flusher.
type Flusher struct {
	DB      *db.DB
	Reg     *Registry
	Process string
	Role    config.Role
	Pinger  *Pinger
	Log     *slog.Logger
	// DiskPaths: nhãn → đường dẫn; mỗi lần Flush ghi gauge disk_used_pct (phần trăm đĩa đã dùng) cho từng nhãn, để
	// luật disk:<process> của Task 19 đọc. Nhãn là tên cố định ("root", "spool"), không phải đường dẫn. Rỗng: không đo.
	DiskPaths map[string]string

	pending []pendingRow
}

type pendingRow struct {
	bucket time.Time
	row    Row
}

// ProcessName là tên tiến trình trong process_heartbeats và metrics_minute: "<vai trò>@<tên máy>". Trong container,
// tên máy là mã container, nên `crm healthcheck` chạy trong cùng container tính ra cùng tên.
func ProcessName(role config.Role) string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return string(role) + "@" + host
}

// Flush lấy mọi chỉ số của Reg (gán vào phút của now, UTC), cộng dồn vào platform.metrics_minute bằng
// ON CONFLICT … DO UPDATE và cập nhật nhịp sống, trong một transaction riêng (ADR-46 nhóm (b)). Ghi lỗi thì giữ lô
// để ghi ở lần sau vào đúng phút gốc (tối đa maxPending dòng) và trả lỗi. Ghi xong mới gửi nhịp sống ra ngoài; lỗi
// gửi chỉ log và đếm heartbeat_ping_failed, không làm Flush lỗi.
func (f *Flusher) Flush(ctx context.Context, now time.Time) error {
	bucket := now.UTC().Truncate(time.Minute)
	f.measureDisks()
	for _, r := range f.Reg.Drain() {
		f.pending = append(f.pending, pendingRow{bucket: bucket, row: r})
	}
	if over := len(f.pending) - maxPending; over > 0 {
		f.pending = append([]pendingRow(nil), f.pending[over:]...)
	}
	err := f.DB.RetryTx(ctx, db.TxOptions{}, func(tx pgx.Tx) error {
		q := store.New(tx)
		for _, p := range f.pending {
			if err := q.UpsertMetricMinute(ctx, upsertParams(p, f.Process)); err != nil {
				return fmt.Errorf("ghi chỉ số %s: %w", p.row.Name, err)
			}
		}
		return upsertHeartbeat(ctx, q, f.Process, f.Role, now)
	})
	if err != nil {
		return fmt.Errorf("ghi chỉ số theo phút: %w", err)
	}
	f.pending = nil
	if err := f.Pinger.Ping(ctx); err != nil {
		f.Reg.Inc("heartbeat_ping_failed", "", 1)
		f.logger().WarnContext(ctx, "gửi nhịp sống ra ngoài lỗi", logging.Err(err))
	}
	return nil
}

// measureDisks ghi gauge disk_used_pct của từng nhãn trong DiskPaths; đo lỗi thì bỏ qua nhãn đó (luật đĩa không có
// số mới, nhịp sống vẫn ghi).
func (f *Flusher) measureDisks() {
	for label, path := range f.DiskPaths {
		if pct, err := DiskUsedPct(path); err == nil {
			f.Reg.Gauge("disk_used_pct", pct, label)
		}
	}
}

// Beat ghi nhịp sống (at = now) của tiến trình trong một transaction riêng.
func (f *Flusher) Beat(ctx context.Context, now time.Time) error {
	err := f.DB.RetryTx(ctx, db.TxOptions{}, func(tx pgx.Tx) error {
		return upsertHeartbeat(ctx, store.New(tx), f.Process, f.Role, now)
	})
	if err != nil {
		return fmt.Errorf("ghi nhịp sống: %w", err)
	}
	return nil
}

// Run ghi nhịp sống ngay rồi mỗi 15 s, ghi chỉ số mỗi 60 s, tới khi ctx bị hủy; khi đó ghi chỉ số lần cuối (hạn
// 5 s). Lỗi chỉ log rồi chạy tiếp ở nhịp sau.
func (f *Flusher) Run(ctx context.Context) {
	f.beatLogged(ctx)
	beat := time.NewTicker(beatEvery)
	defer beat.Stop()
	flush := time.NewTicker(flushEvery)
	defer flush.Stop()
	for {
		select {
		case <-ctx.Done():
			fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finalFlushTimeout)
			defer cancel()
			f.flushLogged(fctx)
			return
		case <-beat.C:
			f.beatLogged(ctx)
		case <-flush.C:
			f.flushLogged(ctx)
		}
	}
}

func (f *Flusher) beatLogged(ctx context.Context) {
	if err := f.Beat(ctx, time.Now()); err != nil && ctx.Err() == nil {
		f.logger().WarnContext(ctx, "ghi nhịp sống lỗi", logging.Err(err))
	}
}

func (f *Flusher) flushLogged(ctx context.Context) {
	if err := f.Flush(ctx, time.Now()); err != nil && ctx.Err() == nil {
		f.logger().WarnContext(ctx, "ghi chỉ số theo phút lỗi", logging.Err(err),
			slog.Int("pending", len(f.pending)))
	}
}

func (f *Flusher) logger() *slog.Logger {
	if f.Log != nil {
		return f.Log
	}
	return slog.Default()
}

// HeartbeatAge trả tuổi nhịp sống của process theo đồng hồ database; chưa có dòng thì ErrNoHeartbeat.
func HeartbeatAge(ctx context.Context, d *db.DB, process string) (time.Duration, error) {
	secs, err := store.New(d.Pool).HeartbeatAgeSeconds(ctx, process)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNoHeartbeat
	}
	if err != nil {
		return 0, fmt.Errorf("đọc nhịp sống: %w", err)
	}
	return time.Duration(secs * float64(time.Second)), nil
}

func upsertHeartbeat(ctx context.Context, q *store.Queries, process string, role config.Role, now time.Time) error {
	err := q.UpsertHeartbeat(ctx, store.UpsertHeartbeatParams{Process: process, Role: string(role), At: now.UTC()})
	if err != nil {
		return fmt.Errorf("ghi nhịp sống: %w", err)
	}
	return nil
}

func upsertParams(p pendingRow, process string) store.UpsertMetricMinuteParams {
	var hist []int32
	if p.row.Hist != nil {
		hist = make([]int32, len(p.row.Hist))
		for i, n := range p.row.Hist {
			hist[i] = int32(min(n, 1<<31-1)) //nolint:gosec // đã chặn trên ở MaxInt32
		}
	}
	return store.UpsertMetricMinuteParams{
		Bucket: p.bucket, Process: process, Name: p.row.Name, Label: p.row.Label,
		Count: p.row.Count, Sum: p.row.Sum, Max: p.row.Max, Hist: hist, Kind: string(p.row.Kind),
	}
}
