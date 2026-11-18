package testkit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// ErrSSETimeout là lỗi khi Next không nhận được khung nào trong thời hạn.
var ErrSSETimeout = errors.New("testkit: hết hạn chờ khung SSE")

// ErrSSEClosed là lỗi khi luồng SSE đã đóng (máy chủ đóng hoặc Close).
var ErrSSEClosed = errors.New("testkit: luồng SSE đã đóng")

// SSEClient đọc luồng text/event-stream của GET /api/v1/stream. Khung chú thích (dòng bắt đầu bằng ":", ví dụ
// ": ping") được trả với event ":" + nội dung sau dấu hai chấm (": ping" → ":ping") và data rỗng.
type SSEClient struct {
	frames chan sseFrame
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

type sseFrame struct {
	event string
	data  json.RawMessage
}

// OpenSSE mở GET baseURL + "/api/v1/stream" với thẻ token trong header cf-access-jwt-assertion. Câu trả lời khác 200
// hay khác text/event-stream làm test đỏ (test mong 401, 403 dùng Do). Luồng đóng khi test xong.
func OpenSSE(t testing.TB, baseURL, token string) *SSEClient {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/stream", nil)
	if err != nil {
		cancel()
		t.Fatalf("testkit: dựng yêu cầu SSE: %v", err)
	}
	req.Header.Set("Cf-Access-Jwt-Assertion", token)
	// Không đặt Client.Timeout: hạn đó cắt cả luồng dài; Close hủy bằng ctx.
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		cancel()
		t.Fatalf("testkit: mở luồng SSE: %v", err)
	}
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		_ = resp.Body.Close()
		cancel()
		t.Fatalf("testkit: mở luồng SSE: mã %d, Content-Type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	c := &SSEClient{frames: make(chan sseFrame, 1024), cancel: cancel, done: make(chan struct{})}
	go c.read(resp)
	t.Cleanup(c.Close)
	return c
}

// read tách luồng thành khung (ngăn bằng dòng trống) tới khi luồng đóng; chủ của goroutine là Close (chờ done).
func (c *SSEClient) read(resp *http.Response) {
	defer close(c.done)
	defer close(c.frames)
	defer func() { _ = resp.Body.Close() }()
	sc := bufio.NewScanner(resp.Body)
	var f sseFrame
	var data []string
	have := false
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if have {
				if len(data) > 0 {
					f.data = json.RawMessage(strings.Join(data, "\n"))
				}
				c.frames <- f
			}
			f, data, have = sseFrame{}, nil, false
		case strings.HasPrefix(line, ":"):
			f.event, have = ":"+strings.TrimSpace(line[1:]), true
		case strings.HasPrefix(line, "event:"):
			f.event, have = strings.TrimSpace(line[len("event:"):]), true
		case strings.HasPrefix(line, "data:"):
			data, have = append(data, strings.TrimSpace(line[len("data:"):])), true
		}
	}
}

// Next trả khung kế tiếp trong timeout; hết hạn → ErrSSETimeout, luồng đóng → ErrSSEClosed.
func (c *SSEClient) Next(timeout time.Duration) (event string, data json.RawMessage, err error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case f, ok := <-c.frames:
		if !ok {
			return "", nil, ErrSSEClosed
		}
		return f.event, f.data, nil
	case <-timer.C:
		return "", nil, fmt.Errorf("%w (%s)", ErrSSETimeout, timeout)
	}
}

// Close đóng luồng và chờ goroutine đọc dừng; gọi nhiều lần được.
func (c *SSEClient) Close() {
	c.once.Do(func() {
		c.cancel()
		for range c.frames { // xả khung còn lại để goroutine đọc không kẹt khi gửi
		}
		<-c.done
	})
}
