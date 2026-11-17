package metrics

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// pingTimeout là hạn của một lần gửi nhịp sống ra ngoài.
const pingTimeout = 5 * time.Second

// Pinger gửi nhịp sống ra dịch vụ bên ngoài (ADR-38) bằng GET tới URL (HEARTBEAT_URL). Client nil dùng một
// http.Client riêng có hạn 5 s.
type Pinger struct {
	URL    string
	Client *http.Client
}

// Ping gửi một GET, hạn 5 s. URL rỗng (hay p nil) thì không làm gì. Lỗi không chứa URL (thường mang mã bí mật).
// Bên gọi chỉ log và đếm lỗi, không thử lại.
func (p *Pinger) Ping(ctx context.Context) error {
	if p == nil || p.URL == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return fmt.Errorf("tạo yêu cầu nhịp sống: %w", stripURL(err))
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: pingTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("gửi nhịp sống: %w", stripURL(err))
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("gửi nhịp sống: %w %d", ErrPingStatus, resp.StatusCode)
	}
	return nil
}

// ErrPingStatus báo dịch vụ nhận nhịp sống trả mã không phải 2xx.
var ErrPingStatus = errors.New("dịch vụ nhận nhịp sống trả mã")

// ErrPingDNS báo không phân giải được tên máy của dịch vụ nhận nhịp sống.
var ErrPingDNS = errors.New("không phân giải được tên máy nhận nhịp sống")

// stripURL bóc *url.Error, *net.OpError, *net.DNSError chỉ còn lỗi gốc, để URL và địa chỉ máy nhận không lọt vào log.
// Lỗi hết hạn vẫn khớp errors.Is(err, context.DeadlineExceeded).
func stripURL(err error) error {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		err = ue.Err
	}
	if _, ok := errors.AsType[*net.DNSError](err); ok {
		return ErrPingDNS
	}
	if oe, ok := errors.AsType[*net.OpError](err); ok {
		return fmt.Errorf("%s: %w", oe.Op, oe.Err)
	}
	return err
}
