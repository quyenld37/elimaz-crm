package fulfillment

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/inbound"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng inbound (kế hoạch M3, Task M3-10): webhook hãng vào ingest. Mỗi hãng có khả năng Webhook một handler vào
// (nguồn = khóa hãng): tín hiệu chỉ ghi lệnh tra fulfillment.refresh_status. Nguồn webhook (webhooks.RoutedSource, kiểm
// chữ ký bằng http.Header) không ở module (module không nhập net/http): cmd/crm nối carrier.WebhookSources, tách gói
// bằng SplitSignals.

func (*Module) inboundHandlers() []commands.Handler { return nil }

// inboundInbound trả handler tín hiệu của mọi hãng trong Carriers khai Capabilities().Webhook.
func (m *Module) inboundInbound() []webhooks.InboundHandler {
	if m.d.Carriers == nil {
		return nil
	}
	var out []webhooks.InboundHandler
	for _, k := range m.d.Carriers.Keys() {
		a, _ := m.d.Carriers.Get(k)
		if !a.Capabilities().Webhook {
			continue
		}
		out = append(out, inbound.New(inbound.Options{Source: k, Enqueue: m.enqueueSignalRefresh, Metrics: m.d.Metrics}))
	}
	return out
}

func (*Module) inboundSources() []webhooks.Source        { return nil }
func (*Module) inboundConsumers() []events.Consumer      { return nil }
func (*Module) inboundSchedules() []schedules.Schedule   { return nil }
func (*Module) inboundCleanups() []schedules.CleanupFunc { return nil }

// signalPayload là payload lệnh tra do tín hiệu webhook ghi (RefreshCommand chỉ đọc shipmentId).
type signalPayload struct {
	ShipmentID uuid.UUID `json:"shipmentId"`
	Reason     string    `json:"reason"`
}

// enqueueSignalRefresh ghi lệnh tra vận đơn id trong tx của việc vào (khóa dedupe, thứ tự shipment:<id> như M1).
func (m *Module) enqueueSignalRefresh(ctx context.Context, tx pgx.Tx, tenant, id uuid.UUID, dedupe string) error {
	if _, _, err := m.Service.queue().Enqueue(ctx, tx, RefreshCommandKind(), queue.NewJob{
		TenantID: &tenant, Payload: signalPayload{ShipmentID: id, Reason: "webhook"}, DedupeKey: dedupe,
		OrderKey: shipmentOrderKey(id),
	}); err != nil {
		return fmt.Errorf("ghi lệnh tra trạng thái theo tín hiệu hãng: %w", err)
	}
	return nil
}
