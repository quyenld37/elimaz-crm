// Package flashstate giữ cờ hết suất và số còn của món flash sale trên cổng (cache.AdmissionGate) khớp với PostgreSQL
// (kế hoạch M5 Task 204 / M5-21): Loop (chạy ở api) đọc số đơn vị suất còn trống của món thuộc đợt đã lên lịch hay đang
// mở rồi ghi SetState, mỗi nhịp và ngay khi có báo hiệu flash_item, flash_sale (webcache.TopicInvalidate) hay
// system.resync. Trạng thái trên cổng sống 10 s: vòng ngừng thì trạng thái tự hết, lượt mua đi xuống database. Món
// vòng đã ghi mà lượt đọc sau (cùng phạm vi) không còn trả — đợt đóng, hủy, món bị bỏ — bị xóa ngay (ClearState,
// Task 520), không chờ 10 s.
//
// Khi nào không dùng: không phải nguồn sự thật (lượt mua chốt trong PostgreSQL); không ghi database. Gói chỉ được cmd/
// nhập (luật kiến trúc (m)).
package flashstate

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/wiring/webcache"
)

const (
	// DefaultEvery là nhịp của vòng khi New nhận every ≤ 0 (kế hoạch: 2 s, nhỏ hơn hạn sống 10 s của trạng thái).
	DefaultEvery = 2 * time.Second
	// queueSize: số việc chờ tối đa; đầy thì bỏ việc mới (nhịp kế tiếp là lượt đầy đủ).
	queueSize = 256
)

// target là phạm vi một lần tính lại: mọi món của mọi tenant (all), mọi món của một đợt, hay một món của một tenant.
type target struct {
	all        bool
	tenant     uuid.UUID
	sale, item uuid.UUID
}

// Loop là vòng dựng lại trạng thái món trên cổng flash sale. Dựng bằng New; Start chạy goroutine, Stop dừng và chờ.
type Loop struct {
	d     *db.DB
	gate  cache.AdmissionGate
	bus   realtime.Bus
	every time.Duration
	log   *slog.Logger

	queue chan target
	// written: món đã ghi lên cổng theo tenant (món → đợt), để xóa món rời phạm vi. Chỉ goroutine run đọc, ghi (refresh
	// chỉ chạy ở đó), không cần khóa. Tiến trình mới khởi động có sổ rỗng: trạng thái cũ tự hết sau 10 s.
	written map[uuid.UUID]map[uuid.UUID]uuid.UUID

	mu      sync.Mutex
	started bool
	cancel  context.CancelFunc
	unsub   func()
	wg      sync.WaitGroup
}

// New dựng Loop đọc d, ghi gate, nghe bus; every ≤ 0 → DefaultEvery. Log lỗi bằng slog.Default (không có SĐT, địa chỉ:
// chỉ mã).
func New(d *db.DB, gate cache.AdmissionGate, bus realtime.Bus, every time.Duration) *Loop {
	if every <= 0 {
		every = DefaultEvery
	}
	return &Loop{
		d: d, gate: gate, bus: bus, every: every, log: slog.Default(),
		queue: make(chan target, queueSize), written: map[uuid.UUID]map[uuid.UUID]uuid.UUID{},
	}
}

// Start đăng ký nhận báo hiệu và chạy vòng (lượt đầy đủ đầu tiên chạy ngay) tới khi ctx bị hủy hay Stop; gọi lần hai
// không làm gì.
func (l *Loop) Start(ctx context.Context) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.started {
		return
	}
	l.started = true
	ctx, l.cancel = context.WithCancel(ctx)
	l.push(target{all: true})
	l.wg.Go(func() { l.run(ctx) })
	l.unsub = l.bus.SubscribeSystem(webcache.TopicInvalidate, l.enqueue)
}

// Stop hủy đăng ký, dừng vòng và chờ nó kết thúc (trạng thái đã ghi tự hết hạn sau 10 s). Gọi trước Start hay nhiều
// lần không sao.
func (l *Loop) Stop() {
	l.mu.Lock()
	cancel, unsub := l.cancel, l.unsub
	l.cancel, l.unsub = nil, nil
	l.mu.Unlock()
	if cancel != nil {
		unsub()
		cancel()
	}
	l.wg.Wait() // chưa Start: không có goroutine nào
}

func (l *Loop) run(ctx context.Context) {
	t := time.NewTicker(l.every)
	defer t.Stop()
	failing := false
	for {
		var tg target
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tg.all = true
		case tg = <-l.queue:
		}
		err := l.refresh(ctx, tg)
		switch {
		case ctx.Err() != nil:
		case err != nil && !failing:
			// Chỉ ghi lần lỗi đầu của một chuỗi lỗi (Valkey ngừng: mỗi 2 s một lượt lỗi), không ghi mỗi nhịp.
			failing = true
			l.log.LogAttrs(ctx, slog.LevelWarn, "flashstate: dựng lại trạng thái món flash sale lỗi, lượt sau làm lại",
				logging.Err(err))
		case err == nil && failing && tg.all:
			failing = false
			l.log.LogAttrs(ctx, slog.LevelInfo, "flashstate: dựng lại trạng thái món flash sale đã chạy lại được")
		}
	}
}

// refresh tính lại phạm vi tg; lượt đầy đủ đi qua mọi tenant, lỗi của một tenant không chặn tenant khác (trả lỗi đầu).
func (l *Loop) refresh(ctx context.Context, tg target) error {
	if !tg.all {
		return l.refreshTenant(ctx, tg.tenant, tg.sale, tg.item)
	}
	ids, err := tenancy.ListTenantIDs(ctx, l.d)
	if err != nil {
		return fmt.Errorf("flashstate: liệt kê tenant: %w", err)
	}
	var first error
	for _, id := range ids {
		if err := l.refreshTenant(ctx, id, uuid.Nil, uuid.Nil); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// refreshTenant đọc số còn của các món (lọc theo sale, item nếu khác uuid.Nil) trong một transaction chỉ đọc, rồi ghi
// cổng sau khi transaction xong (không gọi Valkey trong RetryTx): xóa trạng thái của món đã ghi trước đây trong cùng
// phạm vi mà lượt đọc này không còn trả, rồi ghi các món được trả. Xóa lỗi thì giữ món trong sổ (lượt sau xóa lại).
func (l *Loop) refreshTenant(ctx context.Context, tenant, sale, item uuid.UUID) error {
	var rs []promotion.FlashItemRemaining
	if err := tenancy.WithTenant(ctx, l.d, tenant, func(tx pgx.Tx) error {
		var err error
		rs, err = promotion.FlashRemaining(ctx, tx, sale, item) // gán lại mỗi lần chạy
		return err                                              //nolint:wrapcheck // promotion đã bọc
	}, db.TxAPI); err != nil {
		return fmt.Errorf("flashstate: đọc số còn của tenant %s: %w", tenant, err)
	}
	if err := l.clearGone(ctx, tenant, sale, item, rs); err != nil {
		return err
	}
	for _, r := range rs {
		if err := l.gate.SetState(ctx, tenant, r.ItemID, r.Available, r.Available == 0); err != nil {
			return fmt.Errorf("flashstate: ghi trạng thái món %s: %w", r.ItemID, err)
		}
		l.remember(tenant, r.ItemID, r.SaleID)
	}
	return nil
}

// clearGone xóa trạng thái trên cổng của các món trong sổ của tenant thuộc phạm vi (sale, item; uuid.Nil = mọi) mà rs
// không có, và bỏ chúng khỏi sổ khi xóa được.
func (l *Loop) clearGone(ctx context.Context, tenant, sale, item uuid.UUID, rs []promotion.FlashItemRemaining) error {
	known := l.written[tenant]
	if len(known) == 0 {
		return nil
	}
	present := make(map[uuid.UUID]struct{}, len(rs))
	for _, r := range rs {
		present[r.ItemID] = struct{}{}
	}
	for it, sl := range known {
		if _, ok := present[it]; ok || (sale != uuid.Nil && sl != sale) || (item != uuid.Nil && it != item) {
			continue
		}
		if err := l.gate.ClearState(ctx, tenant, it); err != nil {
			return fmt.Errorf("flashstate: xóa trạng thái món %s: %w", it, err)
		}
		delete(known, it)
	}
	if len(known) == 0 {
		delete(l.written, tenant)
	}
	return nil
}

// remember ghi món item (của đợt sale) của tenant vào sổ món đã ghi.
func (l *Loop) remember(tenant, item, sale uuid.UUID) {
	m := l.written[tenant]
	if m == nil {
		m = map[uuid.UUID]uuid.UUID{}
		l.written[tenant] = m
	}
	m[item] = sale
}
