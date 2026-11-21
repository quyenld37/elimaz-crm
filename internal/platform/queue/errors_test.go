package queue_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
)

type errorEvent struct {
	Count   int64
	Role    string
	Name    string
	Message string
	Frames  string
	JobID   *uuid.UUID
}

func (e *env) errorEvents(t *testing.T) []errorEvent {
	t.Helper()
	rows, err := e.owner.Pool.Query(context.Background(),
		"SELECT count, role, name, message, top_frames, sample_job_id FROM platform.error_events ORDER BY first_seen_at")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []errorEvent
	for rows.Next() {
		var r errorEvent
		if err := rows.Scan(&r.Count, &r.Role, &r.Name, &r.Message, &r.Frames, &r.JobID); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// Handler lỗi → một dòng error_events có sample_job_id của việc, role worker; counter errors cộng 1.
func TestHandlerErrorRecordsErrorEvent(t *testing.T) {
	e := newEnv(t)
	h := newFH(kind("t.fail"), func(context.Context, queue.Job, pgx.Tx) (json.RawMessage, error) {
		return nil, errTemp
	})
	reg := metrics.NewRegistry()
	id := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "k"})
	e.runOnce(t, queue.NewWorker(e.app, []queue.Handler{h}, queue.WorkerOptions{WorkerID: "w1", Metrics: reg}), 10)
	rows := e.errorEvents(t)
	if len(rows) != 1 || rows[0].JobID == nil || *rows[0].JobID != id || rows[0].Role != "worker" || rows[0].Count != 1 {
		t.Fatalf("error_events: %+v, muốn một dòng role worker, sample_job_id %s", rows, id)
	}
	var errs int64
	for _, r := range reg.Drain() {
		if r.Name == "errors" && r.Label == "worker" {
			errs += r.Count
		}
	}
	if errs != 1 {
		t.Fatalf("counter errors{worker} = %d, muốn 1", errs)
	}
}

// Hai loại việc lỗi cùng kiểu lỗi → hai dòng (dấu vân tay theo loại việc).
func TestHandlerErrorFingerprintPerKind(t *testing.T) {
	e := newEnv(t)
	fail := func(context.Context, queue.Job, pgx.Tx) (json.RawMessage, error) { return nil, errTemp }
	a, b := newFH(kind("t.a"), fail), newFH(kind("t.b"), fail)
	e.enqueue(t, a.kind, queue.NewJob{DedupeKey: "a"})
	e.enqueue(t, b.kind, queue.NewJob{DedupeKey: "b"})
	e.runOnce(t, worker(e, "w1", a, b), 10)
	if rows := e.errorEvents(t); len(rows) != 2 {
		t.Fatalf("error_events có %d dòng, muốn 2 (mỗi loại việc một dòng)", len(rows))
	}
}

// Handler panic: worker không sập; việc lùi và chờ thử lại như lỗi thường; error_events ghi panic tại chỗ panic, nội
// dung đã lọc SĐT.
func TestHandlerPanicIsRecoveredAndRecorded(t *testing.T) {
	e := newEnv(t)
	h := newFH(kind("t.panic"), func(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
		if err := insertSide(ctx, tx, j); err != nil {
			return nil, err
		}
		panicInHandler()
		return nil, nil
	})
	id := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "k"})
	e.runOnce(t, worker(e, "w1", h), 10)
	r := e.job(t, id)
	if r.Status != "pending" || r.Attempts != 1 || r.LastError == nil {
		t.Fatalf("việc sau panic: %+v, muốn pending, attempts 1, có last_error", r)
	}
	if strings.Contains(*r.LastError, "0912345678") {
		t.Fatalf("last_error có SĐT: %q", *r.LastError)
	}
	if n := e.count(t, "SELECT count(*) FROM probe.side"); n != 0 {
		t.Fatalf("handler panic mà thay đổi còn %d dòng", n)
	}
	rows := e.errorEvents(t)
	if len(rows) != 1 || rows[0].Name != "panic(string)" || rows[0].JobID == nil || *rows[0].JobID != id {
		t.Fatalf("error_events: %+v", rows)
	}
	if strings.Contains(rows[0].Message, "0912345678") || !strings.Contains(rows[0].Frames, "panicInHandler") {
		t.Fatalf("dòng panic: message %q, frames %q", rows[0].Message, rows[0].Frames)
	}
}

func panicInHandler() { panic("boom 0912345678") }

// Handler tự báo ErrPermanent (lỗi đã lường trước): việc dead như cũ, không ghi error_events (đã có cảnh báo dead).
func TestPermanentErrorNotRecorded(t *testing.T) {
	e := newEnv(t)
	h, id := e.enqueuePermanentError(t)
	e.runOnce(t, worker(e, "w1", h), 10)
	if st := e.job(t, id).Status; st != "dead" {
		t.Fatalf("trạng thái %s, muốn dead", st)
	}
	if rows := e.errorEvents(t); len(rows) != 0 {
		t.Fatalf("ErrPermanent không phải lỗi ứng dụng, error_events có %d dòng", len(rows))
	}
}
