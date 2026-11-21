package testkit_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

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
