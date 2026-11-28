package reporting_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

// runState là trạng thái cuối đã thấy của một lần chạy (ready: mốc xong và kết quả không được đổi về sau).
type runState struct {
	status   string
	finished time.Time
	result   string
}

// propRuns là trạng thái của test quy luật lần chạy: env, handler, báo hiệu đã nhận theo mã lần chạy.
type propRuns struct {
	t       *testing.T
	e       *runsEnv
	h       *reporting.RunHandler
	mu      sync.Mutex
	signals map[string]int
}

// collect đếm báo hiệu report.run_finished theo mã lần chạy tới khi test xong.
func (p *propRuns) collect(ch <-chan string) {
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			select {
			case id := <-ch:
				p.mu.Lock()
				p.signals[id]++
				p.mu.Unlock()
			case <-ctx.Done():
				return
			}
		}
	})
	p.t.Cleanup(func() { cancel(); wg.Wait() })
}

func (p *propRuns) handlers() []commands.Handler { return []commands.Handler{p.h} }

// step chạy một bước ngẫu nhiên trên các lần chạy ids.
func (p *propRuns) step(rt *rapid.T, ids []uuid.UUID) {
	t, e := p.t, p.e
	switch rapid.IntRange(0, 4).Draw(rt, "op") {
	case 0:
		testkit.RunRound(t, e.st.Owner, e.worker(p.h), 10)
	case 1: // worker nhận lệnh rồi chết (giữ chỗ đã qua)
		id := ids[rapid.IntRange(0, len(ids)-1).Draw(rt, "pick")]
		e.ownerExec(t, `UPDATE platform.jobs SET status = 'running', attempts = attempts + 1, locked_by = 'dead',
			lease_until = now() - interval '1 second' WHERE dedupe_key = $1 AND status = 'pending'`, id.String())
	case 2:
		if _, _, _, err := commands.ResolveInDoubt(t.Context(), e.st.App, p.handlers(), 10); err != nil {
			rt.Fatal(err)
		}
	case 3: // hết chỗ trong một lượt
		release := reporting.HoldRunSlots(p.h)
		testkit.RunRound(t, e.st.Owner, e.worker(p.h), 10)
		release()
	case 4: // tắt worker giữa chừng
		w := queue.NewWorker(e.st.App, nil, queue.WorkerOptions{
			WorkerID: "stop-" + uuid.NewString(), Commands: commands.NewRunner(e.st.App, p.handlers()),
		})
		testkit.MakeDueSoon(t, e.st.Owner)
		if err := w.Start(context.Background()); err != nil {
			rt.Fatal(err)
		}
		time.Sleep(time.Duration(rapid.IntRange(0, 15).Draw(rt, "stop_after_ms")) * time.Millisecond)
		_ = w.Stop(5 * time.Millisecond)
	}
}

// check đối chiếu trạng thái hiện tại với seen: lần chạy đã ready không được đổi (mốc xong, kết quả); ghi trạng thái
// cuối mới thấy.
func (p *propRuns) check(rt *rapid.T, ids []uuid.UUID, seen map[uuid.UUID]runState) {
	for _, id := range ids {
		st := p.e.state(p.t, id)
		prev, ok := seen[id]
		if ok && prev.status == "ready" && (st.status != "ready" || !st.finished.Equal(prev.finished) || st.result != prev.result) {
			rt.Fatalf("lần chạy %s đã ready rồi đổi: %+v → %+v", id, prev, st)
		}
		if (st.status == "ready" || st.status == "failed") && (!ok || prev.status != st.status) {
			seen[id] = st
		}
	}
}

// finish tra lại, đưa việc hẹn về hạn, chạy tới khi mọi lần chạy và lệnh của chúng kết thúc.
func (p *propRuns) finish(rt *rapid.T, ids []uuid.UUID, seen map[uuid.UUID]runState) {
	t, e := p.t, p.e
	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, id.String())
	}
	for range 30 {
		if _, _, _, err := commands.ResolveInDoubt(t.Context(), e.st.App, p.handlers(), 50); err != nil {
			rt.Fatal(err)
		}
		e.ownerExec(t, `UPDATE platform.jobs SET run_at = now() WHERE status = 'pending'`)
		testkit.RunRound(t, e.st.Owner, e.worker(p.h), 50)
		p.check(rt, ids, seen)
		open := e.st.CountIn(t, e.tenant, `SELECT count(*) FROM platform.jobs WHERE kind = 'command.report_run'
			AND dedupe_key = ANY($1) AND status NOT IN ('done', 'dead', 'expired')`, keys)
		if len(seen) == len(ids) && open == 0 {
			return
		}
	}
	rt.Fatalf("lần chạy chưa kết thúc sau 30 vòng")
}

// waitSignals đòi mỗi lần chạy có ít nhất một báo hiệu (trong 3 s).
func (p *propRuns) waitSignals(rt *rapid.T, ids []uuid.UUID) {
	deadline := time.Now().Add(3 * time.Second)
	for _, id := range ids {
		for {
			p.mu.Lock()
			got := p.signals[id.String()]
			p.mu.Unlock()
			if got >= 1 {
				break
			}
			if time.Now().After(deadline) {
				rt.Fatalf("lần chạy %s không có báo hiệu", id)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// Quy luật (rapid): chuỗi ngẫu nhiên các bước (worker chạy một lượt, worker chết giữ chỗ, tra lại in_doubt, hết chỗ,
// tắt worker giữa chừng) trên 1–5 lần chạy → khi mọi việc đã xử lý xong, mỗi lần chạy ở đúng một trạng thái cuối (ready
// hay failed); lần chạy đã ready thì không bao giờ ghi ready lần hai (mốc xong và kết quả giữ nguyên ở mọi bước sau);
// mỗi lần chạy đã kết thúc có ít nhất một báo hiệu report.run_finished.
func TestRuns_LifecycleProperty(t *testing.T) {
	t.Parallel()
	e := newRunsEnv(t)
	u := e.user(t, "an@elimaz.test", salesAll(), "all", "")
	p := &propRuns{t: t, e: e, h: e.handler(config.ReportConfig{WorkerSlots: 2}, sleepRunDef(0, nil)), signals: map[string]int{}}
	p.collect(e.subscribe(t, u))
	limits := reporting.RunLimits{MaxPendingPerUser: 1 << 20, JSONTTL: time.Hour}
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 5).Draw(rt, "runs")
		ids := make([]uuid.UUID, 0, n)
		for range n {
			r, err := e.create(t, u, dashboardInput(), limits)
			if err != nil {
				rt.Fatal(err)
			}
			ids = append(ids, r.ID)
		}
		seen := map[uuid.UUID]runState{}
		for range rapid.IntRange(1, 8).Draw(rt, "steps") {
			p.step(rt, ids)
			p.check(rt, ids, seen)
		}
		p.finish(rt, ids, seen)
		p.waitSignals(rt, ids)
	})
}

// state đọc trạng thái, mốc xong, kết quả của lần chạy.
func (e *runsEnv) state(t *testing.T, id uuid.UUID) runState {
	t.Helper()
	var st runState
	var finished *time.Time
	var result *string
	e.st.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT status, finished_at, result::text FROM reporting.report_runs WHERE id = $1`,
			id).Scan(&st.status, &finished, &result)
	})
	if finished != nil {
		st.finished = *finished
	}
	if result != nil {
		st.result = *result
	}
	return st
}
