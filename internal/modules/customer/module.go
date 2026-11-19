package customer

import (
	"elimaz.com.vn/crm/internal/platform/background"
	"elimaz.com.vn/crm/internal/platform/files"
)

// Background trả phần chạy nền của module customer (khách): việc nền, consumer sự kiện, lịch. cmd/crm gọi đúng một lần mỗi
// tiến trình (kế hoạch M2, Task M2-02); task M2 sau thêm phần tử vào đây. Lệnh nhập khách customer.import (Task M2-13)
// là lệnh ra ngoài (đọc kho tệp ngoài transaction), cmd/crm nối bằng NewImporter cùng các lệnh khác.
func Background(_ background.Deps) background.Parts { return background.Parts{} }

// RegisterFilePurposes đăng ký các mục đích tệp của module customer vào sổ của tiến trình (files.RegisterPurpose):
// customer_import (Task M2-13, dòng của platform.file_purposes ở migration 0208). cmd/crm gọi lúc khởi động, trước
// files.CheckPurposes; gọi lại nhiều lần không sao.
func RegisterFilePurposes() { files.RegisterPurpose(ImportPurpose()) }
