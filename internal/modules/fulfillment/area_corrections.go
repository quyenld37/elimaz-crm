package fulfillment

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng corrections (kế hoạch M3, Task M3-27): đảo "Thành công": hãng sửa lại, nhân viên sửa nhầm.
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

func (*Module) correctionsHandlers() []commands.Handler       { return nil }
func (*Module) correctionsInbound() []webhooks.InboundHandler { return nil }
func (*Module) correctionsSources() []webhooks.Source         { return nil }
func (*Module) correctionsConsumers() []events.Consumer       { return nil }
func (*Module) correctionsSchedules() []schedules.Schedule    { return nil }
func (*Module) correctionsCleanups() []schedules.CleanupFunc  { return nil }
