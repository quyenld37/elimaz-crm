package fulfillment

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng inbound (kế hoạch M3, Task M3-10): webhook hãng vào ingest (nguồn theo hãng, tín hiệu → lệnh tra lại).
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

func (*Module) inboundHandlers() []commands.Handler       { return nil }
func (*Module) inboundInbound() []webhooks.InboundHandler { return nil }
func (*Module) inboundSources() []webhooks.Source         { return nil }
func (*Module) inboundConsumers() []events.Consumer       { return nil }
func (*Module) inboundSchedules() []schedules.Schedule    { return nil }
func (*Module) inboundCleanups() []schedules.CleanupFunc  { return nil }
