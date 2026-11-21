// Package background giữ kiểu chung của phần chạy nền (việc nền, consumer sự kiện, lịch) mà mỗi module nghiệp vụ
// khai trong module.go (kế hoạch M2, Task M2-02): phụ thuộc Deps và kết quả Parts. cmd/crm dựng Deps một lần cho mỗi
// tiến trình, gọi Background của từng module đúng một lần rồi đăng ký Parts.
//
// Khi nào không dùng: phụ thuộc của đường yêu cầu HTTP (httpapi.Deps); phụ thuộc riêng của một module (thêm trường
// vào struct riêng của module đó, không thêm vào đây).
package background

import (
	"errors"
	"fmt"
	"time"

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
	// Now là đồng hồ của phần chạy nền (module không gọi time.Now, kế hoạch M2 "Đồng hồ"); nil → time.Now. Đọc qua
	// Clock.
	Now func() time.Time
}

// Clock trả đồng hồ của d: d.Now, hoặc time.Now khi nil.
func (d Deps) Clock() func() time.Time {
	if d.Now != nil {
		return d.Now
	}
	return time.Now
}

// Parts là phần chạy nền một module khai: việc nền (worker đăng ký), consumer sự kiện nội bộ (cmd/crm đăng ký vào
// events.Registry), lịch định kỳ (Kind có handler trong JobHandlers).
type Parts struct {
	JobHandlers []queue.Handler
	Consumers   []events.Consumer
	Schedules   []schedules.Schedule
}

// ErrDuplicatePart báo hai phần chạy nền (lệnh, handler vào, nguồn webhook, consumer, lịch) của module cùng tên.
var ErrDuplicatePart = errors.New("background: hai phần chạy nền trùng tên")

// Collect nối mọi lát trong parts theo thứ tự (khung nối theo vùng của M3: mỗi vùng trả một lát, có thể nil).
func Collect[T any](parts ...[]T) []T {
	var out []T
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// UniqueNames trả lỗi bọc ErrDuplicatePart nêu loại what và tên đầu tiên bị trùng trong items (mỗi phần đăng ký một
// lần); nil khi không trùng.
func UniqueNames[T any](what string, items []T, name func(T) string) error {
	seen := make(map[string]bool, len(items))
	for _, it := range items {
		n := name(it)
		if seen[n] {
			return fmt.Errorf("%s %q: %w", what, n, ErrDuplicatePart)
		}
		seen[n] = true
	}
	return nil
}
