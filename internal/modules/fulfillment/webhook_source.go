package fulfillment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// SignalType là webhooks.InboundEvent.Type của tín hiệu hãng: "vận đơn này vừa đổi, hãy hỏi lại".
const SignalType = "carrier.signal"

// Signal là payload chuẩn của một tín hiệu hãng (nguồn webhook của từng hãng — internal/adapters/<hãng> — tách gói
// ra dạng này). Không mang trạng thái: trạng thái thật chỉ đọc qua API có xác thực (B10).
type Signal struct {
	CarrierKey   string `json:"carrierKey"`
	AccountKey   string `json:"accountKey"`
	TrackingCode string `json:"trackingCode"`
	EventID      string `json:"eventId"`
}

// SignalHandler là webhooks.InboundHandler của một nguồn hãng: webhook chỉ là tín hiệu, handler không gọi hãng, chỉ
// ghi lệnh fulfillment.refresh_status (DedupeKey evt:<eventId>, OrderKey shipment:<id>) trong tx của việc.
type SignalHandler struct {
	source string
	q      queue.JobQueue
}

// NewSignalHandler tạo handler cho nguồn source (/hooks/<source>).
func NewSignalHandler(source string) *SignalHandler {
	return &SignalHandler{source: source, q: queue.NewQueue()}
}

// Source trả tên nguồn.
func (h *SignalHandler) Source() string { return h.source }

// Handle ghi lệnh tra trạng thái cho vận đơn của tín hiệu. Loại lạ, payload không đọc được, thiếu trường → queue.ErrPermanent. Vận đơn lạ (mã hãng không có trong
// tenant, hay của tài khoản khác) → bỏ qua, không lỗi.
func (h *SignalHandler) Handle(ctx context.Context, e webhooks.Inbound, tx pgx.Tx) error {
	if e.Type != SignalType {
		return fmt.Errorf("fulfillment: loại sự kiện %q: %w", e.Type, queue.ErrPermanent)
	}
	var s Signal
	if err := json.Unmarshal(e.Data, &s); err != nil || s.CarrierKey == "" || s.AccountKey == "" ||
		s.TrackingCode == "" || s.EventID == "" {
		return fmt.Errorf("fulfillment: tín hiệu hãng thiếu trường: %w", queue.ErrPermanent)
	}
	id, err := store.New(tx).ShipmentByTracking(ctx, store.ShipmentByTrackingParams{
		CarrierKey: s.CarrierKey, TrackingCode: pgtype.Text{String: s.TrackingCode, Valid: true}, AccountRef: s.AccountKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("tìm vận đơn của tín hiệu: %w", err)
	}
	return enqueueRefresh(ctx, tx, h.q, e.TenantID, id, "evt:"+s.EventID)
}
