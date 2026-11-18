package queue_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
)

var errTemp = errors.New("lỗi tạm")

func TestHandlerSuccessMarksDoneWithResult(t *testing.T) {
	e := newEnv(t)
	h := newFH(kind("t.ok"), func(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
		if err := insertSide(ctx, tx, j); err != nil {
			return nil, err
		}
		return json.RawMessage(`{"ok":true}`), nil
	})
	id := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "k", Payload: map[string]string{"ref": "x"}})
	reg := metrics.NewRegistry()
	w := queue.NewWorker(e.app, []queue.Handler{h}, queue.WorkerOptions{WorkerID: "w1", Metrics: reg})
	if n := runOnce(t, w, 10); n != 1 {
		t.Fatalf("RunOnce xử lý %d việc, muốn 1", n)
	}
	r := e.job(t, id)
	if r.Status != "done" || r.Attempts != 1 || r.FinishedAt == nil || string(r.Result) != `{"ok": true}` {
		t.Fatalf("muốn done, attempts 1, có finished_at và result: %+v (result %s)", r, r.Result)
	}
	if n := e.count(t, "SELECT count(*) FROM probe.side WHERE job_id = $1", id); n != 1 {
		t.Fatalf("thay đổi của handler: %d dòng, muốn 1", n)
	}
	if n := runOnce(t, w, 10); n != 0 || h.callsOf(id) != 1 {
		t.Fatalf("việc done không được chạy lại: n=%d calls=%d", n, h.callsOf(id))
	}
	names := map[string]bool{}
	for _, row := range reg.Drain() {
		names[row.Name+"/"+row.Label] = true
	}
	if !names["job_ms/t.ok"] || !names["job_pickup_ms/t.ok"] {
		t.Fatalf("thiếu chỉ số job_ms, job_pickup_ms theo kind: %v", names)
	}
}

func TestHandlerErrorRollsBackAndReschedules(t *testing.T) {
	e := newEnv(t)
	h := newFH(kind("t.fail"), func(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
		if err := insertSide(ctx, tx, j); err != nil {
			return nil, err
		}
		return nil, errTemp
	})
	id := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "k"})
	// Mốc giờ database trước và sau lượt chạy: run_at = now() + chờ (5 s ±20%) với now() nằm giữa hai mốc. Không so
	// với now() lúc đọc (máy bận thì thời gian từ lúc ghi tới lúc đọc làm lệch biên dưới).
	before := e.dbNow(t)
	runOnce(t, worker(e, "w1", h), 10)
	after := e.dbNow(t)
	r := e.job(t, id)
	if r.Status != "pending" || r.Attempts != 1 || r.LockedBy != nil ||
		r.RunAt.Before(before.Add(4*time.Second)) || r.RunAt.After(after.Add(6*time.Second)) {
		t.Fatalf("muốn pending, attempts 1, run_at sau 4–6 s, bỏ locked_by: %+v (lượt chạy %s → %s)", r, before, after)
	}
	if r.LastError == nil || !strings.Contains(*r.LastError, "lỗi tạm") {
		t.Fatalf("last_error: %v", r.LastError)
	}
	if n := e.count(t, "SELECT count(*) FROM probe.side"); n != 0 {
		t.Fatalf("handler lỗi mà thay đổi còn %d dòng", n)
	}
}

// Worker mất giữ chỗ trong lúc handler chạy (một worker khác đã nhận lại): lệnh done 0 dòng → lùi cả thay đổi của
// handler; không ghi đè trạng thái của bên đã nhận lại.
func TestLostLeaseRollsBack(t *testing.T) {
	e := newEnv(t)
	started, release := make(chan struct{}), make(chan struct{})
	h := newFH(kind("t.lost"), sideThen(started, func(context.Context) error { <-release; return nil }))
	id := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "k"})
	done := make(chan error, 1)
	go func() {
		_, err := worker(e, "w1", h).RunOnce(t.Context(), 10)
		done <- err
	}()
	<-started
	e.ownerExec(t, fmt.Sprintf("UPDATE platform.jobs SET locked_by = 'w2' WHERE id = '%s'", id))
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	r := e.job(t, id)
	if r.Status != "running" || r.LockedBy == nil || *r.LockedBy != "w2" {
		t.Fatalf("việc phải giữ nguyên của w2 (running), không done: %+v", r)
	}
	if n := e.count(t, "SELECT count(*) FROM probe.side"); n != 0 {
		t.Fatalf("mất giữ chỗ mà thay đổi của handler còn %d dòng", n)
	}
}

func TestPermanentErrorGoesDeadOnce(t *testing.T) {
	e := newEnv(t)
	h, id := e.enqueuePermanentError(t)
	reg := metrics.NewRegistry()
	w := queue.NewWorker(e.app, []queue.Handler{h}, queue.WorkerOptions{WorkerID: "w1", Metrics: reg})
	runOnce(t, w, 10)
	e.makeDue(t)
	runOnce(t, w, 10)
	r := e.job(t, id)
	if r.Status != "dead" || r.Attempts != 1 || r.FinishedAt == nil {
		t.Fatalf("ErrPermanent phải dead ngay: %+v", r)
	}
	if got := h.terminalList(); len(got) != 1 || got[0] != id.String()+":dead" {
		t.Fatalf("OnTerminal phải gọi đúng một lần với dead: %v", got)
	}
	if !hasCounter(reg.Drain(), "jobs_dead", "t.perm", 1) {
		t.Fatal("thiếu chỉ số jobs_dead")
	}
}

func hasCounter(rows []metrics.Row, name, label string, n int64) bool {
	for _, r := range rows {
		if r.Name == name && r.Label == label && r.Count == n {
			return true
		}
	}
	return false
}

func TestEighthFailureGoesDead(t *testing.T) {
	e := newEnv(t)
	h := newFH(kind("t.eight"), func(context.Context, queue.Job, pgx.Tx) (json.RawMessage, error) { return nil, errTemp })
	id := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "k"})
	w := worker(e, "w1", h)
	for i := 1; i <= 7; i++ {
		runOnce(t, w, 10)
		if r := e.job(t, id); r.Status != "pending" || r.Attempts != i {
			t.Fatalf("sau lần lỗi %d: %+v", i, r)
		}
		e.makeDue(t)
	}
	runOnce(t, w, 10)
	if r := e.job(t, id); r.Status != "dead" || r.Attempts != 8 {
		t.Fatalf("lỗi lần thứ 8 phải dead: %+v", r)
	}
	if got := h.terminalList(); len(got) != 1 || got[0] != id.String()+":dead" {
		t.Fatalf("OnTerminal: %v", got)
	}
}

// Worker chết: dòng running hết giữ chỗ. Chính sách retry → về hàng chờ rồi chạy lại tới done.
func TestExpiredLeaseRetry(t *testing.T) {
	e := newEnv(t)
	h := newFH(kind("t.lease"), nil)
	id := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "k"})
	e.ownerExec(t, fmt.Sprintf(`UPDATE platform.jobs SET status = 'running', attempts = 1, locked_by = 'dead-worker',
		lease_until = now() - interval '1 second' WHERE id = '%s'`, id))
	runOnce(t, worker(e, "w1", h), 10)
	if r := e.job(t, id); r.Status != "done" || r.Attempts != 2 || h.callsOf(id) != 1 {
		t.Fatalf("muốn done sau khi chạy lại: %+v, calls %d", r, h.callsOf(id))
	}
	if len(h.terminalList()) != 0 {
		t.Fatalf("retry không gọi OnTerminal: %v", h.terminalList())
	}
}

// Giữ chỗ còn hạn: không ai nhận lại.
func TestLiveLeaseNotReclaimed(t *testing.T) {
	e := newEnv(t)
	h := newFH(kind("t.live"), nil)
	id := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "k"})
	e.ownerExec(t, fmt.Sprintf(`UPDATE platform.jobs SET status = 'running', attempts = 1, locked_by = 'other',
		lease_until = now() + interval '60 seconds' WHERE id = '%s'`, id))
	runOnce(t, worker(e, "w1", h), 10)
	if r := e.job(t, id); r.Status != "running" || h.callsOf(id) != 0 {
		t.Fatalf("giữ chỗ còn hạn bị nhận lại: %+v", r)
	}
}

// Hết giữ chỗ ở lần thử cuối (attempts = max_attempts) với retry: dead thay vì chạy lại mãi (xem Quyết định 3).
func TestExpiredLeaseAtMaxAttemptsGoesDead(t *testing.T) {
	e := newEnv(t)
	h := newFH(queue.Kind{Name: "t.leasemax", Policy: queue.Policy{MaxAttempts: 2}}, nil)
	id := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "k"})
	e.ownerExec(t, fmt.Sprintf(`UPDATE platform.jobs SET status = 'running', attempts = 2, locked_by = 'dead-worker',
		lease_until = now() - interval '1 second' WHERE id = '%s'`, id))
	runOnce(t, worker(e, "w1", h), 10)
	if r := e.job(t, id); r.Status != "dead" || h.callsOf(id) != 0 || r.LastError == nil || !strings.Contains(*r.LastError, "lease") {
		t.Fatalf("muốn dead, không chạy lại: %+v", r)
	}
	if got := h.terminalList(); len(got) != 1 || got[0] != id.String()+":dead" {
		t.Fatalf("OnTerminal: %v", got)
	}
}

// Chính sách in_doubt: hết giữ chỗ → in_doubt + OnTerminal, không chạy lại.
func TestExpiredLeaseInDoubt(t *testing.T) {
	e := newEnv(t)
	h := newFH(queue.Kind{Name: "t.doubt", Policy: queue.Policy{OnLeaseExpired: "in_doubt"}}, nil)
	id := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "k"})
	e.ownerExec(t, fmt.Sprintf(`UPDATE platform.jobs SET status = 'running', attempts = 1, locked_by = 'dead-worker',
		lease_until = now() - interval '1 second' WHERE id = '%s'`, id))
	reg := metrics.NewRegistry()
	w := queue.NewWorker(e.app, []queue.Handler{h}, queue.WorkerOptions{WorkerID: "w1", Metrics: reg})
	runOnce(t, w, 10)
	runOnce(t, w, 10)
	if r := e.job(t, id); r.Status != "in_doubt" || h.callsOf(id) != 0 {
		t.Fatalf("muốn in_doubt, không chạy lại: %+v", r)
	}
	if got := h.terminalList(); len(got) != 1 || got[0] != id.String()+":in_doubt" {
		t.Fatalf("OnTerminal: %v", got)
	}
	if !hasCounter(reg.Drain(), "jobs_in_doubt", "t.doubt", 1) {
		t.Fatal("thiếu chỉ số jobs_in_doubt")
	}
}

func TestTwoWorkersHandleEachJobOnce(t *testing.T) {
	e := newEnv(t)
	h := newFH(kind("t.par"), insertSideHandler)
	ids := make([]uuid.UUID, 20)
	for i := range ids {
		ids[i] = e.enqueue(t, h.kind, queue.NewJob{DedupeKey: fmt.Sprint(i)})
	}
	w1, w2 := worker(e, "w1", h), worker(e, "w2", h)
	for range 20 {
		var wg sync.WaitGroup
		for _, w := range []*queue.Worker{w1, w2} {
			wg.Go(func() {
				if _, err := w.RunOnce(t.Context(), 5); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if e.count(t, "SELECT count(*) FROM platform.jobs WHERE status <> 'done'") == 0 {
			break
		}
	}
	for _, id := range ids {
		if c := h.callsOf(id); c != 1 {
			t.Errorf("việc %s: Handle %d lần", id, c)
		}
	}
	if n := e.count(t, "SELECT count(*) FROM probe.side"); n != 20 {
		t.Fatalf("muốn 20 dòng thay đổi, có %d", n)
	}
}

func insertSideHandler(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	return nil, insertSide(ctx, tx, j)
}

// Handler chạy trong ngữ cảnh tenant của việc: bảng RLS chỉ thấy dòng của tenant đó; việc không tenant thấy 0 dòng.
func TestHandlerRunsInTenantContext(t *testing.T) {
	e := newEnv(t)
	a, b := e.tenant(t, "A"), e.tenant(t, "B")
	e.ownerExec(t,
		"CREATE TABLE probe.secret (tenant_id uuid NOT NULL, note text NOT NULL)",
		"SELECT platform.enable_tenant_rls('probe.secret')",
		"GRANT SELECT ON probe.secret TO crm_app",
	)
	insertSecret(t, e, a, 2)
	insertSecret(t, e, b, 1)
	h := newFH(kind("t.rls"), func(ctx context.Context, _ queue.Job, tx pgx.Tx) (json.RawMessage, error) {
		var n int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM probe.secret").Scan(&n); err != nil {
			return nil, err //nolint:wrapcheck // test
		}
		return json.RawMessage(fmt.Sprintf(`{"seen":%d}`, n)), nil
	})
	ja := e.enqueue(t, h.kind, queue.NewJob{TenantID: &a, DedupeKey: "a"})
	jn := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "none"})
	runOnce(t, worker(e, "w1", h), 10)
	if r := e.job(t, ja); r.Status != "done" || string(r.Result) != `{"seen": 2}` {
		t.Fatalf("việc của A phải thấy đúng 2 dòng của A: %+v %s", r, r.Result)
	}
	if r := e.job(t, jn); r.Status != "done" || string(r.Result) != `{"seen": 0}` {
		t.Fatalf("việc không tenant phải thấy 0 dòng: %+v %s", r, r.Result)
	}
}

func insertSecret(t *testing.T, e *env, tenant uuid.UUID, n int) {
	t.Helper()
	for range n {
		if _, err := e.owner.Pool.Exec(t.Context(), `WITH s AS (SELECT set_config('app.tenant_id', $1, true))
			INSERT INTO probe.secret (tenant_id, note) SELECT $2::uuid, 'x' FROM s`, tenant.String(), tenant); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandlerTimeout(t *testing.T) {
	e := newEnv(t)
	var sawErr bool
	h := newFH(queue.Kind{Name: "t.slow", Policy: queue.Policy{Timeout: 50 * time.Millisecond}},
		func(ctx context.Context, _ queue.Job, _ pgx.Tx) (json.RawMessage, error) {
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
			sawErr = ctx.Err() != nil
			return nil, ctx.Err()
		})
	id := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "k"})
	start := time.Now()
	runOnce(t, worker(e, "w1", h), 10)
	if time.Since(start) > 2*time.Second || !sawErr {
		t.Fatalf("handler phải bị hủy sau Timeout: %v, ctx.Err %v", time.Since(start), sawErr)
	}
	r := e.job(t, id)
	if r.Status != "pending" || r.LastError == nil || !strings.Contains(*r.LastError, "deadline") {
		t.Fatalf("muốn pending, last_error chứa deadline: %+v %v", r, r.LastError)
	}
}

// Việc pending đã quá expires_at: expired, không Handle, OnTerminal("expired") một lần.
func TestPendingPastExpiryExpires(t *testing.T) {
	e := newEnv(t)
	h := newFH(queue.Kind{Name: "t.exp", Policy: queue.Policy{ExpiresAfter: time.Hour}}, nil)
	id := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "k"})
	e.ownerExec(t, fmt.Sprintf("UPDATE platform.jobs SET expires_at = now() - interval '1 second' WHERE id = '%s'", id))
	w := worker(e, "w1", h)
	runOnce(t, w, 10)
	runOnce(t, w, 10)
	if r := e.job(t, id); r.Status != "expired" || r.FinishedAt == nil || h.callsOf(id) != 0 {
		t.Fatalf("muốn expired, không Handle: %+v calls %d", r, h.callsOf(id))
	}
	if got := h.terminalList(); len(got) != 1 || got[0] != id.String()+":expired" {
		t.Fatalf("OnTerminal: %v", got)
	}
}

// Việc pending quá hạn sống → expired được đếm jobs_expired theo kind (Task 19 cảnh báo, việc treo của Task 14).
func TestExpiredCountsMetric(t *testing.T) {
	e := newEnv(t)
	h := newFH(queue.Kind{Name: "t.expm", Policy: queue.Policy{ExpiresAfter: time.Hour}}, nil)
	id := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "k"})
	e.ownerExec(t, fmt.Sprintf("UPDATE platform.jobs SET expires_at = now() - interval '1 second' WHERE id = '%s'", id))
	reg := metrics.NewRegistry()
	runOnce(t, queue.NewWorker(e.app, []queue.Handler{h}, queue.WorkerOptions{WorkerID: "w1", Metrics: reg}), 10)
	if !hasCounter(reg.Drain(), "jobs_expired", "t.expm", 1) {
		t.Fatal("việc expired phải đếm jobs_expired")
	}
}

func TestNoHandlerGoesDead(t *testing.T) {
	e := newEnv(t)
	id := e.enqueue(t, kind("t.unknown"), queue.NewJob{DedupeKey: "k"})
	runOnce(t, worker(e, "w1", newFH(kind("t.other"), nil)), 10)
	r := e.job(t, id)
	if r.Status != "dead" || r.LastError == nil || *r.LastError != "no handler" {
		t.Fatalf("muốn dead, last_error 'no handler': %+v", r)
	}
}

// Worker không có CommandRunner (WorkerOptions.Commands nil) không nhận việc command: giữ pending (Task 12 giao việc
// command cho commands.Runner; test của nhánh có runner ở internal/platform/commands).
func TestCommandJobsNotClaimed(t *testing.T) {
	e := newEnv(t)
	k := queue.Kind{Name: "command.t", Policy: queue.Policy{Mode: "command", OnLeaseExpired: "in_doubt"}}
	h := newFH(k, nil)
	id := e.enqueue(t, k, queue.NewJob{DedupeKey: "k"})
	runOnce(t, worker(e, "w1", h), 10)
	if r := e.job(t, id); r.Status != "pending" || r.Attempts != 0 || h.callsOf(id) != 0 {
		t.Fatalf("worker không có runner không được nhận việc command: %+v", r)
	}
}

func TestRunAtInFutureNotClaimed(t *testing.T) {
	e := newEnv(t)
	h := newFH(kind("t.later"), nil)
	id := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "k", RunAt: time.Now().Add(time.Hour)})
	runOnce(t, worker(e, "w1", h), 10)
	if r := e.job(t, id); r.Status != "pending" || r.RunInS < 3500 {
		t.Fatalf("việc hoãn tới giờ bị chạy sớm: %+v", r)
	}
}

// Việc mới được ghi khi worker đang chờ: NOTIFY đánh thức, nhận trong dưới 1 s dù Poll là 1 phút.
func TestNotifyWakesWorker(t *testing.T) {
	e := newEnv(t)
	got := make(chan time.Time, 1)
	h := newFH(kind("t.wake"), func(context.Context, queue.Job, pgx.Tx) (json.RawMessage, error) {
		got <- time.Now()
		return nil, nil
	})
	startWorker(t, e, time.Minute, h)
	time.Sleep(200 * time.Millisecond) // vòng đầu đã chạy xong, worker đang chờ
	at := time.Now()
	e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "k"})
	select {
	case handled := <-got:
		if d := handled.Sub(at); d > time.Second {
			t.Fatalf("nhận sau %v", d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("NOTIFY không đánh thức worker")
	}
}

// Poll vẫn chạy làm lưới an toàn: việc tới hạn (run_at qua) được nhận dù không có NOTIFY mới.
func TestPollPicksDueJob(t *testing.T) {
	e := newEnv(t)
	got := make(chan struct{}, 1)
	h := newFH(kind("t.poll"), func(context.Context, queue.Job, pgx.Tx) (json.RawMessage, error) {
		got <- struct{}{}
		return nil, nil
	})
	e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "k", RunAt: time.Now().Add(300 * time.Millisecond)})
	startWorker(t, e, 100*time.Millisecond, h)
	select {
	case <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("Poll không nhận việc tới hạn")
	}
}

func TestOldestPendingGauge(t *testing.T) {
	e := newEnv(t)
	h := newFH(kind("t.gauge"), nil)
	id := e.enqueue(t, kind("t.nohandler-yet"), queue.NewJob{DedupeKey: "k"})
	e.ownerExec(t, fmt.Sprintf("UPDATE platform.jobs SET run_at = now() - interval '42 seconds', mode = 'command' WHERE id = '%s'", id))
	reg := metrics.NewRegistry()
	w := queue.NewWorker(e.app, []queue.Handler{h}, queue.WorkerOptions{WorkerID: "w1", Metrics: reg})
	runOnce(t, w, 10)
	for _, r := range reg.Drain() {
		if r.Name == "jobs_oldest_pending_age_s" {
			if r.Sum < 41 || r.Sum > 60 {
				t.Fatalf("gauge %v, muốn khoảng 42", r.Sum)
			}
			return
		}
	}
	t.Fatal("thiếu gauge jobs_oldest_pending_age_s")
}

// Hạn của lần ghi kết quả tính từ lúc handler xong, không từ lúc nhận việc: handler chạy lâu hơn hạn đó vẫn được hẹn
// lại theo lịch (phát hiện của reviewer Task 11). Hạn 1 s đủ rộng cho lần ghi khi máy tải cao (50 ms từng đỏ, Task
// 317); handler chạy quá hạn 200 ms, nên nếu hạn tính từ lúc nhận việc thì ctx ghi đã hết, dù máy nhanh hay chậm.
func TestRecordAfterLongHandler(t *testing.T) {
	const record = time.Second
	e := newEnv(t)
	h := newFH(kind("t.long"), func(context.Context, queue.Job, pgx.Tx) (json.RawMessage, error) {
		time.Sleep(record + 200*time.Millisecond)
		return nil, errTemp
	})
	id := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "k"})
	w := worker(e, "w1", h)
	queue.SetRecordTimeout(w, record)
	runOnce(t, w, 10)
	if r := e.job(t, id); r.Status != "pending" || r.RunInS < 3 {
		t.Fatalf("muốn pending hẹn theo lịch sau handler chạy lâu: %+v", r)
	}
}
