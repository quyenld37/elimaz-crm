package imager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxResultBody: JSON kết quả của một việc (tối đa 32 đầu ra) nhỏ hơn nhiều.
const maxResultBody = 1 << 20

// ErrBusy là lỗi khi imager đang chạy đủ IMAGER_MAX_PARALLEL việc (503); bên gọi chờ BusyError.RetryAfter rồi gửi lại.
var ErrBusy = errors.New("imager: đang bận")

var errStatus = errors.New("imager: mã HTTP lạ")

// BusyError là lỗi 503 kèm thời gian chờ imager đề nghị (Retry-After; không có thì 1 s).
type BusyError struct{ RetryAfter time.Duration }

func (e *BusyError) Error() string {
	return "imager: đang bận, thử lại sau " + e.RetryAfter.String()
}

// Is cho errors.Is(err, ErrBusy).
func (e *BusyError) Is(target error) bool { return target == ErrBusy }

// Client gọi POST /v1/process của imager (worker dùng, Task M1-43). Hạn của một lần gọi là hạn của ctx. Client giữ
// tối đa maxParallel việc gửi cùng lúc (bằng IMAGER_MAX_PARALLEL của imager, để imager không phải trả 503); việc thừa
// chờ chỗ trống. Gặp 503 (imager đang làm việc của worker khác) thì chờ Retry-After rồi gửi lại, tới khi ctx hết hạn.
type Client struct {
	base  string
	hc    *http.Client
	slots chan struct{}
}

// NewClient trả client tới imager ở baseURL (IMAGER_URL, không có "/" cuối cũng được); maxParallel < 1 coi như 1.
func NewClient(baseURL string, hc *http.Client, maxParallel int) *Client {
	return &Client{base: strings.TrimSuffix(baseURL, "/"), hc: hc, slots: make(chan struct{}, max(1, maxParallel))}
}

// Process gửi req và trả Result (cả khi imager từ chối: Status rejected vẫn là 200). ctx hết hạn khi đang chờ chỗ
// trống hay chờ sau 503 → *BusyError (ErrBusy: chắc chắn imager chưa nhận việc); mã khác 200, 503 → lỗi; lỗi mạng,
// hết hạn ctx khi đang gửi → lỗi bọc nguyên nhân (context.DeadlineExceeded…).
func (c *Client) Process(ctx context.Context, req Request) (Result, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return Result{}, fmt.Errorf("mã hóa yêu cầu imager: %w", err)
	}
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return Result{}, &BusyError{RetryAfter: time.Second}
	}
	for {
		res, err := c.send(ctx, body)
		be, busy := errors.AsType[*BusyError](err)
		if !busy {
			return res, err
		}
		t := time.NewTimer(be.RetryAfter)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return Result{}, be
		}
	}
}

func (c *Client) send(ctx context.Context, body []byte) (Result, error) {
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/process", bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("tạo yêu cầu imager: %w", err)
	}
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(hreq)
	if err != nil {
		return Result{}, fmt.Errorf("gọi imager: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusServiceUnavailable:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResultBody))
		wait := time.Second
		if n, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && n > 0 && n <= 300 {
			wait = time.Duration(n) * time.Second
		}
		return Result{}, &BusyError{RetryAfter: wait}
	default:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResultBody))
		return Result{}, fmt.Errorf("%w: %d", errStatus, resp.StatusCode)
	}
	var res Result
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResultBody)).Decode(&res); err != nil {
		return Result{}, fmt.Errorf("đọc kết quả imager: %w", err)
	}
	return res, nil
}
