package testkit

import (
	"testing"

	"elimaz.com.vn/crm/internal/platform/queue"
)

// drainRounds là trần số vòng của DrainJobs: quá số này mà vẫn còn việc thì test đỏ (không lặp mãi).
const drainRounds = 50

// DrainJobs chạy worker w (RunOnce, mỗi lượt tối đa 10 việc) tới khi database thử không còn việc pending nào, kể cả
// việc được hẹn chạy lại sau (lỗi tạm, hết ngân sách thời gian khi máy bận): hết việc đến hạn thì đưa run_at của
// việc hẹn về hiện tại thay vì chờ lịch thử lại. Việc vẫn đi đúng số lượt và chính sách của nó (hết lượt → dead).
// Quá drainRounds vòng, hay RunOnce lỗi, thì test dừng.
//
// Khi nào không dùng: test cố ý kiểm việc còn pending, hẹn chạy lại sau một lỗi (dùng RunOnce trực tiếp); worker chạy
// song song với test (câu đưa về hạn có thể chờ khóa dòng của worker).
func (s *Staff) DrainJobs(t testing.TB, w *queue.Worker) {
	t.Helper()
	for range drainRounds {
		n, err := w.RunOnce(t.Context(), 10)
		if err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		if n == 0 && s.makeJobsDue(t) == 0 {
			return
		}
	}
	t.Fatalf("worker chưa hết việc sau %d vòng", drainRounds)
}

// makeJobsDue đưa việc pending hẹn chạy sau về hạn ngay; trả số việc đã đưa (platform.jobs không có RLS).
func (s *Staff) makeJobsDue(t testing.TB) int64 {
	t.Helper()
	tag, err := s.Owner.Pool.Exec(t.Context(),
		"UPDATE platform.jobs SET run_at = now() WHERE status = 'pending' AND run_at > now()")
	if err != nil {
		t.Fatalf("đưa việc hẹn về hạn: %v", err)
	}
	return tag.RowsAffected()
}
