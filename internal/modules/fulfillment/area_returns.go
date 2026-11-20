package fulfillment

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng returns (kế hoạch M3, Task M3-23): nhận hàng hoàn của hãng về kho theo số lượng tốt, hỏng, thiếu. Kho nhận qua
// receiveReturn của order (M2); vùng này chỉ có consumer đưa vận đơn sang returned (return_received.go).

func (*Module) returnsHandlers() []commands.Handler       { return nil }
func (*Module) returnsInbound() []webhooks.InboundHandler { return nil }
func (*Module) returnsSources() []webhooks.Source         { return nil }
func (m *Module) returnsConsumers() []events.Consumer {
	return []events.Consumer{ReturnReceivedConsumer{s: m.Service}}
}
func (*Module) returnsSchedules() []schedules.Schedule   { return nil }
func (*Module) returnsCleanups() []schedules.CleanupFunc { return nil }
