package reporting_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/reportseed"
)

// runDay là ngày của kịch bản chín đơn trong test lần chạy nền.
const runDay = "2026-11-17"

// runLimits là giới hạn mặc định của test (5 lần đang chờ mỗi người, như REPORT_MAX_PENDING_PER_USER).
func runLimits() reporting.RunLimits {
	return reporting.RunLimits{MaxPendingPerUser: 5, JSONTTL: reporting.JSONTTL, FileTTL: 168 * time.Hour, CustomerTTL: 24 * time.Hour}
}

// runsEnv là một tenant có kịch bản chín đơn, nhân viên thật trong identity (worker nạp lại người tạo) và bus realtime.
type runsEnv struct {
	st     *testkit.Staff
	tenant uuid.UUID
	sc     reportseed.Scenario
	hub    *realtime.PGHub
}

func newRunsEnv(t *testing.T) *runsEnv {
	t.Helper()
	st := testkit.OpenStaff(t)
	tenant := st.Tenant(t, "a")
	seed := reportseed.New(t, st.Owner, tenant)
	e := &runsEnv{st: st, tenant: tenant, sc: reportseed.NineOrders(seed, runDay)}
	e.hub = realtime.NewPGHub(realtime.PGHubOptions{URL: st.App.Pool.Config().ConnString()})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.hub.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	waitListening(t, e.hub)
	return e
}

// waitListening chờ hub LISTEN được (tối đa 10 s) để báo hiệu phát sau đó không bị lỡ.
func waitListening(t *testing.T, hub *realtime.PGHub) {
	t.Helper()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	timeout := time.After(10 * time.Second)
	for {
		if ok, _ := hub.Status(); ok {
			return
		}
		select {
		case <-tick.C:
		case <-timeout:
			t.Fatal("hub chưa nghe được")
		}
	}
}

// user tạo nhân viên active có perms ở phạm vi (scopeType, scopeID) và trả AuthUser như lúc yêu cầu.
func (e *runsEnv) user(t *testing.T, email string, perms []string, scopeType, scopeID string) identity.AuthUser {
	t.Helper()
	id := e.st.User(t, e.tenant, email, "active")
	e.st.Grant(t, e.tenant, id, perms, scopeType, scopeID)
	var u identity.AuthUser
	e.st.Exec(t, e.tenant, func(tx pgx.Tx) error {
		var err error
		u, err = identity.LoadUserByID(t.Context(), tx, id)
		return err //nolint:wrapcheck // test
	})
	return u
}

// handler dựng RunHandler với cấu hình cfg (chỗ, hạn) và sổ defs (nil: Defs()).
func (e *runsEnv) handler(cfg config.ReportConfig, defs map[reporting.Key]reporting.Def) *reporting.RunHandler {
	return reporting.NewRunHandler(reporting.Deps{
		DB: e.st.App, Bus: e.hub, Queue: queue.NewQueue(), Cfg: config.Config{Report: cfg}, Defs: defs,
	})
}

func (e *runsEnv) worker(h *reporting.RunHandler) *queue.Worker {
	return queue.NewWorker(e.st.App, nil, queue.WorkerOptions{Commands: commands.NewRunner(e.st.App, []commands.Handler{h})})
}

func (e *runsEnv) create(t *testing.T, u identity.AuthUser, in reporting.CreateRunInput, l reporting.RunLimits) (reporting.Run, error) {
	t.Helper()
	var run reporting.Run
	err := tenancy.WithTenant(t.Context(), e.st.App, e.tenant, func(tx pgx.Tx) error {
		var err error
		run, err = reporting.CreateRun(t.Context(), tx, queue.NewQueue(), u, in, time.Now(), l)
		return err //nolint:wrapcheck // test
	})
	return run, err //nolint:wrapcheck // test
}

func (e *runsEnv) mustCreate(t *testing.T, u identity.AuthUser, in reporting.CreateRunInput) reporting.Run {
	t.Helper()
	run, err := e.create(t, u, in, runLimits())
	if err != nil {
		t.Fatalf("tạo lần chạy %+v: %v", in, err)
	}
	return run
}

func (e *runsEnv) get(t *testing.T, u identity.AuthUser, f reporting.Family, id uuid.UUID) (reporting.Run, error) {
	t.Helper()
	var run reporting.Run
	err := tenancy.WithTenant(t.Context(), e.st.App, u.TenantID, func(tx pgx.Tx) error {
		var err error
		run, err = reporting.GetRun(t.Context(), tx, u, f, id)
		return err //nolint:wrapcheck // test
	})
	return run, err //nolint:wrapcheck // test
}

// row đọc trạng thái, lý do và kết quả của lần chạy bằng chủ bảng.
func (e *runsEnv) row(t *testing.T, id uuid.UUID) (status, reason string, result []byte) {
	t.Helper()
	e.st.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT status, coalesce(reason, ''), result FROM reporting.report_runs WHERE id = $1`,
			id).Scan(&status, &reason, &result)
	})
	return status, reason, result
}

// job đọc việc của lần chạy bằng chủ bảng.
func (e *runsEnv) job(t *testing.T, runID uuid.UUID) queue.Job {
	t.Helper()
	var j queue.Job
	var tenant uuid.UUID
	e.st.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT id, tenant_id, kind, payload, attempts, created_at FROM platform.jobs
			WHERE kind = 'command.report_run' AND dedupe_key = $1`, runID.String()).
			Scan(&j.ID, &tenant, &j.Kind, &j.Payload, &j.Attempts, &j.CreatedAt)
	})
	j.TenantID = &tenant
	return j
}

func (e *runsEnv) jobStatus(t *testing.T, runID uuid.UUID) string {
	t.Helper()
	var s string
	e.st.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT status FROM platform.jobs WHERE kind = 'command.report_run' AND dedupe_key = $1`,
			runID.String()).Scan(&s)
	})
	return s
}

func (e *runsEnv) ownerExec(t *testing.T, q string, args ...any) {
	t.Helper()
	e.st.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), q, args...)
		return err //nolint:wrapcheck // test
	})
}

// subscribe đăng ký báo hiệu cho u; trả kênh mã lần chạy của các khung report.run_finished.
func (e *runsEnv) subscribe(t *testing.T, u identity.AuthUser) <-chan string {
	t.Helper()
	ch := make(chan string, 64)
	cancel := e.hub.Subscribe(u, func(f realtime.Frame) {
		var d struct {
			Topic  string          `json:"topic"`
			Entity realtime.Entity `json:"entity"`
		}
		if f.Event == "changed" && json.Unmarshal(f.Data, &d) == nil && d.Topic == reporting.TopicRunFinished {
			select {
			case ch <- d.Entity.ID:
			default:
			}
		}
	})
	t.Cleanup(cancel)
	return ch
}

func waitSignal(t *testing.T, ch <-chan string, id uuid.UUID, within time.Duration) {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case got := <-ch:
			if got == id.String() {
				return
			}
		case <-deadline:
			t.Fatalf("không nhận báo hiệu report.run_finished của %s trong %v", id, within)
		}
	}
}

func ordersInput(format string) reporting.CreateRunInput {
	return reporting.CreateRunInput{
		Family: reporting.FamilySales, Report: reporting.KeyOrders, Format: format,
		Params: json.RawMessage(`{"from":"` + runDay + `","to":"` + runDay + `","basis":"placed","groupBy":"care_by"}`),
	}
}

// salesAll là quyền xem báo cáo bán hàng (gán phạm vi all trong test).
func salesAll() []string { return []string{"report.sales.read"} }

// Tạo lần chạy bảng báo cáo đơn → queued, đúng 1 lệnh có dedupe_key = mã lần chạy, 1 dòng audit có after.params bằng
// tham số; worker chạy → ready, result bằng đúng kết quả của bản xem tại chỗ cùng tham số; người tạo nhận báo hiệu
// report.run_finished trong 2 s; GetRun trả kết quả.
func TestRuns_CreateAndRun(t *testing.T) {
	t.Parallel()
	e := newRunsEnv(t)
	u := e.user(t, "an@elimaz.test", salesAll(), "all", "")
	run := e.mustCreate(t, u, ordersInput("json"))
	if run.Status != "queued" || run.Family != reporting.FamilySales || run.Report != reporting.KeyOrders ||
		run.Format != "json" || run.RequestedBy != u.UserID || run.RequestedByName == "" || run.Result != nil {
		t.Fatalf("lần chạy mới = %+v", run)
	}
	if n := e.st.CountIn(t, e.tenant, `SELECT count(*) FROM platform.jobs WHERE kind = 'command.report_run' AND dedupe_key = $1`,
		run.ID.String()); n != 1 {
		t.Fatalf("số lệnh = %d, mong 1", n)
	}
	var params []byte
	e.st.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT after -> 'params' FROM identity.audit_logs
			WHERE action = 'report.run_requested' AND entity_type = 'report_run' AND entity_id = $1`, run.ID.String()).Scan(&params)
	})
	var gotParams, wantParams map[string]any
	_ = json.Unmarshal(params, &gotParams)
	_ = json.Unmarshal(ordersInput("json").Params, &wantParams)
	if fmt.Sprint(gotParams) != fmt.Sprint(wantParams) {
		t.Errorf("audit after.params = %s, mong %s", params, ordersInput("json").Params)
	}

	sig := e.subscribe(t, u)
	h := e.handler(config.ReportConfig{}, nil)
	testkit.DrainDue(t, e.st.Owner, e.worker(h))
	waitSignal(t, sig, run.ID, 2*time.Second)

	status, _, result := e.row(t, run.ID)
	if status != "ready" {
		t.Fatalf("trạng thái = %s, mong ready", status)
	}
	p, err := reporting.DecodeParams(reporting.KeyOrders, ordersInput("json").Params)
	if err != nil {
		t.Fatal(err)
	}
	live, err := reporting.NewInteractive(e.st.App, reporting.InteractiveOptions{}).Run(t.Context(), u, reporting.KeyOrders, p)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(live)
	if !jsonEqual(result, want) {
		t.Errorf("result = %s\nmong %s", result, want)
	}
	got, err := e.get(t, u, reporting.FamilySales, run.ID)
	if err != nil || got.Status != "ready" || !jsonEqual(got.Result, want) || got.RowCount == nil || got.FinishedAt == nil {
		t.Errorf("GetRun = %+v, %v", got, err)
	}
	if s := e.jobStatus(t, run.ID); s != "done" {
		t.Errorf("lệnh = %s, mong done", s)
	}
}

func jsonEqual(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	ax, _ := json.Marshal(x)
	by, _ := json.Marshal(y)
	return string(ax) == string(by)
}

// Lỗi khi tạo: xlsx không có report.export → ErrExportForbidden, không dòng, không lệnh; báo cáo của họ khác →
// ErrUnknownReport; tham số lạ → ErrBadParams; products dạng json → ErrBadParams; không có quyền đọc hay phạm vi rỗng →
// ErrForbidden; 5 lần đang chờ → lần 6 ErrTooManyRuns, người khác vẫn tạo được.
func TestRuns_CreateErrors(t *testing.T) {
	t.Parallel()
	e := newRunsEnv(t)
	u := e.user(t, "an@elimaz.test", salesAll(), "all", "")
	inv := e.user(t, "kho@elimaz.test", []string{"report.inventory.read"}, "all", "")
	cases := []struct {
		name string
		u    identity.AuthUser
		in   reporting.CreateRunInput
		want error
	}{
		{"xlsx không report.export", u, ordersInput("xlsx"), reporting.ErrExportForbidden},
		{"họ khác", inv, reporting.CreateRunInput{
			Family: reporting.FamilyInventory, Report: reporting.KeyOrders, Format: "json",
			Params: ordersInput("json").Params,
		}, reporting.ErrUnknownReport},
		{"trường lạ", u, reporting.CreateRunInput{
			Family: reporting.FamilySales, Report: reporting.KeyOrders, Format: "json",
			Params: json.RawMessage(`{"from":"2026-11-01","to":"2026-11-02","x":1}`),
		}, reporting.ErrBadParams},
		{"định dạng lạ", u, reporting.CreateRunInput{
			Family: reporting.FamilySales, Report: reporting.KeyOrders, Format: "csv",
			Params: ordersInput("json").Params,
		}, reporting.ErrBadParams},
		{"products json", inv, reporting.CreateRunInput{
			Family: reporting.FamilyInventory, Report: reporting.KeyProducts,
			Format: "json", Params: json.RawMessage(`{}`),
		}, reporting.ErrBadParams},
		{"không quyền đọc", inv, reporting.CreateRunInput{
			Family: reporting.FamilySales, Report: reporting.KeyOrders,
			Format: "json", Params: ordersInput("json").Params,
		}, reporting.ErrForbidden},
	}
	for _, c := range cases {
		if _, err := e.create(t, c.u, c.in, runLimits()); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, mong %v", c.name, err, c.want)
		}
	}
	if n := e.st.CountIn(t, e.tenant, `SELECT count(*) FROM reporting.report_runs`); n != 0 {
		t.Fatalf("có %d dòng report_runs sau các lần bị từ chối", n)
	}
	if n := e.st.CountIn(t, e.tenant, `SELECT count(*) FROM platform.jobs WHERE kind = 'command.report_run'`); n != 0 {
		t.Fatalf("có %d lệnh sau các lần bị từ chối", n)
	}
	for range 5 {
		e.mustCreate(t, u, ordersInput("json"))
	}
	if _, err := e.create(t, u, ordersInput("json"), runLimits()); !errors.Is(err, reporting.ErrTooManyRuns) {
		t.Errorf("lần 6: %v, mong ErrTooManyRuns", err)
	}
	other := e.user(t, "binh@elimaz.test", salesAll(), "all", "")
	e.mustCreate(t, other, ordersInput("json"))
}

// sleepRunDef là Def thử thay báo cáo dashboard trong RunHandler: ngủ ms trong database; calls đếm số lần chạy.
func sleepRunDef(ms int, calls *atomic.Int32) map[reporting.Key]reporting.Def {
	d := reporting.Defs()[reporting.KeyDashboard]
	d.JSON = func(ctx context.Context, tx pgx.Tx, _ reporting.DataScope, _ reporting.Params) (any, error) {
		if calls != nil {
			calls.Add(1)
		}
		if _, err := tx.Exec(ctx, `SELECT pg_sleep($1::float8 / 1000)`, ms); err != nil {
			return nil, fmt.Errorf("ngủ: %w", err)
		}
		return map[string]int{"slept": ms}, nil
	}
	return map[reporting.Key]reporting.Def{reporting.KeyDashboard: d}
}

func dashboardInput() reporting.CreateRunInput {
	return reporting.CreateRunInput{
		Family: reporting.FamilySales, Report: reporting.KeyDashboard, Format: "json",
		Params: json.RawMessage(`{"from":"` + runDay + `","to":"` + runDay + `"}`),
	}
}

// REPORT_WORKER_SLOTS = 1, Def ngủ 300 ms: lần chạy thứ hai lúc lần đầu đang chạy → not_sent, RetryAfter 15 s, không
// tính lượt, vẫn queued; chạy lại sau → ready.
func TestRuns_WorkerBusy(t *testing.T) {
	t.Parallel()
	e := newRunsEnv(t)
	u := e.user(t, "an@elimaz.test", salesAll(), "all", "")
	h := e.handler(config.ReportConfig{WorkerSlots: 1}, sleepRunDef(300, nil))
	a, b := e.mustCreate(t, u, dashboardInput()), e.mustCreate(t, u, dashboardInput())
	var wg sync.WaitGroup
	var first commands.Outcome
	wg.Go(func() { first, _ = h.Execute(t.Context(), e.job(t, a.ID)) })
	deadline := time.Now().Add(5 * time.Second)
	for s, _, _ := e.row(t, a.ID); s != "running"; s, _, _ = e.row(t, a.ID) {
		if time.Now().After(deadline) {
			t.Fatal("lần đầu chưa chạy")
		}
		time.Sleep(5 * time.Millisecond)
	}
	second, err := h.Execute(t.Context(), e.job(t, b.ID))
	if err != nil || second.Status != commands.StatusNotSent || second.RetryAfter != 15*time.Second || !second.KeepAttempt {
		t.Errorf("lần thứ hai = %+v, %v; mong not_sent, 15 s, không tính lượt", second, err)
	}
	if s, _, _ := e.row(t, b.ID); s != "queued" {
		t.Errorf("lần thứ hai = %s, mong queued", s)
	}
	wg.Wait()
	if first.Status != commands.StatusDone {
		t.Errorf("lần đầu = %+v", first)
	}
	again, err := h.Execute(t.Context(), e.job(t, b.ID))
	if err != nil || again.Status != commands.StatusDone {
		t.Errorf("chạy lại = %+v, %v", again, err)
	}
	if s, _, _ := e.row(t, b.ID); s != "ready" {
		t.Errorf("sau khi chạy lại = %s, mong ready", s)
	}
}

// REPORT_BACKGROUND_TIMEOUT 200 ms, Def ngủ 1 s → failed/too_heavy; lệnh done (không dead).
func TestRuns_TooHeavy(t *testing.T) {
	t.Parallel()
	e := newRunsEnv(t)
	u := e.user(t, "an@elimaz.test", salesAll(), "all", "")
	h := e.handler(config.ReportConfig{BackgroundTimeout: 200 * time.Millisecond}, sleepRunDef(1000, nil))
	run := e.mustCreate(t, u, dashboardInput())
	testkit.DrainDue(t, e.st.Owner, e.worker(h))
	if s, reason, _ := e.row(t, run.ID); s != "failed" || reason != "too_heavy" {
		t.Errorf("lần chạy = %s/%s, mong failed/too_heavy", s, reason)
	}
	if s := e.jobStatus(t, run.ID); s != "done" {
		t.Errorf("lệnh = %s, mong done", s)
	}
	if n := e.st.CountIn(t, e.tenant, `SELECT count(*) FROM platform.jobs WHERE status = 'dead'`); n != 0 {
		t.Errorf("có %d việc dead", n)
	}
}

// Worker chết sau khi ghi ready, trước khi lệnh done: lệnh running, giữ chỗ đã qua → worker quét sang in_doubt (không
// đổi lần chạy) → ResolveInDoubt tra thấy ready → lệnh done; vẫn một lần ready, kết quả không đổi, báo cáo không chạy
// lại.
func TestRuns_InDoubtAfterReady(t *testing.T) {
	t.Parallel()
	e := newRunsEnv(t)
	u := e.user(t, "an@elimaz.test", salesAll(), "all", "")
	var calls atomic.Int32
	h := e.handler(config.ReportConfig{}, sleepRunDef(0, &calls))
	run := e.mustCreate(t, u, dashboardInput())
	j := e.job(t, run.ID)
	if out, err := h.Execute(t.Context(), j); err != nil || out.Status != commands.StatusDone {
		t.Fatalf("Execute = %+v, %v", out, err)
	}
	_, _, before := e.row(t, run.ID)
	e.ownerExec(t, `UPDATE platform.jobs SET status = 'running', attempts = attempts + 1, locked_by = 'dead-worker',
		lease_until = now() - interval '1 second' WHERE id = $1`, j.ID)
	testkit.RunRound(t, e.st.Owner, e.worker(h), 10)
	if s := e.jobStatus(t, run.ID); s != "in_doubt" {
		t.Fatalf("lệnh = %s, mong in_doubt", s)
	}
	if s, _, _ := e.row(t, run.ID); s != "ready" {
		t.Fatalf("in_doubt đổi lần chạy thành %s", s)
	}
	done, _, _, err := commands.ResolveInDoubt(t.Context(), e.st.App, []commands.Handler{h}, 10)
	if err != nil || done != 1 {
		t.Fatalf("ResolveInDoubt: done %d, %v", done, err)
	}
	if s := e.jobStatus(t, run.ID); s != "done" {
		t.Errorf("lệnh = %s, mong done", s)
	}
	s, _, after := e.row(t, run.ID)
	if s != "ready" || string(after) != string(before) || calls.Load() != 1 {
		t.Errorf("sau tra lại: %s, kết quả đổi %v, báo cáo chạy %d lần", s, string(after) != string(before), calls.Load())
	}
	// Lệnh lặp lại sau khi đã ready: MarkRunning 0 dòng → done, không chạy báo cáo.
	if out, err := h.Execute(t.Context(), j); err != nil || out.Status != commands.StatusDone || calls.Load() != 1 {
		t.Errorf("Execute lần hai = %+v, %v, báo cáo chạy %d lần", out, err, calls.Load())
	}
}

// Worker chết khi lần chạy đang running (chưa ghi kết quả): in_doubt → tra lại thấy running → not_sent → chạy lại →
// ready.
func TestRuns_InDoubtWhileRunning(t *testing.T) {
	t.Parallel()
	e := newRunsEnv(t)
	u := e.user(t, "an@elimaz.test", salesAll(), "all", "")
	h := e.handler(config.ReportConfig{}, sleepRunDef(0, nil))
	run := e.mustCreate(t, u, dashboardInput())
	j := e.job(t, run.ID)
	e.ownerExec(t, `UPDATE reporting.report_runs SET status = 'running', started_at = now() WHERE id = $1`, run.ID)
	e.ownerExec(t, `UPDATE platform.jobs SET status = 'running', attempts = attempts + 1, locked_by = 'dead-worker',
		lease_until = now() - interval '1 second' WHERE id = $1`, j.ID)
	w := e.worker(h)
	testkit.RunRound(t, e.st.Owner, w, 10)
	if _, requeued, _, err := commands.ResolveInDoubt(t.Context(), e.st.App, []commands.Handler{h}, 10); err != nil || requeued != 1 {
		t.Fatalf("ResolveInDoubt: requeued %d, %v", requeued, err)
	}
	testkit.DrainDue(t, e.st.Owner, w)
	if s, _, _ := e.row(t, run.ID); s != "ready" {
		t.Errorf("lần chạy = %s, mong ready", s)
	}
}

// Người tạo bị khóa trước khi worker chạy → failed/forbidden; người tạo mất report.sales.read → như vậy; có báo hiệu.
func TestRuns_ForbiddenAtRun(t *testing.T) {
	t.Parallel()
	e := newRunsEnv(t)
	locked := e.user(t, "khoa@elimaz.test", salesAll(), "all", "")
	lost := e.user(t, "mat@elimaz.test", salesAll(), "all", "")
	viewer := e.user(t, "xem@elimaz.test", salesAll(), "all", "")
	a, b := e.mustCreate(t, locked, dashboardInput()), e.mustCreate(t, lost, dashboardInput())
	e.st.SetStatus(t, e.tenant, locked.UserID, "locked")
	e.ownerExec(t, `DELETE FROM identity.user_role_grants WHERE user_id = $1`, lost.UserID)
	sig := e.subscribe(t, viewer)
	testkit.DrainDue(t, e.st.Owner, e.worker(e.handler(config.ReportConfig{}, nil)))
	for _, id := range []uuid.UUID{a.ID, b.ID} {
		if s, reason, _ := e.row(t, id); s != "failed" || reason != "forbidden" {
			t.Errorf("%s = %s/%s, mong failed/forbidden", id, s, reason)
		}
		if s := e.jobStatus(t, id); s != "done" {
			t.Errorf("lệnh %s = %s, mong done", id, s)
		}
	}
	waitSignal(t, sig, a.ID, 2*time.Second)
}

// Lệnh quá hạn khi luôn hết chỗ → expired → OnTerminal → failed/busy, có báo hiệu.
func TestRuns_ExpiredBusy(t *testing.T) {
	t.Parallel()
	e := newRunsEnv(t)
	u := e.user(t, "an@elimaz.test", salesAll(), "all", "")
	h := e.handler(config.ReportConfig{WorkerSlots: 1}, nil)
	release := reporting.HoldRunSlots(h)
	defer release()
	run := e.mustCreate(t, u, dashboardInput())
	e.ownerExec(t, `UPDATE platform.jobs SET expires_at = now() + interval '100 milliseconds' WHERE dedupe_key = $1`, run.ID.String())
	time.Sleep(150 * time.Millisecond)
	sig := e.subscribe(t, u)
	testkit.RunRound(t, e.st.Owner, e.worker(h), 10)
	if s := e.jobStatus(t, run.ID); s != "expired" {
		t.Fatalf("lệnh = %s, mong expired", s)
	}
	if s, reason, _ := e.row(t, run.ID); s != "failed" || reason != "busy" {
		t.Errorf("lần chạy = %s/%s, mong failed/busy", s, reason)
	}
	waitSignal(t, sig, run.ID, 2*time.Second)
}

// viewers tạo ba nhân viên xem báo cáo bán hàng toàn shop: người tạo, người khác, người có report.exports.manage.
func (e *runsEnv) viewers(t *testing.T) (u, other, manager identity.AuthUser) {
	t.Helper()
	return e.user(t, "an@elimaz.test", salesAll(), "all", ""), e.user(t, "binh@elimaz.test", salesAll(), "all", ""),
		e.user(t, "ql@elimaz.test", []string{"report.sales.read", "report.exports.manage"}, "all", "")
}

// Xem: người khác → ErrRunNotFound; người có report.exports.manage → thấy; tenant B → không thấy; họ khác → không thấy.
func TestRuns_Get(t *testing.T) {
	t.Parallel()
	e := newRunsEnv(t)
	u, other, manager := e.viewers(t)
	run := e.mustCreate(t, u, dashboardInput())
	uB := u
	uB.TenantID = e.st.Tenant(t, "b")
	for _, c := range []struct {
		name string
		u    identity.AuthUser
		f    reporting.Family
		ok   bool
	}{
		{"người tạo", u, reporting.FamilySales, true},
		{"người khác", other, reporting.FamilySales, false},
		{"người quản lý", manager, reporting.FamilySales, true},
		{"họ khác", u, reporting.FamilyInventory, false},
		{"tenant B", uB, reporting.FamilySales, false},
	} {
		got, err := e.get(t, c.u, c.f, run.ID)
		if c.ok && (err != nil || got.ID != run.ID) || !c.ok && !errors.Is(err, reporting.ErrRunNotFound) {
			t.Errorf("%s: %+v, %v", c.name, got.ID, err)
		}
	}
}

// listAll đọc mọi trang (50 dòng) lần chạy bán hàng mà viewer thấy; kiểm mới nhất trước, không kèm result, con trỏ đọc
// lại được.
func (e *runsEnv) listAll(t *testing.T, viewer identity.AuthUser) []uuid.UUID {
	t.Helper()
	var ids []uuid.UUID
	var cursor *reporting.RunCursor
	for range 10 {
		var runs []reporting.Run
		var next *reporting.RunCursor
		e.st.Exec(t, e.tenant, func(tx pgx.Tx) error {
			var err error
			runs, next, err = reporting.ListRuns(t.Context(), tx, viewer, reporting.FamilySales, cursor, 50)
			return err //nolint:wrapcheck // test
		})
		for i, r := range runs {
			if r.Result != nil || i > 0 && r.CreatedAt.After(runs[i-1].CreatedAt) {
				t.Errorf("dòng %d: có result hay không mới nhất trước", i)
			}
			ids = append(ids, r.ID)
		}
		if next == nil {
			return ids
		}
		if c, err := reporting.ParseRunCursor(next.String()); err != nil || c.ID != next.ID || !c.CreatedAt.Equal(next.CreatedAt) {
			t.Fatalf("con trỏ đọc lại = %+v, %v", c, err)
		}
		cursor = next
	}
	t.Fatal("quá nhiều trang")
	return nil
}

// Liệt kê 120 lần chạy trang 50 → 120 mã khác nhau; người thường chỉ thấy của mình; report.exports.manage thấy mọi
// người; con trỏ hỏng → ErrBadParams.
func TestRuns_List(t *testing.T) {
	t.Parallel()
	e := newRunsEnv(t)
	u, other, manager := e.viewers(t)
	many := reporting.RunLimits{MaxPendingPerUser: 1000, JSONTTL: time.Hour}
	for range 120 {
		if _, err := e.create(t, u, dashboardInput(), many); err != nil {
			t.Fatal(err)
		}
	}
	e.mustCreate(t, other, dashboardInput())
	mine := e.listAll(t, u)
	distinct := map[uuid.UUID]bool{}
	for _, id := range mine {
		distinct[id] = true
	}
	if len(mine) != 120 || len(distinct) != 120 {
		t.Errorf("người tạo thấy %d lần chạy (khác nhau %d), mong 120", len(mine), len(distinct))
	}
	if all := e.listAll(t, manager); len(all) != 121 {
		t.Errorf("người quản lý thấy %d, mong 121", len(all))
	}
	if o := e.listAll(t, other); len(o) != 1 {
		t.Errorf("người khác thấy %d, mong 1", len(o))
	}
	if _, err := reporting.ParseRunCursor("x"); !errors.Is(err, reporting.ErrBadParams) {
		t.Errorf("con trỏ hỏng: %v", err)
	}
}
