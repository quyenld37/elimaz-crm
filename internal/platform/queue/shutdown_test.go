package queue_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/queue"
)

// Tắt êm: Stop(100ms) khi handler còn chờ 5 s → hủy handler sau grace (ErrStopGrace); việc về pending với attempts
// trả lại (0).
func TestStopReturnsUnfinishedJob(t *testing.T) {
	e := newEnv(t)
	started := make(chan struct{})
	h := newFH(kind("t.stop"), sideThen(started, func(ctx context.Context) error {
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
		return ctx.Err()
	}))
	id := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "k"})
	w := startWorker(t, e, 50*time.Millisecond, h)
	select {
	case <-started:
	case <-time.After(hangGuard):
		t.Fatal("handler không chạy")
	}
	// Stop phải hủy handler sau grace: nếu không, handler còn chạy (running) hoặc xong sau 5 s (done), không về pending.
	if err := w.Stop(100 * time.Millisecond); !errors.Is(err, queue.ErrStopGrace) {
		t.Fatalf("Stop: %v, muốn ErrStopGrace", err)
	}
	r := e.job(t, id)
	if r.Status != "pending" || r.Attempts != 0 || r.LockedBy != nil || r.RunInS > 1 {
		t.Fatalf("muốn pending, attempts 0, chạy lại ngay: %+v", r)
	}
	if n := e.count(t, "SELECT count(*) FROM probe.side"); n != 0 {
		t.Fatalf("việc dở mà thay đổi còn %d dòng", n)
	}
}

// Stop chờ việc đang chạy xong trong grace (không hủy), và sau Stop không nhận việc mới.
func TestStopWaitsThenStopsClaiming(t *testing.T) {
	e := newEnv(t)
	started := make(chan struct{}, 1)
	h := newFH(kind("t.grace"), func(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
		started <- struct{}{}
		time.Sleep(200 * time.Millisecond)
		return nil, insertSide(ctx, tx, j)
	})
	first := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "1"})
	w := startWorker(t, e, 50*time.Millisecond, h)
	<-started
	if err := w.Stop(5 * time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if r := e.job(t, first); r.Status != "done" {
		t.Fatalf("việc đang chạy phải xong trong grace: %+v", r)
	}
	later := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "2"})
	time.Sleep(300 * time.Millisecond)
	if r := e.job(t, later); r.Status != "pending" || h.callsOf(later) != 0 {
		t.Fatalf("sau Stop không được nhận việc: %+v", r)
	}
}

// Stop hủy một handler đã chạy lâu hơn hạn ghi: việc vẫn về pending với attempts trả lại.
func TestStopAfterLongHandlerReleases(t *testing.T) {
	e := newEnv(t)
	started := make(chan struct{})
	h := newFH(kind("t.stoplong"), sideThen(started, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}))
	id := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "k"})
	w := queue.NewWorker(e.app, []queue.Handler{h}, queue.WorkerOptions{WorkerID: "w1", Poll: 50 * time.Millisecond})
	queue.SetRecordTimeout(w, 50*time.Millisecond)
	if err := w.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := w.Stop(300 * time.Millisecond); err == nil {
		t.Fatal("Stop quá grace phải trả ErrStopGrace")
	}
	if r := e.job(t, id); r.Status != "pending" || r.Attempts != 0 {
		t.Fatalf("muốn pending, attempts 0: %+v", r)
	}
}
