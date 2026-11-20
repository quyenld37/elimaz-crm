package fulfillment

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng track (kế hoạch M3, Task M3-11): lệnh tra trạng thái và tra bù theo lịch.
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

func (*Module) trackHandlers() []commands.Handler       { return nil }
func (*Module) trackInbound() []webhooks.InboundHandler { return nil }
func (*Module) trackSources() []webhooks.Source         { return nil }
func (*Module) trackConsumers() []events.Consumer       { return nil }
func (*Module) trackSchedules() []schedules.Schedule    { return nil }
func (*Module) trackCleanups() []schedules.CleanupFunc  { return nil }
