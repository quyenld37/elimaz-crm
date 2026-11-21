package webcache

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Mặc định của Invalidator (QĐ-185-5).
const (
	queueSize          = 1024
	defaultResyncRetry = 2 * time.Second
)

// Invalidator (chạy ở api) nhận báo hiệu hệ thống TopicInvalidate và realtime.TopicSystemResync rồi xóa khóa của
// ReadCache theo bảng ánh xạ (mapping.go), trong goroutine riêng, ngoài mọi transaction. Hàng chờ đầy, một lần gọi
// Valkey lỗi, hay kết nối LISTEN nối lại → resync: BumpSpace mọi space của mọi tenant, thử lại mỗi 2 s tới khi được.
type Invalidator struct {
	bus realtime.Bus
	rc  cache.ReadCache
	d   *db.DB
	log *slog.Logger

	retry  time.Duration
	queue  chan realtime.Signal
	wake   chan struct{} // đệm 1: có việc resync
	resync atomic.Bool

	mu      sync.Mutex
	started bool
	unsub   func()
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewInvalidator dựng Invalidator; d dùng để liệt kê tenant khi resync (QĐ-185-2); log nil thì slog.Default.
func NewInvalidator(bus realtime.Bus, rc cache.ReadCache, d *db.DB, log *slog.Logger) *Invalidator {
	if log == nil {
		log = slog.Default()
	}
	return &Invalidator{
		bus: bus, rc: rc, d: d, log: log, retry: defaultResyncRetry,
		queue: make(chan realtime.Signal, queueSize), wake: make(chan struct{}, 1),
	}
}

// Start đăng ký nhận báo hiệu và chạy goroutine xử lý; gọi lần hai không làm gì.
func (v *Invalidator) Start() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.started {
		return
	}
	v.started = true
	ctx, cancel := context.WithCancel(context.Background())
	v.cancel = cancel
	v.wg.Go(func() { v.run(ctx) })
	v.unsub = v.bus.SubscribeSystem(TopicInvalidate, v.enqueue)
}

// Stop hủy đăng ký và chờ goroutine xử lý dừng (việc đang chờ trong hàng bị bỏ: bộ đệm tự lành theo hạn sống). Gọi
// trước Start hay gọi nhiều lần không sao.
func (v *Invalidator) Stop() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.started || v.cancel == nil {
		return
	}
	v.unsub()
	v.cancel()
	v.wg.Wait()
	v.cancel = nil
}

// enqueue chạy trong goroutine của hub: không chặn. Hàng đầy thì đánh dấu resync.
func (v *Invalidator) enqueue(s realtime.Signal) {
	if s.Topic == realtime.TopicSystemResync {
		v.markResync()
		return
	}
	select {
	case v.queue <- s:
	default:
		v.markResync()
	}
}

func (v *Invalidator) markResync() {
	v.resync.Store(true)
	select {
	case v.wake <- struct{}{}:
	default:
	}
}

func (v *Invalidator) run(ctx context.Context) {
	t := time.NewTicker(v.retry)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case s := <-v.queue:
			v.apply(ctx, s)
		case <-v.wake:
		case <-t.C:
		}
		if v.resync.Swap(false) {
			if err := v.resyncAll(ctx); err != nil && ctx.Err() == nil {
				v.resync.Store(true)
				v.log.LogAttrs(ctx, slog.LevelWarn, "webcache: làm mới toàn bộ bộ đệm lỗi, thử lại sau",
					logging.Err(err), slog.Duration("retry", v.retry))
			}
		}
	}
}

// apply làm các việc của một báo hiệu; lỗi thì ghi log và đánh dấu resync.
func (v *Invalidator) apply(ctx context.Context, s realtime.Signal) {
	acts := actionsFor(s.Entity.Type)
	if len(acts) == 0 {
		v.log.LogAttrs(ctx, slog.LevelWarn, "webcache: báo hiệu có loại đối tượng chưa biết",
			slog.String("type", s.Entity.Type))
		return
	}
	for _, a := range acts {
		var err error
		if a.byID {
			err = v.rc.Invalidate(ctx, s.TenantID, a.space, s.Entity.ID)
		} else {
			err = v.rc.BumpSpace(ctx, s.TenantID, a.space)
		}
		if err != nil {
			if ctx.Err() == nil {
				v.log.LogAttrs(ctx, slog.LevelWarn, "webcache: xóa khóa bộ đệm lỗi, sẽ làm mới toàn bộ",
					slog.String("space", string(a.space)), logging.Err(err))
			}
			v.markResync()
			return
		}
	}
}

// resyncAll tăng thế hệ của mọi space của mọi tenant; dừng ở lỗi đầu tiên (lượt sau làm lại từ đầu).
func (v *Invalidator) resyncAll(ctx context.Context) error {
	ids, err := tenancy.ListTenantIDs(ctx, v.d)
	if err != nil {
		return fmt.Errorf("webcache: resync: %w", err)
	}
	for _, id := range ids {
		if err := v.bumpAll(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (v *Invalidator) bumpAll(ctx context.Context, tenant uuid.UUID) error {
	for _, sp := range cache.Spaces() {
		if err := v.rc.BumpSpace(ctx, tenant, sp); err != nil {
			return fmt.Errorf("webcache: resync: %w", err)
		}
	}
	return nil
}
