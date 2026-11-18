package queue

import "time"

// RetrySchedule là lịch chờ trước lần chạy lại thứ n (spec 04 mục 5.1); các lần sau lần thứ 6 giữ 6 giờ.
//
//nolint:gochecknoglobals // Interfaces của kế hoạch M0 Task 11 khai là var; chỉ đọc, không mã nào gán lại.
var RetrySchedule = []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute, time.Hour, 6 * time.Hour}

// jitter ±20%: hệ số trong [0.8, 1.2].
const (
	jitterMin  = 0.8
	jitterSpan = 0.4
)

// NextDelay là thời gian chờ sau lần chạy lỗi thứ attempt (≥ 1; nhỏ hơn coi như 1): RetrySchedule[min(attempt-1, 5)]
// nhân hệ số 0,8 + 0,4·rnd() với rnd trong [0, 1].
func NextDelay(attempt int, rnd func() float64) time.Duration {
	i := min(max(attempt-1, 0), len(RetrySchedule)-1)
	return time.Duration(float64(RetrySchedule[i]) * (jitterMin + jitterSpan*rnd()))
}
