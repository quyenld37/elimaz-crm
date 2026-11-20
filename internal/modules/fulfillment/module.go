package fulfillment

import (
	"time"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Deps là phụ thuộc của module vận chuyển (kế hoạch M3, Task M3-04), cmd/crm (wire_m3.go) dựng một lần mỗi tiến trình.
// Trường nil: phần cần nó chưa được nối ở vai trò này (task của vùng kiểm khi dùng).
type Deps struct {
	DB *db.DB
	// Queue ghi việc nền, lệnh trong transaction của bên gọi.
	Queue queue.JobQueue
	// Bus phát báo hiệu realtime (nil: bỏ qua, báo hiệu được phép mất).
	Bus realtime.Bus
	// Events là sổ sự kiện nội bộ (Emit trong transaction; đường ghi của order cần).
	Events *events.Registry
	// Carriers là bộ adapter hãng (internal/adapters/carrier.All). Rỗng: không lệnh vận chuyển nào được đăng ký.
	Carriers *Registry
	// Keys giải, mã khóa API hãng (fieldcrypt); chỉ dùng trong Execute của lệnh.
	Keys *fieldcrypt.Keyring
	// Files là kho tệp (nhãn hãng).
	Files   files.FileStore
	Alerter observability.Alerter
	Metrics *metrics.Registry
	// Now là đồng hồ (nil: time.Now).
	Now func() time.Time
}

// Service là public API ghi, đọc của M3 cho lớp HTTP: task của từng vùng thêm phương thức trong tệp area_<vùng>.go (hay
// tệp riêng của vùng), không sửa tệp này. Hàm cấp gói của M1 (CreateForOrder, Get, …) giữ nguyên.
type Service struct {
	d Deps
}

// Module là phần nối của fulfillment: Service cho lớp HTTP và các phần chạy nền (wire.go) gom từ các vùng.
type Module struct {
	d       Deps
	Service *Service
}

// New dựng Module từ d (Now nil → time.Now).
func New(d Deps) *Module {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Module{d: d, Service: &Service{d: d}}
}
