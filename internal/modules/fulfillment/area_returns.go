package fulfillment

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng returns (kế hoạch M3, Task M3-23): nhận hàng hoàn của hãng về kho theo số lượng tốt, hỏng, thiếu.
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

func (*Module) returnsHandlers() []commands.Handler       { return nil }
func (*Module) returnsInbound() []webhooks.InboundHandler { return nil }
func (*Module) returnsSources() []webhooks.Source         { return nil }
func (*Module) returnsConsumers() []events.Consumer       { return nil }
func (*Module) returnsSchedules() []schedules.Schedule    { return nil }
func (*Module) returnsCleanups() []schedules.CleanupFunc  { return nil }
