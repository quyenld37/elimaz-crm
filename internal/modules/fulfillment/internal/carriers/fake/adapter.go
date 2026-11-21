// Package fake là hãng vận chuyển giả trong bộ nhớ (không mạng) cho test của fulfillment: hiện thực
// fulfillment.CarrierAdapter, tạo kiện theo mã của CRM (cùng mã lần hai trả lại kiện cũ), đổi chế độ lỗi, đổi trạng
// thái kiện bằng Advance. Bản gọi mạng tới chương trình tools/fakecarrier ở internal/adapters/carrier/fake.
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
	// ModeReject: hãng từ chối yêu cầu tạo (4xx), không tạo kiện.
	ModeReject = "reject"
	// ModeTimeoutNoCreate: quá giờ mà hãng chưa tạo kiện (tra theo mã sẽ nói thật "không có").
	ModeTimeoutNoCreate = "timeout_no_create"
)

// Trạng thái thô của kiện.
const (
	RawCreated   = "created"
	RawPickedUp  = "picked_up"
	RawDelivered = "delivered"
	RawCancelled = "cancelled"
)

// Chế độ hủy của hãng giả (SetCancelMode).
const (
	// CancelModeOK: kiện chờ thu gom → hủy (CancelDone); đã lấy → CancelRejectedPickedUp.
	CancelModeOK = "ok"
	// CancelModePending: hãng nhận yêu cầu, chưa trả lời (CancelPending), kiện chưa đổi.
	CancelModePending = "pending"
	// CancelModeTimeoutAfterSend: hãng hủy kiện rồi quá giờ (lỗi không rõ).
	CancelModeTimeoutAfterSend = "timeout_after_send"
	// CancelModeNotSent: chắc chưa gửi (ErrNotSent).
	CancelModeNotSent = "not_sent"
	// CancelModeReject: hãng từ chối yêu cầu hủy (ErrRejected), kiện chưa đổi.
	CancelModeReject = "reject"
)

// Phí cố định của mỗi kiện.
const fee = 25000

var errTimeout = errors.New("fake: quá giờ sau khi tạo kiện")

type parcel struct {
	code, tracking, status string
	seq                    int
}

// Carrier là hãng giả, an toàn khi gọi song song. Làm phần M1 (tạo, tra theo mã, đọc trạng thái) và hủy (Task M3-14);
// phần M3 khác trả fulfillment.ErrNotSupported (hãng giả đủ khả năng là internal/adapters/carrier/fake).
type Carrier struct {
	fulfillment.Unsupported

	mu           sync.Mutex
	mode         string
	cancelMode   string
	noCancel     bool
	idempotent   bool
	inconclusive bool
	createHook   func()
	calls        map[string]int
	byCode       map[string]*parcel
	byTracking   map[string]*parcel
}

// New tạo hãng giả ở chế độ ok.
func New() *Carrier {
	return &Carrier{mode: ModeOK, cancelMode: CancelModeOK, calls: map[string]int{}, byCode: map[string]*parcel{}, byTracking: map[string]*parcel{}}
}

// SetLookupInconclusive đặt v = true thì LookupByCode trả Inconclusive (hãng không trả lời chắc chắn), không tra kiện.
func (c *Carrier) SetLookupInconclusive(v bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inconclusive = v
}

// SetCreateHook đặt hàm chạy trong CreateShipment sau khi đã xử lý yêu cầu, trước khi trả lời (nil: bỏ). Hàm chạy
// ngoài khóa của hãng giả (giả lập tiến trình chết, chậm giữa lúc chờ trả lời).
func (c *Carrier) SetCreateHook(f func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.createHook = f
}

// Calls trả số lần gọi op ("create", "lookup", "cancel").
func (c *Carrier) Calls(op string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[op]
}

// Has cho biết hãng có kiện mang mã CRM code.
func (c *Carrier) Has(code string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.byCode[code]
	return ok
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
	return c.field(c.byCode, code, func(p *parcel) string { return p.tracking })
}

// field đọc một trường của kiện khóa k trong m (dưới khóa của hãng giả); không có → "".
func (c *Carrier) field(m map[string]*parcel, k string, f func(*parcel) string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := m[k]; ok {
		return f(p)
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

// CreateShipment theo chế độ: fail_before_create → ErrNotSent; reject → ErrRejected; timeout_after_create → tạo kiện
// rồi trả lỗi không rõ; timeout_no_create → lỗi không rõ, không tạo; ok → tạo (hoặc trả kiện cũ cùng mã). Móc
// SetCreateHook chạy trước khi trả lời.
func (c *Carrier) CreateShipment(_ context.Context, _ fulfillment.Account, r fulfillment.CreateRequest) (fulfillment.CreateResult, error) {
	res, hook, err := c.create(r)
	if hook != nil {
		hook()
	}
	return res, err
}

func (c *Carrier) create(r fulfillment.CreateRequest) (fulfillment.CreateResult, func(), error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls["create"]++
	switch c.mode {
	case ModeFailBeforeCreate:
		return fulfillment.CreateResult{}, c.createHook, fulfillment.ErrNotSent
	case ModeReject:
		return fulfillment.CreateResult{}, c.createHook, fulfillment.ErrRejected
	case ModeTimeoutNoCreate:
		return fulfillment.CreateResult{}, c.createHook, errTimeout
	}
	p, ok := c.byCode[r.Code]
	if !ok {
		p = &parcel{code: r.Code, tracking: fmt.Sprintf("FK%06d", len(c.byCode)+1), status: RawCreated}
		c.byCode[r.Code], c.byTracking[p.tracking] = p, p
	}
	if c.mode == ModeTimeoutAfterCreate {
		return fulfillment.CreateResult{}, c.createHook, errTimeout
	}
	return fulfillment.CreateResult{TrackingCode: p.tracking, Fee: fee, FeeKnown: true}, c.createHook, nil
}

// LookupByCode tra kiện theo mã CRM.
func (c *Carrier) LookupByCode(_ context.Context, _ fulfillment.Account, code string) (fulfillment.LookupResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls["lookup"]++
	if c.inconclusive {
		return fulfillment.LookupResult{Inconclusive: true}, nil
	}
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

// Capabilities trả khả năng: tạo kiện, tra theo mã, hủy (trừ khi SetCancelSupported(false)); CancelIdempotent theo
// SetCancelIdempotent.
func (c *Carrier) Capabilities() fulfillment.Capabilities {
	c.mu.Lock()
	defer c.mu.Unlock()
	return fulfillment.Capabilities{Create: true, LookupByRef: true, Cancel: !c.noCancel, CancelIdempotent: c.idempotent}
}

// SetCancelMode đổi chế độ hủy (CancelMode*).
func (c *Carrier) SetCancelMode(m string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancelMode = m
}

// SetCancelSupported đặt hãng có hủy qua API không (mặc định có).
func (c *Carrier) SetCancelSupported(v bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.noCancel = !v
}

// SetCancelIdempotent đặt cờ CancelIdempotent của Capabilities (mặc định false, Q-24).
func (c *Carrier) SetCancelIdempotent(v bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.idempotent = v
}

// Status trả trạng thái thô của kiện tracking ("" nếu không có).
func (c *Carrier) Status(tracking string) string {
	return c.field(c.byTracking, tracking, func(p *parcel) string { return p.status })
}

// Cancel hủy kiện tracking theo chế độ hủy (đếm "cancel"): ok → kiện chờ thu gom thành cancelled (CancelDone), đã
// hủy → CancelDone, đã lấy → CancelRejectedPickedUp; pending → CancelPending; timeout_after_send → hủy rồi lỗi không
// rõ; not_sent → ErrNotSent; reject, không có kiện, hay hãng không hủy qua API → ErrRejected / ErrNotSupported.
func (c *Carrier) Cancel(_ context.Context, _ fulfillment.Account, tracking string) (fulfillment.CancelResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls["cancel"]++
	if c.noCancel {
		return "", fulfillment.ErrNotSupported
	}
	p, ok := c.byTracking[tracking]
	switch {
	case c.cancelMode == CancelModeNotSent:
		return "", fulfillment.ErrNotSent
	case !ok || c.cancelMode == CancelModeReject:
		return "", fulfillment.ErrRejected
	case c.cancelMode == CancelModePending:
		return fulfillment.CancelPending, nil
	case p.status != RawCreated && p.status != RawCancelled:
		return fulfillment.CancelRejectedPickedUp, nil
	}
	if p.status != RawCancelled {
		p.status = RawCancelled
		p.seq++
	}
	if c.cancelMode == CancelModeTimeoutAfterSend {
		return "", errTimeout
	}
	return fulfillment.CancelDone, nil
}

// MapCanonical đổi trạng thái thô: created → awaiting_pickup, picked_up → picked_up, delivered → delivered,
// cancelled → cancelled.
func (*Carrier) MapCanonical(raw string) (fulfillment.Canonical, bool) {
	c, ok := map[string]fulfillment.Canonical{
		RawCreated: fulfillment.CanonAwaitingPickup, RawPickedUp: fulfillment.CanonPickedUp,
		RawDelivered: fulfillment.CanonDelivered, RawCancelled: fulfillment.CanonCancelled,
	}[raw]
	if !ok {
		return fulfillment.CanonUnknown, false
	}
	return c, true
}

// MapStatus đổi trạng thái thô qua MapCanonical: created → awaiting_pickup, picked_up → shipping, delivered → delivered.
// Mã lạ ra CanonUnknown, StatusFromCanonical của nó là ("", false).
func (c *Carrier) MapStatus(raw string) (fulfillment.Status, bool) {
	canon, _ := c.MapCanonical(raw)
	return fulfillment.StatusFromCanonical(canon)
}
