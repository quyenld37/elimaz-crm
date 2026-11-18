package files

import (
	"time"

	"elimaz.com.vn/crm/internal/platform/commands"
)

// SetProcessTimeout đổi hạn một lần chạy của lệnh xử lý ảnh (test treo không phải chờ 30 s). Gọi trước khi dựng
// Runner, Worker (chúng đọc Kind lúc dựng).
func SetProcessTimeout(h commands.Handler, d time.Duration) { h.(*Processor).timeout = d }

// SetPresetsForTest thay bảng bộ kích thước bằng table tới hết test (TP07: thêm v2 mà không đổi bảng hằng của mã
// chạy thật). Gọi trước khi dựng env có worker; bảng cũ được trả lại khi test xong.
func SetPresetsForTest(t interface{ Cleanup(func()) }, table map[int][]VariantSpec) {
	old := presets.Swap(&table)
	t.Cleanup(func() { presets.Store(old) })
}

// SetVerifyBatch đổi số tệp một lượt của lệnh file.verify (test đi qua đường nối lệnh lô sau mà không cần 500 tệp).
func SetVerifyBatch(v *Verifier, n int) { v.batch = n }
