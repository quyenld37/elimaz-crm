package queue_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

// Câu nhận việc khi hàng chờ dài (Task 394, thử tải M0 phát hiện 2): trước đây câu con "việc đứng trước" so tenant bằng
// IS NOT DISTINCT FROM nên không dùng được index, quét tuần tự jobs cho mỗi ứng viên (2,4 s ở 78.000 việc chờ).

// claimBufferBudget là trần số trang (8 KiB) câu nhận việc chạm ở bộ dữ liệu của longQueueSeed: mỗi việc bị chặn đứng
// trước trong hàng chờ tốn ~4 trang (đọc việc, dò index jobs_order_key, đọc việc đứng trước), 10.000 việc → ~40.000.
// Đây là tiêu chí không phụ thuộc máy bận hay rảnh (thời gian thì có). Câu cũ quét bảng: hàng triệu trang. Trần thời
// gian 50 ms nằm ở test có nhãn perf (claimplan_perf_test.go, make perf-test), không ở đây (Task 400).
const claimBufferBudget = 60_000

// planStats là phần của kế hoạch EXPLAIN ANALYZE mà test cần.
type planStats struct {
	execMs   float64
	buffers  int64    // trang đọc từ bộ đệm hoặc đĩa (nút gốc, gồm mọi nút con)
	seqScans []string // tên bảng của mọi nút Seq Scan
}

type planJSON struct {
	NodeType string     `json:"Node Type"`
	Relation string     `json:"Relation Name"`
	Hit      int64      `json:"Shared Hit Blocks"`
	Read     int64      `json:"Shared Read Blocks"`
	Plans    []planJSON `json:"Plans"`
}

func (p planJSON) seqScans(out []string) []string {
	if p.NodeType == "Seq Scan" {
		out = append(out, p.Relation)
	}
	for _, c := range p.Plans {
		out = c.seqScans(out)
	}
	return out
}

// explainAnalyze chạy EXPLAIN (ANALYZE) câu sql bằng crm_app trong một transaction rồi lùi (câu nhận việc là UPDATE: lùi
// để lần đo sau thấy đúng dữ liệu cũ). Chạy n lần, trả lần nhanh nhất (máy thử bận, nhiều phiên song song; lần đầu
// còn nạp trang vào bộ đệm).
func explainAnalyze(t *testing.T, e *env, n int, sql string, args ...any) planStats {
	t.Helper()
	var best planStats
	for i := range n {
		var raw []byte
		err := pgx.BeginFunc(t.Context(), e.app.Pool, func(tx pgx.Tx) error {
			if err := tx.QueryRow(t.Context(), "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+sql, args...).Scan(&raw); err != nil {
				return err //nolint:wrapcheck // test
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("EXPLAIN: %v", err)
		}
		var top []struct {
			Plan planJSON `json:"Plan"`
			Exec float64  `json:"Execution Time"`
		}
		if err := json.Unmarshal(raw, &top); err != nil || len(top) != 1 {
			t.Fatalf("đọc kế hoạch: %v", err)
		}
		s := planStats{execMs: top[0].Exec, buffers: top[0].Plan.Hit + top[0].Plan.Read, seqScans: top[0].Plan.seqScans(nil)}
		if i == 0 || s.execMs < best.execMs {
			best = s
		}
	}
	return best
}

// longQueueSeed dựng 100.000 việc pending tới hạn: 10.000 việc cùng order_key "hot" của tenant A ở đầu hàng (run_at
// sớm nhất, chế độ mode, việc đầu đang running khi headRunning), 30.000 việc không tenant chung 3.000 order_key, 30.000
// việc của tenant B mỗi việc một order_key, 30.000 việc không order_key; cùng 50.000 việc done; VACUUM ANALYZE. Chạy
// bằng chủ bảng như trước Task 423; tenant A, B mã cố định để dựng một lần làm database mẫu. Hạn giữ chỗ của việc
// đầu đang running là now() + 1 ngày (trước Task 423: 1 giờ): dữ liệu của mẫu cũ tới một giờ, hạn 1 giờ có thể vừa hết
// lúc đo.
func longQueueSeed(mode string, headRunning bool) testkit.Seed {
	a, b := testkit.FixedUUID("queue-long-queue/A"), testkit.FixedUUID("queue-long-queue/B")
	head := "'pending', NULL, NULL"
	if headRunning {
		head = "CASE WHEN g = 1 THEN 'running' ELSE 'pending' END, CASE WHEN g = 1 THEN now() + interval '1 day' END, CASE WHEN g = 1 THEN 'w0' END"
	}
	return testkit.Seed{Name: fmt.Sprintf("queue-long-queue/%s/%v", mode, headRunning), SQL: []string{
		"SET ROLE crm_owner",
		fmt.Sprintf("INSERT INTO platform.tenants (id, name) VALUES ('%s', 'A'), ('%s', 'B')", a, b),
		fmt.Sprintf(`INSERT INTO platform.jobs (tenant_id, kind, mode, dedupe_key, order_key, order_seq, order_mode,
			on_lease_expired, max_attempts, status, lease_until, locked_by, run_at)
			SELECT '%s', 'inbound.t.hot', 'transactional', 'h' || g, 'hot', g, '%s', 'retry', 8, %s,
				now() - interval '2 hours' + g * interval '1 ms'
			FROM generate_series(1, 10000) g`, a, mode, head),
		`INSERT INTO platform.jobs (kind, mode, dedupe_key, order_key, order_seq, order_mode, on_lease_expired,
			max_attempts, run_at)
			SELECT 't.null', 'transactional', 'n' || g, 'n:' || (g % 3000), g, 'strict', 'retry', 8,
				now() - interval '1 hour' + g * interval '1 ms'
			FROM generate_series(1, 30000) g`,
		fmt.Sprintf(`INSERT INTO platform.jobs (tenant_id, kind, mode, dedupe_key, order_key, order_seq, order_mode,
			on_lease_expired, max_attempts, run_at)
			SELECT '%s', 'inbound.t.b', 'transactional', 'b' || g, 'b:' || g, 1, 'best_effort', 'retry', 8,
				now() - interval '1 hour' + g * interval '1 ms'
			FROM generate_series(1, 30000) g`, b),
		fmt.Sprintf(`INSERT INTO platform.jobs (tenant_id, kind, mode, dedupe_key, on_lease_expired, max_attempts, run_at)
			SELECT '%s', 't.plain', 'transactional', 'p' || g, 'retry', 8, now() - interval '1 hour' + g * interval '1 ms'
			FROM generate_series(1, 30000) g`, a),
		fmt.Sprintf(`INSERT INTO platform.jobs (tenant_id, kind, mode, dedupe_key, order_key, order_seq, on_lease_expired,
			max_attempts, status, finished_at)
			SELECT '%s', 't.done', 'transactional', 'd' || g, 'hot', g, 'retry', 8, 'done', now()
			FROM generate_series(1, 50000) g`, a),
		"VACUUM ANALYZE platform.jobs",
	}}
}

// eachLongQueueCase chạy check (mỗi ca một subtest, database riêng) với kết quả EXPLAIN ANALYZE của câu nhận việc trên
// hàng chờ dài của longQueueSeed, ở bốn ca: hai chế độ thứ tự, việc đầu của khóa nóng đang chờ hay đang running. Dùng
// chung cho test đúng/sai ở đây và test thời gian (claimplan_perf_test.go, nhãn perf).
func eachLongQueueCase(t *testing.T, check func(t *testing.T, s planStats)) {
	t.Helper()
	for _, c := range []struct {
		mode        string
		headRunning bool
	}{{"strict", false}, {"strict", true}, {"best_effort", false}, {"best_effort", true}} {
		t.Run(fmt.Sprintf("%s/head_running=%v", c.mode, c.headRunning), func(t *testing.T) {
			e := newSeededEnv(t, longQueueSeed(c.mode, c.headRunning))
			check(t, explainAnalyze(t, e, 5, claimSQL(t), int64(90000), "explain", false, int32(10)))
		})
	}
}

// 100.000 việc chờ, 10.000 cùng order_key của một tenant ở đầu hàng: câu nhận việc không Seq Scan trên jobs và chạm
// dưới claimBufferBudget trang, ở cả bốn ca của eachLongQueueCase. Thời gian đo ở TestClaimLongQueueTimeBudget (nhãn
// perf).
func TestClaimLongQueueSameOrderKey(t *testing.T) {
	eachLongQueueCase(t, func(t *testing.T, s planStats) {
		t.Logf("ClaimJobs ở 100.000 việc chờ: %.2f ms, %d trang, Seq Scan %v", s.execMs, s.buffers, s.seqScans)
		for _, rel := range s.seqScans {
			if rel == "jobs" {
				t.Errorf("kế hoạch có Seq Scan trên jobs")
			}
		}
		if s.buffers > claimBufferBudget {
			t.Errorf("ClaimJobs chạm %d trang, trần %d", s.buffers, claimBufferBudget)
		}
	})
}

// Việc không tenant (chưa định tuyến) vẫn theo thứ tự trong nhóm không tenant: việc đầu chờ thử lại chặn việc sau cùng
// order_key (strict), việc đang running chặn việc sau (best_effort); việc của tenant A cùng order_key không bị chặn và
// không chặn nhóm không tenant.
func TestNullTenantOrdering(t *testing.T) {
	for _, mode := range []string{queue.OrderStrict, queue.OrderBestEffort} {
		t.Run(mode, func(t *testing.T) {
			e := newEnv(t)
			a := e.tenant(t, "A")
			k := queue.Kind{Name: "t.nullten", Policy: queue.Policy{OrderMode: mode}}
			ok := newFH(k, nil)
			first := e.enqueue(t, k, queue.NewJob{DedupeKey: "n1", OrderKey: "o", OrderSeq: 1})
			second := e.enqueue(t, k, queue.NewJob{DedupeKey: "n2", OrderKey: "o", OrderSeq: 2})
			onlyA := e.enqueue(t, k, queue.NewJob{TenantID: &a, DedupeKey: "a2", OrderKey: "o", OrderSeq: 2})
			// Việc đầu không tenant: strict → chờ thử lại (pending, run_at tương lai); best_effort → đang running.
			if mode == queue.OrderStrict {
				e.ownerExec(t, fmt.Sprintf(`UPDATE platform.jobs SET run_at = now() + interval '1 hour', attempts = 1
					WHERE id = '%s'`, first))
			} else {
				e.ownerExec(t, fmt.Sprintf(`UPDATE platform.jobs SET status = 'running', attempts = 1,
					lease_until = now() + interval '1 hour', locked_by = 'other' WHERE id = '%s'`, first))
			}
			e.drain(t, orderedWorker(e, ok))
			if r := e.job(t, second); r.Status != "pending" || ok.callsOf(second) != 0 {
				t.Fatalf("việc sau không tenant phải bị chặn: %+v", r)
			}
			if r := e.job(t, onlyA); r.Status != "done" {
				t.Fatalf("việc của A cùng order_key không bị nhóm không tenant chặn: %+v", r)
			}
			// Việc đầu xong → việc sau được nhận.
			e.ownerExec(t, fmt.Sprintf(`UPDATE platform.jobs SET status = 'done', finished_at = now(), lease_until = NULL,
				locked_by = NULL WHERE id = '%s'`, first))
			e.drain(t, orderedWorker(e, ok))
			if r := e.job(t, second); r.Status != "done" {
				t.Fatalf("việc sau không tenant phải chạy sau khi việc đầu xong: %+v", r)
			}
		})
	}
}
