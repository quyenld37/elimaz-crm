package queue_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math/rand/v2"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

// Sự kiện vào (OrderBestEffort, spec 04 mục 5.1): một tin lỗi không chặn tin sau của cùng hội thoại; chỉ việc cùng
// hội thoại đang running mới chặn (không hai worker cùng xử lý một hội thoại).

func inboundKind(name string) queue.Kind {
	return queue.Kind{Name: name, Policy: queue.Policy{OrderMode: queue.OrderBestEffort}}
}

// (a) Tin 1 lỗi tạm (chờ thử lại 6 giờ), tin 2, 3 đến sau → tin 2, 3 xử lý ngay; tin 1 chạy lại đúng lịch.
func TestBestEffortRetryingHeadDoesNotBlock(t *testing.T) {
	e := newEnv(t)
	var first uuid.UUID
	var failFirst atomic.Bool
	failFirst.Store(true)
	h := newFH(inboundKind("inbound.t.retry"), func(_ context.Context, j queue.Job, _ pgx.Tx) (json.RawMessage, error) {
		if j.ID == first && failFirst.Load() {
			return nil, errTemp
		}
		return nil, nil
	})
	w := orderedWorker(e, h)
	first = e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "1", OrderKey: "conv:x", OrderSeq: 1})
	drain(t, w)
	if r := e.job(t, first); r.Status != "pending" || r.Attempts != 1 {
		t.Fatalf("tin 1 phải chờ thử lại: %+v", r)
	}
	e.ownerExec(t, fmt.Sprintf("UPDATE platform.jobs SET run_at = now() + interval '6 hours' WHERE id = '%s'", first))
	second := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "2", OrderKey: "conv:x", OrderSeq: 2})
	third := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "3", OrderKey: "conv:x", OrderSeq: 3})
	drain(t, w)
	if e.job(t, second).Status != "done" || e.job(t, third).Status != "done" {
		t.Fatalf("tin 2, 3 phải xong ngay: %+v %+v", e.job(t, second), e.job(t, third))
	}
	if r := e.job(t, first); r.Status != "pending" || r.RunInS < 5*3600 || h.callsOf(first) != 1 {
		t.Fatalf("tin 1 phải giữ lịch 6 giờ, chưa chạy lại: %+v, %d lần", r, h.callsOf(first))
	}
	// Tới lịch: tin 1 chạy lại và xong.
	failFirst.Store(false)
	e.makeDue(t)
	drain(t, w)
	if r := e.job(t, first); r.Status != "done" || r.Attempts != 2 {
		t.Fatalf("tin 1 phải chạy lại đúng lịch: %+v", r)
	}
}

// (b) Tin 1 dead → tin sau vẫn chạy; tin dead vẫn báo động (jobs_dead).
func TestBestEffortDeadDoesNotBlockButAlerts(t *testing.T) {
	e := newEnv(t)
	k := inboundKind("inbound.t.dead")
	var first uuid.UUID
	h := newFH(k, func(_ context.Context, j queue.Job, _ pgx.Tx) (json.RawMessage, error) {
		if j.ID == first {
			return nil, queue.ErrPermanent
		}
		return nil, nil
	})
	first = e.enqueue(t, k, queue.NewJob{DedupeKey: "1", OrderKey: "conv:x", OrderSeq: 1})
	second := e.enqueue(t, k, queue.NewJob{DedupeKey: "2", OrderKey: "conv:x", OrderSeq: 2})
	reg := metrics.NewRegistry()
	drain(t, queue.NewWorker(e.app, []queue.Handler{h}, queue.WorkerOptions{WorkerID: "w1", Concurrency: 3, Metrics: reg}))
	if e.job(t, first).Status != "dead" || e.job(t, second).Status != "done" {
		t.Fatalf("muốn dead rồi done: %+v %+v", e.job(t, first), e.job(t, second))
	}
	if !hasCounter(reg.Drain(), "jobs_dead", k.Name, 1) {
		t.Fatal("tin dead phải báo động jobs_dead")
	}
	third := e.enqueue(t, k, queue.NewJob{DedupeKey: "3", OrderKey: "conv:x", OrderSeq: 3})
	drain(t, orderedWorker(e, h))
	if e.job(t, third).Status != "done" {
		t.Fatalf("tin đến sau tin dead phải chạy: %+v", e.job(t, third))
	}
}

// blockingFirst trả handler giữ việc first trong Handle tới khi release đóng, và kênh started đóng khi nó bắt đầu.
func blockingFirst(k queue.Kind, first *uuid.UUID) (h *fh, started, release chan struct{}) {
	started, release = make(chan struct{}), make(chan struct{})
	var once sync.Once
	h = newFH(k, func(ctx context.Context, j queue.Job, _ pgx.Tx) (json.RawMessage, error) {
		if j.ID == *first {
			once.Do(func() { close(started) })
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return nil, nil
	})
	return h, started, release
}

// (c) Tin 1 đang running → tin 2 cùng hội thoại chờ (worker khác không nhận) tới khi tin 1 xong.
func TestBestEffortRunningBlocksSameConversation(t *testing.T) {
	e := newEnv(t)
	var first uuid.UUID
	h, started, release := blockingFirst(inboundKind("inbound.t.run"), &first)
	first = e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "1", OrderKey: "conv:x", OrderSeq: 1})
	second := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "2", OrderKey: "conv:x", OrderSeq: 2})
	other := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "o", OrderKey: "conv:y", OrderSeq: 1})
	w1, w2 := worker(e, "w1", h), worker(e, "w2", h)
	finish := holdRunOnce(t, w1, 1, started, release)
	// w2: tin 2 bị chặn vì tin 1 đang running; hội thoại khác không bị chặn.
	if n := runOnce(t, w2, 10); n != 1 || h.callsOf(second) != 0 || e.job(t, other).Status != "done" {
		t.Fatalf("w2 nhận %d việc; tin 2 gọi %d lần; hội thoại khác %+v", n, h.callsOf(second), e.job(t, other))
	}
	finish()
	runOnce(t, w2, 10)
	if e.job(t, first).Status != "done" || e.job(t, second).Status != "done" {
		t.Fatalf("muốn cả hai xong: %+v %+v", e.job(t, first), e.job(t, second))
	}
}

// (c) Việc sau đang running cũng chặn việc trước vừa tới lịch thử lại (không chỉ việc đứng trước mới chặn).
func TestBestEffortRunningLaterBlocksDueEarlier(t *testing.T) {
	e := newEnv(t)
	var second uuid.UUID
	h, started, release := blockingFirst(inboundKind("inbound.t.later"), &second)
	first := e.enqueue(t, h.kind, queue.NewJob{
		DedupeKey: "1", OrderKey: "conv:x", OrderSeq: 1,
		RunAt: time.Now().Add(time.Hour),
	})
	second = e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "2", OrderKey: "conv:x", OrderSeq: 2})
	w1, w2 := worker(e, "w1", h), worker(e, "w2", h)
	finish := holdRunOnce(t, w1, 10, started, release)
	e.makeDue(t) // tin 1 tới lịch trong lúc tin 2 đang chạy
	if n := runOnce(t, w2, 10); n != 0 || h.callsOf(first) != 0 {
		t.Fatalf("tin 1 không được chạy song song với tin 2: nhận %d, gọi %d", n, h.callsOf(first))
	}
	finish()
	runOnce(t, w2, 10)
	if e.job(t, first).Status != "done" {
		t.Fatalf("tin 1 phải xong sau khi tin 2 xong: %+v", e.job(t, first))
	}
}

// (c) Chốt chặn cuối ở database: lần nhận thua cuộc đua (một transaction khác vừa đặt việc cùng hội thoại sang running
// mà ảnh chụp của câu nhận chưa thấy) không chạy việc, không báo lỗi; việc vẫn pending và lượt sau nhận.
func TestBestEffortClaimRaceLosesQuietly(t *testing.T) {
	e := newEnv(t)
	h := newFH(inboundKind("inbound.t.race"), nil)
	first := e.enqueue(t, h.kind, queue.NewJob{
		DedupeKey: "1", OrderKey: "conv:x", OrderSeq: 1,
		RunAt: time.Now().Add(time.Hour),
	})
	second := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "2", OrderKey: "conv:x", OrderSeq: 2})
	// "Worker khác" đặt tin 1 sang running nhưng chưa commit.
	tx, err := e.owner.Pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(t.Context(), `UPDATE platform.jobs SET status = 'running', attempts = 1, locked_by = 'ghost',
		lease_until = now() + interval '90 seconds' WHERE id = $1`, first); err != nil {
		t.Fatal(err)
	}
	w := worker(e, "w1", h)
	type res struct {
		n   int
		err error
	}
	got := make(chan res, 1)
	go func() { n, err := w.RunOnce(context.Background(), 10); got <- res{n, err} }()
	waitForLockWait(t, e)
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	r := <-got
	if r.err != nil || r.n != 0 || h.callsOf(second) != 0 {
		t.Fatalf("lần nhận thua cuộc đua: n=%d err=%v, tin 2 gọi %d lần", r.n, r.err, h.callsOf(second))
	}
	if s := e.job(t, second); s.Status != "pending" || s.Attempts != 0 {
		t.Fatalf("tin 2 phải còn pending, attempts 0: %+v", s)
	}
}

// holdRunOnce chạy w.RunOnce(limit) trên một goroutine và chờ handler giữ việc bắt đầu (started đóng). Trả hàm thả
// việc (đóng release) rồi chờ RunOnce đó xong, test đỏ khi nó lỗi.
func holdRunOnce(t *testing.T, w *queue.Worker, limit int, started, release chan struct{}) func() {
	t.Helper()
	done := make(chan error, 1)
	go func() { _, err := w.RunOnce(context.Background(), limit); done <- err }()
	waitClosed(t, started)
	return func() {
		t.Helper()
		close(release)
		if err := waitErr(t, done); err != nil {
			t.Fatal(err)
		}
	}
}

// waitClosed chờ ch đóng, tối đa 10 s (test đỏ thay vì treo).
func waitClosed(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("hết 10 s chờ handler bắt đầu")
	}
}

// waitErr chờ một kết quả lỗi của ch, tối đa 10 s.
func waitErr(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("hết 10 s chờ RunOnce")
		return nil
	}
}

// waitForLockWait chờ tới khi có một phiên đang chờ khóa trong database thử (câu nhận chờ index duy nhất).
func waitForLockWait(t *testing.T, e *env) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if e.count(t, `SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid
			WHERE NOT l.granted AND a.datname = current_database()`) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("câu nhận việc không chờ khóa của index duy nhất")
}

// (e) 50 tin cùng một hội thoại, 10 tin lỗi ngẫu nhiên, 4 worker song song: mọi tin không lỗi xong trong một vòng
// (không cần chờ lịch thử lại), không tin nào bị xử lý hai lần, không lúc nào hai worker cùng xử lý hội thoại đó.
func TestBestEffortConcurrentConversation(t *testing.T) {
	e := newEnv(t)
	var (
		mu       sync.Mutex
		fails    = map[uuid.UUID]bool{}
		inflight atomic.Int32
		overlap  atomic.Bool
	)
	h := newFH(inboundKind("inbound.t.conc"), func(_ context.Context, j queue.Job, _ pgx.Tx) (json.RawMessage, error) {
		if inflight.Add(1) > 1 {
			overlap.Store(true)
		}
		defer inflight.Add(-1)
		time.Sleep(time.Millisecond)
		mu.Lock()
		fail := fails[j.ID]
		mu.Unlock()
		if fail {
			return nil, errTemp
		}
		return nil, nil
	})
	ids := make([]uuid.UUID, 50)
	failIdx := rand.Perm(50)[:10] //nolint:gosec // test
	for i := range ids {
		ids[i] = e.enqueue(t, h.kind, queue.NewJob{DedupeKey: strconv.Itoa(i), OrderKey: "conv:x", OrderSeq: int64(i)})
	}
	mu.Lock()
	for _, i := range failIdx {
		fails[ids[i]] = true
	}
	mu.Unlock()
	runParallelUntilIdle(t, worker(e, "w1", h), worker(e, "w2", h), worker(e, "w3", h), worker(e, "w4", h))
	if overlap.Load() {
		t.Fatal("hai worker cùng xử lý một hội thoại")
	}
	for i, id := range ids {
		r := e.job(t, id)
		want := "done"
		if fails[id] {
			want = "pending"
		}
		if r.Status != want || h.callsOf(id) != 1 {
			t.Errorf("tin %d: %s (muốn %s), gọi %d lần", i, r.Status, want, h.callsOf(id))
		}
	}
}

// runParallelUntilIdle cho các worker chạy RunOnce song song từng lượt (không đưa việc chờ thử lại về hạn) tới khi
// một lượt không worker nào nhận được việc.
func runParallelUntilIdle(t *testing.T, ws ...*queue.Worker) {
	t.Helper()
	for round := 0; ; round++ {
		if round > 200 {
			t.Fatal("chưa hết việc nhận được sau 200 lượt")
		}
		var (
			wg    sync.WaitGroup
			total atomic.Int64
		)
		for _, w := range ws {
			wg.Go(func() {
				n, err := w.RunOnce(context.Background(), 4)
				if err != nil {
					t.Error(err)
				}
				total.Add(int64(n))
			})
		}
		wg.Wait()
		if total.Load() == 0 {
			return
		}
	}
}

// claimSQL đọc câu ClaimJobs từ mã sqlc sinh (hằng claimJobs), để EXPLAIN đúng câu Worker chạy.
func claimSQL(t *testing.T) string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "../internal/store/jobs.sql.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, s := range g.Specs {
			vs, ok := s.(*ast.ValueSpec)
			if ok && len(vs.Names) == 1 && vs.Names[0].Name == "claimJobs" {
				lit, ok := vs.Values[0].(*ast.BasicLit)
				if !ok {
					break
				}
				sql, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				return sql
			}
		}
	}
	t.Fatal(errors.New("không thấy hằng claimJobs"))
	return ""
}

// (f) Câu nhận việc không quét toàn bảng jobs khi bảng có 100.000 dòng (nhiều done, nhiều hội thoại, cả hai chế độ).
func TestClaimPlanUsesIndexes(t *testing.T) {
	e := newEnv(t)
	tenant := e.tenant(t, "A")
	e.ownerExec(t, fmt.Sprintf(`INSERT INTO platform.jobs (tenant_id, kind, mode, dedupe_key, order_key, order_seq,
		on_lease_expired, max_attempts, status, order_mode, run_at, finished_at)
		SELECT '%s', 'inbound.t.plan', 'transactional', 'd' || g, 'conv:' || (g %% 5000), g, 'retry', 8,
			CASE WHEN g %% 50 = 0 THEN 'pending' WHEN g %% 97 = 0 THEN 'dead' ELSE 'done' END,
			CASE WHEN g %% 2 = 0 THEN 'best_effort' ELSE 'strict' END,
			now() + CASE WHEN g %% 100 = 0 THEN interval '1 hour' ELSE interval '0' END,
			CASE WHEN g %% 50 = 0 THEN NULL ELSE now() END
		FROM generate_series(1, 100000) g`, tenant), "ANALYZE platform.jobs")
	nodes, err := testkit.ExplainAsApp(t.Context(), e.app, tenant, claimSQL(t), int64(90000), "explain", false, int32(10))
	if err != nil {
		t.Fatal(err)
	}
	testkit.RequireNoSeqScan(t, nodes)
}
