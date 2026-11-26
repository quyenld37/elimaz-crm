package httpapi_test

import (
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/httpapi"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/testkit"
)

// guardClock là đồng hồ giả dùng chung giữa test và bộ đếm.
type guardClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *guardClock) now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *guardClock) add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

// guardLimits: ngưỡng thử SĐT 3, IP 5 lần sai / 15 phút (dự phòng bằng ngưỡng); guardSecret: secret HMAC thử.
func guardLimits() (customer, ip cache.Limit) {
	return cache.Limit{Name: "coupon_guess_customer", Max: 3, Window: 15 * time.Minute, FallbackMax: 3},
		cache.Limit{Name: "coupon_guess_ip", Max: 5, Window: 15 * time.Minute, FallbackMax: 5}
}

func guardSecret() []byte { return []byte("khoa-hmac-thu-cua-test-du-32-ky-tu!!") }

func newGuard(rl cache.RateLimiter) *httpapi.CouponGuard {
	customer, ip := guardLimits()
	return httpapi.NewCouponGuard(rl, customer, ip, guardSecret())
}

func fail(t *testing.T, g *httpapi.CouponGuard, s httpapi.CouponSubject, n int) {
	t.Helper()
	for range n {
		g.Record(t.Context(), s, true)
	}
}

func mustBlocked(t *testing.T, g *httpapi.CouponGuard, s httpapi.CouponSubject, want bool, what string) time.Duration {
	t.Helper()
	blocked, wait := g.Blocked(t.Context(), s)
	if blocked != want {
		t.Fatalf("%s: chặn = %v, muốn %v", what, blocked, want)
	}
	if blocked && wait <= 0 {
		t.Fatalf("%s: chặn mà RetryAfter = %s", what, wait)
	}
	return wait
}

// Đếm lần đoán sai theo SĐT (3) và IP (5), cửa sổ 15 phút, đồng hồ ở phút 5: lần dùng mã đúng không đếm; sau 3 lần sai
// SĐT bị chặn tới hết cửa sổ (RetryAfter 10 phút) dù sau đó dùng mã đúng; SĐT khác cùng IP chưa bị chặn tới khi IP
// đủ 5 lần sai; SĐT khác, IP khác không ảnh hưởng; IPv6 cùng /64 chung bộ đếm, khác /64 riêng; không IP → đếm theo
// khóa dịch vụ; sang cửa sổ mới hết chặn.
func TestCouponGuard_BlocksAfterFailures(t *testing.T) {
	t.Parallel()
	clk := &guardClock{t: time.Date(2026, 11, 24, 10, 5, 0, 0, time.UTC)}
	g := newGuard(cache.NewRateLimiter(nil, clk.now, nil))
	svc := uuid.New()
	ipA := netip.MustParseAddr("203.0.113.7")
	a := httpapi.CouponSubject{Phone: logging.Phone("+84901000001"), IP: ipA, ServiceKeyID: svc}

	for range 10 {
		g.Record(t.Context(), a, false)
	}
	mustBlocked(t, g, a, false, "10 lần mã đúng")
	fail(t, g, a, 2)
	mustBlocked(t, g, a, false, "2 lần sai")
	fail(t, g, a, 1)
	if wait := mustBlocked(t, g, a, true, "3 lần sai"); wait != 10*time.Minute {
		t.Fatalf("RetryAfter = %s, muốn 10 phút (tới hết cửa sổ)", wait)
	}
	g.Record(t.Context(), a, false) // mã đúng khi đã bị chặn: vẫn chặn
	mustBlocked(t, g, a, true, "mã đúng sau khi bị chặn")

	sameIP := httpapi.CouponSubject{Phone: logging.Phone("+84901000002"), IP: ipA, ServiceKeyID: svc}
	mustBlocked(t, g, sameIP, false, "SĐT khác cùng IP, IP mới 3 lần sai")
	fail(t, g, sameIP, 2)
	mustBlocked(t, g, sameIP, true, "IP đủ 5 lần sai")
	other := httpapi.CouponSubject{
		Phone: logging.Phone("+84901000003"), IP: netip.MustParseAddr("198.51.100.9"), ServiceKeyID: svc,
	}
	mustBlocked(t, g, other, false, "SĐT khác, IP khác")

	v6 := httpapi.CouponSubject{IP: netip.MustParseAddr("2001:db8:1:2::1"), ServiceKeyID: svc}
	fail(t, g, v6, 5)
	mustBlocked(t, g, httpapi.CouponSubject{IP: netip.MustParseAddr("2001:db8:1:2:ffff::9"), ServiceKeyID: svc}, true,
		"IPv6 cùng /64")
	mustBlocked(t, g, httpapi.CouponSubject{IP: netip.MustParseAddr("2001:db8:1:3::1"), ServiceKeyID: svc}, false,
		"IPv6 khác /64")
	mapped := httpapi.CouponSubject{IP: netip.MustParseAddr("::ffff:203.0.113.7"), ServiceKeyID: svc}
	mustBlocked(t, g, mapped, true, "IPv4 viết dạng IPv6 (::ffff:) là cùng IP")

	noIP := httpapi.CouponSubject{ServiceKeyID: svc}
	fail(t, g, noIP, 5)
	mustBlocked(t, g, noIP, true, "không IP: đếm theo khóa dịch vụ")
	mustBlocked(t, g, httpapi.CouponSubject{ServiceKeyID: uuid.New()}, false, "không IP, khóa dịch vụ khác")

	clk.add(10 * time.Minute)
	mustBlocked(t, g, a, false, "cửa sổ mới")
}

// Nhiều lần đoán sai song song của một SĐT: sau khi xong, chủ thể bị chặn (đếm nguyên tử, không mất lượt).
func TestCouponGuard_ParallelFailures(t *testing.T) {
	t.Parallel()
	clk := &guardClock{t: time.Date(2026, 11, 24, 10, 5, 0, 0, time.UTC)}
	g := newGuard(cache.NewRateLimiter(nil, clk.now, nil))
	s := httpapi.CouponSubject{Phone: logging.Phone("+84901000009"), IP: netip.MustParseAddr("203.0.113.70")}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { g.Record(t.Context(), s, true) })
	}
	wg.Wait()
	mustBlocked(t, g, s, true, "20 lần sai song song")
}

// valkeyTestOpTimeout là hạn mỗi lệnh Valkey của test (Task 496): hạn mặc định 50 ms của mã chạy thật hết khi máy chạy
// test đang tải, lệnh coi như Valkey ngừng và test đếm nguồn, lượt gọi hãng đỏ dù mã đúng.
const valkeyTestOpTimeout = 2 * time.Second

// Valkey ngừng giữa chừng: bộ đếm lùi về trong tiến trình (FallbackMax 2 của SĐT) — vẫn chặn, không mở toang.
func TestCouponGuard_ValkeyDownFallback(t *testing.T) {
	vk := testkit.StartValkey(t)
	gate, err := cache.Dial(t.Context(), vk.URL("crm_gate"), cache.DialOptions{Name: "gate", OpTimeout: valkeyTestOpTimeout})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(gate.Close)
	clk := &guardClock{t: time.Date(2026, 11, 24, 10, 5, 0, 0, time.UTC)}
	customer, ip := guardLimits()
	customer.FallbackMax = 2
	g := httpapi.NewCouponGuard(cache.NewRateLimiter(gate, clk.now, nil), customer, ip, guardSecret())
	s := httpapi.CouponSubject{Phone: logging.Phone("+84901000010"), IP: netip.MustParseAddr("203.0.113.80")}
	fail(t, g, s, 2)
	mustBlocked(t, g, s, false, "Valkey chạy, 2 lần sai (ngưỡng 3)")
	fail(t, g, s, 1)
	mustBlocked(t, g, s, true, "Valkey chạy, 3 lần sai")

	vk.Stop(t)
	s2 := httpapi.CouponSubject{Phone: logging.Phone("+84901000011"), IP: netip.MustParseAddr("203.0.113.81")}
	mustBlocked(t, g, s2, false, "Valkey ngừng, chủ thể mới")
	fail(t, g, s2, 2)
	mustBlocked(t, g, s2, true, "Valkey ngừng, 2 lần sai (FallbackMax 2)")
}
