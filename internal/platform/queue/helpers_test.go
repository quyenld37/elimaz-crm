package queue_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

// env: database thử đã migrate, pool crm_app (như tiến trình worker, đủ kết nối cho hai worker và LISTEN) và pool chủ
// bảng (dựng dữ liệu, đọc và sửa trạng thái việc từ "bên ngoài").
type env struct {
	app   *db.DB
	owner *db.DB
	q     queue.JobQueue
}

func newEnv(t testing.TB) *env {
	t.Helper()
	_, ownerURL, appURL := testkit.StartPostgres(t)
	return openEnv(t, ownerURL, appURL)
}

// newSeededEnv như newEnv trên database có sẵn dữ liệu s (testkit.StartPostgresSeeded).
func newSeededEnv(t testing.TB, s testkit.Seed) *env {
	t.Helper()
	_, ownerURL, appURL := testkit.StartPostgresSeeded(t, s)
	return openEnv(t, ownerURL, appURL)
}

// openEnv mở hai pool tới database thử đã có và dựng schema probe.
func openEnv(t testing.TB, ownerURL, appURL string) *env {
	t.Helper()
	e := &env{q: queue.NewQueue()}
	for _, p := range []struct {
		dst **db.DB
		url string
		max int32
	}{{&e.app, appURL, 24}, {&e.owner, ownerURL, 4}} {
		d, err := db.Open(t.Context(), p.url, db.PoolOptions{MaxConns: p.max})
		if err != nil {
			t.Fatalf("mở pool thử: %v", err)
		}
		t.Cleanup(d.Pool.Close)
		*p.dst = d
	}
	e.ownerExec(t,
		"CREATE SCHEMA probe",
		"GRANT USAGE ON SCHEMA probe TO crm_app",
		// probe.side: "nghiệp vụ" giả, handler chèn một dòng; không RLS.
		"CREATE TABLE probe.side (job_id uuid NOT NULL, note text NOT NULL DEFAULT '')",
		"GRANT SELECT, INSERT ON probe.side TO crm_app",
	)
	return e
}

func (e *env) ownerExec(t testing.TB, qs ...string) {
	t.Helper()
	for _, q := range qs {
		if _, err := e.owner.Pool.Exec(context.Background(), q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func (e *env) tenant(t testing.TB, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := e.owner.Pool.QueryRow(t.Context(), "INSERT INTO platform.tenants (name) VALUES ($1) RETURNING id", name).Scan(&id); err != nil {
		t.Fatalf("chèn tenant: %v", err)
	}
	return id
}

// enqueue ghi một việc trong transaction riêng của crm_app.
func (e *env) enqueue(t testing.TB, k queue.Kind, j queue.NewJob) uuid.UUID {
	t.Helper()
	id, _ := e.tryEnqueue(t, k, j)
	return id
}

func (e *env) tryEnqueue(t testing.TB, k queue.Kind, j queue.NewJob) (uuid.UUID, bool) {
	t.Helper()
	var (
		id      uuid.UUID
		created bool
	)
	err := e.app.RetryTx(t.Context(), db.TxOptions{}, func(tx pgx.Tx) error {
		var err error
		id, created, err = e.q.Enqueue(t.Context(), tx, k, j)
		return err //nolint:wrapcheck // test
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	return id, created
}

// jobRow là trạng thái một việc đọc bằng chủ bảng.
type jobRow struct {
	Status     string
	Attempts   int
	RunInS     float64 // run_at − now(), giây
	RunAt      time.Time
	LastError  *string
	LockedBy   *string
	Result     []byte
	FinishedAt *time.Time
}

func (e *env) job(t testing.TB, id uuid.UUID) jobRow {
	t.Helper()
	var r jobRow
	err := e.owner.Pool.QueryRow(context.Background(), `SELECT status, attempts, extract(epoch FROM run_at - now())::float8,
		run_at, last_error, locked_by, result, finished_at FROM platform.jobs WHERE id = $1`, id).
		Scan(&r.Status, &r.Attempts, &r.RunInS, &r.RunAt, &r.LastError, &r.LockedBy, &r.Result, &r.FinishedAt)
	if err != nil {
		t.Fatalf("đọc việc %s: %v", id, err)
	}
	return r
}

func (e *env) count(t testing.TB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := e.owner.Pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// dbNow là giờ của database (cùng đồng hồ với now() trong câu lệnh của Worker).
func (e *env) dbNow(t testing.TB) time.Time {
	t.Helper()
	var now time.Time
	if err := e.owner.Pool.QueryRow(context.Background(), "SELECT clock_timestamp()").Scan(&now); err != nil {
		t.Fatalf("đọc giờ database: %v", err)
	}
	return now
}

// makeDue đưa mọi việc pending về hạn chạy ngay (thay cho chờ lịch thử lại).
func (e *env) makeDue(t testing.TB) {
	t.Helper()
	e.ownerExec(t, "UPDATE platform.jobs SET run_at = now() WHERE status = 'pending'")
}

// fh là handler thử: fn quyết định kết quả; đếm số lần Handle và ghi các lần OnTerminal.
type fh struct {
	kind queue.Kind
	fn   func(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error)

	mu        sync.Mutex
	calls     map[uuid.UUID]int
	order     []uuid.UUID
	terminals []string
}

func newFH(k queue.Kind, fn func(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error)) *fh {
	return &fh{kind: k, fn: fn, calls: map[uuid.UUID]int{}}
}

func (h *fh) Kind() queue.Kind { return h.kind }

func (h *fh) Handle(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	h.mu.Lock()
	h.calls[j.ID]++
	h.order = append(h.order, j.ID)
	h.mu.Unlock()
	if h.fn == nil {
		return nil, nil
	}
	return h.fn(ctx, j, tx)
}

func (h *fh) OnTerminal(_ context.Context, j queue.Job, status string, _ pgx.Tx) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.terminals = append(h.terminals, j.ID.String()+":"+status)
	return nil
}

func (h *fh) callsOf(id uuid.UUID) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls[id]
}

func (h *fh) handled() []uuid.UUID {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]uuid.UUID(nil), h.order...)
}

func (h *fh) terminalList() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.terminals...)
}

func kind(name string) queue.Kind { return queue.Kind{Name: name, Policy: queue.DefaultPolicy} }

func worker(e *env, id string, hs ...queue.Handler) *queue.Worker {
	return queue.NewWorker(e.app, hs, queue.WorkerOptions{WorkerID: id, Concurrency: 8})
}

// runOnce gọi RunOnce và làm test đỏ khi lỗi.
func runOnce(t testing.TB, w *queue.Worker, limit int) int {
	t.Helper()
	n, err := w.RunOnce(t.Context(), limit)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	return n
}

func insertSide(ctx context.Context, tx pgx.Tx, j queue.Job) error {
	_, err := tx.Exec(ctx, "INSERT INTO probe.side (job_id) VALUES ($1)", j.ID)
	return err //nolint:wrapcheck // test
}

// sideThen là handler thử: chèn một dòng probe.side, đóng started, rồi trả lỗi của then (chặn tới khi test cho đi).
func sideThen(started chan struct{}, then func(ctx context.Context) error) func(context.Context, queue.Job, pgx.Tx) (json.RawMessage, error) {
	return func(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
		if err := insertSide(ctx, tx, j); err != nil {
			return nil, err
		}
		close(started)
		return nil, then(ctx)
	}
}

// startWorker tạo Worker hỏi mỗi poll và Start nó; Stop(1 s) khi test xong (Stop lần hai không làm gì).
func startWorker(t testing.TB, e *env, poll time.Duration, hs ...queue.Handler) *queue.Worker {
	t.Helper()
	w := queue.NewWorker(e.app, hs, queue.WorkerOptions{WorkerID: "w1", Poll: poll})
	if err := w.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Stop(time.Second) })
	return w
}

// enqueuePermanentError ghi một việc mà handler luôn trả ErrPermanent (lỗi đã lường trước, không thử lại).
func (e *env) enqueuePermanentError(t testing.TB) (*fh, uuid.UUID) {
	t.Helper()
	h := newFH(kind("t.perm"), func(context.Context, queue.Job, pgx.Tx) (json.RawMessage, error) {
		return nil, fmt.Errorf("payload sai: %w", queue.ErrPermanent)
	})
	return h, e.enqueue(t, h.kind, queue.NewJob{DedupeKey: "k"})
}
