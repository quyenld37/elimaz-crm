package observability_test

import (
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/testkit"
)

// valkeyTestOpTimeout là hạn mỗi lệnh Valkey của test (Task 496): hạn mặc định 50 ms của mã chạy thật hết khi máy chạy
// test đang tải, lệnh coi như Valkey ngừng và test đếm nguồn, lượt gọi hãng đỏ dù mã đúng.
const valkeyTestOpTimeout = 2 * time.Second

// /health/deep báo Valkey cache, gate là ok|down|off (kế hoạch M5 Task 184); Valkey ngừng không làm OK thành false.
func TestDeepHealthValkey(t *testing.T) {
	e := newEnv(t)
	fresh(t, e)
	if r := deep(t, e); !r.OK || r.Valkey.Cache != cache.HealthOff || r.Valkey.Gate != cache.HealthOff {
		t.Fatalf("không nối Valkey: muốn OK, off/off: %+v", r)
	}
	vk := testkit.StartValkey(t)
	dial := func(user, name string) *cache.Conn {
		c, err := cache.Dial(t.Context(), vk.URL(user), cache.DialOptions{Name: name, OpTimeout: valkeyTestOpTimeout})
		if err != nil {
			t.Fatalf("Dial: %v", err)
		}
		t.Cleanup(c.Close)
		return c
	}
	v := observability.Valkey{Cache: dial("crm_cache", "cache"), Gate: dial("crm_gate", "gate")}
	r, err := observability.DeepHealth(t.Context(), e.d, v)
	if err != nil || !r.OK || r.Valkey.Cache != cache.HealthOK || r.Valkey.Gate != cache.HealthOK {
		t.Fatalf("Valkey chạy: %+v, %v", r, err)
	}
	vk.Stop(t)
	r, err = observability.DeepHealth(t.Context(), e.d, v)
	if err != nil || !r.OK || r.Valkey.Cache != cache.HealthDown || r.Valkey.Gate != cache.HealthDown {
		t.Fatalf("Valkey dừng: muốn OK true, down/down: %+v, %v", r, err)
	}
	r, err = observability.DeepHealth(t.Context(), e.d, observability.Valkey{Gate: v.Gate})
	if err != nil || r.Valkey.Cache != cache.HealthOff || r.Valkey.Gate != cache.HealthDown {
		t.Fatalf("chỉ gate: %+v, %v", r, err)
	}
}
