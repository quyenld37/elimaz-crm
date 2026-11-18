package observability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

// ErrNotLive báo /health/live trả mã khác 200.
var ErrNotLive = errors.New("/health/live không trả 200")

// CheckLive gọi GET http://127.0.0.1:<port>/health/live của tiến trình đang chạy cùng máy (lệnh `healthcheck`
// trong image không có curl). Lỗi khi không nối được, quá timeout, hoặc mã khác 200 (ErrNotLive).
func CheckLive(ctx context.Context, port int, timeout time.Duration) error {
	url := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) + "/health/live"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("tạo yêu cầu: %w", err)
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return fmt.Errorf("gọi %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s trả %d", ErrNotLive, url, resp.StatusCode)
	}
	return nil
}
