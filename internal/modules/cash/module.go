package cash

import (
	"time"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Deps là phụ thuộc của module tiền (kế hoạch M3, Task M3-04), cmd/crm (wire_m3.go) dựng một lần mỗi tiến trình.
// Trường nil: phần cần nó chưa được nối ở vai trò này.
type Deps struct {
	DB *db.DB
	// Queue ghi việc nền, lệnh trong transaction của bên gọi.
	Queue queue.JobQueue
	// Bus phát báo hiệu realtime (nil: bỏ qua).
	Bus realtime.Bus
	// Events là sổ sự kiện nội bộ.
	Events *events.Registry
	// Carriers là bộ adapter hãng: lấy định dạng bảng kê (Registry.StatementFormats).
	Carriers *fulfillment.Registry
	// Files là kho tệp (đọc tệp bảng kê).
	Files   files.FileStore
	Alerter observability.Alerter
	Metrics *metrics.Registry
	// Now là đồng hồ (nil: time.Now).
	Now func() time.Time
}

// Service là public API ghi, đọc của M3 cho lớp HTTP: task của từng vùng thêm phương thức trong tệp của vùng, không
// sửa tệp này. Hàm cấp gói của M1 (sổ COD, Consumer) giữ nguyên.
type Service struct {
	d Deps
}

// Module là phần nối của cash: Service cho lớp HTTP và các phần chạy nền (wire.go) gom từ các vùng.
type Module struct {
	d       Deps
	Service *Service
}

// New dựng Module từ d (Now nil → time.Now); Service dùng chung d.
func New(d Deps) *Module {
	m := &Module{d: d}
	if m.d.Now == nil {
		m.d.Now = time.Now
	}
	m.Service = &Service{d: m.d}
	return m
}
