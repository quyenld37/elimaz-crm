package order

import (
	"time"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Nhập lại bảng tính đơn dự phòng (kế hoạch M7, Task M7-23 = 268, để sau). Khung của Task 260 (M7-15): hai hàm nối
// dây đã được cmd/crm gọi; Task 268 điền thân hàm cùng commit với dòng INSERT mục đích order_fallback_import của
// migration 0710 (R5), không sửa cmd/crm.

// FallbackImportDeps là phụ thuộc của việc nền order.fallback_import (đọc tệp ngoài transaction, mỗi đơn một
// transaction).
type FallbackImportDeps struct {
	DB    *db.DB
	Files files.FileStore
	Queue queue.JobQueue
	// Now là đồng hồ của việc nền; nil → time.Now.
	Now func() time.Time
}

// RegisterFallbackPurpose đăng ký mục đích tệp order_fallback_import vào sổ của tiến trình (gọi một lần lúc khởi
// động). Khung: chưa đăng ký gì (bảng platform.file_purposes chưa có dòng, TestPurposes_GoMatchesTable giữ khớp).
func RegisterFallbackPurpose() {}

// FallbackImportJobs trả handler của việc nền order.fallback_import cho worker. Khung: chưa có việc nào.
func FallbackImportJobs(_ FallbackImportDeps) []queue.Handler { return nil }
