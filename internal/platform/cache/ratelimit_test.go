package cache_test

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/valkey-io/valkey-go"

	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/testkit"
)

// windowBase là một mốc chia hết cho mọi cửa sổ trong test (giây tròn của 2026-11-21 00:00 UTC).
func windowBase() time.Time { return time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC) }

func dialGate(t *testing.T, url string, reg *metrics.Registry, opTimeout time.Duration) *cache.Conn {
	t.Helper()
	c, err := cache.Dial(t.Context(), url, cache.DialOptions{Name: "gate", Metrics: reg, OpTimeout: opTimeout})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

// allowN gọi Allow n lần, trả số lần được phép và quyết định cuối.
func allowN(t *testing.T, rl cache.RateLimiter, l cache.Limit, subject string, n int) (int, cache.RateDecision) {
	t.Helper()
	ok := 0
	var last cache.RateDecision
	for range n {
		last = rl.Allow(t.Context(), l, subject)
		if last.Allowed {
			ok++
		}
	}
	return ok, last
}

// Max 5, cửa sổ 1 s: lần 1–5 được, lần 6 từ chối với RetryAfter ≤ 1 s; sang cửa sổ mới lại được; hai chủ thể độc
// lập; RetryAfter là thời gian tới hết cửa sổ (tối thiểu 1 s); chỉ số ratelimit_denied mang nhãn tên.
func TestRateLimiter_ValkeyFixedWindow(t *testing.T) {
	vk := testkit.StartValkey(t)
	reg := metrics.NewRegistry()
	clk := &fakeClock{t: windowBase()}
	rl := cache.NewRateLimiter(dialGate(t, vk.URL("crm_gate"), nil, 0), clk.now, reg)
	l := cache.Limit{Name: "t1", Max: 5, Window: time.Second, FallbackMax: 2}

	ok, last := allowN(t, rl, l, "svc:a", 5)
	if ok != 5 || last.Degraded {
		t.Fatalf("5 lần đầu: %d được, Degraded %v; muốn 5, false", ok, last.Degraded)
	}
	d := rl.Allow(t.Context(), l, "svc:a")
	if d.Allowed || d.Degraded || d.RetryAfter <= 0 || d.RetryAfter > time.Second {
		t.Fatalf("lần 6 = %+v; muốn từ chối, 0 < RetryAfter ≤ 1 s, không Degraded", d)
	}
	// Chủ thể khác cùng cửa sổ: bộ đếm riêng.
	if ok, _ := allowN(t, rl, l, "svc:b", 5); ok != 5 {
		t.Fatalf("chủ thể b: %d được, muốn 5", ok)
	}
	// Sang cửa sổ mới.
	clk.add(time.Second)
	if d := rl.Allow(t.Context(), l, "svc:a"); !d.Allowed {
		t.Fatalf("cửa sổ mới: %+v, muốn được", d)
	}

	// Cửa sổ 10 s, đang ở giây 3,5: RetryAfter = 6,5 s; ở giây 9,9: RetryAfter tối thiểu 1 s.
	l10 := cache.Limit{Name: "t10", Max: 1, Window: 10 * time.Second, FallbackMax: 1}
	clk.add(9*time.Second + 3500*time.Millisecond) // windowBase() + 13,5 s: giây 3,5 của cửa sổ [10 s, 20 s)
	rl.Allow(t.Context(), l10, "svc:a")
	if d := rl.Allow(t.Context(), l10, "svc:a"); d.Allowed || d.RetryAfter != 6500*time.Millisecond {
		t.Fatalf("cửa sổ 10 s ở giây 3,5: %+v, muốn từ chối, RetryAfter 6,5 s", d)
	}
	clk.add(6400 * time.Millisecond)
	if d := rl.Allow(t.Context(), l10, "svc:a"); d.Allowed || d.RetryAfter != time.Second {
		t.Fatalf("cửa sổ 10 s ở giây 9,9: %+v, muốn RetryAfter tối thiểu 1 s", d)
	}
	c := counts(reg.Drain())
	if c["ratelimit_denied/t1"] != 1 || c["ratelimit_denied/t10"] != 2 || c["ratelimit_degraded/t1"] != 0 {
		t.Fatalf("chỉ số: %v; muốn ratelimit_denied t1 = 1, t10 = 2, không degraded", c)
	}
}

// Khóa Valkey có hạn sống = phần còn lại của cửa sổ + 1 s. Đồng hồ giả cố định ở ms thứ 300 của cửa sổ 1 s nên hạn
// đặt ra luôn là 1.700 ms; Valkey đếm lùi theo giờ của nó, cận dưới 1.000 ms chừa gần 700 ms cho thời gian chạy.
func TestRateLimiter_KeyExpires(t *testing.T) {
	vk := testkit.StartValkey(t)
	admin := dial(t, vk.URL("crm_admin"), nil)
	now := windowBase().Add(300 * time.Millisecond)
	rl := cache.NewRateLimiter(dialGate(t, vk.URL("crm_gate"), nil, 0), func() time.Time { return now }, nil)
	l := cache.Limit{Name: "ttl", Max: 3, Window: time.Second, FallbackMax: 1}
	rl.Allow(t.Context(), l, "svc:x")
	keys := scanRL(t, admin)
	if len(keys) != 1 {
		t.Fatalf("khóa rl:* = %v, muốn 1", keys)
	}
	var pttl int64
	if err := admin.Do(t.Context(), "pttl", func(ctx context.Context, cl valkey.Client) error {
		var e error
		pttl, e = cl.Do(ctx, cl.B().Pttl().Key(keys[0]).Build()).AsInt64()
		return e //nolint:wrapcheck // Do của gói đang test
	}); err != nil {
		t.Fatalf("PTTL: %v", err)
	}
	if pttl <= 1000 || pttl > 1700 {
		t.Fatalf("PTTL = %d ms, muốn trong (1000, 1700]", pttl)
	}
}

// 1.000 goroutine cùng chủ thể trong một cửa sổ, Max 50 → đúng 50 Allowed (INCR nguyên tử ở máy chủ).
func TestRateLimiter_ParallelExact(t *testing.T) {
	vk := testkit.StartValkey(t)
	clk := &fakeClock{t: windowBase()}
	// Hạn lệnh dài: test này kiểm tính nguyên tử, không kiểm hạn 50 ms (1.000 lệnh dưới -race trên máy bận).
	rl := cache.NewRateLimiter(dialGate(t, vk.URL("crm_gate"), nil, 5*time.Second), clk.now, nil)
	l := cache.Limit{Name: "par", Max: 50, Window: time.Minute, FallbackMax: 10}
	var allowed, degraded atomic.Int64
	var wg sync.WaitGroup
	for range 1000 {
		wg.Go(func() {
			d := rl.Allow(t.Context(), l, "phone:abc")
			if d.Allowed {
				allowed.Add(1)
			}
			if d.Degraded {
				degraded.Add(1)
			}
		})
	}
	wg.Wait()
	if allowed.Load() != 50 || degraded.Load() != 0 {
		t.Fatalf("%d Allowed, %d Degraded; muốn 50, 0", allowed.Load(), degraded.Load())
	}
}

// Valkey ngừng: không lỗi, Degraded; FallbackMax 2 → lần 3 từ chối; sau 5 lỗi cầu dao mở (lời gọi sau không chạm
// mạng); chỉ số ratelimit_degraded.
func TestRateLimiter_FallbackInProcess(t *testing.T) {
	vk := testkit.StartValkey(t)
	reg := metrics.NewRegistry()
	gate := dialGate(t, vk.URL("crm_gate"), reg, 0)
	clk := &fakeClock{t: windowBase()}
	rl := cache.NewRateLimiter(gate, clk.now, reg)
	l := cache.Limit{Name: "fb", Max: 100, Window: 10 * time.Second, FallbackMax: 2}
	if d := rl.Allow(t.Context(), l, "svc:a"); !d.Allowed || d.Degraded {
		t.Fatalf("khi Valkey chạy: %+v", d)
	}
	vk.Stop(t)
	// Bộ đếm dự phòng bắt đầu từ 0 (không biết số của Valkey): 2 được, lần 3 từ chối.
	for i := range 2 {
		if d := rl.Allow(t.Context(), l, "svc:a"); !d.Allowed || !d.Degraded {
			t.Fatalf("lần dự phòng %d: %+v, muốn được, Degraded", i+1, d)
		}
	}
	d := rl.Allow(t.Context(), l, "svc:a")
	if d.Allowed || !d.Degraded || d.RetryAfter != 10*time.Second {
		t.Fatalf("lần dự phòng 3: %+v; muốn từ chối, Degraded, RetryAfter 10 s", d)
	}
	// Chủ thể khác có bộ đếm dự phòng riêng; lần gọi thứ 5 khi Valkey ngừng làm cầu dao mở.
	if ok, last := allowN(t, rl, l, "svc:b", 3); ok != 2 || !last.Degraded {
		t.Fatalf("chủ thể b dự phòng: %d được, muốn 2", ok)
	}
	c := counts(reg.Drain()) // gauge cầu dao đọc ngay sau, sau một lời gọi nữa
	if c["ratelimit_degraded/fb"] != 6 || c["ratelimit_denied/fb"] != 2 {
		t.Fatalf("chỉ số: %v; muốn ratelimit_degraded fb = 6, ratelimit_denied fb = 2", c)
	}
	if d := rl.Allow(t.Context(), l, "svc:c"); !d.Allowed || !d.Degraded {
		t.Fatalf("khi cầu dao mở: %+v", d)
	}
	if g := gauge(reg, "gate"); g != 1 {
		t.Fatalf("valkey_breaker_open{gate} = %v, muốn 1 (cầu dao mở sau 5 lỗi)", g)
	}
	// Sang cửa sổ dự phòng mới: được lại.
	clk.add(10 * time.Second)
	if d := rl.Allow(t.Context(), l, "svc:a"); !d.Allowed || !d.Degraded {
		t.Fatalf("cửa sổ mới khi dự phòng: %+v", d)
	}
}

// Conn nil (không cấu hình Valkey gate): luôn dự phòng; đồng hồ đi lùi không làm bộ đếm về 0.
func TestRateLimiter_NilConn(t *testing.T) {
	clk := &fakeClock{t: windowBase().Add(20 * time.Second)}
	rl := cache.NewRateLimiter(nil, clk.now, nil)
	l := cache.Limit{Name: "nil", Max: 10, Window: 10 * time.Second, FallbackMax: 3}
	if ok, last := allowN(t, rl, l, "svc:a", 2); ok != 2 || !last.Degraded {
		t.Fatalf("Conn nil: %d được, Degraded %v", ok, last.Degraded)
	}
	clk.add(-15 * time.Second) // cửa sổ trước
	if ok, _ := allowN(t, rl, l, "svc:a", 3); ok != 1 {
		t.Fatalf("đồng hồ lùi: %d được, muốn 1 (đếm vào cửa sổ đang giữ)", ok)
	}
	// Tên giới hạn khác, cùng chủ thể: bộ đếm riêng.
	l2 := l
	l2.Name = "nil2"
	if ok, _ := allowN(t, rl, l2, "svc:a", 4); ok != 3 {
		t.Fatalf("giới hạn khác: %d được, muốn 3", ok)
	}
}

// Bộ đếm dự phòng giữ tối đa cap chủ thể: đầy thì bỏ chủ thể cũ nhất (chủ thể đó đếm lại từ 0).
func TestRateLimiter_FallbackEvictsOldest(t *testing.T) {
	clk := &fakeClock{t: windowBase()}
	rl, size := cache.NewRateLimiterWithCap(nil, clk.now, nil, 3)
	l := cache.Limit{Name: "cap", Max: 5, Window: time.Minute, FallbackMax: 1}
	for _, s := range []string{"svc:a", "svc:b", "svc:c"} {
		if d := rl.Allow(t.Context(), l, s); !d.Allowed {
			t.Fatalf("%s lần 1: %+v", s, d)
		}
	}
	if d := rl.Allow(t.Context(), l, "svc:a"); d.Allowed {
		t.Fatal("svc:a lần 2 phải bị từ chối")
	}
	rl.Allow(t.Context(), l, "svc:d") // đầy: bỏ svc:a (cũ nhất)
	if n := size(); n != 3 {
		t.Fatalf("số chủ thể = %d, muốn 3", n)
	}
	if d := rl.Allow(t.Context(), l, "svc:a"); !d.Allowed {
		t.Fatal("svc:a đã bị bỏ thì đếm lại từ 0")
	}
	// svc:b bị bỏ khi svc:a vào lại; svc:c, svc:d còn giữ.
	for _, s := range []string{"svc:c", "svc:d"} {
		if d := rl.Allow(t.Context(), l, s); d.Allowed {
			t.Fatalf("%s còn trong bộ đếm, lần 2 phải bị từ chối", s)
		}
	}
	if n := size(); n != 3 {
		t.Fatalf("số chủ thể = %d, muốn 3", n)
	}
}

// Trần mặc định là 100.000 chủ thể.
func TestRateLimiter_FallbackDefaultCap(t *testing.T) {
	clk := &fakeClock{t: windowBase()}
	rl, size := cache.NewRateLimiterWithCap(nil, clk.now, nil, 0)
	l := cache.Limit{Name: "big", Max: 5, Window: time.Minute, FallbackMax: 1}
	var b strings.Builder
	for i := range 100_001 {
		b.Reset()
		b.WriteString("svc:")
		b.WriteString(strings.Repeat("x", 1+i%3))
		rl.Allow(t.Context(), l, b.String()+strconv.Itoa(i))
	}
	if n := size(); n != 100_000 {
		t.Fatalf("số chủ thể = %d, muốn 100000", n)
	}
}

// Limit hay chủ thể sai: từ chối (đóng), RetryAfter 1 s, không chạm Valkey, chỉ số nhãn invalid.
func TestRateLimiter_InvalidInput(t *testing.T) {
	reg := metrics.NewRegistry()
	rl := cache.NewRateLimiter(nil, windowBase, reg)
	good := cache.Limit{Name: "ok", Max: 5, Window: time.Second, FallbackMax: 2}
	for _, tc := range []struct {
		name    string
		l       cache.Limit
		subject string
	}{
		{"tên có dấu :", cache.Limit{Name: "a:b", Max: 5, Window: time.Second, FallbackMax: 2}, "svc:a"},
		{"tên rỗng", cache.Limit{Max: 5, Window: time.Second, FallbackMax: 2}, "svc:a"},
		{"Max 0", cache.Limit{Name: "x", Max: 0, Window: time.Second, FallbackMax: 0}, "svc:a"},
		{"FallbackMax 0", cache.Limit{Name: "x", Max: 5, Window: time.Second}, "svc:a"},
		{"FallbackMax > Max", cache.Limit{Name: "x", Max: 5, Window: time.Second, FallbackMax: 6}, "svc:a"},
		{"cửa sổ 500 ms", cache.Limit{Name: "x", Max: 5, Window: 500 * time.Millisecond, FallbackMax: 2}, "svc:a"},
		{"cửa sổ 1,5 s", cache.Limit{Name: "x", Max: 5, Window: 1500 * time.Millisecond, FallbackMax: 2}, "svc:a"},
		{"cửa sổ quá 1 giờ", cache.Limit{Name: "x", Max: 5, Window: time.Hour + time.Second, FallbackMax: 2}, "svc:a"},
		{"chủ thể rỗng", good, ""},
		{"chủ thể có khoảng trắng", good, "svc a"},
		{"chủ thể có *", good, "svc:*"},
		{"chủ thể dài 161", good, strings.Repeat("a", 161)},
	} {
		d := rl.Allow(t.Context(), tc.l, tc.subject)
		if d.Allowed || d.RetryAfter != time.Second {
			t.Errorf("%s: %+v, muốn từ chối, RetryAfter 1 s", tc.name, d)
		}
	}
	if c := counts(reg.Drain()); c["ratelimit_denied/invalid"] != 12 {
		t.Fatalf("chỉ số: %v, muốn ratelimit_denied invalid = 12", c)
	}
	if d := rl.Allow(t.Context(), good, strings.Repeat("a", 160)); !d.Allowed {
		t.Fatalf("chủ thể dài 160: %+v, muốn được", d)
	}
}

// Khóa rl:* trong Valkey không chứa chuỗi chủ thể gốc khi chủ thể là kết quả của SubjectKey (QĐ22).
func TestRateLimiter_KeysHaveNoPersonalData(t *testing.T) {
	vk := testkit.StartValkey(t)
	admin := dial(t, vk.URL("crm_admin"), nil)
	rl := cache.NewRateLimiter(dialGate(t, vk.URL("crm_gate"), nil, 0), windowBase, nil)
	secret := []byte(strings.Repeat("k", 32))
	limits := cache.DefaultLimits()
	phone, ip := "0901234567", "203.0.113.7"
	rl.Allow(t.Context(), limits.FlashCustomer, cache.SubjectKey(secret, "phone", phone))
	rl.Allow(t.Context(), limits.FlashIP, cache.SubjectKey(secret, "ip", ip))
	keys := scanRL(t, admin)
	if len(keys) != 2 {
		t.Fatalf("khóa rl:* = %v, muốn 2", keys)
	}
	wantPrefix := map[string]bool{"rl:flash_customer:phone:": false, "rl:flash_ip:ip:": false}
	for _, k := range keys {
		if strings.Contains(k, phone) || strings.Contains(k, ip) || strings.Contains(k, "901234567") {
			t.Errorf("khóa %q chứa dữ liệu gốc", k)
		}
		for p := range wantPrefix {
			if strings.HasPrefix(k, p) && strings.HasSuffix(k, ":"+strconv.FormatInt(windowBase().Unix(), 10)) {
				wantPrefix[p] = true
			}
		}
	}
	for p, ok := range wantPrefix {
		if !ok {
			t.Errorf("không có khóa dạng %s<băm>:%d trong %v", p, windowBase().Unix(), keys)
		}
	}
}

// Ngưỡng QĐ23 của khóa dịch vụ: 1.200 yêu cầu mỗi phút, yêu cầu thứ 1.201 bị từ chối (threat model M5, dòng D).
func TestRateLimit_ServiceKeyPerMinute(t *testing.T) {
	vk := testkit.StartValkey(t)
	clk := &fakeClock{t: windowBase().Add(30 * time.Second)}
	rl := cache.NewRateLimiter(dialGate(t, vk.URL("crm_gate"), nil, 5*time.Second), clk.now, nil)
	l := cache.DefaultLimits().WebKey
	ok, last := allowN(t, rl, l, "svc:0b1c", 1201)
	if ok != 1200 || last.Allowed || last.RetryAfter != 30*time.Second {
		t.Fatalf("%d được, lần cuối %+v; muốn 1200, từ chối, RetryAfter 30 s", ok, last)
	}
}

// scanRL liệt kê mọi khóa rl:* bằng crm_admin.
func scanRL(t *testing.T, admin *cache.Conn) []string {
	t.Helper()
	var out []string
	var cursor uint64
	for {
		var e valkey.ScanEntry
		if err := admin.Do(t.Context(), "scan", func(ctx context.Context, cl valkey.Client) error {
			var err error
			e, err = cl.Do(ctx, cl.B().Scan().Cursor(cursor).Match("rl:*").Count(1000).Build()).AsScanEntry()
			return err //nolint:wrapcheck // Do của gói đang test
		}); err != nil {
			t.Fatalf("SCAN: %v", err)
		}
		out = append(out, e.Elements...)
		if cursor = e.Cursor; cursor == 0 {
			return out
		}
	}
}
