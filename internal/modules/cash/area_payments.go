package cash

import (
	"elimaz.com.vn/crm/internal/modules/cash/internal/refunds"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng payments (kế hoạch M3, Task M3-30): phiếu thu, cọc (payments.go), nghĩa vụ hoàn (refunds.go).
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

func (*Module) paymentsHandlers() []commands.Handler       { return nil }
func (*Module) paymentsInbound() []webhooks.InboundHandler { return nil }

// paymentsConsumers: consumer cash_refunds (mở nghĩa vụ hoàn theo sự kiện đơn, cấn trừ đổi hàng qua order.Writer).
func (m *Module) paymentsConsumers() []events.Consumer {
	return []events.Consumer{refunds.Consumer{Orders: m.Service.orderWriter(), Now: m.d.Now}}
}
func (*Module) paymentsSchedules() []schedules.Schedule   { return nil }
func (*Module) paymentsCleanups() []schedules.CleanupFunc { return nil }
