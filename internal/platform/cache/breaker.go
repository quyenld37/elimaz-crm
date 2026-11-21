package cache

import (
	"context"
	"errors"
	"sync"
	"time"

	"elimaz.com.vn/crm/internal/platform/metrics"
)

// Mặc định của cầu dao (QĐ22): mở sau 5 lỗi liền, mở 5 giây.
const (
	defaultFailThreshold = 5
	defaultOpenFor       = 5 * time.Second
)

// BreakerOptions là tham số của cầu dao. Số 0 dùng mặc định (FailThreshold 5, OpenFor 5 s, Now time.Now).
type BreakerOptions struct {
	FailThreshold int
	OpenFor       time.Duration
	Now           func() time.Time
}

// Breaker là cầu dao của một tiến trình Valkey: đóng thì cho mọi lượt qua; FailThreshold lỗi liền thì mở, mọi lượt
// trả ErrUnavailable ngay không gọi fn; hết OpenFor thì cho đúng một lượt thử (các lượt khác vẫn ErrUnavailable): thử
// tốt thì đóng, thử hỏng thì mở thêm OpenFor. Lượt mà bên gọi tự hủy ctx không tính tốt hay hỏng. Gauge
// valkey_breaker_open (nhãn tên) là 1 khi mở, 0 khi đóng, đặt ở mỗi lượt. An toàn khi gọi từ nhiều goroutine.
type Breaker struct {
	name string
	o    BreakerOptions
	m    *metrics.Registry

	mu        sync.Mutex
	fails     int
	openUntil time.Time // zero: đóng
	trial     bool      // đang có lượt thử
}

// NewBreaker tạo cầu dao tên name ("cache" | "gate", là nhãn gauge); m nil thì không ghi chỉ số.
func NewBreaker(name string, o BreakerOptions, m *metrics.Registry) *Breaker {
	if o.FailThreshold <= 0 {
		o.FailThreshold = defaultFailThreshold
	}
	if o.OpenFor <= 0 {
		o.OpenFor = defaultOpenFor
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Breaker{name: name, o: o, m: m}
}

// Do chạy fn qua cầu dao; mọi lỗi của fn là một lỗi của Valkey (bên gọi lọc lỗi không phải ngừng trước), trừ
// ErrUnavailable (fn không tới được Valkey) và lỗi khi ctx của bên gọi đã hủy. Cầu dao mở thì trả ErrUnavailable, fn
// không được gọi.
func (b *Breaker) Do(ctx context.Context, fn func(context.Context) error) error {
	trial, ok := b.enter()
	if !ok {
		return ErrUnavailable
	}
	err := fn(ctx)
	b.leave(ctx, trial, err)
	return err
}

// enter quyết định lượt có được chạy không; trial đúng khi đây là lượt thử của nửa mở.
func (b *Breaker) enter() (trial, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	defer b.gauge()
	switch {
	case b.openUntil.IsZero():
		return false, true
	case b.trial || b.o.Now().Before(b.openUntil):
		return false, false
	default:
		b.trial = true
		return true, true
	}
}

// leave ghi kết quả của lượt.
func (b *Breaker) leave(ctx context.Context, trial bool, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	defer b.gauge()
	if trial {
		b.trial = false
	}
	switch {
	case err != nil && (ctx.Err() != nil || errors.Is(err, ErrUnavailable)):
		// Bên gọi tự hủy, hay lượt không tới được Valkey (goroutine khác đang nối): không nói gì về Valkey.
	case err != nil && trial:
		b.openUntil = b.o.Now().Add(b.o.OpenFor)
	case err != nil && b.openUntil.IsZero():
		b.fails++
		if b.fails >= b.o.FailThreshold {
			b.openUntil = b.o.Now().Add(b.o.OpenFor)
		}
	case err == nil && trial:
		b.openUntil, b.fails = time.Time{}, 0
	case err == nil && b.openUntil.IsZero():
		b.fails = 0
	}
}

// gauge đặt valkey_breaker_open theo trạng thái hiện tại (giữ b.mu).
func (b *Breaker) gauge() {
	if b.m == nil {
		return
	}
	v := 0.0
	if !b.openUntil.IsZero() {
		v = 1
	}
	b.m.Gauge("valkey_breaker_open", v, b.name)
}
