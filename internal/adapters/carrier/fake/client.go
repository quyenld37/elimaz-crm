// Package fake nối CRM với chương trình hãng vận chuyển giả tools/fakecarrier (chỉ dev, staging: cmd/crm chỉ
// đăng ký khi CRM_ENV khác production và có FAKECARRIER_URL). Client hiện thực fulfillment.CarrierAdapter qua HTTP
// (Bearer FAKECARRIER_WEBHOOK_SECRET); Source hiện thực webhooks.Source cho /hooks/fakecarrier (chữ ký
// x-fake-signature: sha256=<HMAC-SHA256>), tách gói thành tín hiệu fulfillment.Signal.
//
// Khi nào không dùng: không làm mẫu dữ liệu cho hãng thật (mỗi hãng một gói internal/adapters/<hãng>); không chạy ở
// production.
package fake

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// CarrierKey là carrier_key của hãng giả trong CRM.
const CarrierKey = "fake"

// Trạng thái thô của hãng giả (chương trình tools/fakecarrier dùng đúng các chuỗi này).
const (
	RawCreated        = "created"
	RawPickedUp       = "picked_up"
	RawInTransit      = "in_transit"
	RawOutForDelivery = "out_for_delivery"
	RawDeliveryFailed = "delivery_failed"
	RawDelivered      = "delivered"
	RawReturning      = "returning"
	RawReturned       = "returned"
	RawCancelled      = "cancelled"
	RawLost           = "lost"
)

// maxBody giới hạn thân trả lời đọc vào.
const maxBody = 64 << 10

var errStatus = errors.New("fakecarrier: mã trả lời lạ")

// retryAfterDefault là thời gian chờ khi hãng trả 429 mà không có Retry-After đọc được.
const retryAfterDefault = 30 * time.Second

// Client gọi API của hãng giả.
type Client struct {
	key    string
	base   string
	secret string
	hc     *http.Client
}

// NewClient tạo client khóa "fake" tới base (FAKECARRIER_URL), xác thực bằng secret; hc nil → http.Client mặc định
// (hạn theo ctx).
func NewClient(base, secret string, hc *http.Client) *Client {
	return NewClientWithKey(CarrierKey, base, secret, hc)
}

// NewClientWithKey như NewClient nhưng với khóa hãng key (test dựng hãng giả thứ hai, ví dụ "fake_b").
func NewClientWithKey(key, base, secret string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{}
	}
	return &Client{key: key, base: strings.TrimRight(base, "/"), secret: secret, hc: hc}
}

// Key trả khóa hãng ("fake" khi dựng bằng NewClient).
func (c *Client) Key() string { return c.key }

type createBody struct {
	Code       string `json:"code"`
	CODAmount  int64  `json:"codAmount"`
	AccountKey string `json:"accountKey"`
	RouteKey   string `json:"routeKey,omitempty"`
}

type parcelReply struct {
	Tracking  string    `json:"tracking"`
	Fee       int64     `json:"fee"`
	Status    string    `json:"status"`
	Seq       int       `json:"seq"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// reply là mã trả lời và header của hãng.
type reply struct {
	status int
	header http.Header
}

// CreateShipment gọi POST /v1/parcels (gửi kèm routeKey để webhook về đúng tài khoản). 200 → kết quả (hãng giả luôn
// báo phí); lỗi theo fail: 503, 429, không nối được → ErrNotSent; 401, 4xx khác → ErrRejected; quá giờ, 5xx khác → lỗi
// không rõ. Người nhận, SĐT, địa chỉ không gửi cho hãng giả.
func (c *Client) CreateShipment(ctx context.Context, a fulfillment.Account, r fulfillment.CreateRequest) (fulfillment.CreateResult, error) {
	var p parcelReply
	body := createBody{Code: r.Code, CODAmount: r.CODAmount, AccountKey: a.ExternalAccountRef, RouteKey: a.RouteKey}
	rep, err := c.do(ctx, http.MethodPost, "/v1/parcels", body, &p)
	switch {
	case err != nil:
		return fulfillment.CreateResult{}, err
	case rep.status == http.StatusOK && p.Tracking != "":
		return fulfillment.CreateResult{TrackingCode: p.Tracking, Fee: p.Fee, FeeKnown: true}, nil
	}
	return fulfillment.CreateResult{}, fmt.Errorf("tạo kiện: %w", fail(rep))
}

// LookupByCode gọi GET /v1/parcels?code=…: 200 → có; 404 → chắc chắn không có; lỗi khác theo fail.
func (c *Client) LookupByCode(ctx context.Context, a fulfillment.Account, code string) (fulfillment.LookupResult, error) {
	var p parcelReply
	q := url.Values{"code": {code}, "accountKey": {a.ExternalAccountRef}}
	rep, err := c.do(ctx, http.MethodGet, "/v1/parcels?"+q.Encode(), nil, &p)
	switch {
	case err != nil:
		return fulfillment.LookupResult{}, err
	case rep.status == http.StatusOK:
		return fulfillment.LookupResult{Found: true, TrackingCode: p.Tracking, RawStatus: p.Status}, nil
	case rep.status == http.StatusNotFound:
		return fulfillment.LookupResult{}, nil
	}
	return fulfillment.LookupResult{}, fmt.Errorf("tra kiện theo mã: %w", failRead(rep))
}

// GetStatus gọi GET /v1/parcels/{tracking}; EventKey = <trạng thái>:<lần đổi>. 404 → ErrRejected; lỗi khác theo
// failRead.
func (c *Client) GetStatus(ctx context.Context, _ fulfillment.Account, tracking string) (fulfillment.StatusResult, error) {
	var p parcelReply
	rep, err := c.do(ctx, http.MethodGet, "/v1/parcels/"+url.PathEscape(tracking), nil, &p)
	switch {
	case err != nil:
		return fulfillment.StatusResult{}, err
	case rep.status == http.StatusOK && p.Status != "":
		return fulfillment.StatusResult{
			RawStatus: p.Status, OccurredAt: p.UpdatedAt.UTC(), EventKey: fmt.Sprintf("%s:%d", p.Status, p.Seq),
		}, nil
	}
	return fulfillment.StatusResult{}, fmt.Errorf("đọc kiện: %w", failRead(rep))
}

// fail phân lớp trả lời không thành công: 401, 403 → ClassAuth (bọc ErrRejected: không tự gửi lại); 429 → ClassRateLimited
// có RetryAfter (bọc ErrNotSent: hãng chưa nhận); 503 → ClassNotSentRetry (ErrNotSent); 4xx khác → ClassRejected
// (ErrRejected); còn lại → ClassUnknownOutcome. Thân trả lời không chép vào lỗi.
func fail(rep reply) *fulfillment.CarrierError {
	code := "http_" + strconv.Itoa(rep.status)
	switch {
	case rep.status == http.StatusUnauthorized || rep.status == http.StatusForbidden:
		return &fulfillment.CarrierError{Class: fulfillment.ClassAuth, Code: code, Err: fulfillment.ErrRejected}
	case rep.status == http.StatusTooManyRequests:
		return &fulfillment.CarrierError{
			Class: fulfillment.ClassRateLimited, Code: code, RetryAfter: retryAfter(rep.header), Err: fulfillment.ErrNotSent,
		}
	case rep.status == http.StatusServiceUnavailable:
		return &fulfillment.CarrierError{Class: fulfillment.ClassNotSentRetry, Code: code, Err: fulfillment.ErrNotSent}
	case rep.status >= 400 && rep.status < 500:
		return &fulfillment.CarrierError{Class: fulfillment.ClassRejected, Code: code, Err: fulfillment.ErrRejected}
	}
	return &fulfillment.CarrierError{Class: fulfillment.ClassUnknownOutcome, Code: code, Err: errStatus}
}

// failRead như fail nhưng giữ nghĩa M1 của hai hàm đọc (LookupByCode, GetStatus): chỉ 404 bọc ErrRejected
// (refresh_status coi ErrRejected là dừng hẳn); 401, 429, 4xx khác, 5xx vẫn mang lớp lỗi nhưng bọc errStatus để lệnh
// đọc thử lại (khóa bị thu hồi rồi sửa thì vận đơn lại được cập nhật).
func failRead(rep reply) error {
	if rep.status == http.StatusNotFound {
		return fail(rep)
	}
	ce := fail(rep)
	ce.Err = errStatus
	return ce
}

// retryAfter đọc Retry-After dạng số giây; không đọc được → retryAfterDefault.
func retryAfter(h http.Header) time.Duration {
	if n, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After"))); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return retryAfterDefault
}

// do gửi yêu cầu, đọc thân JSON vào out khi 200. Không dựng hay không nối được → ClassNotSentRetry (ErrNotSent); lỗi
// khác sau khi gửi (quá giờ, đứt) hay thân 200 không đọc được → ClassUnknownOutcome.
func (c *Client) do(ctx context.Context, method, path string, in, out any) (reply, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return reply{}, fmt.Errorf("mã hóa yêu cầu hãng giả: %w", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return reply{}, &fulfillment.CarrierError{
			Class: fulfillment.ClassNotSentRetry, Code: "build_request",
			Err: fmt.Errorf("dựng yêu cầu hãng giả: %w: %w", err, fulfillment.ErrNotSent),
		}
	}
	req.Header.Set("Authorization", "Bearer "+c.secret)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		if opErr, ok := errors.AsType[*net.OpError](err); ok && opErr.Op == "dial" {
			return reply{}, &fulfillment.CarrierError{
				Class: fulfillment.ClassNotSentRetry, Code: "dial",
				Err: fmt.Errorf("nối hãng giả: %w", fulfillment.ErrNotSent),
			}
		}
		return reply{}, &fulfillment.CarrierError{
			Class: fulfillment.ClassUnknownOutcome, Code: "transport",
			Err: fmt.Errorf("gọi hãng giả: %w", err),
		}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK && out != nil {
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out); err != nil {
			return reply{}, &fulfillment.CarrierError{
				Class: fulfillment.ClassUnknownOutcome, Code: "bad_reply",
				Err: fmt.Errorf("đọc trả lời hãng giả: %w", err),
			}
		}
	}
	return reply{status: resp.StatusCode, header: resp.Header}, nil
}
