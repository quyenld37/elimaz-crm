package fake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

var errNoTracking = errors.New("fakecarrier: thiếu mã vận đơn")

// Capabilities trả mọi khả năng: hãng giả làm đủ mọi khả năng trừ bảng kê đối soát.
func (*Client) Capabilities() fulfillment.Capabilities {
	return fulfillment.Capabilities{
		Quote: true, Create: true, LookupByRef: true, Cancel: true, CancelIdempotent: true, Track: true, Webhook: true,
		Labels: true, LabelsBatch: true, UpdateAddress: true, Redeliver: true, Locations: true,
		Inspection: true, DeclaredValue: true, FeeOnRefuse: true,
	}
}

// MapCanonical đổi trạng thái thô của hãng giả sang trạng thái chuẩn; mã lạ → (CanonUnknown, false).
func (*Client) MapCanonical(raw string) (fulfillment.Canonical, bool) {
	switch raw {
	case RawCreated:
		return fulfillment.CanonAwaitingPickup, true
	case RawPickedUp:
		return fulfillment.CanonPickedUp, true
	case RawInTransit:
		return fulfillment.CanonInTransit, true
	case RawOutForDelivery:
		return fulfillment.CanonOutForDelivery, true
	case RawDeliveryFailed:
		return fulfillment.CanonDeliveryFailed, true
	case RawDelivered:
		return fulfillment.CanonDelivered, true
	case RawReturning:
		return fulfillment.CanonReturning, true
	case RawReturned:
		return fulfillment.CanonReturnedToShop, true
	case RawCancelled:
		return fulfillment.CanonCancelled, true
	case RawLost:
		return fulfillment.CanonLost, true
	}
	return fulfillment.CanonUnknown, false
}

// MapStatus đổi trạng thái thô qua MapCanonical và StatusFromCanonical: created → awaiting_pickup, picked_up →
// shipping, delivered → delivered (như M1).
// Mã lạ ra CanonUnknown, StatusFromCanonical của nó là ("", false).
func (c *Client) MapStatus(raw string) (fulfillment.Status, bool) {
	canon, _ := c.MapCanonical(raw)
	return fulfillment.StatusFromCanonical(canon)
}

// VerifyWebhook kiểm chữ ký x-fake-signature với bí mật secret (rỗng → false).
func (*Client) VerifyWebhook(raw []byte, h fulfillment.Header, secret string) bool {
	return Verify(raw, http.Header(h), secret)
}

// SplitWebhook đọc gói {"events":[…]}: mỗi sự kiện đủ eventId, tracking thành một tín hiệu (code của kiện nếu hãng
// gửi kèm); sự kiện thiếu trường bị bỏ. Thân không phải JSON đúng dạng là lỗi.
func (*Client) SplitWebhook(raw []byte) ([]fulfillment.WebhookSignal, error) {
	var p Packet
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("đọc gói fakecarrier: %w", err)
	}
	out := make([]fulfillment.WebhookSignal, 0, len(p.Events))
	for _, e := range p.Events {
		if e.EventID == "" || e.Tracking == "" {
			continue
		}
		out = append(out, fulfillment.WebhookSignal{EventKey: e.EventID, TrackingCode: e.Tracking, ShipmentCode: e.Code})
	}
	return out, nil
}

// CheckConnection gọi GET /v1/me: 200 → OK; lỗi theo fail (401 → ClassAuth).
func (c *Client) CheckConnection(ctx context.Context, _ fulfillment.Account) (fulfillment.Health, error) {
	rep, err := c.do(ctx, http.MethodGet, "/v1/me", nil, nil)
	if err != nil {
		return fulfillment.Health{}, err
	}
	if rep.status != http.StatusOK {
		return fulfillment.Health{Detail: http.StatusText(rep.status)}, fmt.Errorf("kiểm kết nối: %w", fail(rep))
	}
	return fulfillment.Health{OK: true}, nil
}

type quoteBody struct {
	FromProvince  string `json:"fromProvince"`
	ToProvince    string `json:"toProvince"`
	ToWard        string `json:"toWard"`
	WeightGrams   int    `json:"weightGrams"`
	DeclaredValue int64  `json:"declaredValue"`
	COD           int64  `json:"cod"`
	Service       string `json:"service,omitempty"`
	Inspection    string `json:"inspection,omitempty"`
	FeeOnRefuse   int64  `json:"feeOnRefuse,omitempty"`
}

type quoteReply struct {
	Quotes []struct {
		Service      string           `json:"service"`
		Name         string           `json:"name"`
		Total        int64            `json:"total"`
		Breakdown    map[string]int64 `json:"breakdown"`
		ExpectedDays int              `json:"expectedDays"`
	} `json:"quotes"`
}

// Quote gọi POST /v1/quote (chỉ mã địa giới, cân nặng, tiền; không gửi tên, SĐT, địa chỉ).
func (c *Client) Quote(ctx context.Context, _ fulfillment.Account, in fulfillment.QuoteInput) ([]fulfillment.QuoteResult, error) {
	var q quoteReply
	body := quoteBody{
		FromProvince: in.From.ProvinceCode, ToProvince: in.To.ProvinceCode, ToWard: in.To.WardCode,
		WeightGrams: in.Parcel.WeightGrams, DeclaredValue: in.Parcel.DeclaredValue, COD: in.COD, Service: in.Service,
		Inspection: in.Options.Inspection, FeeOnRefuse: in.Options.FeeOnRefuse,
	}
	rep, err := c.do(ctx, http.MethodPost, "/v1/quote", body, &q)
	if err != nil {
		return nil, err
	}
	if rep.status != http.StatusOK {
		return nil, fmt.Errorf("báo phí: %w", fail(rep))
	}
	out := make([]fulfillment.QuoteResult, 0, len(q.Quotes))
	for _, x := range q.Quotes {
		out = append(out, fulfillment.QuoteResult{
			Service: x.Service, ServiceName: x.Name, Total: x.Total, Breakdown: x.Breakdown, ExpectedDays: x.ExpectedDays,
		})
	}
	return out, nil
}

type resultReply struct {
	Result string `json:"result"`
}

// Cancel gọi POST /v1/parcels/{tracking}/cancel: done (cả khi đã hủy trước đó), rejected_picked_up khi hãng đã lấy.
func (c *Client) Cancel(ctx context.Context, _ fulfillment.Account, tracking string) (fulfillment.CancelResult, error) {
	var r resultReply
	if err := c.parcelOp(ctx, tracking, "cancel", nil, &r); err != nil {
		return "", fmt.Errorf("hủy kiện: %w", err)
	}
	return fulfillment.CancelResult(r.Result), nil
}

type eventReply struct {
	Key    string    `json:"key"`
	Status string    `json:"status"`
	At     time.Time `json:"at"`
}

type trackReply struct {
	Tracking     string       `json:"tracking"`
	Events       []eventReply `json:"events"`
	CODCollected *int64       `json:"codCollected"`
	FeeActual    *int64       `json:"feeActual"`
}

// Track gọi GET /v1/parcels/{tracking}/events; mỗi sự kiện đổi trạng thái chuẩn qua MapCanonical.
func (c *Client) Track(ctx context.Context, _ fulfillment.Account, tracking, _ string) (fulfillment.Tracking, error) {
	if tracking == "" {
		return fulfillment.Tracking{}, &fulfillment.CarrierError{Class: fulfillment.ClassRejected, Code: "no_tracking", Err: errNoTracking}
	}
	var tr trackReply
	rep, err := c.do(ctx, http.MethodGet, "/v1/parcels/"+url.PathEscape(tracking)+"/events", nil, &tr)
	if err != nil {
		return fulfillment.Tracking{}, err
	}
	if rep.status != http.StatusOK {
		return fulfillment.Tracking{}, fmt.Errorf("đọc hành trình: %w", fail(rep))
	}
	out := fulfillment.Tracking{TrackingCode: tr.Tracking, CODCollected: tr.CODCollected, FeeActual: tr.FeeActual}
	for _, e := range tr.Events {
		canon, _ := c.MapCanonical(e.Status)
		out.Events = append(out.Events, fulfillment.CarrierEvent{
			Key: e.Key, RawStatus: e.Status, Canonical: canon, OccurredAt: e.At.UTC(),
		})
	}
	return out, nil
}

type labelReply struct {
	PDF   []byte `json:"pdf"`
	Pages int    `json:"pages"`
}

// Labels gọi POST /v1/labels {trackings}: một PDF, mỗi kiện một trang.
func (c *Client) Labels(ctx context.Context, _ fulfillment.Account, trackingCodes []string) (fulfillment.Label, error) {
	if len(trackingCodes) == 0 {
		return fulfillment.Label{}, &fulfillment.CarrierError{Class: fulfillment.ClassRejected, Code: "no_tracking", Err: errNoTracking}
	}
	var l labelReply
	rep, err := c.do(ctx, http.MethodPost, "/v1/labels", map[string][]string{"trackings": trackingCodes}, &l)
	if err != nil {
		return fulfillment.Label{}, err
	}
	if rep.status != http.StatusOK {
		return fulfillment.Label{}, fmt.Errorf("in nhãn: %w", fail(rep))
	}
	return fulfillment.Label{PDF: l.PDF, Pages: l.Pages}, nil
}

// UpdateAddress gọi POST /v1/parcels/{tracking}/address với mã địa giới và dòng địa chỉ mới (hãng giả không nhận tên,
// SĐT).
func (c *Client) UpdateAddress(ctx context.Context, _ fulfillment.Account, tracking string, to fulfillment.Address) (fulfillment.UpdateResult, error) {
	var r resultReply
	in := map[string]string{"provinceCode": to.ProvinceCode, "wardCode": to.WardCode, "line": to.Line}
	if err := c.parcelOp(ctx, tracking, "address", in, &r); err != nil {
		return "", fmt.Errorf("sửa địa chỉ: %w", err)
	}
	return fulfillment.UpdateResult(r.Result), nil
}

// Redeliver gọi POST /v1/parcels/{tracking}/redeliver.
func (c *Client) Redeliver(ctx context.Context, _ fulfillment.Account, tracking, note string) (fulfillment.UpdateResult, error) {
	var r resultReply
	if err := c.parcelOp(ctx, tracking, "redeliver", map[string]string{"note": note}, &r); err != nil {
		return "", fmt.Errorf("giao lại: %w", err)
	}
	return fulfillment.UpdateResult(r.Result), nil
}

// parcelOp gọi POST /v1/parcels/{tracking}/{op}, đọc {result} vào out.
func (c *Client) parcelOp(ctx context.Context, tracking, op string, in any, out *resultReply) error {
	if tracking == "" {
		return &fulfillment.CarrierError{Class: fulfillment.ClassRejected, Code: "no_tracking", Err: errNoTracking}
	}
	rep, err := c.do(ctx, http.MethodPost, "/v1/parcels/"+url.PathEscape(tracking)+"/"+op, in, out)
	if err != nil {
		return err
	}
	if rep.status != http.StatusOK || out.Result == "" {
		return fail(rep)
	}
	return nil
}

type locationsReply struct {
	Locations []struct {
		Level  string `json:"level"`
		Code   string `json:"code"`
		Name   string `json:"name"`
		Parent string `json:"parent"`
	} `json:"locations"`
}

// Locations gọi GET /v1/locations?level=&parent=.
func (c *Client) Locations(ctx context.Context, _ fulfillment.Account, level, parent string) ([]fulfillment.Location, error) {
	var l locationsReply
	q := url.Values{"level": {level}, "parent": {parent}}
	rep, err := c.do(ctx, http.MethodGet, "/v1/locations?"+q.Encode(), nil, &l)
	if err != nil {
		return nil, err
	}
	if rep.status != http.StatusOK {
		return nil, fmt.Errorf("đọc địa giới: %w", fail(rep))
	}
	out := make([]fulfillment.Location, 0, len(l.Locations))
	for _, x := range l.Locations {
		out = append(out, fulfillment.Location{Level: x.Level, CarrierCode: x.Code, Name: x.Name, ParentCarrierCode: x.Parent})
	}
	return out, nil
}
