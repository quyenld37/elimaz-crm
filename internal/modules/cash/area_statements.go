package cash

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng statements (kế hoạch M3, Task M3-31, M3-32): nhập bảng kê COD, tự khớp và chốt, "Lệch đối soát", "Quá hạn chưa có tiền".
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

func (*Module) statementsHandlers() []commands.Handler       { return nil }
func (*Module) statementsInbound() []webhooks.InboundHandler { return nil }
func (*Module) statementsConsumers() []events.Consumer       { return nil }
func (*Module) statementsSchedules() []schedules.Schedule    { return nil }
func (*Module) statementsCleanups() []schedules.CleanupFunc  { return nil }
