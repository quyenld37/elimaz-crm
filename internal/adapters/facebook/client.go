package facebook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/urlerr"
)

const (
	// callTimeout là hạn của một lời gọi Graph (Global Constraints M4).
	callTimeout = 10 * time.Second
	// maxReply là cỡ tối đa của thân trả lời Graph được đọc.
	maxReply = 4 << 20
	// MetricGraphMs là histogram thời gian một lời gọi Graph, nhãn "<METHOD> <đường dẫn đã bỏ mã>".
	MetricGraphMs = "graph_ms"
	// MetricGraphErrors là counter lời gọi Graph lỗi, nhãn là mã lỗi Meta, "http_<mã>", "build", "not_sent" hoặc
	// "transport".
	MetricGraphErrors = "graph_errors"
)

// Options cấu hình Graph client; tên trường theo config.MetaConfig. HTTP là http.Client của bên gọi (không dùng
// http.DefaultClient); hạn mỗi lời gọi do client tự đặt. Logger nil thì không ghi log.
type Options struct {
	BaseURL, StreamingBaseURL, Version, AppID string
	AppSecret, VerifyToken                    logging.Secret
	HTTP                                      *http.Client
	Now                                       func() time.Time
	Metrics                                   *metrics.Registry
	// AttachmentHosts là hậu tố tên máy được phép tải tệp đính kèm (fbcdn.net, fbsbx.com…).
	AttachmentHosts []string
	Logger          *slog.Logger
	// OAuthRedirectURL là redirect_uri đã khai với Meta, dùng khi đổi mã đăng nhập lấy token (ExchangeCode).
	OAuthRedirectURL string
}

// Client gọi Meta Graph API: mọi lời gọi có appsecret_proof, hạn 10 s, đọc header mức dùng, phân biệt "chắc chắn
// chưa gửi" với "có thể đã gửi". Không tự gửi lại. Dùng được từ nhiều goroutine.
type Client struct {
	o    Options
	base string
	dl   *downloader
}

// NewClient tạo Graph client.
func NewClient(o Options) *Client {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.HTTP == nil {
		o.HTTP = &http.Client{Timeout: callTimeout}
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	return &Client{o: o, base: strings.TrimRight(o.BaseURL, "/"), dl: newDownloader(o.HTTP, o.AttachmentHosts)}
}

// Do gọi Graph: đường dẫn /<Version>/<path>, token gửi bằng header Authorization (không nằm trong URL), thêm
// appsecret_proof = hex(HMAC-SHA256(token, AppSecret)); body khác nil gửi JSON; out khác nil nhận JSON trả về. Lỗi luôn
// là *GraphError (Sent cho biết yêu cầu đã ghi trọn lên kết nối chưa); Meta vẫn trả khi có câu trả lời, kể cả lỗi.
func (c *Client) Do(ctx context.Context, method, path string, token logging.Secret, q url.Values, body, out any) (Meta, error) {
	start := c.o.Now()
	meta, err := c.do(ctx, method, path, token, q, body, out)
	c.record(ctx, method, path, c.o.Now().Sub(start), err)
	return meta, err
}

func (c *Client) do(ctx context.Context, method, path string, token logging.Secret, q url.Values, body, out any) (Meta, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	var wrote atomic.Bool
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		WroteRequest: func(i httptrace.WroteRequestInfo) {
			if i.Err == nil {
				wrote.Store(true)
			}
		},
	})
	req, err := c.newRequest(ctx, method, path, token, q, body)
	if err != nil {
		return Meta{}, &GraphError{Sent: NotSent, Message: "dựng yêu cầu Graph", build: true, err: err}
	}
	resp, err := c.o.HTTP.Do(req)
	if err != nil {
		return Meta{}, transportError(err, wrote.Load(), token)
	}
	defer func() { _ = resp.Body.Close() }()
	meta := Meta{Usage: ParseUsage(resp.Header)}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxReply))
	if err != nil {
		return meta, transportError(err, true, token)
	}
	if ge := decodeError(resp.StatusCode, raw, token); ge != nil {
		ge.RetryAfter = retryHint(meta.Usage, resp.Header)
		return meta, ge
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return meta, &GraphError{HTTPStatus: resp.StatusCode, Sent: MaybeSent, Message: "đọc trả lời Graph", err: err}
		}
	}
	return meta, nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, token logging.Secret, q url.Values, body any) (*http.Request, error) {
	v := url.Values{}
	for k, vals := range q {
		v[k] = append([]string(nil), vals...)
	}
	if token != "" {
		v.Set("appsecret_proof", c.proof(token))
	}
	u := c.base + "/" + c.o.Version + "/" + strings.TrimLeft(path, "/")
	if len(v) > 0 {
		u += "?" + v.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("mã hóa thân yêu cầu: %w", err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, fmt.Errorf("dựng yêu cầu: %w", urlerr.Strip(err))
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+string(token))
	}
	return req, nil
}

// proof là appsecret_proof của token.
func (c *Client) proof(token logging.Secret) string {
	m := hmac.New(sha256.New, []byte(c.o.AppSecret))
	m.Write([]byte(token))
	return hex.EncodeToString(m.Sum(nil))
}

// transportError: lỗi mạng. Yêu cầu chưa ghi trọn lên kết nối → chắc chắn chưa gửi; còn lại → có thể đã gửi. Bỏ URL
// khỏi lỗi; lỗi hết hạn vẫn khớp errors.Is(err, context.DeadlineExceeded).
func transportError(err error, wrote bool, token logging.Secret) *GraphError {
	err = urlerr.Strip(err)
	sent := NotSent
	if wrote {
		sent = MaybeSent
	}
	return &GraphError{Sent: sent, Message: scrub(redactToken(err.Error(), token)), err: err}
}

// record ghi chỉ số và một dòng log debug (không token, không thân, đường dẫn đã bỏ mã).
func (c *Client) record(ctx context.Context, method, path string, took time.Duration, err error) {
	label := method + " " + metricPath(path)
	ms := float64(took.Milliseconds())
	status, code := 0, ""
	if ge, ok := asGraphError(err); ok {
		status, code = ge.HTTPStatus, errorLabel(ge)
	}
	if c.o.Metrics != nil {
		c.o.Metrics.Observe(MetricGraphMs, ms, label)
		if err != nil {
			c.o.Metrics.Inc(MetricGraphErrors, code, 1)
		}
	}
	c.o.Logger.DebugContext(ctx, "graph", "call", label, "ms", ms, "http", status, "error_code", code,
		"class", string(Classify(err)))
}

func errorLabel(ge *GraphError) string {
	switch {
	case ge.build:
		return "build"
	case ge.Code != 0:
		return strconv.Itoa(ge.Code)
	case ge.HTTPStatus != 0:
		return "http_" + strconv.Itoa(ge.HTTPStatus)
	case ge.Sent == NotSent:
		return "not_sent"
	}
	return "transport"
}

// metricPath thay mỗi đoạn đường dẫn có chữ số (mã Page, PSID, comment, video) bằng ":id".
func metricPath(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i, s := range parts {
		if strings.ContainsAny(s, "0123456789") {
			parts[i] = ":id"
		}
	}
	return strings.Join(parts, "/")
}
