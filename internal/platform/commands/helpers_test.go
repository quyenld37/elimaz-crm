package commands_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

// env: database thử đã migrate, pool crm_app (như tiến trình worker) và pool chủ bảng (dựng dữ liệu, đọc và sửa
// trạng thái việc từ "bên ngoài").
type env struct {
	app    *db.DB
	owner  *db.DB
	q      queue.JobQueue
	tenant uuid.UUID
}

func newEnv(t testing.TB) *env {
	t.Helper()
	s := testkit.OpenStaff(t)
	return &env{app: s.App, owner: s.Owner, q: queue.NewQueue(), tenant: s.Tenant(t, "A")}
}

func (e *env) ownerExec(t testing.TB, q string, args ...any) {
	t.Helper()
	if _, err := e.owner.Pool.Exec(context.Background(), q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// enqueue ghi một lệnh của tenant thử trong transaction riêng của crm_app.
func (e *env) enqueue(t testing.TB, k queue.Kind, key string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := e.app.RetryTx(t.Context(), db.TxOptions{}, func(tx pgx.Tx) error {
		var err error
		id, _, err = e.q.Enqueue(t.Context(), tx, k, queue.NewJob{TenantID: &e.tenant, DedupeKey: key})
		return err //nolint:wrapcheck // test
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	return id
}

type jobRow struct {
	Status    string
	Attempts  int
	RunInS    float64
	LastError *string
	Result    []byte
}

func (e *env) job(t testing.TB, id uuid.UUID) jobRow {
	t.Helper()
	var r jobRow
	if err := e.owner.Pool.QueryRow(context.Background(), `SELECT status, attempts,
		extract(epoch FROM run_at - now())::float8, last_error, result FROM platform.jobs WHERE id = $1`, id).
		Scan(&r.Status, &r.Attempts, &r.RunInS, &r.LastError, &r.Result); err != nil {
		t.Fatalf("đọc việc %s: %v", id, err)
	}
	return r
}

// makeDue đưa mọi việc pending về hạn chạy ngay (thay cho chờ lịch thử lại).
func (e *env) makeDue(t testing.TB) {
	t.Helper()
	e.ownerExec(t, "UPDATE platform.jobs SET run_at = now() WHERE status = 'pending'")
}

// crash giả lập worker chết sau khi nhận việc: running, giữ chỗ đã qua, của một worker khác.
func (e *env) crash(t testing.TB, id uuid.UUID) {
	t.Helper()
	e.ownerExec(t, `UPDATE platform.jobs SET status = 'running', attempts = attempts + 1, locked_by = 'dead-worker',
		lease_until = now() - interval '1 second' WHERE id = $1`, id)
}

// cmdH là lệnh thử: gửi ref = mã việc ra FakeExternal, đổi lỗi của nó thành Outcome. Hiện thực DoneHook và
// TerminalHook để test đếm.
type cmdH struct {
	kind       queue.Kind
	ext        *testkit.FakeExternal
	autoRetry  bool
	retryAfter time.Duration
	// exec khác nil thì thay cho gửi ra FakeExternal.
	exec func(ctx context.Context, j queue.Job) (commands.Outcome, error)

	mu        sync.Mutex
	executes  int
	dones     []string
	terminals []string
	tenantOK  bool // OnDone chạy trong ngữ cảnh tenant của việc
}

func newCmd(name string, ext *testkit.FakeExternal) *cmdH {
	return &cmdH{kind: commands.CommandKind(name, 0, time.Second), ext: ext, autoRetry: true}
}

func (h *cmdH) Kind() queue.Kind       { return h.kind }
func (h *cmdH) AutoRetryNotSent() bool { return h.autoRetry }

func (h *cmdH) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	h.mu.Lock()
	h.executes++
	h.mu.Unlock()
	if h.exec != nil {
		return h.exec(ctx, j)
	}
	err := h.ext.Send(ctx, j.ID.String())
	switch {
	case err == nil:
		return commands.Outcome{Status: commands.StatusDone, Result: json.RawMessage(`{"external_id":"x-1"}`)}, nil
	case errors.Is(err, testkit.ErrFakeNotSent):
		return commands.Outcome{Status: commands.StatusNotSent, Reason: "connect refused", RetryAfter: h.retryAfter}, nil
	case errors.Is(err, testkit.ErrFakeRejected):
		return commands.Outcome{Status: commands.StatusRejected, Reason: "policy"}, nil
	}
	return commands.Outcome{}, err //nolint:wrapcheck // test: lỗi của bên ngoài giả trả nguyên
}

func (h *cmdH) OnDone(ctx context.Context, j queue.Job, _ json.RawMessage, tx pgx.Tx) error {
	var cur string
	if err := tx.QueryRow(ctx, "SELECT current_setting('app.tenant_id', true)").Scan(&cur); err != nil {
		return err //nolint:wrapcheck // test
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dones = append(h.dones, j.ID.String())
	h.tenantOK = j.TenantID != nil && cur == j.TenantID.String()
	return nil
}

func (h *cmdH) OnTerminal(_ context.Context, j queue.Job, status string, _ pgx.Tx) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.terminals = append(h.terminals, fmt.Sprintf("%s:%s", j.ID, status))
	return nil
}

func (h *cmdH) snapshot() (executes int, dones, terminals []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.executes, append([]string(nil), h.dones...), append([]string(nil), h.terminals...)
}

// lookH là cmdH có Looker; look nil thì nói thật theo FakeExternal (đã ghi nhận → done, chưa → not_sent).
type lookH struct {
	*cmdH
	look func(ctx context.Context, j queue.Job) (string, error)
}

func (h *lookH) Lookup(ctx context.Context, j queue.Job) (string, error) {
	if h.look != nil {
		return h.look(ctx, j)
	}
	if h.ext.Has(j.ID.String()) {
		return commands.StatusDone, nil
	}
	return commands.StatusNotSent, nil
}

func worker(e *env, id string, hs ...commands.Handler) *queue.Worker {
	return queue.NewWorker(e.app, nil, queue.WorkerOptions{WorkerID: id, Commands: commands.NewRunner(e.app, hs)})
}

func runOnce(t testing.TB, w *queue.Worker) {
	t.Helper()
	if _, err := w.RunOnce(t.Context(), 10); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
}
