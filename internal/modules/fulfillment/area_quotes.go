package fulfillment

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng quotes (kế hoạch M3, Task M3-13): so phí các hãng trên đơn (lệnh báo phí, dọn báo phí quá 7 ngày).
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

func (*Module) quotesHandlers() []commands.Handler       { return nil }
func (*Module) quotesInbound() []webhooks.InboundHandler { return nil }
func (*Module) quotesSources() []webhooks.Source         { return nil }
func (*Module) quotesConsumers() []events.Consumer       { return nil }
func (*Module) quotesSchedules() []schedules.Schedule    { return nil }
func (*Module) quotesCleanups() []schedules.CleanupFunc  { return nil }
