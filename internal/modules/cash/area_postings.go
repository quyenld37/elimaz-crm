package cash

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng postings (kế hoạch M3, Task M3-29): ghi sổ COD theo sự kiện vận đơn.
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

func (*Module) postingsHandlers() []commands.Handler       { return nil }
func (*Module) postingsInbound() []webhooks.InboundHandler { return nil }
func (*Module) postingsConsumers() []events.Consumer       { return nil }
func (*Module) postingsSchedules() []schedules.Schedule    { return nil }
func (*Module) postingsCleanups() []schedules.CleanupFunc  { return nil }
