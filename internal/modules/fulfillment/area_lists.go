package fulfillment

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng lists (kế hoạch M3, Task M3-20): đơn kẹt, "Cần xem", cập nhật lần cuối.
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

func (*Module) listsHandlers() []commands.Handler       { return nil }
func (*Module) listsInbound() []webhooks.InboundHandler { return nil }
func (*Module) listsSources() []webhooks.Source         { return nil }
func (*Module) listsConsumers() []events.Consumer       { return nil }
func (*Module) listsSchedules() []schedules.Schedule    { return nil }
func (*Module) listsCleanups() []schedules.CleanupFunc  { return nil }
