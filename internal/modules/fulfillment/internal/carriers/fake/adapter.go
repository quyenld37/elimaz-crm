// Package fake là hãng vận chuyển giả trong bộ nhớ (không mạng) cho test của fulfillment: hiện thực
// fulfillment.CarrierAdapter, tạo kiện theo mã của CRM (cùng mã lần hai trả lại kiện cũ), đổi chế độ lỗi, đổi trạng
// thái kiện bằng Advance. Bản gọi mạng tới chương trình tools/fakecarrier ở internal/adapters/fakecarrier.
package fake

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// Key là carrier_key của hãng giả.
const Key = "fake"

// Chế độ của hãng giả.
const (
	ModeOK                 = "ok"
	ModeTimeoutAfterCreate = "timeout_after_create"
	ModeFailBeforeCreate   = "fail_before_create"
)

// Trạng thái thô của kiện.
const (
	RawCreated   = "created"
	RawPickedUp  = "picked_up"
	RawDelivered = "delivered"
)

// Phí cố định của mỗi kiện.
const fee = 25000

var errTimeout = errors.New("fake: quá giờ sau khi tạo kiện")

type parcel struct {
	code, tracking, status string
	seq                    int
}

// Carrier là hãng giả, an toàn khi gọi song song.
type Carrier struct {
	mu         sync.Mutex
	mode       string
	byCode     map[string]*parcel
	byTracking map[string]*parcel
}

// New tạo hãng giả ở chế độ ok.
func New() *Carrier {
	return &Carrier{mode: ModeOK, byCode: map[string]*parcel{}, byTracking: map[string]*parcel{}}
}

// SetMode đổi chế độ.
func (c *Carrier) SetMode(m string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mode = m
}

// Parcels là số kiện đã tạo.
func (c *Carrier) Parcels() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.byCode)
}

// Tracking trả mã hãng của kiện theo mã CRM ("" nếu chưa có).
func (c *Carrier) Tracking(code string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.byCode[code]; ok {
		return p.tracking
	}
	return ""
}

// Advance đặt trạng thái thô của kiện tracking (kể cả lùi, để thử tin đến muộn).
func (c *Carrier) Advance(tracking, raw string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.byTracking[tracking]; ok {
		p.status = raw
		p.seq++
	}
}

// Key trả "fake".
func (*Carrier) Key() string { return Key }

// CreateShipment theo chế độ: fail_before_create → ErrNotSent; timeout_after_create → tạo kiện rồi trả lỗi không rõ;
// ok → tạo (hoặc trả kiện cũ cùng mã).
func (c *Carrier) CreateShipment(_ context.Context, _ fulfillment.Account, r fulfillment.CreateRequest) (fulfillment.CreateResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mode == ModeFailBeforeCreate {
		return fulfillment.CreateResult{}, fulfillment.ErrNotSent
	}
	p, ok := c.byCode[r.Code]
	if !ok {
		p = &parcel{code: r.Code, tracking: fmt.Sprintf("FK%06d", len(c.byCode)+1), status: RawCreated}
		c.byCode[r.Code], c.byTracking[p.tracking] = p, p
	}
	if c.mode == ModeTimeoutAfterCreate {
		return fulfillment.CreateResult{}, errTimeout
	}
	return fulfillment.CreateResult{TrackingCode: p.tracking, Fee: fee}, nil
}

// LookupByCode tra kiện theo mã CRM.
func (c *Carrier) LookupByCode(_ context.Context, _ fulfillment.Account, code string) (fulfillment.LookupResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.byCode[code]
	if !ok {
		return fulfillment.LookupResult{}, nil
	}
	return fulfillment.LookupResult{Found: true, TrackingCode: p.tracking, RawStatus: p.status}, nil
}

// GetStatus trả trạng thái thật; EventKey = <trạng thái>:<lần đổi>.
func (c *Carrier) GetStatus(_ context.Context, _ fulfillment.Account, tracking string) (fulfillment.StatusResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.byTracking[tracking]
	if !ok {
		return fulfillment.StatusResult{}, fulfillment.ErrRejected
	}
	return fulfillment.StatusResult{
		RawStatus: p.status, OccurredAt: time.Now().UTC(), EventKey: fmt.Sprintf("%s:%d", p.status, p.seq),
	}, nil
}

// MapStatus đổi trạng thái thô: created → awaiting_pickup, picked_up → shipping, delivered → delivered.
func (*Carrier) MapStatus(raw string) (fulfillment.Status, bool) {
	st, ok := map[string]fulfillment.Status{
		RawCreated: fulfillment.StatusAwaitingPickup, RawPickedUp: fulfillment.StatusShipping,
		RawDelivered: fulfillment.StatusDelivered,
	}[raw]
	return st, ok
}
