// Package nhanh là khách HTTP CHỈ ĐỌC của API Nhanh POS v3 (ADR-48, Task 540). Dùng duy nhất cho lệnh
// `crm nhanh-snapshot` (chụp dữ liệu khách và tối đa 50 sản phẩm mới trước và trong đêm mở); module nghiệp vụ không
// nhập gói này (test kiến trúc TestNhanhAdapter_OnlySnapshotCommandImports). Không ghi gì lên Nhanh: mọi đường gọi
// phải nằm trong danh sách trắng đường đọc, đường khác trả ErrPathNotAllowed TRƯỚC khi gửi.
package nhanh

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Đường đọc được phép (API v3, POST + JSON nhưng chỉ đọc).
const (
	PathProductList      = "product/list"
	PathProductDetail    = "product/detail"
	PathProductInventory = "product/inventory"
	PathCustomerList     = "customer/list"
)

// DefaultBaseURL là địa chỉ gốc của API Nhanh POS v3.
const DefaultBaseURL = "https://pos.open.nhanh.vn/v3.0"

func allowed(path string) bool {
	switch path {
	case PathProductList, PathProductDetail, PathProductInventory, PathCustomerList:
		return true
	}
	return false
}

// ReadPaths trả danh sách đường được phép gọi, theo thứ tự chữ.
func ReadPaths() []string {
	return []string{PathCustomerList, PathProductDetail, PathProductInventory, PathProductList}
}

// Lỗi của khách. Thân trả lời của Nhanh có thể chứa dữ liệu khách nên không bao giờ nằm trong lỗi.
var (
	// ErrPathNotAllowed: đường không nằm trong danh sách trắng; không có lời gọi mạng nào được gửi.
	ErrPathNotAllowed = errors.New("nhanh: đường gọi không nằm trong danh sách đường đọc")
	// ErrRateLimited: Nhanh khóa (ERR_429) lâu hơn trần chờ hay hết lượt thử.
	ErrRateLimited = errors.New("nhanh: bị giới hạn tốc độ")
	// ErrAPI: Nhanh trả code 0 (kèm mã lỗi, không kèm thân).
	ErrAPI = errors.New("nhanh: API trả lỗi")
	// ErrTransport: mạng hay HTTP lỗi sau các lượt thử.
	ErrTransport = errors.New("nhanh: không gọi được")
	// ErrBadResponse: trả lời không đúng khuôn (không phải JSON, thiếu code).
	ErrBadResponse = errors.New("nhanh: trả lời sai khuôn")
)

// Cấu hình mặc định.
const (
	defaultTimeout    = 30 * time.Second
	defaultMaxWait    = 2 * time.Minute
	defaultMaxRetries = 3
	maxBody           = 64 << 20
)

// Client gọi API Nhanh. Giá trị rỗng không dùng được: dựng bằng New.
type Client struct {
	// Now và Sleep tiêm được để test dùng một đồng hồ giả. Sleep trả lỗi khi ctx hủy.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
	// Pace là khoảng nghỉ tối thiểu giữa hai lời gọi (mức Nhanh: 150 lời gọi / 30 s mỗi đường).
	Pace time.Duration
	// MaxWait là thời gian chờ ERR_429 tối đa cho một lần; lâu hơn thì ErrRateLimited.
	MaxWait time.Duration
	// MaxRetries là số lượt thử lại cho ERR_429 và lỗi mạng (đọc nên thử lại an toàn).
	MaxRetries int

	base       string
	appID      string
	businessID string
	token      string
	http       *http.Client

	mu       sync.Mutex
	lockedTo map[string]time.Time
	lastCall time.Time
}

// Config là tham số của New.
type Config struct {
	BaseURL    string // rỗng: DefaultBaseURL
	AppID      string
	BusinessID string
	Token      string
	HTTP       *http.Client // rỗng: khách có thời gian chờ 30 s
}

// New dựng khách. Thiếu AppID, BusinessID hay Token là lỗi (không lộ giá trị).
func New(c Config) (*Client, error) {
	if c.AppID == "" || c.BusinessID == "" || c.Token == "" {
		return nil, errors.New("nhanh: thiếu appId, businessId hay token")
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	h := c.HTTP
	if h == nil {
		h = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{
		Now: time.Now, Sleep: sleepCtx, MaxWait: defaultMaxWait, MaxRetries: defaultMaxRetries,
		base: base, appID: c.AppID, businessID: c.BusinessID, token: c.Token, http: h,
		lockedTo: map[string]time.Time{},
	}, nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("chờ: %w", err)
		}
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("chờ: %w", ctx.Err())
	case <-t.C:
		return nil
	}
}

// Page là một trang trả lời thành công.
type Page struct {
	Data json.RawMessage
	// Next là con trỏ trang kế, gửi lại nguyên dạng; rỗng khi hết dữ liệu.
	Next json.RawMessage
}

type envelope struct {
	Code      int             `json:"code"`
	Data      json.RawMessage `json:"data"`
	Paginator *struct {
		Next json.RawMessage `json:"next"`
	} `json:"paginator"`
	ErrorCode string `json:"errorCode"`
}

type lockData struct {
	LockedSeconds float64 `json:"lockedSeconds"`
	UnlockedAt    float64 `json:"unlockedAt"`
}

// Call gửi một lời gọi đọc tới path với thân body (JSON). Đường ngoài danh sách trắng: ErrPathNotAllowed, không gửi gì.
// ERR_429: chờ tới unlockedAt (không gọi trong lúc khóa) rồi thử lại, tối đa MaxRetries lần, mỗi lần chờ không quá
// MaxWait. Lỗi mạng và HTTP 5xx thử lại với khoảng lùi tăng dần.
func (c *Client) Call(ctx context.Context, path string, body any) (Page, error) {
	if !allowed(path) {
		return Page{}, fmt.Errorf("gọi %q: %w", path, ErrPathNotAllowed)
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Page{}, fmt.Errorf("mã hóa thân %s: %w", path, err)
	}
	var lastErr error
	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		if err := c.waitTurn(ctx, path); err != nil {
			return Page{}, err
		}
		page, retryAfter, err := c.once(ctx, path, payload)
		if err == nil {
			return page, nil
		}
		lastErr = err
		if stop, serr := c.beforeRetry(ctx, path, attempt, err, retryAfter); stop {
			return Page{}, serr
		}
	}
	return Page{}, lastErr
}

// beforeRetry quyết định sau một lời gọi lỗi: dừng (stop, kèm lỗi trả về) hay thử lại. Lỗi khác ERR_429 và lỗi mạng
// dừng ngay; ERR_429 khóa quá MaxWait dừng; lỗi mạng chờ lùi tăng dần.
func (c *Client) beforeRetry(ctx context.Context, path string, attempt int, err error, retryAfter time.Duration) (stop bool, out error) {
	switch {
	case errors.Is(err, ErrRateLimited):
		return retryAfter > c.MaxWait, err
	case errors.Is(err, ErrTransport):
		if cerr := ctx.Err(); cerr != nil {
			return true, fmt.Errorf("gọi %s: %w", path, cerr)
		}
		if serr := c.Sleep(ctx, time.Duration(attempt+1)*time.Second); serr != nil {
			return true, serr
		}
		return false, nil
	}
	return true, err
}

// waitTurn chờ hết khóa ERR_429 của path và khoảng nghỉ Pace giữa hai lời gọi.
func (c *Client) waitTurn(ctx context.Context, path string) error {
	c.mu.Lock()
	until := c.lockedTo[path]
	if p := c.lastCall.Add(c.Pace); p.After(until) {
		until = p
	}
	c.mu.Unlock()
	if d := until.Sub(c.Now()); d > 0 {
		if err := c.Sleep(ctx, d); err != nil {
			return err
		}
	}
	c.mu.Lock()
	c.lastCall = c.Now()
	c.mu.Unlock()
	return nil
}

// once gửi đúng một lời gọi. retryAfter chỉ có nghĩa với ErrRateLimited.
func (c *Client) once(ctx context.Context, path string, payload []byte) (Page, time.Duration, error) {
	q := url.Values{"appId": {c.appID}, "businessId": {c.businessID}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/"+path+"?"+q.Encode(), bytes.NewReader(payload))
	if err != nil {
		return Page{}, 0, fmt.Errorf("dựng lời gọi %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		// Lỗi của net/http chứa URL (có appId) nhưng không có token; vẫn chỉ ghi đường gọi.
		return Page{}, 0, fmt.Errorf("gọi %s: %w", path, ErrTransport)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return Page{}, 0, fmt.Errorf("đọc trả lời %s: %w", path, ErrTransport)
	}
	if resp.StatusCode >= 500 {
		return Page{}, 0, fmt.Errorf("gọi %s: HTTP %d: %w", path, resp.StatusCode, ErrTransport)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil || (env.Code != 0 && env.Code != 1) {
		return Page{}, 0, fmt.Errorf("gọi %s: HTTP %d: %w", path, resp.StatusCode, ErrBadResponse)
	}
	if env.Code == 1 {
		p := Page{Data: env.Data}
		if env.Paginator != nil && string(env.Paginator.Next) != "null" {
			p.Next = env.Paginator.Next
		}
		return p, 0, nil
	}
	if env.ErrorCode == "ERR_429" || env.ErrorCode == "ERR_EXCEEDED_RATE_LIMIT" {
		return Page{}, c.lock(path, env.Data), fmt.Errorf("gọi %s: %w", path, ErrRateLimited)
	}
	return Page{}, 0, fmt.Errorf("gọi %s: %s: %w", path, safeCode(env.ErrorCode), ErrAPI)
}

// lock ghi khóa của path theo unlockedAt (giây Unix) hay lockedSeconds, trả thời gian phải chờ.
func (c *Client) lock(path string, data json.RawMessage) time.Duration {
	var ld lockData
	_ = json.Unmarshal(data, &ld)
	now := c.Now()
	until := now.Add(time.Second)
	switch {
	case ld.UnlockedAt > 0:
		until = time.Unix(int64(ld.UnlockedAt), 0)
	case ld.LockedSeconds > 0:
		until = now.Add(time.Duration(ld.LockedSeconds * float64(time.Second)))
	}
	// Lệch đồng hồ hai bên: chờ thêm một giây cho chắc.
	until = until.Add(time.Second)
	c.mu.Lock()
	c.lockedTo[path] = until
	c.mu.Unlock()
	return until.Sub(now)
}

// safeCode chỉ giữ mã lỗi dạng ERR_XXX (không chép chuỗi lạ vào lỗi).
func safeCode(s string) string {
	if len(s) > 48 || !strings.HasPrefix(s, "ERR_") {
		return "ERR_UNKNOWN"
	}
	for _, r := range s {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return "ERR_UNKNOWN"
		}
	}
	return s
}

// Rows tách data của một trang thành các dòng: mảng thì từng phần tử; object (kiểu khóa theo mã) thì các giá trị theo
// thứ tự khóa; null hay rỗng thì không dòng.
func Rows(data json.RawMessage) ([]json.RawMessage, error) {
	t := bytes.TrimSpace(data)
	if len(t) == 0 || string(t) == "null" {
		return nil, nil
	}
	switch t[0] {
	case '[':
		var out []json.RawMessage
		if err := json.Unmarshal(t, &out); err != nil {
			return nil, fmt.Errorf("tách mảng: %w", ErrBadResponse)
		}
		return out, nil
	case '{':
		var m map[string]json.RawMessage
		if err := json.Unmarshal(t, &m); err != nil {
			return nil, fmt.Errorf("tách object: %w", ErrBadResponse)
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sortKeys(keys)
		out := make([]json.RawMessage, 0, len(m))
		for _, k := range keys {
			out = append(out, m[k])
		}
		return out, nil
	}
	return nil, fmt.Errorf("data không phải mảng hay object: %w", ErrBadResponse)
}

// sortKeys sắp khóa theo số khi đều là số, không thì theo chữ (ổn định để bản chụp lặp lại được).
func sortKeys(keys []string) {
	nums := make(map[string]int64, len(keys))
	for _, k := range keys {
		n, err := strconv.ParseInt(k, 10, 64)
		if err != nil {
			slices.Sort(keys)
			return
		}
		nums[k] = n
	}
	slices.SortFunc(keys, func(a, b string) int { return cmp.Compare(nums[a], nums[b]) })
}
