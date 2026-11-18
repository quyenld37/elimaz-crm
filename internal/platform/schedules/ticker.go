package schedules

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// defaultInterval: Ticker hỏi lịch tới hạn mỗi 5 s (kế hoạch M0, Task 13).
const defaultInterval = 5 * time.Second

// TickerOptions là tùy chọn của Ticker: Interval rỗng là 5 s; Logger rỗng là slog.Default().
type TickerOptions struct {
	Interval time.Duration
	Logger   *slog.Logger
}

// Ticker tạo việc cho các lịch tới hạn. Nhiều Ticker (nhiều worker) chạy cùng lúc an toàn: dòng lịch bị khóa bằng
// FOR UPDATE SKIP LOCKED, next_run_at đổi cùng transaction với việc được ghi, và DedupeKey theo kỳ chặn việc trùng.
type Ticker struct {
	d      *db.DB
	q      queue.JobQueue
	r      *Registry
	o      TickerOptions
	synced atomic.Bool
}

// NewTicker dựng Ticker cho các lịch của r.
func NewTicker(d *db.DB, q queue.JobQueue, r *Registry, o TickerOptions) *Ticker {
	if o.Interval <= 0 {
		o.Interval = defaultInterval
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &Ticker{d: d, q: q, r: r, o: o}
}

// slotPayload là payload của việc định kỳ: kỳ của việc.
type slotPayload struct {
	Slot time.Time `json:"slot"`
}

// Tick ghi các lịch của sổ vào bảng (lần đầu), rồi với mỗi lịch tới hạn ở now ghi việc của kỳ (một việc, hoặc mỗi
// tenant một việc) và đặt next_run_at là mốc kế tiếp sau now. Trả số việc mới ghi.
func (t *Ticker) Tick(ctx context.Context, now time.Time) (int, error) {
	now = now.UTC().Truncate(time.Microsecond) // độ chính xác của timestamptz
	if !t.synced.Load() {
		if err := t.sync(ctx, now); err != nil {
			return 0, err
		}
		t.synced.Store(true)
	}
	var tenants []uuid.UUID
	if t.r.anyPerTenant() {
		ids, err := tenancy.ListTenantIDs(ctx, t.d)
		if err != nil {
			return 0, fmt.Errorf("tick: %w", err)
		}
		tenants = ids
	}
	var created int
	err := t.d.RetryTx(ctx, db.TxOptions{}, func(tx pgx.Tx) error {
		created = 0 // fn có thể chạy lại: gán lại từ đầu
		qs := store.New(tx)
		due, err := qs.LockDueSchedules(ctx, store.LockDueSchedulesParams{Names: t.r.names(), Now: now})
		if err != nil {
			return fmt.Errorf("khóa lịch tới hạn: %w", err)
		}
		for _, row := range due {
			n, err := t.enqueue(ctx, tx, row, now, tenants)
			if err != nil {
				return err
			}
			created += n
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("tick: %w", err)
	}
	return created, nil
}

// enqueue ghi việc của một lịch tới hạn và dời next_run_at.
func (t *Ticker) enqueue(ctx context.Context, tx pgx.Tx, row store.LockDueSchedulesRow, now time.Time, tenants []uuid.UUID) (int, error) {
	s, _ := t.r.find(row.Name) // LockDueSchedules chỉ trả tên trong sổ
	every := time.Duration(row.EverySeconds) * time.Second
	slot, following := slotAt(row.NextRunAt, now, every)
	job := queue.NewJob{Payload: slotPayload{Slot: slot.UTC()}, DedupeKey: s.Name + ":" + slot.UTC().Format(time.RFC3339)}
	targets := []*uuid.UUID{nil}
	if s.PerTenant {
		targets = targets[:0]
		for i := range tenants {
			targets = append(targets, &tenants[i])
		}
	}
	n := 0
	for _, tenant := range targets {
		job.TenantID = tenant
		_, created, err := t.q.Enqueue(ctx, tx, s.Kind, job)
		if err != nil {
			return 0, fmt.Errorf("ghi việc của lịch %s: %w", s.Name, err)
		}
		if created {
			n++
		}
	}
	err := store.New(tx).SetScheduleNextRun(ctx, store.SetScheduleNextRunParams{Name: s.Name, NextRunAt: following, EnqueuedAt: now})
	if err != nil {
		return 0, fmt.Errorf("dời lịch %s: %w", s.Name, err)
	}
	return n, nil
}

// sync ghi lịch mới (kỳ đầu là now) và cập nhật chu kỳ, phạm vi của lịch đã có; next_run_at của lịch đã có giữ nguyên.
func (t *Ticker) sync(ctx context.Context, now time.Time) error {
	err := t.d.RetryTx(ctx, db.TxOptions{}, func(tx pgx.Tx) error {
		qs := store.New(tx)
		for _, s := range t.r.defs {
			every := int32(s.Every / time.Second) //nolint:gosec // Define chặn chu kỳ lạ; lịch thực tế dưới vài ngày
			if err := qs.InsertSchedule(ctx, store.InsertScheduleParams{
				Name: s.Name, EverySeconds: every, PerTenant: s.PerTenant, NextRunAt: now,
			}); err != nil {
				return fmt.Errorf("ghi lịch %s: %w", s.Name, err)
			}
			if err := qs.UpdateScheduleDefinition(ctx, store.UpdateScheduleDefinitionParams{
				Name: s.Name, EverySeconds: every, PerTenant: s.PerTenant,
			}); err != nil {
				return fmt.Errorf("cập nhật lịch %s: %w", s.Name, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("ghi sổ lịch: %w", err)
	}
	return nil
}

// Run gọi Tick ngay rồi mỗi Interval tới khi ctx bị hủy. Lỗi được log (một lần, ở đây) và thử lại ở lần sau.
func (t *Ticker) Run(ctx context.Context) {
	tk := time.NewTicker(t.o.Interval)
	defer tk.Stop()
	for {
		if _, err := t.Tick(ctx, time.Now()); err != nil && ctx.Err() == nil {
			t.o.Logger.WarnContext(ctx, "việc định kỳ", logging.Err(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
		}
	}
}
