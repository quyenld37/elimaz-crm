package cache

import (
	"container/list"
	"context"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/valkey-io/valkey-go"

	"elimaz.com.vn/crm/internal/platform/metrics"
)

// Limit là một giới hạn tần suất cửa sổ cố định: tối đa Max lượt mỗi Window với mỗi chủ thể khi có Valkey,
// FallbackMax (chặt hơn, 1 ≤ FallbackMax ≤ Max) khi đếm trong tiến trình. Name khớp ^[A-Za-z0-9_-]{1,100}$ (phần khóa
// và nhãn chỉ số); Window là bội của 1 s trong [1 s, 1 giờ].
type Limit struct {
	Name        string
	Max         int
	Window      time.Duration
	FallbackMax int
}

// RateDecision là kết quả của Allow. RetryAfter (chỉ khi không Allowed) là thời gian tới hết cửa sổ, tối thiểu 1 s.
// Degraded: Valkey không dùng được, quyết định lấy từ bộ đếm trong tiến trình.
type RateDecision struct {
	Allowed    bool
	RetryAfter time.Duration
	Degraded   bool
}

// RateLimiter giới hạn tần suất theo (giới hạn, chủ thể) trên Valkey gate (kế hoạch M5 Task 186). Không trả lỗi:
// Valkey lỗi thì tự lùi về bộ đếm trong tiến trình với FallbackMax và đặt Degraded.
//
// Khi nào không dùng: không phải chốt nghiệp vụ (giới hạn mỗi khách của flash sale chốt trong PostgreSQL); không gọi
// trong hàm của RetryTx (gọi mạng); chủ thể là SĐT, IP, email thì truyền SubjectKey, không truyền chuỗi gốc; mỗi tiến
// trình api đếm dự phòng riêng (ngưỡng thật khi dự phòng là FallbackMax × số tiến trình).
type RateLimiter interface {
	// Allow tính một lượt của subjectKey (^[A-Za-z0-9_:-]{1,160}$) với l. Limit hay chủ thể sai: từ chối, RetryAfter
	// 1 s, không chạm Valkey (lỗi lập trình, đóng thay vì mở).
	Allow(ctx context.Context, l Limit, subjectKey string) RateDecision
	// Peek trả quyết định mà lượt Allow tiếp theo sẽ nhận, không tính lượt nào (Task 482: chỉ đếm lần đoán mã sai
	// nhưng chặn mọi lần dùng mã khi đã quá ngưỡng). Valkey lỗi → đọc bộ đếm dự phòng, Degraded. Limit hay chủ thể sai:
	// từ chối như Allow.
	Peek(ctx context.Context, l Limit, subjectKey string) RateDecision
}

// Hằng của RateLimiter.
const (
	// fallbackMaxSubjects: trần số (giới hạn, chủ thể) của bộ đếm dự phòng; đầy thì bỏ cái cũ nhất.
	fallbackMaxSubjects = 100_000
	// keySlack: khóa Valkey sống thêm chừng này sau cửa sổ (đồng hồ các tiến trình api lệch nhau).
	keySlack = time.Second
	// minRetryAfter: RetryAfter tối thiểu (Retry-After của HTTP tính bằng giây).
	minRetryAfter = time.Second
	maxWindow     = time.Hour
	// invalidLabel: nhãn ratelimit_denied khi Limit hay chủ thể sai (tên sai không được làm nhãn).
	invalidLabel = "invalid"
)

// subjectPart là dạng của subjectKey: kết quả SubjectKey ("phone:<hex>"), "svc:<uuid>"…
var subjectPart = regexp.MustCompile(`^[A-Za-z0-9_:-]{1,160}$`)

// Script đếm: INCR, đặt hạn nếu khóa chưa có hạn (lần đầu của cửa sổ), trả số đếm. KEYS[1] = khóa; ARGV[1] = hạn ms.
const incrScript = `local n = redis.call('INCR', KEYS[1])
if redis.call('PTTL', KEYS[1]) < 0 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
return n`

type rateLimiter struct {
	c   *Conn
	now func() time.Time
	m   *metrics.Registry
	fb  *fallbackCounter
}

// NewRateLimiter dựng RateLimiter trên c (kết nối gate); c nil hợp lệ (luôn dự phòng). now nil dùng time.Now; m nil
// thì không ghi ratelimit_denied, ratelimit_degraded (nhãn tên giới hạn).
func NewRateLimiter(c *Conn, now func() time.Time, m *metrics.Registry) RateLimiter { //nolint:ireturn // Interfaces của kế hoạch M5 Task 186: bên dùng (httpapi) cắm bản giả
	return newRateLimiter(c, now, m, 0)
}

func newRateLimiter(c *Conn, now func() time.Time, m *metrics.Registry, capacity int) *rateLimiter {
	if now == nil {
		now = time.Now
	}
	if capacity <= 0 {
		capacity = fallbackMaxSubjects
	}
	return &rateLimiter{c: c, now: now, m: m, fb: newFallbackCounter(capacity)}
}

// valid: Limit dùng được (xem Limit).
func (l Limit) valid() bool {
	return keyPart.MatchString(l.Name) && l.Max > 0 && l.FallbackMax > 0 && l.FallbackMax <= l.Max &&
		l.Window >= time.Second && l.Window <= maxWindow && l.Window%time.Second == 0
}

func (r *rateLimiter) Allow(ctx context.Context, l Limit, subjectKey string) RateDecision {
	return r.decide(ctx, l, subjectKey, true)
}

func (r *rateLimiter) Peek(ctx context.Context, l Limit, subjectKey string) RateDecision {
	return r.decide(ctx, l, subjectKey, false)
}

// decide là phần chung của Allow (count: tính một lượt) và Peek (chỉ đọc bộ đếm).
func (r *rateLimiter) decide(ctx context.Context, l Limit, subjectKey string, count bool) RateDecision {
	if !l.valid() || !subjectPart.MatchString(subjectKey) {
		r.inc("ratelimit_denied", invalidLabel)
		return RateDecision{RetryAfter: minRetryAfter}
	}
	now := r.now()
	winMs := l.Window.Milliseconds()
	startMs := floorDiv(now.UnixMilli(), winMs) * winMs
	key := "rl:" + l.Name + ":" + subjectKey + ":" + strconv.FormatInt(startMs/1000, 10) //nolint:mnd // ms → giây
	untilEnd := time.Duration(startMs+winMs-now.UnixMilli()) * time.Millisecond

	d := RateDecision{}
	var n int64
	var err error
	if count {
		n, err = r.incr(ctx, key, untilEnd+keySlack)
	} else {
		n, err = r.get(ctx, key)
		n++ // lượt tiếp theo sẽ là n+1
	}
	if err == nil {
		d.Allowed = n <= int64(l.Max)
	} else {
		r.inc("ratelimit_degraded", l.Name)
		d.Degraded = true
		var fbEnd int64
		d.Allowed, fbEnd = r.fb.allow(l.Name+"\x00"+subjectKey, startMs, winMs, l.FallbackMax, count)
		untilEnd = time.Duration(fbEnd-now.UnixMilli()) * time.Millisecond
	}
	if !d.Allowed {
		r.inc("ratelimit_denied", l.Name)
		d.RetryAfter = max(untilEnd, minRetryAfter)
	}
	return d
}

// get đọc số đếm của key (chưa có → 0) qua Do; lỗi → bên gọi đi đường dự phòng.
func (r *rateLimiter) get(ctx context.Context, key string) (int64, error) {
	var n int64
	err := r.c.Do(ctx, "ratelimit.get", func(ctx context.Context, cl valkey.Client) error {
		v, e := cl.Do(ctx, cl.B().Get().Key(key).Build()).AsInt64()
		if valkey.IsValkeyNil(e) {
			n = 0
			return nil
		}
		n = v
		return e //nolint:wrapcheck // Do phân loại lỗi thật của valkey-go
	})
	return n, err
}

// incr chạy incrScript qua Do (hạn 50 ms, cầu dao); mọi lỗi (ngừng hay lỗi lệnh) làm bên gọi đi đường dự phòng.
func (r *rateLimiter) incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	var n int64
	err := r.c.Do(ctx, "ratelimit.incr", func(ctx context.Context, cl valkey.Client) error {
		var e error
		n, e = eval(ctx, cl, incrScript, key, strconv.FormatInt(max(ttl.Milliseconds(), 1), 10)).AsInt64()
		return e //nolint:wrapcheck // Do phân loại lỗi thật của valkey-go
	})
	return n, err
}

func (r *rateLimiter) inc(name, label string) {
	if r.m != nil {
		r.m.Inc(name, label, 1)
	}
}

// floorDiv chia làm tròn xuống (mốc trước 1970 vẫn đúng cửa sổ).
func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// fallbackCounter là bộ đếm cửa sổ cố định trong bộ nhớ, tối đa capacity khóa; đầy thì bỏ khóa có cửa sổ bắt đầu
// sớm nhất (danh sách theo lần mở cửa sổ, cũ ở đầu). An toàn khi gọi từ nhiều goroutine.
type fallbackCounter struct {
	capacity int

	mu    sync.Mutex
	items map[string]*list.Element
	order *list.List // phần tử *fbEntry, cũ nhất ở đầu
}

type fbEntry struct {
	key     string
	startMs int64
	endMs   int64
	count   int
}

func newFallbackCounter(capacity int) *fallbackCounter {
	return &fallbackCounter{capacity: capacity, items: map[string]*list.Element{}, order: list.New()}
}

// allow tính một lượt của key trong cửa sổ [startMs, startMs+winMs) (count false: chỉ xem, không tính, không thêm
// khóa); trả có được không và mốc hết cửa sổ đang đếm. Cửa sổ mới hơn cửa sổ đang giữ thì đếm lại từ 0; cũ hơn (đồng
// hồ lùi) thì đếm vào cửa sổ đang giữ.
func (f *fallbackCounter) allow(key string, startMs, winMs int64, maxN int, count bool) (allowed bool, endMs int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	el, ok := f.items[key]
	if !count {
		if !ok || startMs > el.Value.(*fbEntry).startMs { //nolint:forcetypeassert // danh sách chỉ chứa *fbEntry
			return true, startMs + winMs
		}
		e := el.Value.(*fbEntry) //nolint:forcetypeassert // danh sách chỉ chứa *fbEntry
		return e.count < maxN, e.endMs
	}
	if !ok {
		if f.order.Len() >= f.capacity {
			oldest := f.order.Front()
			delete(f.items, oldest.Value.(*fbEntry).key) //nolint:forcetypeassert // danh sách chỉ chứa *fbEntry
			f.order.Remove(oldest)
		}
		el = f.order.PushBack(&fbEntry{key: key, startMs: startMs, endMs: startMs + winMs})
		f.items[key] = el
	}
	e := el.Value.(*fbEntry) //nolint:forcetypeassert // danh sách chỉ chứa *fbEntry
	if startMs > e.startMs {
		e.startMs, e.endMs, e.count = startMs, startMs+winMs, 0
		f.order.MoveToBack(el)
	}
	if e.count >= maxN {
		return false, e.endMs
	}
	e.count++
	return true, e.endMs
}

func (f *fallbackCounter) size() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.order.Len()
}
