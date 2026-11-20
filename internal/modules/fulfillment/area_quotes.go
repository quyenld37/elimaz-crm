package fulfillment

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng quotes (kế hoạch M3, Task M3-13): so phí các hãng trên đơn (lệnh báo phí, dọn báo phí quá 7 ngày).
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

// quotesHandlers: lệnh báo phí command.shipping.quote (quote_command.go) khi có database.
func (m *Module) quotesHandlers() []commands.Handler {
	if m.d.DB == nil {
		return nil
	}
	return []commands.Handler{NewQuoteCommand(m.Service)}
}

func (*Module) quotesInbound() []webhooks.InboundHandler { return nil }
func (*Module) quotesSources() []webhooks.Source         { return nil }
func (*Module) quotesConsumers() []events.Consumer       { return nil }
func (*Module) quotesSchedules() []schedules.Schedule    { return nil }

// quotesCleanups: dọn báo phí quá 7 ngày (trừ báo phí vận đơn đã dùng).
func (*Module) quotesCleanups() []schedules.CleanupFunc {
	return []schedules.CleanupFunc{cleanupQuotes}
}
