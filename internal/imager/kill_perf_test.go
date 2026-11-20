//go:build perf

package imager_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/imager"
)

// Test thời gian (nhãn perf, make perf-test, chạy trên máy rảnh — Task 400, 440): trần thời gian từ lúc lệnh treo
// khởi động tới lúc trả lời. Test đúng/sai cùng việc (TestImager_HangKilledWithinDeadline,
// TestExecRunner_TimeoutKillsProcessGroup) không lấy giờ thật làm tiêu chí.

// Process trả lời ≤ 2 s kể từ lúc lệnh treo đã khởi động (giờ sửa của pidfile mà script ghi), hạn bước 1 s.
func TestImager_HangKilledTimeBudget(t *testing.T) {
	st := newStore(t)
	lim, pidfile := hangLimits(t)
	r := imager.NewExecRunner([]string{"FAKEVIPS_PIDFILE=" + pidfile})
	res := imager.Process(t.Context(), r, imager.NewHTTPClient(5*time.Second),
		request(st.addSource(t, "hang", readImage(t, "photo-gps.jpg")), twelve(st.srv.URL)), lim)
	done := time.Now()
	if res.Status != imager.StatusRejected || res.Reason != imager.ReasonTimeout {
		t.Fatalf("kết quả %+v", res)
	}
	fi, err := os.Stat(pidfile)
	if err != nil {
		t.Fatalf("script giả chưa chạy trong hạn bước (máy quá bận?): %v", err)
	}
	if elapsed := done.Sub(fi.ModTime()); elapsed > 2*time.Second {
		t.Fatalf("trả lời sau %v kể từ lúc lệnh treo chạy, cần ≤ 2 s", elapsed)
	}
	assertGroupGone(t, pidfile)
}

// ExecRunner.Run trả về ≤ 1,5 s với hạn 500 ms.
func TestExecRunner_TimeoutTimeBudget(t *testing.T) {
	pidfile := t.TempDir() + "/pid"
	r := imager.NewExecRunner([]string{"FAKEVIPS_PIDFILE=" + pidfile})
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := r.Run(ctx, fakeVips(t, "hang.sh"), nil, 0)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lỗi %v, cần bọc context.DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 1500*time.Millisecond {
		t.Fatalf("Run trả về sau %v", d)
	}
	assertGroupGone(t, pidfile)
}
