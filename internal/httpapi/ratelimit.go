package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Giới hạn mặc định của mỗi nhân viên (spec 04 mục 11, ADR-30): 300 yêu cầu trong 60 s trượt.
const (
	defaultRateMax    = 300
	defaultRateWindow = 60 * time.Second
	// defaultServiceRateMax: ngưỡng mỗi khóa dịch vụ mỗi phút khi chưa nối Deps.RateLimiter (bằng FallbackMax của
	// cache.DefaultLimits().WebKey).
	defaultServiceRateMax = 600
)

// WebRateLimiter giới hạn tần suất theo khóa dịch vụ (M5-05): subject là "svc:<ServiceKeyID>"; trả có được phép không
// và thời gian chờ khi không. Không trả lỗi (bản Valkey tự lùi về bộ đếm trong tiến trình). Bản thật: NewWebKeyLimiter
// (cache.RateLimiter với cache.Limits.WebKey, Task 189).
type WebRateLimiter interface {
	AllowWebKey(ctx context.Context, subject string) (allowed bool, retryAfter time.Duration)
}

// NewWebKeyLimiter nối cache.RateLimiter vào WebRateLimiter với giới hạn l (cmd/crm: cache.Limits.WebKey của
// RATE_LIMITS_JSON).
func NewWebKeyLimiter(rl cache.RateLimiter, l cache.Limit) WebRateLimiter { //nolint:ireturn // trả interface phía dùng (Deps.RateLimiter)
	return webKeyLimiter{rl: rl, l: l}
}

type webKeyLimiter struct {
	rl cache.RateLimiter
	l  cache.Limit
}

func (w webKeyLimiter) AllowWebKey(ctx context.Context, subject string) (bool, time.Duration) {
	d := w.rl.Allow(ctx, w.l, subject)
	return d.Allowed, d.RetryAfter
}

// WebOrderLimiter giới hạn số đơn web của mỗi khách (Task 194, QĐ23 web_order_customer) theo SĐT đã chuẩn hóa; trả có
// được phép không và thời gian chờ khi không. Bản thật: NewWebOrderLimiter.
type WebOrderLimiter interface {
	AllowWebOrder(ctx context.Context, phone logging.Phone) (allowed bool, retryAfter time.Duration)
}

// NewWebOrderLimiter nối cache.RateLimiter vào WebOrderLimiter với giới hạn l; chủ thể là cache.SubjectKey(secret,
// "phone", SĐT) — khóa Valkey không mang SĐT (QĐ22). cmd/crm: cache.Limits.WebOrderCustomer, CACHE_KEY_SECRET. secret
// rỗng (không Valkey, không CACHE_KEY_SECRET) → 32 byte ngẫu nhiên của tiến trình (chỉ bộ đếm trong tiến trình thấy).
func NewWebOrderLimiter(rl cache.RateLimiter, l cache.Limit, secret []byte) WebOrderLimiter { //nolint:ireturn // trả interface phía dùng (Deps.WebOrderLimiter)
	if len(secret) == 0 {
		secret = make([]byte, randomSecretLen)
		_, _ = rand.Read(secret) // crypto/rand.Read không trả lỗi (Go ≥ 1.24)
	}
	return webOrderLimiter{rl: rl, l: l, secret: bytes.Clone(secret)}
}

// randomSecretLen: số byte secret HMAC ngẫu nhiên khi không có CACHE_KEY_SECRET.
const randomSecretLen = 32

type webOrderLimiter struct {
	rl     cache.RateLimiter
	l      cache.Limit
	secret []byte
}

func (w webOrderLimiter) AllowWebOrder(ctx context.Context, phone logging.Phone) (bool, time.Duration) {
	d := w.rl.Allow(ctx, w.l, cache.SubjectKey(w.secret, "phone", string(phone)))
	return d.Allowed, d.RetryAfter
}

// RateLimit là ngưỡng giới hạn tần suất theo nhân viên; trường bằng 0 lấy mặc định (300 / 60 s, time.Now). Test đặt
// ngưỡng nhỏ và đồng hồ giả.
type RateLimit struct {
	Max    int
	Window time.Duration
	Now    func() time.Time
}

// userLimiter đếm yêu cầu của từng UserID trong bộ nhớ theo cửa sổ trượt (giữ mốc thời gian của tối đa Max yêu cầu
// gần nhất). Chỉ điều tiết, không quyết định nghiệp vụ; mỗi tiến trình api đếm riêng.
type userLimiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu        sync.Mutex
	hits      map[uuid.UUID][]time.Time
	lastSweep time.Time
}

func newUserLimiter(o RateLimit) *userLimiter {
	l := &userLimiter{max: o.Max, window: o.Window, now: o.Now, hits: map[uuid.UUID][]time.Time{}}
	if l.max <= 0 {
		l.max = defaultRateMax
	}
	if l.window <= 0 {
		l.window = defaultRateWindow
	}
	if l.now == nil {
		l.now = time.Now
	}
	return l
}

// allow ghi một yêu cầu của u nếu còn chỗ trong cửa sổ; không thì trả thời gian tới khi yêu cầu cũ nhất ra khỏi
// cửa sổ.
func (l *userLimiter) allow(u uuid.UUID) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	hs := prune(l.hits[u], now.Add(-l.window))
	if len(hs) >= l.max {
		l.hits[u] = hs
		return false, hs[0].Add(l.window).Sub(now)
	}
	l.hits[u] = append(hs, now)
	return true, 0
}

// prune bỏ các mốc không sau cutoff (mốc đúng bằng cutoff đã đủ tuổi window).
func prune(hs []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(hs) && !hs[i].After(cutoff) {
		i++
	}
	return hs[i:]
}

// sweep (mỗi window, tối thiểu mỗi phút) xóa nhân viên không còn yêu cầu nào trong cửa sổ, để bản đồ không lớn mãi.
func (l *userLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < max(l.window, time.Minute) {
		return
	}
	l.lastSweep = now
	cutoff := now.Add(-l.window)
	for u, hs := range l.hits {
		if len(prune(hs, cutoff)) == 0 {
			delete(l.hits, u)
		}
	}
}

func (l *userLimiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.hits)
}

// retryAfterSeconds: giây chờ, làm tròn lên, tối thiểu 1 (header Retry-After).
func retryAfterSeconds(d time.Duration) string {
	return strconv.Itoa(max(1, int(math.Ceil(d.Seconds()))))
}

// userRateLimit: vượt ngưỡng → 429 rate_limited kèm Retry-After. Đặt sau auth và guard (đếm theo UserID); route
// Stream không gắn (cmd chain bỏ qua).
func userRateLimit(l *userLimiter, next http.Handler) http.Handler {
	return limitBy(func(_ *http.Request, u identity.AuthUser) (bool, time.Duration) { return l.allow(u.UserID) }, next)
}

// serviceRateLimit: giới hạn tần suất của route service_key theo khóa (ServiceKeyID), đặt sau serviceKeyAuth và guard.
// d.RateLimiter nil → bộ đếm trong tiến trình d.serviceLimiter. Vượt → 429 rate_limited kèm Retry-After.
func serviceRateLimit(d Deps, next http.Handler) http.Handler {
	return limitBy(func(r *http.Request, u identity.AuthUser) (bool, time.Duration) {
		if d.RateLimiter != nil {
			return d.RateLimiter.AllowWebKey(r.Context(), "svc:"+u.ServiceKeyID.String())
		}
		return d.serviceLimiter.allow(u.ServiceKeyID)
	}, next)
}

// limitBy: không có người gọi trong ctx → 403; allow từ chối → 429 rate_limited kèm Retry-After (giây, làm tròn lên).
func limitBy(allow func(*http.Request, identity.AuthUser) (bool, time.Duration), next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := authUser(r.Context())
		if !ok {
			writeError(w, r, http.StatusForbidden, codeForbidden)
			return
		}
		if allowed, wait := allow(r, u); !allowed {
			w.Header().Set("Retry-After", retryAfterSeconds(wait))
			writeError(w, r, http.StatusTooManyRequests, codeRateLimited)
			return
		}
		next.ServeHTTP(w, r)
	})
}
