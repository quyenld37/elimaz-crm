package fulfillment

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng corrections (kế hoạch M3, Task M3-27): đảo "Thành công": hãng sửa lại, nhân viên sửa nhầm. Phương thức của
// Service ở correction.go (PreviewCorrection, ApplyCorrection). Vùng không có việc nền riêng: lệnh cảnh báo
// carrier-corrected (NewCorrectedAlertCommand) do cmd/crm nối cùng Alerter của các lệnh cảnh báo khác.

func (*Module) correctionsHandlers() []commands.Handler       { return nil }
func (*Module) correctionsInbound() []webhooks.InboundHandler { return nil }
func (*Module) correctionsSources() []webhooks.Source         { return nil }
func (*Module) correctionsConsumers() []events.Consumer       { return nil }
func (*Module) correctionsSchedules() []schedules.Schedule    { return nil }
func (*Module) correctionsCleanups() []schedules.CleanupFunc  { return nil }
