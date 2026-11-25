package channel

import (
	"slices"
	"time"

	"elimaz.com.vn/crm/internal/platform/background"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// Deps là phụ thuộc phần chạy nền của module channel, dựng ở cmd/crm (wire_m4.go).
type Deps struct {
	DB       *db.DB
	Queue    queue.JobQueue
	Bus      realtime.Bus
	Registry *Registry
	// Keys giải mã token Page (nil khi kênh Facebook chưa bật).
	Keys    *fieldcrypt.Keyring
	Alerter observability.Alerter
	Metrics *metrics.Registry
	// Now là đồng hồ (nil → time.Now).
	Now    func() time.Time
	Config config.Config
}

// parts là phần chạy nền một vùng của channel khai (mỗi vùng một tệp khung, task của vùng chỉ sửa tệp đó).
type parts struct {
	Commands  []commands.Handler
	Schedules []schedules.Schedule
}

// Các vùng của channel: đăng ký webhook khi kết nối (subscribe.go, Task 149), sức khỏe kênh (health.go, Task 150), xóa
// dữ liệu theo yêu cầu của Meta (deletion.go, Task 179).

// CommandHandlers là lệnh ra ngoài của channel (command.fb.*), bỏ phần tử nil.
func CommandHandlers(d Deps) []commands.Handler {
	s, h, x := subscribeParts(d), healthParts(d), deletionParts(d)
	all := background.Collect(s.Commands, h.Commands, x.Commands)
	return slices.DeleteFunc(all, func(c commands.Handler) bool { return c == nil })
}

// Schedules là lịch định kỳ của channel.
func Schedules(d Deps) []schedules.Schedule {
	s, h, x := subscribeParts(d), healthParts(d), deletionParts(d)
	return background.Collect(s.Schedules, h.Schedules, x.Schedules)
}
