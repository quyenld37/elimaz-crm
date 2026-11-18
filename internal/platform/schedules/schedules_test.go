package schedules_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/testkit"
)

// env: database thử đã migrate (testkit.Staff: pool crm_app như worker và pool chủ bảng).
type env struct{ *testkit.Staff }

func newEnv(t *testing.T) *env {
	t.Helper()
	return &env{testkit.OpenStaff(t)}
}

func (e *env) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := e.Owner.Pool.Exec(context.Background(), q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func kind(name string) queue.Kind { return queue.Kind{Name: name, Policy: queue.DefaultPolicy} }

func define(t *testing.T, ss ...schedules.Schedule) *schedules.Registry {
	t.Helper()
	r := schedules.NewRegistry()
	for _, s := range ss {
		if err := r.Define(s); err != nil {
			t.Fatalf("Define(%s): %v", s.Name, err)
		}
	}
	return r
}

func (e *env) ticker(r *schedules.Registry) *schedules.Ticker {
	return schedules.NewTicker(e.App, queue.NewQueue(), r, schedules.TickerOptions{})
}

func tick(t *testing.T, tk *schedules.Ticker, now time.Time) int {
	t.Helper()
	n, err := tk.Tick(t.Context(), now)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	return n
}

// Lịch 60 s theo tenant với 2 tenant → Tick tạo 2 việc (mỗi tenant một); Tick lần hai cùng lúc → 0.
func TestTickPerTenant(t *testing.T) {
	e := newEnv(t)
	a, b := e.Tenant(t, "a"), e.Tenant(t, "b")
	tk := e.ticker(define(t, schedules.Schedule{Name: "t.sync", Every: time.Minute, PerTenant: true, Kind: kind("task.t.sync")}))
	now := time.Now().UTC().Truncate(time.Second)
	if n := tick(t, tk, now); n != 2 {
		t.Fatalf("Tick lần đầu tạo %d việc, muốn 2", n)
	}
	for _, id := range []uuid.UUID{a, b} {
		if n := e.Count(t, "SELECT count(*) FROM platform.jobs WHERE kind = 'task.t.sync' AND tenant_id = $1 AND dedupe_key = $2",
			id, "t.sync:"+now.Format(time.RFC3339)); n != 1 {
			t.Fatalf("tenant %s: %d việc", id, n)
		}
	}
	if n := tick(t, tk, now); n != 0 {
		t.Fatalf("Tick lần hai cùng lúc tạo %d việc, muốn 0", n)
	}
	if n := e.Count(t, "SELECT count(*) FROM platform.schedules WHERE name = 't.sync' AND next_run_at = $1 AND every_seconds = 60 AND per_tenant",
		now.Add(time.Minute)); n != 1 {
		t.Fatal("next_run_at phải là mốc kế tiếp sau now")
	}
}

// Lịch không theo tenant → một việc không tenant.
func TestTickGlobal(t *testing.T) {
	e := newEnv(t)
	e.Tenant(t, "a")
	tk := e.ticker(define(t, schedules.Schedule{Name: "t.global", Every: time.Hour, Kind: kind("task.t.global")}))
	if n := tick(t, tk, time.Now()); n != 1 {
		t.Fatalf("muốn 1 việc, nhận %d", n)
	}
	if n := e.Count(t, "SELECT count(*) FROM platform.jobs WHERE kind = 'task.t.global' AND tenant_id IS NULL"); n != 1 {
		t.Fatal("việc phải không có tenant")
	}
}

// Hai Ticker cùng Tick một lúc → tổng 2 (không trùng).
func TestTwoTickersNoDuplicate(t *testing.T) {
	for range 5 {
		e := newEnv(t)
		e.Tenant(t, "a")
		e.Tenant(t, "b")
		r := define(t, schedules.Schedule{Name: "t.sync", Every: time.Minute, PerTenant: true, Kind: kind("task.t.sync")})
		now := time.Now()
		var (
			wg    sync.WaitGroup
			mu    sync.Mutex
			total int
		)
		for range 2 {
			tk := e.ticker(r)
			wg.Go(func() {
				n, err := tk.Tick(t.Context(), now)
				if err != nil {
					t.Errorf("Tick: %v", err)
				}
				mu.Lock()
				total += n
				mu.Unlock()
			})
		}
		wg.Wait()
		if total != 2 || e.Count(t, "SELECT count(*) FROM platform.jobs") != 2 {
			t.Fatalf("tổng %d việc báo, %d việc trong bảng; muốn 2", total, e.Count(t, "SELECT count(*) FROM platform.jobs"))
		}
	}
}

// next_run_at cũ 10 giờ → đúng 1 kỳ (kỳ mới nhất ≤ now) và next_run_at > now.
func TestTickCatchUpRunsOnce(t *testing.T) {
	e := newEnv(t)
	tk := e.ticker(define(t, schedules.Schedule{Name: "t.global", Every: time.Minute, Kind: kind("task.t.global")}))
	now := time.Now().UTC().Truncate(time.Second)
	if n := tick(t, tk, now); n != 1 {
		t.Fatalf("Tick đầu %d", n)
	}
	e.exec(t, "UPDATE platform.schedules SET next_run_at = $1", now.Add(-10*time.Hour))
	later := now.Add(90 * time.Second)
	if n := tick(t, tk, later); n != 1 {
		t.Fatalf("lịch trễ 10 giờ tạo %d việc, muốn 1", n)
	}
	// Kỳ mới nhất ≤ later tính từ now − 10 h theo bước 60 s là now + 60 s.
	slot := now.Add(time.Minute)
	if n := e.Count(t, "SELECT count(*) FROM platform.jobs WHERE dedupe_key = $1", "t.global:"+slot.Format(time.RFC3339)); n != 1 {
		t.Fatalf("việc của kỳ %s không có", slot)
	}
	if n := e.Count(t, "SELECT count(*) FROM platform.schedules WHERE next_run_at = $1 AND next_run_at > $2 AND last_enqueued_at IS NOT NULL",
		slot.Add(time.Minute), later); n != 1 {
		t.Fatal("next_run_at phải là kỳ kế tiếp sau now")
	}
	if n := tick(t, tk, later); n != 0 {
		t.Fatalf("Tick lại cùng lúc tạo %d", n)
	}
}

// Chưa tới hạn → 0; lịch có trong bảng mà chưa Define ở tiến trình này → không đụng.
func TestTickNotDueAndUnknown(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.exec(t, "INSERT INTO platform.schedules (name, every_seconds, per_tenant, next_run_at) VALUES ('old.one', 60, false, $1)", now.Add(-time.Hour))
	tk := e.ticker(define(t, schedules.Schedule{Name: "t.global", Every: time.Minute, Kind: kind("task.t.global")}))
	tick(t, tk, now)
	if n := tick(t, tk, now.Add(30*time.Second)); n != 0 {
		t.Fatalf("chưa tới hạn mà tạo %d", n)
	}
	if n := e.Count(t, "SELECT count(*) FROM platform.schedules WHERE name = 'old.one' AND last_enqueued_at IS NULL"); n != 1 {
		t.Fatal("lịch lạ không được đụng tới")
	}
}

// Đổi chu kỳ trong mã → dòng lịch được cập nhật ở Ticker mới, next_run_at giữ.
func TestDefineUpdatesEvery(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC().Truncate(time.Second)
	tick(t, e.ticker(define(t, schedules.Schedule{Name: "t.g", Every: time.Minute, Kind: kind("task.t.g")})), now)
	tick(t, e.ticker(define(t, schedules.Schedule{Name: "t.g", Every: 2 * time.Minute, PerTenant: true, Kind: kind("task.t.g")})), now)
	if n := e.Count(t, "SELECT count(*) FROM platform.schedules WHERE every_seconds = 120 AND per_tenant AND next_run_at = $1",
		now.Add(time.Minute)); n != 1 {
		t.Fatal("chu kỳ phải được cập nhật, next_run_at giữ")
	}
}

func TestDefineValidation(t *testing.T) {
	r := schedules.NewRegistry()
	if err := r.Define(schedules.Schedule{Name: "a", Every: 15 * time.Second, Kind: kind("task.a")}); err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]schedules.Schedule{
		"trùng":      {Name: "a", Every: time.Minute, Kind: kind("task.a")},
		"rỗng":       {Every: time.Minute, Kind: kind("task.b")},
		"dưới 15 s":  {Name: "b", Every: 14 * time.Second, Kind: kind("task.b")},
		"lẻ giây":    {Name: "b", Every: 15500 * time.Millisecond, Kind: kind("task.b")},
		"thiếu kind": {Name: "b", Every: time.Minute},
	} {
		if err := r.Define(s); !errors.Is(err, schedules.ErrInvalidSchedule) {
			t.Fatalf("%s: muốn ErrInvalidSchedule, nhận %v", name, err)
		}
	}
}

// Run tick ngay khi chạy và dừng khi ctx hủy.
func TestRunTicksAndStops(t *testing.T) {
	e := newEnv(t)
	tk := schedules.NewTicker(e.App, queue.NewQueue(), define(t, schedules.Schedule{Name: "t.g", Every: time.Hour, Kind: kind("task.t.g")}),
		schedules.TickerOptions{Interval: 20 * time.Millisecond})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { tk.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for e.Count(t, "SELECT count(*) FROM platform.jobs") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("Run không tạo việc")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run không dừng khi ctx hủy")
	}
}
