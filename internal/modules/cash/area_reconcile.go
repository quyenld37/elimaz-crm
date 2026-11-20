package cash

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng reconcile (kế hoạch M3, Task M3-33): đối chiếu sổ COD hằng đêm.
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

func (*Module) reconcileHandlers() []commands.Handler       { return nil }
func (*Module) reconcileInbound() []webhooks.InboundHandler { return nil }
func (*Module) reconcileConsumers() []events.Consumer       { return nil }
func (*Module) reconcileSchedules() []schedules.Schedule    { return nil }
func (*Module) reconcileCleanups() []schedules.CleanupFunc  { return nil }
