package cache_test

import (
	"errors"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/cache"
)

// Bốn giới hạn mặc định đúng số của QĐ23 (kế hoạch M5) và hai giới hạn đoán mã (Task 482); FallbackMax là một nửa.
func TestDefaultLimits(t *testing.T) {
	got := cache.DefaultLimits()
	want := cache.Limits{
		WebKey:           cache.Limit{Name: "web_key", Max: 1200, Window: time.Minute, FallbackMax: 600},
		FlashCustomer:    cache.Limit{Name: "flash_customer", Max: 10, Window: 10 * time.Second, FallbackMax: 5},
		FlashIP:          cache.Limit{Name: "flash_ip", Max: 30, Window: 10 * time.Second, FallbackMax: 15},
		WebOrderCustomer: cache.Limit{Name: "web_order_customer", Max: 20, Window: time.Minute, FallbackMax: 10},
		CouponGuessCustomer: cache.Limit{
			Name: "coupon_guess_customer", Max: 10, Window: 15 * time.Minute, FallbackMax: 5,
		},
		CouponGuessIP: cache.Limit{Name: "coupon_guess_ip", Max: 30, Window: 15 * time.Minute, FallbackMax: 15},
	}
	if got != want {
		t.Fatalf("DefaultLimits() = %+v\nmuốn %+v", got, want)
	}
}

func TestParseLimits(t *testing.T) {
	def := cache.DefaultLimits()
	t.Run("rỗng là mặc định", func(t *testing.T) {
		for _, raw := range []string{"", "  ", "{}"} {
			got, err := cache.ParseLimits(raw)
			if err != nil || got != def {
				t.Fatalf("ParseLimits(%q) = %+v, %v", raw, got, err)
			}
		}
	})
	t.Run("đè một phần", func(t *testing.T) {
		got, err := cache.ParseLimits(`{"web_key": {"max": 1500, "fallback_max": 700}, "flash_ip": {"window_s": 20},
			"coupon_guess_customer": {"max": 6}, "coupon_guess_ip": {"window_s": 3600}}`)
		if err != nil {
			t.Fatalf("ParseLimits: %v", err)
		}
		want := def
		want.WebKey.Max, want.WebKey.FallbackMax = 1500, 700
		want.FlashIP.Window = 20 * time.Second
		want.CouponGuessCustomer.Max = 6
		want.CouponGuessIP.Window = time.Hour
		if got != want {
			t.Fatalf("= %+v\nmuốn %+v", got, want)
		}
	})
	for _, raw := range []string{
		`[`,
		`{"khac": {"max": 1}}`,
		`{"web_key": {"max": 1, "la": 2}}`,
		`{"web_key": {"max": 0}}`,
		`{"web_key": {"max": -5}}`,
		`{"web_key": {"fallback_max": 0}}`,
		`{"web_key": {"max": 100}}`, // fallback_max mặc định 600 > max 100
		`{"web_key": {"window_s": 0}}`,
		`{"web_key": {"window_s": 3601}}`,
		`{"web_key": {"max": 1.5}}`,
		`{"web_key": null}`,
		`{"web_key": {}} trailing`,
	} {
		if _, err := cache.ParseLimits(raw); !errors.Is(err, cache.ErrBadLimits) {
			t.Errorf("ParseLimits(%q) lỗi %v, muốn ErrBadLimits", raw, err)
		}
	}
}
