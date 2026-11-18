package files

import (
	"time"

	"elimaz.com.vn/crm/internal/platform/commands"
)

// SetProcessTimeout đổi hạn một lần chạy của lệnh xử lý ảnh (test treo không phải chờ 30 s). Gọi trước khi dựng
// Runner, Worker (chúng đọc Kind lúc dựng).
func SetProcessTimeout(h commands.Handler, d time.Duration) { h.(*Processor).timeout = d }
