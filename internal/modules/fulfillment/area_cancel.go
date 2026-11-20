package fulfillment

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng cancel (kế hoạch M3, Task M3-14): hủy vận đơn, đổi hãng, hủy đơn đã gửi hãng.
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

func (*Module) cancelHandlers() []commands.Handler       { return nil }
func (*Module) cancelInbound() []webhooks.InboundHandler { return nil }
func (*Module) cancelSources() []webhooks.Source         { return nil }
func (*Module) cancelConsumers() []events.Consumer       { return nil }
func (*Module) cancelSchedules() []schedules.Schedule    { return nil }
func (*Module) cancelCleanups() []schedules.CleanupFunc  { return nil }
