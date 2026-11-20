package fulfillment

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng callbacks (kế hoạch M3, Task M3-19): danh sách đơn giao thất bại cần gọi lại.
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

func (*Module) callbacksHandlers() []commands.Handler       { return nil }
func (*Module) callbacksInbound() []webhooks.InboundHandler { return nil }
func (*Module) callbacksSources() []webhooks.Source         { return nil }
func (*Module) callbacksConsumers() []events.Consumer       { return nil }
func (*Module) callbacksSchedules() []schedules.Schedule    { return nil }
func (*Module) callbacksCleanups() []schedules.CleanupFunc  { return nil }
