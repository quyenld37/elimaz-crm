// Package background giữ kiểu chung của phần chạy nền (việc nền, consumer sự kiện, lịch) mà mỗi module nghiệp vụ
// khai trong module.go (kế hoạch M2, Task M2-02): phụ thuộc Deps và kết quả Parts. cmd/crm dựng Deps một lần cho mỗi
// tiến trình, gọi Background của từng module đúng một lần rồi đăng ký Parts.
//
// Khi nào không dùng: phụ thuộc của đường yêu cầu HTTP (httpapi.Deps); phụ thuộc riêng của một module (thêm trường
// vào struct riêng của module đó, không thêm vào đây).
package background

import (
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// Deps là phụ thuộc của phần chạy nền của một module.
type Deps struct {
	DB *db.DB
	// Queue ghi việc nền trong transaction của bên gọi.
	Queue queue.JobQueue
	// Bus phát báo hiệu realtime (nil: bỏ qua, báo hiệu được phép mất).
	Bus realtime.Bus
	// Events là sổ sự kiện nội bộ của tiến trình (Emit trong transaction).
	Events *events.Registry
}

// Parts là phần chạy nền một module khai: việc nền (worker đăng ký), consumer sự kiện nội bộ (cmd/crm đăng ký vào
// events.Registry), lịch định kỳ (Kind có handler trong JobHandlers).
type Parts struct {
	JobHandlers []queue.Handler
	Consumers   []events.Consumer
	Schedules   []schedules.Schedule
}
