package promotion

import "elimaz.com.vn/crm/internal/platform/background"

// Background trả phần chạy nền của module promotion (mã giảm giá): việc nền, consumer sự kiện, lịch. cmd/crm gọi đúng một lần mỗi
// tiến trình (kế hoạch M2, Task M2-02); task M2 sau thêm phần tử vào đây. Task M2-02 chưa có phần nào.
func Background(_ background.Deps) background.Parts { return background.Parts{} }
