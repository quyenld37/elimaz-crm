package schedules

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Dọn dữ liệu (kế hoạch M0, Task 13; Task 394).
const (
	// cleanupEvery: chu kỳ của hai lịch dọn. 5 phút (Task 394, trước là 1 giờ): mỗi kỳ tối đa cleanupBatch dòng mỗi bảng,
	// nên mỗi giờ dọn được tới 60.000 việc xong, hơn số việc sinh ra ở tải 1× của thử tải M0 (~36.000/giờ).
	cleanupEvery = 5 * time.Minute
	// cleanupBatch: mỗi lần tối đa 5.000 dòng mỗi bảng (phần còn lại để kỳ sau).
	cleanupBatch = 5000
	// retention: chỉ số theo phút giữ 30 ngày.
	retention = 30 * 24 * time.Hour
	// DefaultJobRetention: việc done/expired giữ 7 ngày (Task 394; spec 04 mục 7 ghi 30 ngày). Đủ để tra một việc của
	// tuần trước; chống trùng tầng 1 của việc đã xong hết sau hạn này, tầng 2 (khóa tự nhiên của bảng nghiệp vụ) vẫn
	// chặn xử lý lại. Giữ 30 ngày làm bảng jobs lớn ~1,2 GiB mỗi ngày ở tải 1× (thử tải M0 phát hiện 4). Việc dead,
	// in_doubt không bao giờ bị dọn.
	DefaultJobRetention = 7 * 24 * time.Hour

	cleanupName       = "platform.cleanup"
	tenantCleanupName = "platform.cleanup_tenant"
)

// CleanupFunc xóa dòng cũ của một bảng trong tx (không quá 5.000 dòng mỗi lần, câu xóa dùng FOR UPDATE SKIP LOCKED),
// với now là giờ của lần dọn; trả số dòng đã xóa. Chạy lại được (tx có thể bị lùi rồi chạy lại).
type CleanupFunc func(ctx context.Context, tx pgx.Tx, now time.Time) (int64, error)

// Cleanup là các hàm dọn của tiến trình, chạy theo hai lịch mỗi 5 phút: platform.cleanup (một việc không tenant: bảng
// nền tảng không RLS) và platform.cleanup_tenant (mỗi tenant một việc, trong WithTenant: bảng có RLS). Nạp một lần lúc
// khởi động (cmd/crm), không an toàn khi đăng ký đồng thời với lúc chạy.
type Cleanup struct {
	global, perTenant []CleanupFunc
}

// CleanupOptions là cấu hình của các hàm dọn có sẵn.
type CleanupOptions struct {
	// JobRetention: việc done/expired kết thúc quá hạn này thì bị xóa; ≤ 0 → DefaultJobRetention.
	JobRetention time.Duration
}

// NewCleanup trả Cleanup đã có các hàm dọn của M0: việc done/expired quá o.JobRetention và metrics_minute quá 30 ngày
// (không tenant), idempotency_keys quá expires_at (theo tenant).
func NewCleanup(o CleanupOptions) *Cleanup {
	jobRetention := o.JobRetention
	if jobRetention <= 0 {
		jobRetention = DefaultJobRetention
	}
	c := &Cleanup{}
	c.RegisterCleanup(func(ctx context.Context, tx pgx.Tx, now time.Time) (int64, error) {
		n, err := store.New(tx).DeleteOldMetrics(ctx, store.DeleteOldMetricsParams{Before: now.Add(-retention), MaxRows: cleanupBatch})
		if err != nil {
			return 0, fmt.Errorf("dọn metrics_minute: %w", err)
		}
		return n, nil
	})
	c.RegisterCleanup(func(ctx context.Context, tx pgx.Tx, now time.Time) (int64, error) {
		n, err := store.New(tx).DeleteFinishedJobs(ctx, store.DeleteFinishedJobsParams{Before: now.Add(-jobRetention), MaxRows: cleanupBatch})
		if err != nil {
			return 0, fmt.Errorf("dọn jobs: %w", err)
		}
		return n, nil
	})
	c.RegisterTenantCleanup(func(ctx context.Context, tx pgx.Tx, now time.Time) (int64, error) {
		n, err := store.New(tx).DeleteExpiredIdempotencyKeys(ctx, store.DeleteExpiredIdempotencyKeysParams{Before: now, MaxRows: cleanupBatch})
		if err != nil {
			return 0, fmt.Errorf("dọn idempotency_keys: %w", err)
		}
		return n, nil
	})
	return c
}

// RegisterCleanup thêm hàm dọn bảng nền tảng không RLS (chạy trong việc không tenant của lịch platform.cleanup).
func (c *Cleanup) RegisterCleanup(fn CleanupFunc) { c.global = append(c.global, fn) }

// RegisterTenantCleanup thêm hàm dọn bảng có RLS (chạy trong WithTenant của từng tenant, lịch platform.cleanup_tenant).
func (c *Cleanup) RegisterTenantCleanup(fn CleanupFunc) { c.perTenant = append(c.perTenant, fn) }

// cleanupKind: việc định kỳ lỗi thì thử lại ở kỳ sau (spec 04 mục 5.1): một lần chạy, quá một kỳ chưa chạy thì expired.
func cleanupKind(name string) queue.Kind {
	return queue.Kind{Name: "task." + name, Policy: queue.Policy{MaxAttempts: 1, ExpiresAfter: cleanupEvery}}
}

// Define định nghĩa hai lịch dọn trong r.
func (c *Cleanup) Define(r *Registry) error {
	if err := r.Define(Schedule{Name: cleanupName, Every: cleanupEvery, Kind: cleanupKind(cleanupName)}); err != nil {
		return err
	}
	return r.Define(Schedule{Name: tenantCleanupName, Every: cleanupEvery, PerTenant: true, Kind: cleanupKind(tenantCleanupName)})
}

// Handlers trả handler của hai lịch dọn (đăng ký vào queue.NewWorker của vai trò worker).
func (c *Cleanup) Handlers() []queue.Handler {
	return []queue.Handler{
		cleanupHandler{kind: cleanupKind(cleanupName), fns: c.global},
		cleanupHandler{kind: cleanupKind(tenantCleanupName), fns: c.perTenant},
	}
}

type cleanupHandler struct {
	kind queue.Kind
	fns  []CleanupFunc
}

func (h cleanupHandler) Kind() queue.Kind { return h.kind }

// Handle chạy mọi hàm dọn trong tx của việc (worker đặt WithTenant khi việc có tenant); kết quả {"deleted": tổng}.
func (h cleanupHandler) Handle(ctx context.Context, _ queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	now := time.Now()
	var total int64
	for _, fn := range h.fns {
		n, err := fn(ctx, tx, now)
		if err != nil {
			return nil, err
		}
		total += n
	}
	out, err := json.Marshal(map[string]int64{"deleted": total})
	if err != nil {
		return nil, fmt.Errorf("mã hóa kết quả dọn: %w", err)
	}
	return out, nil
}
