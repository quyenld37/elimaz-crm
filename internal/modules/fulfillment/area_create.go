package fulfillment

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng create (kế hoạch M3, Task M3-12): gửi hãng: lệnh tạo vận đơn, "không rõ", gửi lại.
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

func (*Module) createHandlers() []commands.Handler       { return nil }
func (*Module) createInbound() []webhooks.InboundHandler { return nil }
func (*Module) createSources() []webhooks.Source         { return nil }
func (*Module) createConsumers() []events.Consumer       { return nil }
func (*Module) createSchedules() []schedules.Schedule    { return nil }
func (*Module) createCleanups() []schedules.CleanupFunc  { return nil }
