package testkit_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

// countHandler đếm số lần chạy; việc nào cũng xong.
type countHandler struct{ calls atomic.Int64 }

func (*countHandler) Kind() queue.Kind { return queue.Kind{Name: "testkit.drain"} }

func (h *countHandler) Handle(context.Context, queue.Job, pgx.Tx) (json.RawMessage, error) {
	h.calls.Add(1)
	return nil, nil
}

// insertJob chèn một việc pending (không tenant) có run_at = now() + after của PostgreSQL; trả mã việc.
func insertJob(t *testing.T, s *testkit.Staff, after string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := s.Owner.Pool.QueryRow(t.Context(), `INSERT INTO platform.jobs
		(kind, mode, payload, dedupe_key, on_lease_expired, max_attempts, run_at)
		VALUES ('testkit.drain', 'transactional', '{}', gen_random_uuid()::text, 'retry', 8, now() + $1::interval)
		RETURNING id`, after).Scan(&id); err != nil {
		t.Fatalf("chèn việc: %v", err)
	}
	return id
}

func jobStatus(t *testing.T, s *testkit.Staff, id uuid.UUID) string {
	t.Helper()
	var st string
	if err := s.Owner.Pool.QueryRow(t.Context(), "SELECT status FROM platform.jobs WHERE id = $1", id).Scan(&st); err != nil {
		t.Fatal(err)
	}
	return st
}

// Việc ghi "chạy ngay" bằng now() của một transaction trước có thể có run_at lớn hơn now() của lần nhận ngay sau khi
// đồng hồ máy chủ PostgreSQL lùi (máy ảo chỉnh giờ, đo 21/11: lùi tới 0,36 s): giả bằng run_at = now() + 300 ms.
// DrainDue chạy việc đó và việc đã tới hạn; việc hẹn thử lại (3 giây, 6 giờ) giữ lịch.
func TestDrainDueRunsJobsAheadOfClockStep(t *testing.T) {
	s := testkit.OpenStaff(t)
	h := &countHandler{}
	w := queue.NewWorker(s.App, []queue.Handler{h}, queue.WorkerOptions{WorkerID: "drain-" + uuid.NewString()})
	due := insertJob(t, s, "0 seconds")
	stepped := insertJob(t, s, "300 milliseconds")
	retry := insertJob(t, s, "3 seconds")
	later := insertJob(t, s, "6 hours")
	testkit.DrainDue(t, s.Owner, w)
	for id, want := range map[uuid.UUID]string{due: "done", stepped: "done", retry: "pending", later: "pending"} {
		if got := jobStatus(t, s, id); got != want {
			t.Errorf("việc %s = %s, muốn %s", id, got, want)
		}
	}
	if n := h.calls.Load(); n != 2 {
		t.Fatalf("handler chạy %d lần, muốn 2", n)
	}
	if n := testkit.MakeDueSoon(t, s.Owner); n != 0 {
		t.Fatalf("MakeDueSoon đưa %d việc hẹn xa về hạn, muốn 0", n)
	}
}

// DrainJobs vẫn đưa mọi việc hẹn (kể cả 6 giờ) về hạn và chạy hết (Task 429).
func TestDrainJobsRunsScheduledJobs(t *testing.T) {
	s := testkit.OpenStaff(t)
	h := &countHandler{}
	w := queue.NewWorker(s.App, []queue.Handler{h}, queue.WorkerOptions{WorkerID: "drain-" + uuid.NewString()})
	ids := []uuid.UUID{insertJob(t, s, "300 milliseconds"), insertJob(t, s, "6 hours")}
	s.DrainJobs(t, w)
	for _, id := range ids {
		if got := jobStatus(t, s, id); got != "done" {
			t.Errorf("việc %s = %s, muốn done", id, got)
		}
	}
}

// runAt đọc run_at của việc id bằng chủ bảng.
func runAt(t *testing.T, s *testkit.Staff, id uuid.UUID) time.Time {
	t.Helper()
	var at time.Time
	if err := s.Owner.Pool.QueryRow(t.Context(), "SELECT run_at FROM platform.jobs WHERE id = $1", id).Scan(&at); err != nil {
		t.Fatal(err)
	}
	return at
}

// Đồng hồ PostgreSQL có thể lùi SAU câu MakeDueSoon, trước câu nhận của RunOnce (Task 476: commands
// TestUnknownOutcomesInDoubt đỏ "pending RunInS:0.107" dù runOnce đã gọi MakeDueSoon). MakeDueSoon phải để lại lề:
// sau nó, lùi 360 ms (giả bằng dời mọi run_at tới 360 ms) thì một lượt RunOnce vẫn nhận mọi việc đã/sắp tới hạn; giữ
// thứ tự run_at giữa chúng; việc hẹn thử lại (3 s) giữ nguyên; gọi lại ngay thì không còn việc vừa tới hạn nào.
func TestMakeDueSoonLeavesMarginForLaterClockStep(t *testing.T) {
	s := testkit.OpenStaff(t)
	h := &countHandler{}
	w := queue.NewWorker(s.App, []queue.Handler{h}, queue.WorkerOptions{WorkerID: "due-" + uuid.NewString()})
	old := insertJob(t, s, "-2 seconds")
	fresh := insertJob(t, s, "0 seconds")
	stepped := insertJob(t, s, "400 milliseconds")
	retry := insertJob(t, s, "3 seconds")
	retryAt := runAt(t, s, retry)
	if n := testkit.MakeDueSoon(t, s.Owner); n < 1 {
		t.Fatalf("MakeDueSoon báo %d việc vừa tới hạn, muốn ít nhất 1 (việc lệch 400 ms)", n)
	}
	if a, b, c := runAt(t, s, old), runAt(t, s, fresh), runAt(t, s, stepped); !a.Before(b) || !b.Before(c) {
		t.Fatalf("thứ tự run_at đổi: cũ %v, mới %v, lệch %v", a, b, c)
	}
	if got := runAt(t, s, retry); !got.Equal(retryAt) {
		t.Fatalf("việc hẹn thử lại bị dời: %v → %v", retryAt, got)
	}
	if n := testkit.MakeDueSoon(t, s.Owner); n != 0 {
		t.Fatalf("gọi lại MakeDueSoon báo %d việc vừa tới hạn, muốn 0", n)
	}
	if _, err := s.Owner.Pool.Exec(t.Context(),
		"UPDATE platform.jobs SET run_at = run_at + interval '360 milliseconds' WHERE status = 'pending'"); err != nil {
		t.Fatal(err)
	}
	if n, err := w.RunOnce(t.Context(), 10); err != nil || n != 3 {
		t.Fatalf("một lượt sau khi đồng hồ lùi nhận %d việc (lỗi %v), muốn 3", n, err)
	}
	if got := jobStatus(t, s, retry); got != "pending" {
		t.Fatalf("việc hẹn thử lại = %s, muốn pending", got)
	}
}

// RunRound là đúng một lượt nhận: limit 1 với hai việc tới hạn → nhận 1, việc kia còn pending; việc worker này không
// nhận (lệnh, worker không có Commands) → lượt nhận 0, không lặp mãi.
func TestRunRoundIsOneRound(t *testing.T) {
	s := testkit.OpenStaff(t)
	h := &countHandler{}
	w := queue.NewWorker(s.App, []queue.Handler{h}, queue.WorkerOptions{WorkerID: "round-" + uuid.NewString()})
	first, second := insertJob(t, s, "-1 seconds"), insertJob(t, s, "0 seconds")
	if n := testkit.RunRound(t, s.Owner, w, 1); n != 1 {
		t.Fatalf("lượt 1 nhận %d việc, muốn 1", n)
	}
	if a, b := jobStatus(t, s, first), jobStatus(t, s, second); a != "done" || b != "pending" {
		t.Fatalf("sau lượt 1: việc đầu %s, việc sau %s; muốn done, pending", a, b)
	}
	if n := testkit.RunRound(t, s.Owner, w, 1); n != 1 || jobStatus(t, s, second) != "done" {
		t.Fatalf("lượt 2 nhận %d việc, việc sau %s", n, jobStatus(t, s, second))
	}
	foreign := insertJob(t, s, "0 seconds")
	if _, err := s.Owner.Pool.Exec(t.Context(), "UPDATE platform.jobs SET mode = 'command' WHERE id = $1", foreign); err != nil {
		t.Fatal(err)
	}
	if n := testkit.RunRound(t, s.Owner, w, 10); n != 0 || jobStatus(t, s, foreign) != "pending" {
		t.Fatalf("lệnh khi worker không chạy lệnh: lượt nhận %d, trạng thái %s; muốn 0, pending", n, jobStatus(t, s, foreign))
	}
}
