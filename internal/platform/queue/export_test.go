package queue

import "time"

// SetRecordTimeout đổi hạn của các lần ghi trạng thái sau handler (mặc định 5 s) để test ca handler chạy lâu hơn hạn
// đó mà không phải chờ 5 s.
func SetRecordTimeout(w *Worker, d time.Duration) { w.recordTimeout = d }

// Options trả tùy chọn đã chuẩn hóa của Worker (test mặc định của NewWorker).
func Options(w *Worker) WorkerOptions { return w.o }
