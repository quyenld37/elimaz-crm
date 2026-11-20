package files

import (
	"io"
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

// RetryDelay là retryDelay (thời gian chờ trước lần thử lại sau lỗi hạ tầng tạm, Task 409).
func RetryDelay(elapsed, hint, retryMax time.Duration, rnd func() float64) time.Duration {
	return retryDelay(elapsed, hint, retryMax, rnd)
}

// LimitedBody bọc rc như S3Store.Open (đọc tối đa left byte; thừa → ErrTooLarge) để test biên đọc mà không cần kho trả
// sai Content-Length (mutation testing, Task 403).
func LimitedBody(rc io.ReadCloser, left int64) io.ReadCloser { return &limitedBody{rc: rc, left: left} }

// SetRegenerateTimeout đổi hạn một lần chạy của file.regenerate (mutation testing, Task 403).
func SetRegenerateTimeout(h *Regenerator, d time.Duration) { h.timeout = d }
