package fulfillment

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng locations (kế hoạch M3, Task M3-18): đối chiếu địa giới theo hãng.
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

func (*Module) locationsHandlers() []commands.Handler       { return nil }
func (*Module) locationsInbound() []webhooks.InboundHandler { return nil }
func (*Module) locationsSources() []webhooks.Source         { return nil }
func (*Module) locationsConsumers() []events.Consumer       { return nil }
func (*Module) locationsSchedules() []schedules.Schedule    { return nil }
func (*Module) locationsCleanups() []schedules.CleanupFunc  { return nil }
