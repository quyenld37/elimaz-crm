package fulfillment

import (
	"errors"

	"elimaz.com.vn/crm/internal/platform/background"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Check trả lỗi bọc background.ErrDuplicatePart khi hai lệnh (theo Kind.Name), handler vào (theo nguồn), nguồn
// webhook, consumer hay lịch của module trùng tên: mỗi phần đăng ký một lần (kế hoạch M3, Task M3-04). cmd/crm gọi lúc
// khởi động; task của vùng thêm lệnh mới mà trùng tên thì tiến trình không chạy.
func (m *Module) Check() error {
	return errors.Join(
		background.UniqueNames("lệnh", m.CommandHandlers(), func(h commands.Handler) string { return h.Kind().Name }),
		background.UniqueNames("handler vào", m.InboundHandlers(), func(h webhooks.InboundHandler) string { return h.Source() }),
		background.UniqueNames("nguồn webhook", m.WebhookSources(), func(s webhooks.Source) string { return s.Key() }),
		background.UniqueNames("consumer", m.Consumers(), func(c events.Consumer) string { return c.Name() }),
		background.UniqueNames("lịch", m.Schedules(), func(s schedules.Schedule) string { return s.Name }),
	)
}
