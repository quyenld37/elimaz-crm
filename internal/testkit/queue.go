package testkit

import (
	"testing"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// drainRounds là trần số vòng của DrainJobs, DrainDue: quá số này mà vẫn còn việc thì test đỏ (không lặp mãi).
const drainRounds = 50

// DrainJobs chạy worker w (RunOnce, mỗi lượt tối đa 10 việc) tới khi database thử không còn việc pending nào, kể cả
// việc được hẹn chạy lại sau (lỗi tạm, hết ngân sách thời gian khi máy bận): hết việc đến hạn thì đưa run_at của
// việc hẹn về hiện tại thay vì chờ lịch thử lại. Việc vẫn đi đúng số lượt và chính sách của nó (hết lượt → dead).
// Quá drainRounds vòng, hay RunOnce lỗi, thì test dừng.
//
// Khi nào không dùng: test cố ý kiểm việc còn pending, hẹn chạy lại sau một lỗi (dùng DrainDue, hoặc RunOnce trực
// tiếp); worker chạy song song với test (câu đưa về hạn có thể chờ khóa dòng của worker).
func (s *Staff) DrainJobs(t testing.TB, w *queue.Worker) {
	t.Helper()
	drain(t, w, func() int64 { return s.makeJobsDue(t) })
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

// DrainDue chạy worker w (RunOnce, mỗi lượt tối đa 10 việc) tới khi không còn việc tới hạn; việc hẹn thử lại giữ
// lịch. Chịu được đồng hồ máy chủ PostgreSQL lùi (máy ảo chỉnh giờ; đo 21/11 trên crm-shared: lùi tới 0,36 s, vài lần
// mỗi phút): việc vừa ghi "chạy ngay" bằng now() của một transaction trước lần lùi có run_at lớn hơn now() của lần nhận
// sau đó, RunOnce trả 0 dù việc đã tới hạn. Hết việc nhận được thì MakeDueSoon đưa các việc đó về hạn rồi chạy tiếp.
// owner là pool chủ bảng của database thử. Quá drainRounds vòng, hay RunOnce lỗi, thì test dừng.
//
// Khi nào không dùng: test muốn mọi việc hẹn (kể cả lịch thử lại) chạy hết (DrainJobs); test kiểm đúng một lượt nhận
// (RunOnce trực tiếp).
func DrainDue(t testing.TB, owner *db.DB, w *queue.Worker) {
	t.Helper()
	drain(t, w, func() int64 { return MakeDueSoon(t, owner) })
}

// MakeDueSoon đưa về hạn ngay các việc pending hẹn chạy trong clockStepTolerance tới (lệch do đồng hồ PostgreSQL lùi,
// xem DrainDue), bỏ qua dòng đang bị khóa (worker chạy song song đang nhận hay ghi kết quả) thay vì chờ; trả số việc
// đã đưa. Việc hẹn xa hơn (lịch thử lại: hàng đợi từ 4 s, files.retryDelay từ 1 s) giữ nguyên. Dùng trong vòng chạy worker riêng khi vòng đó
// phải giữ cách xử lý lỗi của nó; còn lại dùng DrainDue.
func MakeDueSoon(t testing.TB, owner *db.DB) int64 {
	t.Helper()
	tag, err := owner.Pool.Exec(t.Context(), `UPDATE platform.jobs SET run_at = now() WHERE id IN (
		SELECT id FROM platform.jobs WHERE status = 'pending' AND run_at > now() AND run_at <= now() + $1::interval
		FOR UPDATE SKIP LOCKED)`, clockStepTolerance)
	if err != nil {
		t.Fatalf("đưa việc lệch đồng hồ về hạn: %v", err)
	}
	return tag.RowsAffected()
}

// clockStepTolerance là độ lùi tối đa của đồng hồ PostgreSQL mà MakeDueSoon bù: lớn hơn lần lùi lớn nhất đã đo
// (0,36 s), nhỏ hơn một nửa lịch thử lại nhỏ nhất trong mã (1 s của files.retryDelay) để việc vừa hẹn thử lại không bị
// tính là lệch đồng hồ cả khi lượt sau tới chậm.
const clockStepTolerance = "500 milliseconds"

// drain chạy RunOnce tới khi một lượt không nhận việc nào và makeDue không đưa thêm việc nào về hạn.
func drain(t testing.TB, w *queue.Worker, makeDue func() int64) {
	t.Helper()
	for range drainRounds {
		n, err := w.RunOnce(t.Context(), 10)
		if err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		if n == 0 && makeDue() == 0 {
			return
		}
	}
	t.Fatalf("worker chưa hết việc sau %d vòng", drainRounds)
}
