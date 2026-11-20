package fulfillment

import (
	"elimaz.com.vn/crm/internal/platform/background"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Phần chạy nền của fulfillment (kế hoạch M3, Task M3-04): mỗi hàm gom kết quả của các vùng (area_*.go) cùng phần
// của M1. cmd/crm/wire_m3.go đăng ký: lệnh, handler vào, consumer, lịch, dọn vào worker; nguồn webhook vào ingest và
// worker. Check (kinds.go) kiểm không trùng tên. Thêm vùng mới thì thêm một dòng mỗi hàm ở đây.

// CommandHandlers trả lệnh ra ngoài: hai lệnh của M1 (fulfillment.create_shipment, fulfillment.refresh_status) khi
// Carriers có adapter, cùng lệnh của các vùng.
func (m *Module) CommandHandlers() []commands.Handler {
	var out []commands.Handler
	if m.d.Carriers != nil && len(m.d.Carriers.Keys()) > 0 {
		o := CommandOptions{DB: m.d.DB, Events: m.d.Events, Queue: m.d.Queue, Realtime: m.d.Bus}
		for _, k := range m.d.Carriers.Keys() {
			a, _ := m.d.Carriers.Get(k)
			o.Adapters = append(o.Adapters, a)
		}
		out = append(out, NewCreateCommand(o), NewRefreshCommand(o))
	}
	return background.Collect(out,
		m.accountsHandlers(),
		m.inboundHandlers(),
		m.quotesHandlers(),
		m.createHandlers(),
		m.cancelHandlers(),
		m.trackHandlers(),
		m.manualHandlers(),
		m.labelsHandlers(),
		m.addressHandlers(),
		m.locationsHandlers(),
		m.callbacksHandlers(),
		m.listsHandlers(),
		m.partialHandlers(),
		m.correctionsHandlers(),
		m.returnsHandlers(),
	)
}

// InboundHandlers trả handler sự kiện vào của các vùng (handler tín hiệu hãng giả của M1 do cmd/crm nối cùng nguồn).
func (m *Module) InboundHandlers() []webhooks.InboundHandler {
	return background.Collect(
		m.accountsInbound(),
		m.inboundInbound(),
		m.quotesInbound(),
		m.createInbound(),
		m.cancelInbound(),
		m.trackInbound(),
		m.manualInbound(),
		m.labelsInbound(),
		m.addressInbound(),
		m.locationsInbound(),
		m.callbacksInbound(),
		m.listsInbound(),
		m.partialInbound(),
		m.correctionsInbound(),
		m.returnsInbound(),
	)
}

// WebhookSources trả nguồn webhook của các vùng (nguồn của hãng giả M1 do cmd/crm nối).
func (m *Module) WebhookSources() []webhooks.Source {
	return background.Collect(
		m.accountsSources(),
		m.inboundSources(),
		m.quotesSources(),
		m.createSources(),
		m.cancelSources(),
		m.trackSources(),
		m.manualSources(),
		m.labelsSources(),
		m.addressSources(),
		m.locationsSources(),
		m.callbacksSources(),
		m.listsSources(),
		m.partialSources(),
		m.correctionsSources(),
		m.returnsSources(),
	)
}

// Consumers trả consumer sự kiện nội bộ của các vùng.
func (m *Module) Consumers() []events.Consumer {
	return background.Collect(
		m.accountsConsumers(),
		m.inboundConsumers(),
		m.quotesConsumers(),
		m.createConsumers(),
		m.cancelConsumers(),
		m.trackConsumers(),
		m.manualConsumers(),
		m.labelsConsumers(),
		m.addressConsumers(),
		m.locationsConsumers(),
		m.callbacksConsumers(),
		m.listsConsumers(),
		m.partialConsumers(),
		m.correctionsConsumers(),
		m.returnsConsumers(),
	)
}

// Schedules trả lịch định kỳ của các vùng (Kind có handler trong CommandHandlers hay việc nền của worker).
func (m *Module) Schedules() []schedules.Schedule {
	return background.Collect(
		m.accountsSchedules(),
		m.inboundSchedules(),
		m.quotesSchedules(),
		m.createSchedules(),
		m.cancelSchedules(),
		m.trackSchedules(),
		m.manualSchedules(),
		m.labelsSchedules(),
		m.addressSchedules(),
		m.locationsSchedules(),
		m.callbacksSchedules(),
		m.listsSchedules(),
		m.partialSchedules(),
		m.correctionsSchedules(),
		m.returnsSchedules(),
	)
}

// Cleanups trả lệnh dọn theo tenant của các vùng (báo phí quá 7 ngày, lô tem quá 30 ngày).
func (m *Module) Cleanups() []schedules.CleanupFunc {
	return background.Collect(
		m.accountsCleanups(),
		m.inboundCleanups(),
		m.quotesCleanups(),
		m.createCleanups(),
		m.cancelCleanups(),
		m.trackCleanups(),
		m.manualCleanups(),
		m.labelsCleanups(),
		m.addressCleanups(),
		m.locationsCleanups(),
		m.callbacksCleanups(),
		m.listsCleanups(),
		m.partialCleanups(),
		m.correctionsCleanups(),
		m.returnsCleanups(),
	)
}
