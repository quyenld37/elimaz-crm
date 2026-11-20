package fulfillment

import (
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng track (kế hoạch M3, Task M3-11): lệnh tra trạng thái và tra bù theo lịch.
// Khung của Task M3-04: mọi hàm trả nil; task của vùng điền các hàm dưới (và thêm
// phương thức của Service trong tệp của vùng), không sửa module.go, wire.go.

// trackHandlers: lệnh tra fulfillment.refresh_status (refresh_status.go) khi Carriers có adapter; lệnh của lịch tra
// bù fulfillment.poll (track_poll.go) khi có database. Lệnh cảnh báo đồng bộ (NewTrackAlertCommand) do cmd/crm nối
// cùng Alerter của các lệnh cảnh báo khác.
func (m *Module) trackHandlers() []commands.Handler {
	var out []commands.Handler
	if m.d.Carriers != nil && len(m.d.Carriers.Keys()) > 0 {
		out = append(out, NewRefreshCommand(m.Service))
	}
	if m.d.DB != nil {
		out = append(out, pollCommand{s: m.Service})
	}
	return out
}

func (*Module) trackInbound() []webhooks.InboundHandler { return nil }
func (*Module) trackSources() []webhooks.Source         { return nil }
func (*Module) trackConsumers() []events.Consumer       { return nil }

// trackSchedules: lịch tra bù fulfillment.poll (600 s, theo tenant) khi có database (cùng điều kiện với lệnh của nó).
func (m *Module) trackSchedules() []schedules.Schedule {
	if m.d.DB == nil {
		return nil
	}
	return []schedules.Schedule{pollSchedule()}
}

func (*Module) trackCleanups() []schedules.CleanupFunc { return nil }
