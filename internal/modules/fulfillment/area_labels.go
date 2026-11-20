package fulfillment

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng labels (kế hoạch M3, Task M3-16): in tem theo lô, phiếu nhặt hàng, đóng gói (dọn lô tem quá 30 ngày).
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

func (*Module) labelsHandlers() []commands.Handler       { return nil }
func (*Module) labelsInbound() []webhooks.InboundHandler { return nil }
func (*Module) labelsSources() []webhooks.Source         { return nil }
func (*Module) labelsConsumers() []events.Consumer       { return nil }
func (*Module) labelsSchedules() []schedules.Schedule    { return nil }
func (*Module) labelsCleanups() []schedules.CleanupFunc  { return nil }
