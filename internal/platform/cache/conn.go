// Package cache là lớp nối Valkey duy nhất của CRM (ADR-39, kế hoạch M5): kết nối có hạn giờ mỗi lệnh, cầu dao, khóa
// băm không mang dữ liệu cá nhân. Valkey chỉ làm nhanh và chắn tải, không bao giờ là nguồn sự thật: mọi đường dùng gói
// này phải chạy được khi Do trả ErrUnavailable (Conn nil, Valkey ngừng, cầu dao mở). Module nghiệp vụ không nhập gói
// này (luật kiến trúc (l)).
package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/valkey-io/valkey-go"
	"github.com/valkey-io/valkey-go/valkeyotel"

	"elimaz.com.vn/crm/internal/platform/metrics"
)

// ErrUnavailable báo không dùng được Valkey lúc này (không cấu hình, ngừng, quá hạn, cầu dao mở); bên gọi chuyển sang
// đường dự phòng (đọc PostgreSQL, bộ đếm trong tiến trình).
var ErrUnavailable = errors.New("cache: unavailable")

// ErrBadOp báo tên thao tác của Do ngoài ^[a-z0-9_.-]{1,40}$ (tên là nhãn chỉ số, không mang dữ liệu).
var ErrBadOp = errors.New("cache: tên thao tác không hợp lệ")

// ErrBadURL báo URL Valkey hay tên kết nối sai. Lỗi không kèm URL (URL có mật khẩu).
var ErrBadURL = errors.New("cache: URL Valkey không hợp lệ")

var opName = regexp.MustCompile(`^[a-z0-9_.-]{1,40}$`)

// Hạn mặc định (QĐ22) và hạn nối.
const (
	defaultOpTimeout = 50 * time.Millisecond
	// dialTimeout: hạn quay số TCP; một goroutine nối, các lượt khác trả ErrUnavailable trong lúc đó.
	dialTimeout = 200 * time.Millisecond
	// connTimeout: kết nối không trả lời quá hạn này thì valkey-go đóng và nối lại (ConnWriteTimeout).
	connTimeout = time.Second
)

// DialOptions là tham số của Dial. OpTimeout 0 dùng 50 ms. Metrics nil thì không ghi chỉ số. Name là "cache" hoặc
// "gate" (nhãn chỉ số, gauge cầu dao). Tracing bọc client bằng valkeyotel (span không ghi lệnh và tham số).
type DialOptions struct {
	OpTimeout time.Duration
	Breaker   BreakerOptions
	Metrics   *metrics.Registry
	Name      string
	Tracing   bool
}

// Conn là kết nối tới một tiến trình Valkey (valkey-go, tự gom lệnh gửi đồng thời vào một lượt). Conn nil hợp lệ: mọi
// Do trả ErrUnavailable (chế độ không có Valkey). An toàn khi gọi từ nhiều goroutine.
type Conn struct {
	name    string
	opt     valkey.ClientOption
	timeout time.Duration
	tracing bool
	br      *Breaker
	m       *metrics.Registry

	connMu sync.Mutex // giữ khi đang nối hay đóng
	cl     atomic.Pointer[clientRef]
	closed atomic.Bool
}

type clientRef struct{ c valkey.Client }

// Dial kiểm URL dạng valkey://user:pass@host:port/0 (hoặc valkeys://) và thử nối một lần. Valkey chưa lên không phải
// lỗi (api vẫn chạy khi Valkey ngừng, ADR-39): Conn nối lại lười ở lượt Do sau, mỗi lần nối hỏng là một lỗi của cầu
// dao. Chỉ lỗi khi URL hay Name sai (ErrBadURL, không kèm URL).
func Dial(ctx context.Context, rawURL string, o DialOptions) (*Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cache: nối %s: %w", o.Name, err)
	}
	if o.Name != "cache" && o.Name != "gate" {
		return nil, fmt.Errorf("%w: tên kết nối phải là cache hoặc gate", ErrBadURL)
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "valkey" && u.Scheme != "valkeys") || u.Host == "" {
		return nil, fmt.Errorf("%w (%s)", ErrBadURL, o.Name)
	}
	opt, err := valkey.ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("%w (%s)", ErrBadURL, o.Name)
	}
	opt.DisableCache = true // không dùng bộ đệm phía client (CLIENT TRACKING): bộ đệm đọc là việc của ReadCache
	opt.Dialer.Timeout = dialTimeout
	opt.ConnWriteTimeout = connTimeout
	if o.OpTimeout <= 0 {
		o.OpTimeout = defaultOpTimeout
	}
	c := &Conn{
		name: o.Name, opt: opt, timeout: o.OpTimeout, tracing: o.Tracing, m: o.Metrics,
		br: NewBreaker(o.Name, o.Breaker, o.Metrics),
	}
	_, _ = c.client() // nối hỏng: lượt Do sau thử lại
	return c, nil
}

// Do chạy fn với client đã nối và ctx có hạn OpTimeout, qua cầu dao; ghi valkey_ms và valkey_errors (nhãn name:op).
// op là tên thao tác cố định của mã gọi ("readcache.get"). fn chỉ trả lỗi của valkey-go (xử lý dữ liệu đọc được ở
// ngoài fn). Lỗi ngừng (quá hạn, mạng, máy chủ đầy hay đang nạp, cầu dao mở, Conn nil) bọc ErrUnavailable; lỗi của
// lệnh (khóa không có, NOPERM, sai kiểu) trả nguyên như valkey-go trả, không tính vào cầu dao.
func (c *Conn) Do(ctx context.Context, op string, fn func(ctx context.Context, cl valkey.Client) error) error {
	if c == nil {
		return ErrUnavailable
	}
	if !opName.MatchString(op) {
		return ErrBadOp
	}
	start := time.Now()
	var cmdErr error
	err := c.br.Do(ctx, func(ctx context.Context) error {
		cmdErr = nil
		cl, err := c.client()
		if err != nil {
			return err
		}
		opCtx, cancel := context.WithTimeout(ctx, c.timeout)
		defer cancel()
		err = fn(opCtx, cl)
		if outage(err) {
			return err
		}
		cmdErr = err
		return nil
	})
	switch {
	case errors.Is(err, ErrUnavailable):
		err = fmt.Errorf("valkey %s %s: %w", c.name, op, err)
	case err != nil:
		err = fmt.Errorf("%w: valkey %s %s: %w", ErrUnavailable, c.name, op, err)
	default:
		err = cmdErr
	}
	if c.m != nil {
		label := c.name + ":" + op
		c.m.Observe("valkey_ms", float64(time.Since(start).Microseconds())/1000, label)
		if err != nil && !valkey.IsValkeyNil(err) {
			c.m.Inc("valkey_errors", label, 1)
		}
	}
	return err
}

// Ping gửi PING qua Do.
func (c *Conn) Ping(ctx context.Context) error {
	return c.Do(ctx, "ping", func(ctx context.Context, cl valkey.Client) error {
		return cl.Do(ctx, cl.B().Ping().Build()).Error()
	})
}

// Close đóng kết nối; Do sau đó trả ErrUnavailable. Gọi trên Conn nil hay gọi hai lần không sao.
func (c *Conn) Close() {
	if c == nil {
		return
	}
	c.closed.Store(true)
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if r := c.cl.Swap(nil); r != nil {
		r.c.Close()
	}
}

// client trả client đã nối, hoặc nối nếu chưa. Đang có goroutine khác nối, hay Conn đã đóng: ErrUnavailable (cầu dao
// không tính lượt này).
func (c *Conn) client() (valkey.Client, error) { //nolint:ireturn // valkey-go chỉ có kiểu interface valkey.Client
	if r := c.cl.Load(); r != nil {
		return r.c, nil
	}
	if !c.connMu.TryLock() {
		return nil, ErrUnavailable
	}
	defer c.connMu.Unlock()
	if c.closed.Load() {
		return nil, ErrUnavailable
	}
	if r := c.cl.Load(); r != nil {
		return r.c, nil
	}
	var cl valkey.Client
	var err error
	if c.tracing {
		cl, err = valkeyotel.NewClient(c.opt)
	} else {
		cl, err = valkey.NewClient(c.opt)
	}
	if err != nil {
		return nil, fmt.Errorf("nối valkey %s: %w", c.name, err)
	}
	c.cl.Store(&clientRef{c: cl})
	return cl, nil
}

// outage: lỗi là Valkey ngừng hay quá hạn (tính vào cầu dao), không phải lỗi của lệnh.
func outage(err error) bool {
	if err == nil || valkey.IsValkeyNil(err) {
		return false
	}
	var ve *valkey.ValkeyError
	if errors.As(err, &ve) {
		// Lỗi máy chủ trả mà nghĩa là máy chủ không phục vụ được (đầy với noeviction, đang nạp dữ liệu, bận chạy script,
		// chưa xác thực sau khi khởi động lại): tính là ngừng. Lỗi khác (NOPERM, WRONGTYPE…) là lỗi của lệnh.
		msg := ve.Error()
		for _, p := range []string{"OOM ", "LOADING ", "BUSY ", "MASTERDOWN ", "TRYAGAIN ", "NOAUTH "} {
			if strings.HasPrefix(msg, p) {
				return true
			}
		}
		return false
	}
	var ne net.Error
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) ||
		errors.Is(err, valkey.ErrClosing) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.As(err, &ne)
}
