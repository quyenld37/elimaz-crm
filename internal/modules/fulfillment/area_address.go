package fulfillment

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng address (kế hoạch M3, Task M3-17): đổi địa chỉ sau khi đã gửi hãng.
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

func (*Module) addressHandlers() []commands.Handler       { return nil }
func (*Module) addressInbound() []webhooks.InboundHandler { return nil }
func (*Module) addressSources() []webhooks.Source         { return nil }
func (*Module) addressConsumers() []events.Consumer       { return nil }
func (*Module) addressSchedules() []schedules.Schedule    { return nil }
func (*Module) addressCleanups() []schedules.CleanupFunc  { return nil }
