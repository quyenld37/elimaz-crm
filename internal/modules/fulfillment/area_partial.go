package fulfillment

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng partial (kế hoạch M3, Task M3-26): giao một phần.
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

func (*Module) partialHandlers() []commands.Handler       { return nil }
func (*Module) partialInbound() []webhooks.InboundHandler { return nil }
func (*Module) partialSources() []webhooks.Source         { return nil }
func (*Module) partialConsumers() []events.Consumer       { return nil }
func (*Module) partialSchedules() []schedules.Schedule    { return nil }
func (*Module) partialCleanups() []schedules.CleanupFunc  { return nil }
