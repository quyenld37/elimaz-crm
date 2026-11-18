package httpapi

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeClock: đồng hồ giả cho bộ đếm tần suất.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Ngưỡng thử 5 yêu cầu mỗi giây qua route thử: thứ 6 → 429 có retry-after; user khác 200; sau 1 s lại 200;
// route Stream không bị đếm.
func TestUserRateLimitHTTP(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	e := newAuthEnv(t, RateLimit{Max: 5, Window: time.Second, Now: clk.Now})
	a := e.staff.Tenant(t, "A")
	for _, email := range []string{"a@elimaz.test", "b@elimaz.test"} {
		uid := e.staff.User(t, a, email, "active")
		e.staff.Grant(t, a, uid, []string{"audit.read"}, "all", "")
	}
	ta, tb := e.token("a@elimaz.test"), e.token("b@elimaz.test")
	for i := range 5 {
		expect(t, "yêu cầu "+strconv.Itoa(i+1), e.get("/api/v1/_probe", ta), 200, "")
	}
	// Route Stream không đếm và không bị chặn.
	for range 3 {
		expect(t, "stream", e.get("/api/v1/_probe/stream", ta), 200, "")
	}
	rec := e.get("/api/v1/_probe", ta)
	expect(t, "yêu cầu thứ 6", rec, 429, "rate_limited")
	if rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("retry-after = %q, mong 1", rec.Header().Get("Retry-After"))
	}
	expect(t, "user khác", e.get("/api/v1/_probe", tb), 200, "")
	clk.Add(time.Second)
	expect(t, "sau 1 s", e.get("/api/v1/_probe", ta), 200, "")
}

// Cửa sổ trượt: yêu cầu ra khỏi cửa sổ đúng lúc nó đủ tuổi; retry-after làm tròn lên theo giây.
func TestUserLimiterSliding(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	l := newUserLimiter(RateLimit{Max: 3, Window: 60 * time.Second, Now: clk.Now})
	u := uuid.New()
	for range 3 {
		if ok, _ := l.allow(u); !ok {
			t.Fatal("3 yêu cầu đầu phải qua")
		}
		clk.Add(10 * time.Second)
	}
	// t=30: yêu cầu đầu (t=0) còn trong cửa sổ tới t=60.
	ok, wait := l.allow(u)
	if ok || wait != 30*time.Second {
		t.Fatalf("mong chặn, chờ 30s; có %v %v", ok, wait)
	}
	if retryAfterSeconds(1500*time.Millisecond) != "2" || retryAfterSeconds(0) != "1" {
		t.Fatal("retry-after phải làm tròn lên, tối thiểu 1")
	}
	clk.Add(30 * time.Second) // t=60: yêu cầu t=0 ra khỏi cửa sổ
	if ok, _ := l.allow(u); !ok {
		t.Fatal("t=60 phải qua")
	}
	if ok, _ := l.allow(u); ok {
		t.Fatal("t=60 lần hai phải chặn (t=10, 20, 60 còn trong cửa sổ)")
	}
}

// Mặc định đúng spec: 300 trong 60 s.
func TestRateLimitDefaults(t *testing.T) {
	l := newUserLimiter(RateLimit{})
	if l.max != 300 || l.window != 60*time.Second {
		t.Fatalf("mặc định %d/%v, mong 300/60s", l.max, l.window)
	}
}

// Nhiều goroutine cùng một user: không vượt Max (chạy với -race).
func TestUserLimiterConcurrent(t *testing.T) {
	l := newUserLimiter(RateLimit{Max: 50, Window: time.Minute})
	u := uuid.New()
	var wg sync.WaitGroup
	var mu sync.Mutex
	passed := 0
	for range 200 {
		wg.Go(func() {
			if ok, _ := l.allow(u); ok {
				mu.Lock()
				passed++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if passed != 50 {
		t.Fatalf("mong đúng 50 yêu cầu qua, có %d", passed)
	}
}

// User không còn gọi bị dọn khỏi bộ nhớ.
func TestUserLimiterSweeps(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	l := newUserLimiter(RateLimit{Max: 5, Window: time.Second, Now: clk.Now})
	for range 10 {
		l.allow(uuid.New())
	}
	clk.Add(2 * time.Minute)
	l.allow(uuid.New())
	if n := l.size(); n != 1 {
		t.Fatalf("sau khi dọn còn %d user, mong 1", n)
	}
}
