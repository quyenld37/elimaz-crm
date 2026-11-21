package cache_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/metrics"
)

// fakeClock là đồng hồ giả cho cầu dao.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

var errDown = errors.New("valkey ngừng")

// gauge đọc giá trị cuối của gauge valkey_breaker_open nhãn name trong reg (Drain làm rỗng); -1 khi chưa đặt.
func gauge(reg *metrics.Registry, name string) float64 {
	v := -1.0
	for _, r := range reg.Drain() {
		if r.Name == "valkey_breaker_open" && r.Label == name && r.Kind == metrics.KindGauge {
			v = r.Sum
		}
	}
	return v
}

func newBreaker(clk *fakeClock, reg *metrics.Registry) *cache.Breaker {
	return cache.NewBreaker("cache", cache.BreakerOptions{FailThreshold: 5, OpenFor: 5 * time.Second, Now: clk.now}, reg)
}

func TestBreaker_OpenTrialClose(t *testing.T) {
	clk := &fakeClock{t: time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)}
	reg := metrics.NewRegistry()
	b := newBreaker(clk, reg)
	ctx := t.Context()
	var calls int
	fail := func(context.Context) error { calls++; return errDown }
	ok := func(context.Context) error { calls++; return nil }

	if err := b.Do(ctx, ok); err != nil {
		t.Fatalf("đóng: %v", err)
	}
	if g := gauge(reg, "cache"); g != 0 {
		t.Fatalf("gauge lúc đóng = %v, muốn 0", g)
	}
	for i := range 5 {
		if err := b.Do(ctx, fail); !errors.Is(err, errDown) {
			t.Fatalf("lỗi thứ %d: %v", i+1, err)
		}
	}
	calls = 0
	if err := b.Do(ctx, ok); !errors.Is(err, cache.ErrUnavailable) || calls != 0 {
		t.Fatalf("lần 6: lỗi %v, fn gọi %d lần; muốn ErrUnavailable, 0", err, calls)
	}
	if g := gauge(reg, "cache"); g != 1 {
		t.Fatalf("gauge lúc mở = %v, muốn 1", g)
	}
	clk.add(4999 * time.Millisecond)
	if err := b.Do(ctx, ok); !errors.Is(err, cache.ErrUnavailable) || calls != 0 {
		t.Fatalf("chưa đủ 5 s: lỗi %v, fn gọi %d lần", err, calls)
	}
	clk.add(time.Millisecond)
	// Lượt thử thất bại: mở lại thêm 5 s.
	if err := b.Do(ctx, fail); !errors.Is(err, errDown) || calls != 1 {
		t.Fatalf("lượt thử: lỗi %v, fn gọi %d lần", err, calls)
	}
	if err := b.Do(ctx, ok); !errors.Is(err, cache.ErrUnavailable) || calls != 1 {
		t.Fatalf("sau lượt thử hỏng phải mở lại: lỗi %v, fn gọi %d lần", err, calls)
	}
	clk.add(5 * time.Second)
	if err := b.Do(ctx, ok); err != nil || calls != 2 {
		t.Fatalf("lượt thử tốt: lỗi %v, fn gọi %d lần", err, calls)
	}
	if g := gauge(reg, "cache"); g != 0 {
		t.Fatalf("gauge sau khi đóng = %v, muốn 0", g)
	}
	// Đã đóng: đếm lại từ 0 (4 lỗi chưa mở).
	for range 4 {
		_ = b.Do(ctx, fail)
	}
	if err := b.Do(ctx, ok); err != nil {
		t.Fatalf("4 lỗi sau khi đóng chưa được mở: %v", err)
	}
}

// Một lỗi xen giữa các lần tốt không cộng dồn: chỉ lỗi liền mới mở.
func TestBreaker_SuccessResetsCount(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	b := newBreaker(clk, nil)
	for range 20 {
		for range 4 {
			_ = b.Do(t.Context(), func(context.Context) error { return errDown })
		}
		if err := b.Do(t.Context(), func(context.Context) error { return nil }); err != nil {
			t.Fatalf("4 lỗi liền rồi tốt: %v", err)
		}
	}
}

// Nửa mở cho đúng một lượt thử dù nhiều goroutine gọi cùng lúc.
func TestBreaker_SingleTrial(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	b := newBreaker(clk, nil)
	for range 5 {
		_ = b.Do(t.Context(), func(context.Context) error { return errDown })
	}
	clk.add(5 * time.Second)
	release := make(chan struct{})
	var calls, unavailable atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			err := b.Do(t.Context(), func(context.Context) error { calls.Add(1); <-release; return nil })
			if errors.Is(err, cache.ErrUnavailable) {
				unavailable.Add(1)
			}
		})
	}
	for calls.Load()+unavailable.Load() < 20 && unavailable.Load() < 19 {
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 || unavailable.Load() != 19 {
		t.Fatalf("lượt thử: fn gọi %d, ErrUnavailable %d; muốn 1, 19", calls.Load(), unavailable.Load())
	}
}

// Bên gọi tự hủy ctx: không tính là lỗi của Valkey, cũng không đóng cầu dao từ lượt thử.
func TestBreaker_CallerCancelNeutral(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	b := newBreaker(clk, nil)
	for range 10 {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_ = b.Do(ctx, func(ctx context.Context) error { return ctx.Err() })
	}
	if err := b.Do(t.Context(), func(context.Context) error { return nil }); err != nil {
		t.Fatalf("hủy từ bên gọi không được mở cầu dao: %v", err)
	}
	for range 5 {
		_ = b.Do(t.Context(), func(context.Context) error { return errDown })
	}
	clk.add(5 * time.Second)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_ = b.Do(ctx, func(ctx context.Context) error { return ctx.Err() })
	var called bool
	if err := b.Do(t.Context(), func(context.Context) error { called = true; return errDown }); !called || !errors.Is(err, errDown) {
		t.Fatalf("lượt thử bị hủy phải nhường lượt thử cho lần sau: gọi %v, lỗi %v", called, err)
	}
}
