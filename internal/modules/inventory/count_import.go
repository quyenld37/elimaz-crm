package inventory

import (
	"time"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Nhập số đếm kiểm kho bằng Excel (kế hoạch M7, Task M7-18 = 263). Khung của Task 260 (M7-15): hai hàm nối dây đã
// được cmd/crm gọi; Task 263 điền thân hàm cùng commit với dòng INSERT mục đích inventory_count_import của migration
// 0705 (R5), không sửa cmd/crm.

// CountImportDeps là phụ thuộc của việc nền inventory.count_import (đọc tệp ngoài transaction, ghi dòng đếm).
type CountImportDeps struct {
	DB    *db.DB
	Files files.FileStore
	Queue queue.JobQueue
	// Now là đồng hồ của việc nền; nil → time.Now.
	Now func() time.Time
}

// RegisterCountImportPurpose đăng ký mục đích tệp inventory_count_import vào sổ của tiến trình (gọi một lần lúc khởi
// động). Khung: chưa đăng ký gì (bảng platform.file_purposes chưa có dòng, TestPurposes_GoMatchesTable giữ khớp).
func RegisterCountImportPurpose() {}

// CountImportJobs trả handler của việc nền inventory.count_import cho worker. Khung: chưa có việc nào.
func CountImportJobs(_ CountImportDeps) []queue.Handler { return nil }
