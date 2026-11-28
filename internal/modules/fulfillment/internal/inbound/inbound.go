// Package inbound là handler sự kiện vào của webhook hãng (kế hoạch M3, Task M3-10): mỗi tín hiệu "vận đơn này vừa
// đổi" chỉ ghi lệnh tra lại fulfillment.refresh_status; không đổi trạng thái vận đơn, đơn, tồn (B10). Tài khoản tìm
// theo route_key trong tenant của việc, vận đơn theo đúng tài khoản (Q-7).
package inbound

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// SignalType là webhooks.InboundEvent.Type của tín hiệu hãng.
const SignalType = "carrier.signal"

// Chỉ số của handler (nhãn: khóa hãng của nguồn).
const (
	// MetricRevokedAccount: tín hiệu của tài khoản đã thu hồi, bỏ qua.
	MetricRevokedAccount = "carrier_signal_revoked_account"
	// MetricUnknownAccount: route_key không còn tài khoản (hay tài khoản của hãng khác nguồn), bỏ qua.
	MetricUnknownAccount = "carrier_signal_unknown_account"
	// MetricUnmatched: tín hiệu của vận đơn ngoài hệ thống (đơn cũ còn đang giao), đã ghi unmatched (Task 265).
	MetricUnmatched = "carrier_event_unmatched"
)

// accountRevoked là carrier_accounts.status của tài khoản đã thu hồi.
const accountRevoked = "revoked"

// ErrShipmentNotYetKnown báo tài khoản có nhưng chưa có vận đơn mang mã của tín hiệu (tín hiệu đến trước khi lệnh tạo
// gắn mã hãng). Lỗi tạm: việc vào thử lại theo lịch của hàng đợi; ở lượt thử cuối handler ghi "ngoài hệ thống" thay
// vì trả lỗi này (Task 265).
var ErrShipmentNotYetKnown = errors.New("fulfillment: chưa có vận đơn của tín hiệu hãng")

// Signal là payload của một tín hiệu hãng: khóa tuyến của tài khoản, khóa sự kiện (đã tính sẵn khi hãng không cho) và
// mã vận đơn. Không mang trạng thái: trạng thái thật chỉ đọc qua API có xác thực.
type Signal struct {
	RouteKey     string `json:"routeKey"`
	EventKey     string `json:"eventKey"`
	TrackingCode string `json:"trackingCode,omitempty"`
	ShipmentCode string `json:"shipmentCode,omitempty"`
}

// EnqueueRefresh ghi lệnh tra vận đơn shipment của tenant trong tx với khóa chống trùng dedupe.
type EnqueueRefresh func(ctx context.Context, tx pgx.Tx, tenant, shipment uuid.UUID, dedupe string) error

// RecordUnmatched ghi một sự kiện hãng của vận đơn ngoài hệ thống trong tx (fulfillment.RecordUnmatched).
type RecordUnmatched func(ctx context.Context, tx pgx.Tx, carrierKey string, accountID *uuid.UUID, trackingCode,
	rawStatus string, now time.Time) error

// Options cấu hình Handler.
type Options struct {
	// Source là khóa nguồn (= khóa hãng, /hooks/<Source>/<routeKey>).
	Source string
	// Enqueue ghi lệnh tra (bắt buộc).
	Enqueue EnqueueRefresh
	// Record ghi sự kiện ngoài hệ thống (bắt buộc).
	Record RecordUnmatched
	// Now nil: time.Now.
	Now func() time.Time
	// Metrics nil: không đếm.
	Metrics *metrics.Registry
}

// Handler là webhooks.InboundHandler của một hãng.
type Handler struct {
	o Options
}

// New dựng Handler.
func New(o Options) *Handler { return &Handler{o: o} }

// Source trả khóa nguồn.
func (h *Handler) Source() string { return h.o.Source }

// Handle xử lý một tín hiệu trong tx của việc vào (ngữ cảnh tenant e.TenantID). Loại lạ, payload sai → ErrPermanent;
// tài khoản không có, khác hãng hay đã thu hồi → bỏ (đếm), không lỗi; có vận đơn → ghi lệnh tra với khóa
// sig:<vận đơn>:<eventKey>. Không thấy vận đơn (Task 265, MO07, Q17; không tra hãng, không cảnh báo):
//   - tín hiệu không mang mã CRM và tài khoản không có vận đơn chờ mã hãng → đơn ngoài hệ thống: ghi unmatched, xong;
//   - còn lại (có thể là vận đơn của CRM chưa kịp gắn mã, VC10) → ErrShipmentNotYetKnown (lỗi tạm), trừ lượt thử cuối:
//     ghi unmatched, xong thay vì dead.
func (h *Handler) Handle(ctx context.Context, e webhooks.Inbound, tx pgx.Tx) error {
	if e.Type != SignalType {
		return fmt.Errorf("fulfillment: loại sự kiện %q: %w", e.Type, queue.ErrPermanent)
	}
	var s Signal
	if err := json.Unmarshal(e.Data, &s); err != nil || s.RouteKey == "" || s.EventKey == "" ||
		(s.TrackingCode == "" && s.ShipmentCode == "") {
		return fmt.Errorf("fulfillment: tín hiệu hãng thiếu trường: %w", queue.ErrPermanent)
	}
	q := store.New(tx)
	acc, err := q.InboundAccountByRouteKey(ctx, s.RouteKey)
	switch {
	case errors.Is(err, pgx.ErrNoRows) || (err == nil && acc.CarrierKey != h.o.Source):
		h.inc(MetricUnknownAccount)
		return nil
	case err != nil:
		return fmt.Errorf("tìm tài khoản hãng của tín hiệu: %w", err)
	case acc.Status == accountRevoked:
		h.inc(MetricRevokedAccount)
		return nil
	}
	id, err := shipmentOf(ctx, q, acc.ID, s)
	if errors.Is(err, ErrShipmentNotYetKnown) {
		return h.notFound(ctx, tx, q, acc.ID, s, e.LastAttempt)
	}
	if err != nil {
		return err
	}
	return h.o.Enqueue(ctx, tx, e.TenantID, id, "sig:"+id.String()+":"+s.EventKey)
}

// notFound là nhánh không thấy vận đơn của tín hiệu s trên tài khoản account (xem Handle).
func (h *Handler) notFound(ctx context.Context, tx pgx.Tx, q *store.Queries, account uuid.UUID, s Signal, last bool) error {
	if !last {
		if isCRMShipmentCode(s.ShipmentCode) {
			return ErrShipmentNotYetKnown
		}
		pending, err := q.InboundAccountHasPending(ctx, account)
		if err != nil {
			return fmt.Errorf("kiểm vận đơn chờ mã hãng của tài khoản: %w", err)
		}
		if pending {
			return ErrShipmentNotYetKnown
		}
	}
	code := s.TrackingCode
	if code == "" {
		code = s.ShipmentCode
	}
	now := time.Now
	if h.o.Now != nil {
		now = h.o.Now
	}
	if err := h.o.Record(ctx, tx, h.o.Source, &account, code, "", now().UTC()); err != nil {
		return err
	}
	h.inc(MetricUnmatched)
	return nil
}

// crmShipmentCode là mẫu mã vận đơn của CRM (fulfillment.NewShipmentCode): "E" + yyMMdd + 6 ký tự base32 Crockford.
var crmShipmentCode = regexp.MustCompile(`^E[0-9]{6}[0-9A-HJKMNP-TV-Z]{6}$`)

// isCRMShipmentCode: code đúng mẫu mã vận đơn của CRM.
func isCRMShipmentCode(code string) bool { return crmShipmentCode.MatchString(code) }

// shipmentOf tìm vận đơn của tín hiệu trên tài khoản account: theo mã CRM, rồi theo mã hãng.
func shipmentOf(ctx context.Context, q *store.Queries, account uuid.UUID, s Signal) (uuid.UUID, error) {
	if s.ShipmentCode != "" {
		id, err := q.InboundShipmentByCode(ctx, store.InboundShipmentByCodeParams{CarrierAccountID: account, Code: s.ShipmentCode})
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, fmt.Errorf("tìm vận đơn theo mã CRM: %w", err)
		}
	}
	if s.TrackingCode != "" {
		id, err := q.InboundShipmentByTracking(ctx, store.InboundShipmentByTrackingParams{
			CarrierAccountID: account, TrackingCode: pgtype.Text{String: s.TrackingCode, Valid: true},
		})
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, fmt.Errorf("tìm vận đơn theo mã hãng: %w", err)
		}
	}
	return uuid.Nil, ErrShipmentNotYetKnown
}

func (h *Handler) inc(name string) {
	if h.o.Metrics != nil {
		h.o.Metrics.Inc(name, h.o.Source, 1)
	}
}
