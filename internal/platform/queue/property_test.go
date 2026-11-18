package queue_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
)

func dbTx() db.TxOptions { return db.TxOptions{} }

type propJob struct {
	id       uuid.UUID
	orderKey string
	seq      int64
}

// Quy luật (vùng rủi ro cao, ADR-25): 5–40 việc trên 1–4 OrderKey, OrderSeq xáo trộn, một phần lỗi tạm ở lần đầu, hai
// worker chạy xen kẽ (song song) tới hết: (1) mỗi việc hoàn tất đúng một lần; (2) trong mỗi OrderKey thứ tự hoàn tất
// tăng theo (OrderSeq, ID); (3) không việc nào kẹt running.
func TestPropertyQueue(t *testing.T) {
	e := newEnv(t)
	e.ownerExec(t,
		"CREATE TABLE probe.completed (n bigserial PRIMARY KEY, job_id uuid NOT NULL)",
		"GRANT SELECT, INSERT ON probe.completed TO crm_app",
		"GRANT USAGE ON SEQUENCE probe.completed_n_seq TO crm_app",
	)
	var (
		mu       sync.Mutex
		failOnce map[uuid.UUID]bool
	)
	k := kind("t.prop")
	h := newFH(k, func(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
		mu.Lock()
		fail := failOnce[j.ID] && j.Attempts == 1
		mu.Unlock()
		if fail {
			return nil, errTemp
		}
		_, err := tx.Exec(ctx, "INSERT INTO probe.completed (job_id) VALUES ($1)", j.ID)
		return nil, err //nolint:wrapcheck // test
	})
	w1, w2 := worker(e, "w1", h), worker(e, "w2", h)
	run := 0
	rapid.Check(t, func(rt *rapid.T) {
		run++
		e.ownerExec(t, "DELETE FROM probe.completed", "UPDATE platform.jobs SET status = 'expired' WHERE status <> 'done'")
		nKeys := rapid.IntRange(1, 4).Draw(rt, "keys")
		nJobs := rapid.IntRange(5, 40).Draw(rt, "jobs")
		jobs := make([]propJob, 0, nJobs)
		fails := map[uuid.UUID]bool{}
		for i := range nJobs {
			key := fmt.Sprintf("r%d-k%d", run, rapid.IntRange(0, nKeys-1).Draw(rt, "key"))
			seq := int64(rapid.IntRange(1, 10).Draw(rt, "seq"))
			id := e.enqueue(t, k, queue.NewJob{DedupeKey: fmt.Sprintf("r%d-%d", run, i), OrderKey: key, OrderSeq: seq})
			jobs = append(jobs, propJob{id, key, seq})
			if rapid.Bool().Draw(rt, "fail_first") {
				fails[id] = true
			}
		}
		mu.Lock()
		failOnce = fails
		mu.Unlock()

		runUntilDone(rt, e, t, run, nJobs, w1, w2)
		checkProperty(rt, e, t, jobs)
	})
}

func checkProperty(rt *rapid.T, e *env, t *testing.T, jobs []propJob) {
	rows, err := e.owner.Pool.Query(context.Background(), "SELECT job_id FROM probe.completed ORDER BY n")
	if err != nil {
		rt.Fatal(err)
	}
	completed, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		rt.Fatal(err)
	}
	byID := map[uuid.UUID]propJob{}
	for _, j := range jobs {
		byID[j.id] = j
	}
	seen := map[uuid.UUID]int{}
	last := map[string]propJob{}
	for _, id := range completed {
		seen[id]++
		j := byID[id]
		if prev, ok := last[j.orderKey]; ok && (prev.seq > j.seq || (prev.seq == j.seq && prev.id.String() > j.id.String())) {
			rt.Fatalf("%s: %v (seq %d) hoàn tất sau %v (seq %d)", j.orderKey, j.id, j.seq, prev.id, prev.seq)
		}
		last[j.orderKey] = j
	}
	for _, j := range jobs {
		if seen[j.id] != 1 {
			rt.Fatalf("việc %v hoàn tất %d lần", j.id, seen[j.id])
		}
		if r := e.job(t, j.id); r.Status != "done" {
			rt.Fatalf("việc %v kẹt %s", j.id, r.Status)
		}
	}
}

// runUntilDone cho hai worker chạy song song từng vòng (limit ngẫu nhiên) tới khi mọi việc của lượt run xong.
func runUntilDone(rt *rapid.T, e *env, t *testing.T, run, nJobs int, w1, w2 *queue.Worker) {
	for round := 0; ; round++ {
		if round > 4*nJobs+10 {
			rt.Fatalf("chưa xong sau %d vòng", round)
		}
		var wg sync.WaitGroup
		for _, w := range []*queue.Worker{w1, w2} {
			limit := rapid.IntRange(1, 4).Draw(rt, "limit")
			wg.Go(func() {
				if _, err := w.RunOnce(context.Background(), limit); err != nil {
					rt.Error(err)
				}
			})
		}
		wg.Wait()
		e.makeDue(t) // lỗi tạm hẹn chạy lại sau 5 s: đưa về hạn ngay
		if e.count(t, "SELECT count(*) FROM platform.jobs WHERE status IN ('pending', 'running') AND dedupe_key LIKE $1",
			fmt.Sprintf("r%d-%%", run)) == 0 {
			break
		}
	}
}
