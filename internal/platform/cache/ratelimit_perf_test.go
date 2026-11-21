//go:build perf

// Test thời gian của RateLimiter khi Valkey ngừng (make perf-test). Tách khỏi test đúng/sai (Task 400): thời gian phụ
// thuộc máy bận hay rảnh; TestRateLimiter_FallbackInProcess giữ các tiêu chí không phụ thuộc tải.
package cache_test

import (
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/testkit"
)

// fallbackCallBudget là trần thời gian mỗi lời gọi Allow khi Valkey ngừng (kế hoạch M5 Task 186 Step 1).
const fallbackCallBudget = 60 * time.Millisecond

// Valkey ngừng: mọi lời gọi Allow (cả 5 lời gọi trước khi cầu dao mở) xong dưới 60 ms, Degraded, không lỗi.
func TestRateLimiter_FallbackCallTime(t *testing.T) {
	vk := testkit.StartValkey(t)
	rl := cache.NewRateLimiter(dialGate(t, vk.URL("crm_gate"), nil, 0), nil, nil)
	l := cache.Limit{Name: "perf", Max: 1000, Window: time.Minute, FallbackMax: 1000}
	rl.Allow(t.Context(), l, "svc:a")
	vk.Stop(t)
	var slowest time.Duration
	for i := range 200 {
		start := time.Now()
		d := rl.Allow(t.Context(), l, "svc:a")
		took := time.Since(start)
		slowest = max(slowest, took)
		if !d.Degraded {
			t.Fatalf("lời gọi %d: %+v, muốn Degraded", i+1, d)
		}
		if took > fallbackCallBudget {
			t.Fatalf("lời gọi %d mất %s, trần %s", i+1, took, fallbackCallBudget)
		}
	}
	t.Logf("lời gọi chậm nhất khi Valkey ngừng: %s", slowest)
}
