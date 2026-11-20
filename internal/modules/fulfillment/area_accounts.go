package fulfillment

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng accounts (kế hoạch M3, Task M3-09): tài khoản hãng, khóa API mã hóa, kiểm kết nối, địa chỉ lấy hàng của kho.
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

// accountsHandlers: lệnh kiểm kết nối carrier.check (accounts_check.go) khi có database. Lệnh cảnh báo carrier-auth
// (NewAuthAlertCommand) do cmd/crm nối cùng Alerter của các lệnh cảnh báo khác.
func (m *Module) accountsHandlers() []commands.Handler {
	if m.d.DB == nil {
		return nil
	}
	return []commands.Handler{checkCommand{s: m.Service}}
}

func (*Module) accountsInbound() []webhooks.InboundHandler { return nil }
func (*Module) accountsSources() []webhooks.Source         { return nil }
func (*Module) accountsConsumers() []events.Consumer       { return nil }
func (*Module) accountsSchedules() []schedules.Schedule    { return nil }
func (*Module) accountsCleanups() []schedules.CleanupFunc  { return nil }
