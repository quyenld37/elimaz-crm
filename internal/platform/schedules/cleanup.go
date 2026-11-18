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

// Dọn dữ liệu (kế hoạch M0, Task 13).
const (
	// cleanupEvery: chu kỳ của hai lịch dọn.
	cleanupEvery = time.Hour
	// cleanupBatch: mỗi lần tối đa 5.000 dòng mỗi bảng (phần còn lại để kỳ sau).
	cleanupBatch = 5000
	// retention: việc done/expired và chỉ số theo phút giữ 30 ngày.
	retention = 30 * 24 * time.Hour

	cleanupName       = "platform.cleanup"
	tenantCleanupName = "platform.cleanup_tenant"
)

// CleanupFunc xóa dòng cũ của một bảng trong tx (không quá 5.000 dòng mỗi lần, câu xóa dùng FOR UPDATE SKIP LOCKED),
// với now là giờ của lần dọn; trả số dòng đã xóa. Chạy lại được (tx có thể bị lùi rồi chạy lại).
type CleanupFunc func(ctx context.Context, tx pgx.Tx, now time.Time) (int64, error)

// Cleanup là các hàm dọn của tiến trình, chạy theo hai lịch mỗi giờ: platform.cleanup (một việc không tenant: bảng
// nền tảng không RLS) và platform.cleanup_tenant (mỗi tenant một việc, trong WithTenant: bảng có RLS). Nạp một lần lúc
// khởi động (cmd/crm), không an toàn khi đăng ký đồng thời với lúc chạy.
type Cleanup struct {
	global, perTenant []CleanupFunc
}

// NewCleanup trả Cleanup đã có các hàm dọn của M0: việc done/expired và metrics_minute quá 30 ngày (không tenant),
// idempotency_keys quá expires_at (theo tenant).
func NewCleanup() *Cleanup {
	c := &Cleanup{}
	c.RegisterCleanup(func(ctx context.Context, tx pgx.Tx, now time.Time) (int64, error) {
		n, err := store.New(tx).DeleteOldMetrics(ctx, store.DeleteOldMetricsParams{Before: now.Add(-retention), MaxRows: cleanupBatch})
		if err != nil {
			return 0, fmt.Errorf("dọn metrics_minute: %w", err)
		}
		return n, nil
	})
	c.RegisterCleanup(func(ctx context.Context, tx pgx.Tx, now time.Time) (int64, error) {
		n, err := store.New(tx).DeleteFinishedJobs(ctx, store.DeleteFinishedJobsParams{Before: now.Add(-retention), MaxRows: cleanupBatch})
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
