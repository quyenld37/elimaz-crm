// Package content là module Nội dung website (kế hoạch M5, sắp xếp module 21/11): banner trang chủ (Task 453), bộ
// phối (Task 454). Từ M5 CRM là nguồn sự thật của nội dung trang chủ; website đọc qua API khóa dịch vụ.
//
// Module chỉ đọc view công bố của catalog, promotion, platform (docs/views.md); không nhập Go của order. Ảnh banner là
// tệp của kho tệp CRM (platform/files) với mục đích web_banner. Mọi hàm nhận tx của tenancy.WithTenant; đường ghi là
// phương thức của Writer, phát sự kiện và báo hiệu trong transaction của bên gọi; audit ghi ở lớp HTTP.
package content

import (
	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Quyền của module (identity.Permissions; toàn shop).
const (
	permRead = "content.read"
)

// Actor là nhân viên làm thay đổi.
type Actor struct {
	TenantID uuid.UUID
	UserID   uuid.UUID
}

// Writer là đường ghi nội dung website. Events, Queue bắt buộc (phát content.banners.changed); Realtime nil → không báo
// hiệu màn hình (báo hiệu được phép mất).
type Writer struct {
	Realtime realtime.Bus
	Events   *events.Registry
	Queue    queue.JobQueue
}
