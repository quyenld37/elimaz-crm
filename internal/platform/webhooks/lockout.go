package webhooks

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Ngưỡng mặc định (spec 04 mục 11, ADR-30): quá 20 tin sai chữ ký trong 60 s thì chặn địa chỉ 600 s.
const (
	defaultLockoutThreshold = 20
	defaultLockoutWindow    = 60 * time.Second
	defaultLockoutBlock     = 600 * time.Second
	// lockoutSweepAt: giữ từ chừng này địa chỉ trở lên thì lần ghi lỗi kế tiếp quét bỏ địa chỉ đã hết hạn, tối đa một
	// lần mỗi Window (quét mỗi lần ghi sẽ thành O(số địa chỉ) cho mỗi tin sai khi bị gọi dồn từ nhiều địa chỉ).
	lockoutSweepAt = 10_000
)

// LockoutOptions là ngưỡng của Lockout; trường bằng 0 lấy mặc định (20 lần, 60 s, 600 s, time.Now).
type LockoutOptions struct {
	// Threshold: số lần sai trong Window làm địa chỉ bị chặn.
	Threshold int
	Window    time.Duration
	Block     time.Duration
	Now       func() time.Time
}

// Lockout đếm tin sai chữ ký theo địa chỉ, trong bộ nhớ của ingest, an toàn khi gọi từ nhiều goroutine. Tin đúng
// chữ ký không bị đếm hay giới hạn (để không chặn nhầm Meta trong ca live).
type Lockout struct {
	o         LockoutOptions
	mu        sync.Mutex
	ips       map[string]*lockState
	lastSweep time.Time
}

type lockState struct {
	fails []time.Time // các lần sai còn trong cửa sổ, cũ trước
	until time.Time   // bị chặn tới lúc này
}

// NewLockout tạo Lockout với ngưỡng o.
func NewLockout(o LockoutOptions) *Lockout {
	if o.Threshold <= 0 {
		o.Threshold = defaultLockoutThreshold
	}
	if o.Window <= 0 {
		o.Window = defaultLockoutWindow
	}
	if o.Block <= 0 {
		o.Block = defaultLockoutBlock
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Lockout{o: o, ips: map[string]*lockState{}}
}

// NoteFailure ghi một lần sai chữ ký của ip; đủ Threshold lần trong Window thì chặn ip trong Block.
func (l *Lockout) NoteFailure(ip string) {
	now := l.o.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.ips) >= lockoutSweepAt && now.Sub(l.lastSweep) >= l.o.Window {
		l.sweep(now)
		l.lastSweep = now
	}
	s := l.ips[ip]
	if s == nil {
		s = &lockState{}
		l.ips[ip] = s
	}
	s.fails = append(l.inWindow(s.fails, now), now)
	if len(s.fails) >= l.o.Threshold {
		s.until = now.Add(l.o.Block)
		s.fails = nil
	}
}

// Blocked báo ip đang bị chặn.
func (l *Lockout) Blocked(ip string) bool {
	now := l.o.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.ips[ip]
	return s != nil && now.Before(s.until)
}

// inWindow bỏ các lần sai đã ra khỏi cửa sổ.
func (l *Lockout) inWindow(fails []time.Time, now time.Time) []time.Time {
	cut := now.Add(-l.o.Window)
	i := 0
	for i < len(fails) && !fails[i].After(cut) {
		i++
	}
	return fails[i:]
}

// sweep bỏ các địa chỉ hết chặn và không còn lần sai nào trong cửa sổ.
func (l *Lockout) sweep(now time.Time) {
	for ip, s := range l.ips {
		s.fails = l.inWindow(s.fails, now)
		if len(s.fails) == 0 && !now.Before(s.until) {
			delete(l.ips, ip)
		}
	}
}

// ClientIP là địa chỉ người gửi: header cf-connecting-ip (máy chủ chỉ nhận kết nối qua Cloudflare, spec 04 mục 11),
// thiếu thì địa chỉ kết nối.
func ClientIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("Cf-Connecting-Ip")); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
