package cash

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng payments (kế hoạch M3, Task M3-30): phiếu thu, cọc, nghĩa vụ hoàn.
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

func (*Module) paymentsHandlers() []commands.Handler       { return nil }
func (*Module) paymentsInbound() []webhooks.InboundHandler { return nil }
func (*Module) paymentsConsumers() []events.Consumer       { return nil }
func (*Module) paymentsSchedules() []schedules.Schedule    { return nil }
func (*Module) paymentsCleanups() []schedules.CleanupFunc  { return nil }
