package cash

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng adjustments (kế hoạch M3, Task M3-33): điều chỉnh COD có xem trước.
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

func (*Module) adjustmentsHandlers() []commands.Handler       { return nil }
func (*Module) adjustmentsInbound() []webhooks.InboundHandler { return nil }
func (*Module) adjustmentsConsumers() []events.Consumer       { return nil }
func (*Module) adjustmentsSchedules() []schedules.Schedule    { return nil }
func (*Module) adjustmentsCleanups() []schedules.CleanupFunc  { return nil }
