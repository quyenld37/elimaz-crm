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
	"strings"
	"time"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// CarrierKey là carrier_key của hãng giả trong CRM.
const CarrierKey = "fake"

// Trạng thái thô của hãng giả (chương trình tools/fakecarrier dùng đúng các chuỗi này).
const (
	RawCreated   = "created"
	RawPickedUp  = "picked_up"
	RawDelivered = "delivered"
)

// maxBody giới hạn thân trả lời đọc vào.
const maxBody = 64 << 10

var errStatus = errors.New("fakecarrier: mã trả lời lạ")

// Client gọi API của hãng giả.
type Client struct {
	base   string
	secret string
	hc     *http.Client
}

// NewClient tạo client tới base (FAKECARRIER_URL), xác thực bằng secret; hc nil → http.Client mặc định (hạn theo ctx).
func NewClient(base, secret string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{}
	}
	return &Client{base: strings.TrimRight(base, "/"), secret: secret, hc: hc}
}

// Key trả "fake".
func (*Client) Key() string { return CarrierKey }

type createBody struct {
	Code       string `json:"code"`
	CODAmount  int64  `json:"codAmount"`
	AccountKey string `json:"accountKey"`
}

type parcelReply struct {
	Tracking  string    `json:"tracking"`
	Fee       int64     `json:"fee"`
	Status    string    `json:"status"`
	Seq       int       `json:"seq"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// CreateShipment gọi POST /v1/parcels. 200 → kết quả; 503 (hãng báo chưa tạo) hay không nối được → ErrNotSent; 4xx →
// ErrRejected; còn lại (quá giờ, 5xx khác) → lỗi không rõ. Người nhận, SĐT, địa chỉ không gửi cho hãng giả.
func (c *Client) CreateShipment(ctx context.Context, a fulfillment.Account, r fulfillment.CreateRequest) (fulfillment.CreateResult, error) {
	var p parcelReply
	status, err := c.do(ctx, http.MethodPost, "/v1/parcels", createBody{Code: r.Code, CODAmount: r.CODAmount, AccountKey: a.ExternalAccountRef}, &p)
	switch {
	case err != nil:
		return fulfillment.CreateResult{}, err
	case status == http.StatusOK && p.Tracking != "":
		return fulfillment.CreateResult{TrackingCode: p.Tracking, Fee: p.Fee}, nil
	case status == http.StatusServiceUnavailable:
		return fulfillment.CreateResult{}, fulfillment.ErrNotSent
	case status >= 400 && status < 500:
		return fulfillment.CreateResult{}, fmt.Errorf("tạo kiện %d: %w", status, fulfillment.ErrRejected)
	}
	return fulfillment.CreateResult{}, fmt.Errorf("tạo kiện %d: %w", status, errStatus)
}

// LookupByCode gọi GET /v1/parcels?code=…: 200 → có; 404 → không có.
func (c *Client) LookupByCode(ctx context.Context, a fulfillment.Account, code string) (fulfillment.LookupResult, error) {
	var p parcelReply
	q := url.Values{"code": {code}, "accountKey": {a.ExternalAccountRef}}
	status, err := c.do(ctx, http.MethodGet, "/v1/parcels?"+q.Encode(), nil, &p)
	switch {
	case err != nil:
		return fulfillment.LookupResult{}, err
	case status == http.StatusOK:
		return fulfillment.LookupResult{Found: true, TrackingCode: p.Tracking, RawStatus: p.Status}, nil
	case status == http.StatusNotFound:
		return fulfillment.LookupResult{}, nil
	}
	return fulfillment.LookupResult{}, fmt.Errorf("tra kiện theo mã %d: %w", status, errStatus)
}

// GetStatus gọi GET /v1/parcels/{tracking}; EventKey = <trạng thái>:<lần đổi>. 404 → ErrRejected.
func (c *Client) GetStatus(ctx context.Context, _ fulfillment.Account, tracking string) (fulfillment.StatusResult, error) {
	var p parcelReply
	status, err := c.do(ctx, http.MethodGet, "/v1/parcels/"+url.PathEscape(tracking), nil, &p)
	switch {
	case err != nil:
		return fulfillment.StatusResult{}, err
	case status == http.StatusOK && p.Status != "":
		return fulfillment.StatusResult{
			RawStatus: p.Status, OccurredAt: p.UpdatedAt.UTC(), EventKey: fmt.Sprintf("%s:%d", p.Status, p.Seq),
		}, nil
	case status == http.StatusNotFound:
		return fulfillment.StatusResult{}, fmt.Errorf("đọc kiện: %w", fulfillment.ErrRejected)
	}
	return fulfillment.StatusResult{}, fmt.Errorf("đọc kiện %d: %w", status, errStatus)
}

// MapStatus đổi trạng thái thô: created → awaiting_pickup, picked_up → shipping, delivered → delivered.
func (*Client) MapStatus(raw string) (fulfillment.Status, bool) {
	switch raw {
	case RawCreated:
		return fulfillment.StatusAwaitingPickup, true
	case RawPickedUp:
		return fulfillment.StatusShipping, true
	case RawDelivered:
		return fulfillment.StatusDelivered, true
	}
	return "", false
}

// do gửi yêu cầu, đọc thân JSON vào out khi 200. Không nối được (lỗi dial) → ErrNotSent; lỗi khác sau khi gửi → lỗi
// không rõ (không chép thân trả lời vào lỗi).
func (c *Client) do(ctx context.Context, method, path string, in, out any) (int, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, fmt.Errorf("mã hóa yêu cầu hãng giả: %w", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return 0, fmt.Errorf("dựng yêu cầu hãng giả: %w: %w", err, fulfillment.ErrNotSent)
	}
	req.Header.Set("Authorization", "Bearer "+c.secret)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		if opErr, ok := errors.AsType[*net.OpError](err); ok && opErr.Op == "dial" {
			return 0, fmt.Errorf("nối hãng giả: %w", fulfillment.ErrNotSent)
		}
		return 0, fmt.Errorf("gọi hãng giả: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK && out != nil {
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out); err != nil {
			return 0, fmt.Errorf("đọc trả lời hãng giả: %w", err)
		}
	}
	return resp.StatusCode, nil
}
