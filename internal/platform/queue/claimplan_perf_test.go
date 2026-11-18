//go:build perf

// Test thời gian của câu nhận việc (make perf-test). Tách khỏi test đúng/sai (Task 400): thời gian phụ thuộc máy bận hay
// rảnh, nên chỉ chạy riêng trên máy rảnh; TestClaimLongQueueSameOrderKey giữ các tiêu chí không phụ thuộc tải.
package queue_test

import (
	"testing"
	"time"
)

// claimBudget là trần thời gian chạy của câu nhận việc ở 100.000 việc chờ (kế hoạch M0, Task 394 Step 1).
const claimBudget = 50 * time.Millisecond

// 100.000 việc chờ, 10.000 cùng order_key của một tenant ở đầu hàng: câu nhận việc chạy dưới 50 ms (lần nhanh nhất
// trong 5 lần đo) ở cả bốn ca của eachLongQueueCase.
func TestClaimLongQueueTimeBudget(t *testing.T) {
	eachLongQueueCase(t, func(t *testing.T, s planStats) {
		t.Logf("ClaimJobs ở 100.000 việc chờ: %.2f ms", s.execMs)
		if s.execMs > float64(claimBudget.Milliseconds()) {
			t.Errorf("ClaimJobs %.2f ms, trần %v", s.execMs, claimBudget)
		}
	})
}
