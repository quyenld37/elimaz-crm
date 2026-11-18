package commands

import "time"

// SetRecordTimeout đổi hạn của mỗi lần ghi kết quả (mặc định 5 s) để test ca Execute chạy lâu hơn hạn đó mà không phải
// chờ 5 s.
func SetRecordTimeout(r *Runner, d time.Duration) { r.recordTimeout = d }
