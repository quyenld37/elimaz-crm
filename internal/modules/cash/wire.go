package cash

import (
	"errors"

	"elimaz.com.vn/crm/internal/platform/background"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Phần chạy nền của cash (kế hoạch M3, Task M3-04): mỗi hàm gom kết quả của các vùng (area_*.go) cùng phần của M1.
// cmd/crm/wire_m3.go đăng ký vào worker (consumer cả vào api: Emit cần sổ consumer). Thêm vùng mới thì thêm một
// dòng mỗi hàm ở đây.

// CommandHandlers trả lệnh ra ngoài của các vùng (đọc tệp bảng kê từ kho tệp ngoài transaction).
func (m *Module) CommandHandlers() []commands.Handler {
	return background.Collect(
		m.postingsHandlers(),
		m.statementsHandlers(),
		m.paymentsHandlers(),
		m.adjustmentsHandlers(),
		m.reconcileHandlers(),
	)
}

// InboundHandlers trả handler sự kiện vào của các vùng.
func (m *Module) InboundHandlers() []webhooks.InboundHandler {
	return background.Collect(
		m.postingsInbound(),
		m.statementsInbound(),
		m.paymentsInbound(),
		m.adjustmentsInbound(),
		m.reconcileInbound(),
	)
}

// Consumers trả consumer sổ COD của M1 (Consumer, shipment.status_changed) cùng consumer của các vùng.
func (m *Module) Consumers() []events.Consumer {
	return background.Collect([]events.Consumer{Consumer{}},
		m.postingsConsumers(),
		m.statementsConsumers(),
		m.paymentsConsumers(),
		m.adjustmentsConsumers(),
		m.reconcileConsumers(),
	)
}

// Schedules trả lịch định kỳ của các vùng (đối chiếu sổ hằng đêm).
func (m *Module) Schedules() []schedules.Schedule {
	return background.Collect(
		m.postingsSchedules(),
		m.statementsSchedules(),
		m.paymentsSchedules(),
		m.adjustmentsSchedules(),
		m.reconcileSchedules(),
	)
}

// Cleanups trả lệnh dọn theo tenant của các vùng (sổ, bảng kê không bao giờ xóa).
func (m *Module) Cleanups() []schedules.CleanupFunc {
	return background.Collect(
		m.postingsCleanups(),
		m.statementsCleanups(),
		m.paymentsCleanups(),
		m.adjustmentsCleanups(),
		m.reconcileCleanups(),
	)
}

// Check trả lỗi bọc background.ErrDuplicatePart khi hai lệnh, handler vào, consumer hay lịch của module trùng tên.
func (m *Module) Check() error {
	return errors.Join(
		background.UniqueNames("lệnh", m.CommandHandlers(), func(h commands.Handler) string { return h.Kind().Name }),
		background.UniqueNames("handler vào", m.InboundHandlers(), func(h webhooks.InboundHandler) string { return h.Source() }),
		background.UniqueNames("consumer", m.Consumers(), func(c events.Consumer) string { return c.Name() }),
		background.UniqueNames("lịch", m.Schedules(), func(s schedules.Schedule) string { return s.Name }),
	)
}
