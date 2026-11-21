package cache_test

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/testkit"
)

// Quy luật: chuỗi thời điểm ngẫu nhiên (tăng dần, trải nhiều cửa sổ) trên vài chủ thể → số Allowed của mỗi (chủ thể,
// cửa sổ) đúng bằng min(số lần gọi, Max) khi có Valkey và min(số lần gọi, FallbackMax) khi dự phòng (nên luôn ≤ trần).
func TestRateLimiter_PropertyPerWindow(t *testing.T) {
	vk := testkit.StartValkey(t)
	gate := dialGate(t, vk.URL("crm_gate"), nil, time.Second)
	var run atomic.Int64
	rapid.Check(t, func(rt *rapid.T) {
		n := run.Add(1)
		window := time.Duration(rapid.IntRange(1, 5).Draw(rt, "window_s")) * time.Second
		maxN := rapid.IntRange(1, 6).Draw(rt, "max")
		l := cache.Limit{
			Name: fmt.Sprintf("p%d", n), Max: maxN, Window: window,
			FallbackMax: rapid.IntRange(1, maxN).Draw(rt, "fallback_max"),
		}
		valkeyUp := rapid.Bool().Draw(rt, "valkey")
		clk := &fakeClock{t: windowBase().Add(time.Duration(rapid.IntRange(0, 10_000).Draw(rt, "start_ms")) * time.Millisecond)}
		var rl cache.RateLimiter
		if valkeyUp {
			rl = cache.NewRateLimiter(gate, clk.now, nil)
		} else {
			rl = cache.NewRateLimiter(nil, clk.now, nil)
		}
		type cell struct {
			subject string
			window  int64
		}
		calls, allowed := map[cell]int{}, map[cell]int{}
		steps := rapid.IntRange(1, 60).Draw(rt, "steps")
		for i := range steps {
			clk.add(time.Duration(rapid.IntRange(0, 1500).Draw(rt, fmt.Sprintf("dt%d", i))) * time.Millisecond)
			subject := rapid.SampledFrom([]string{"svc:a", "svc:b", "svc:c"}).Draw(rt, fmt.Sprintf("s%d", i))
			d := rl.Allow(t.Context(), l, subject)
			if d.Degraded == valkeyUp {
				rt.Fatalf("Degraded = %v khi Valkey chạy = %v", d.Degraded, valkeyUp)
			}
			c := cell{subject, clk.now().UnixMilli() / window.Milliseconds()}
			calls[c]++
			if d.Allowed {
				allowed[c]++
			} else if d.RetryAfter < time.Second || d.RetryAfter > window {
				rt.Fatalf("RetryAfter = %s ngoài [1 s, %s]", d.RetryAfter, window)
			}
		}
		ceiling := l.Max
		if !valkeyUp {
			ceiling = l.FallbackMax
		}
		for c, k := range calls {
			if want := min(k, ceiling); allowed[c] != want {
				rt.Fatalf("%v: %d Allowed trên %d lần gọi, muốn %d (trần %d)", c, allowed[c], k, want, ceiling)
			}
		}
	})
}
