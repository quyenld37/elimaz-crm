package queue_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
)

func orderedWorker(e *env, hs ...queue.Handler) *queue.Worker {
	return queue.NewWorker(e.app, hs, queue.WorkerOptions{WorkerID: "w1", Concurrency: 3})
}

// drain gọi RunOnce tới khi không còn việc nhận được (tối đa 20 vòng).
func drain(t *testing.T, w *queue.Worker) {
	t.Helper()
	for range 20 {
		if runOnce(t, w, 10) == 0 {
			return
		}
	}
}

// Ba việc cùng OrderKey ghi theo OrderSeq 3, 2, 1 chạy theo 1, 2, 3 dù Concurrency 3.
func TestOrderingBySeq(t *testing.T) {
	e := newEnv(t)
	h := newFH(kind("t.ord"), nil)
	ids := map[int64]uuid.UUID{}
	for _, seq := range []int64{3, 2, 1} {
		ids[seq] = e.enqueue(t, h.kind, queue.NewJob{DedupeKey: fmt.Sprint(seq), OrderKey: "order:1", OrderSeq: seq})
	}
	drain(t, orderedWorker(e, h))
	if got, want := h.handled(), []uuid.UUID{ids[1], ids[2], ids[3]}; !slices.Equal(got, want) {
		t.Fatalf("thứ tự Handle %v, muốn %v", got, want)
	}
}

func deadFirst(mode string) (*fh, func(e *env, t *testing.T) (first, second uuid.UUID)) {
	k := queue.Kind{Name: "t.block", Policy: queue.Policy{OrderMode: mode}}
	var firstID uuid.UUID
	h := newFH(k, func(_ context.Context, j queue.Job, _ pgx.Tx) (json.RawMessage, error) {
		if j.ID == firstID {
			return nil, queue.ErrPermanent
		}
		return nil, nil
	})
	return h, func(e *env, t *testing.T) (uuid.UUID, uuid.UUID) {
		firstID = e.enqueue(t, k, queue.NewJob{DedupeKey: "1", OrderKey: "o", OrderSeq: 1})
		return firstID, e.enqueue(t, k, queue.NewJob{DedupeKey: "2", OrderKey: "o", OrderSeq: 2})
	}
}

// Sự kiện nội bộ (OrderStrict, cũng là mặc định ""): việc đầu hàng dead chặn việc sau của đúng đối tượng đó và báo
// động bằng jobs_dead, không âm thầm bỏ qua (spec 04 mục 5.1).
func TestStrictDeadBlocksAndAlerts(t *testing.T) {
	for _, mode := range []string{"", queue.OrderStrict} {
		t.Run("mode="+mode, func(t *testing.T) {
			e := newEnv(t)
			h, add := deadFirst(mode)
			first, second := add(e, t)
			reg := metrics.NewRegistry()
			drain(t, queue.NewWorker(e.app, []queue.Handler{h}, queue.WorkerOptions{WorkerID: "w1", Concurrency: 3, Metrics: reg}))
			if r := e.job(t, first); r.Status != "dead" {
				t.Fatalf("việc đầu: %+v", r)
			}
			if r := e.job(t, second); r.Status != "pending" || h.callsOf(second) != 0 {
				t.Fatalf("việc sau phải bị chặn: %+v", r)
			}
			if !hasCounter(reg.Drain(), "jobs_dead", "t.block", 1) {
				t.Fatal("thiếu báo động jobs_dead")
			}
		})
	}
}

// Sự kiện nội bộ (strict): việc đầu đang chờ thử lại (pending, run_at tương lai) vẫn chặn việc sau.
func TestRetryingHeadBlocks(t *testing.T) {
	e := newEnv(t)
	var firstID uuid.UUID
	h := newFH(kind("t.head"), func(_ context.Context, j queue.Job, _ pgx.Tx) (json.RawMessage, error) {
		if j.ID == firstID {
			return nil, errTemp
		}
		return nil, nil
	})
	firstID = e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "1", OrderKey: "o", OrderSeq: 1})
	second := e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "2", OrderKey: "o", OrderSeq: 2})
	drain(t, orderedWorker(e, h))
	if h.callsOf(second) != 0 || e.job(t, second).Status != "pending" {
		t.Fatalf("việc sau chạy khi việc đầu còn chờ thử lại: %+v", e.job(t, second))
	}
}

// Cùng OrderKey nhưng khác tenant (thứ tự theo (tenant_id, order_key), không theo kind) (và việc không tenant) không chặn nhau.
func TestSameOrderKeyOtherTenantDoesNotBlock(t *testing.T) {
	e := newEnv(t)
	a, b := e.tenant(t, "A"), e.tenant(t, "B")
	h := newFH(kind("t.ten"), func(context.Context, queue.Job, pgx.Tx) (json.RawMessage, error) { return nil, errTemp })
	ok := newFH(kind("t.ten2"), nil)
	// Việc đầu của A luôn lỗi tạm (chờ thử lại) → chặn việc sau của A; B và không tenant không bị chặn.
	e.enqueue(t, h.kind, queue.NewJob{TenantID: &a, DedupeKey: "a1", OrderKey: "o", OrderSeq: 1})
	k := ok.kind
	laterA := e.enqueue(t, k, queue.NewJob{TenantID: &a, DedupeKey: "a2", OrderKey: "o", OrderSeq: 2})
	onlyB := e.enqueue(t, k, queue.NewJob{TenantID: &b, DedupeKey: "b2", OrderKey: "o", OrderSeq: 2})
	noTenant := e.enqueue(t, k, queue.NewJob{DedupeKey: "n2", OrderKey: "o", OrderSeq: 2})
	drain(t, orderedWorker(e, h, ok))
	if e.job(t, laterA).Status != "pending" {
		t.Fatalf("việc sau của A phải bị chặn: %+v", e.job(t, laterA))
	}
	if e.job(t, onlyB).Status != "done" || e.job(t, noTenant).Status != "done" {
		t.Fatalf("B và không tenant không bị chặn: %+v %+v", e.job(t, onlyB), e.job(t, noTenant))
	}
}
