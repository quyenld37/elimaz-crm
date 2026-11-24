package fulfillment

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng callbacks (kế hoạch M3, Task M3-19): danh sách đơn giao thất bại cần gọi lại (callbacks.go) và lệnh giao lại ở
// hãng (redeliver_command.go, chỉ khi có adapter hãng).

func (m *Module) callbacksHandlers() []commands.Handler {
	if m.d.Carriers == nil || len(m.d.Carriers.Keys()) == 0 {
		return nil
	}
	return []commands.Handler{NewRedeliverCommand(m.Service)}
}

func (*Module) callbacksInbound() []webhooks.InboundHandler { return nil }
func (*Module) callbacksSources() []webhooks.Source         { return nil }
func (*Module) callbacksConsumers() []events.Consumer       { return nil }
func (*Module) callbacksSchedules() []schedules.Schedule    { return nil }
func (*Module) callbacksCleanups() []schedules.CleanupFunc  { return nil }
