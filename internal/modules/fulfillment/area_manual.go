package fulfillment

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng manual (kế hoạch M3, Task M3-15): nhập mã tay và hãng "tự giao".
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

func (*Module) manualHandlers() []commands.Handler       { return nil }
func (*Module) manualInbound() []webhooks.InboundHandler { return nil }
func (*Module) manualSources() []webhooks.Source         { return nil }
func (*Module) manualConsumers() []events.Consumer       { return nil }
func (*Module) manualSchedules() []schedules.Schedule    { return nil }
func (*Module) manualCleanups() []schedules.CleanupFunc  { return nil }
