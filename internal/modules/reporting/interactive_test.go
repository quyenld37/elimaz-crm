package reporting_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/testkit"
)

// sleepParams là tham số của báo cáo thử: ngủ MS mili giây trong database.
type sleepParams struct{ MS int }

func (sleepParams) Validate() error { return nil }

// reportEnv là một tenant có người bán own và một nhân viên xem báo cáo bán hàng toàn shop.
type reportEnv struct {
	staff  *testkit.Staff
	tenant uuid.UUID
	user   identity.AuthUser
}

func newReportEnv(t *testing.T) reportEnv {
	t.Helper()
	s := testkit.OpenStaff(t)
	tenant := s.Tenant(t, "a")
	s.Exec(t, tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO catalog.sellers (tenant_id, code, name, kind) VALUES ($1, 'ELIMAZ', 'ELIMAZ', 'own')`, tenant)
		return err //nolint:wrapcheck // test
	})
	u := identity.AuthUser{TenantID: tenant, UserID: uuid.New(), Grants: []identity.Grant{
		{Permission: "report.sales.read", Scope: identity.Scope{Type: identity.ScopeAll}},
	}}
	return reportEnv{staff: s, tenant: tenant, user: u}
}

// sleepDef là Def thử (chỉ có trong test): SELECT pg_sleep; started đóng khi hàm bắt đầu chạy lần đầu, calls đếm số
// lần chạy.
func sleepDef(calls *atomic.Int32, started chan struct{}) reporting.Def {
	var once sync.Once
	return reporting.Def{
		Key: reporting.KeyDashboard, Family: reporting.FamilySales, Read: "report.sales.read",
		JSON: func(ctx context.Context, tx pgx.Tx, s reporting.DataScope, p reporting.Params) (any, error) {
			calls.Add(1)
			if started != nil {
				once.Do(func() { close(started) })
			}
			sp, ok := p.(sleepParams)
			if !ok {
				return nil, fmt.Errorf("tham số %T", p)
			}
			if _, err := tx.Exec(ctx, `SELECT pg_sleep($1::float8 / 1000)`, sp.MS); err != nil {
				return nil, fmt.Errorf("ngủ: %w", err)
			}
			return "xong:" + s.SellerID.String(), nil
		},
	}
}

func interactive(e reportEnv, o reporting.InteractiveOptions, d reporting.Def) *reporting.Interactive {
	o.Defs = map[reporting.Key]reporting.Def{d.Key: d}
	return reporting.NewInteractive(e.staff.App, o)
}

// counter trả Count của chỉ số name (nhãn label) trong rows.
func counter(rows []metrics.Row, kind metrics.Kind, name, label string) (metrics.Row, bool) {
	for _, r := range rows {
		if r.Kind == kind && r.Name == name && r.Label == label {
			return r, true
		}
	}
	return metrics.Row{}, false
}

func backgroundReason(err error) string {
	if be, ok := errors.AsType[*reporting.BackgroundError](err); ok {
		return be.Reason
	}
	return ""
}

// Slots 1: hai lần chạy cùng lúc (lần đầu ngủ 300 ms) → lần thứ hai trả BackgroundError{busy} trong dưới 50 ms, không
// chạm database; lần đầu trả kết quả. report_busy tăng 1, report_ms có một lần đo, report_slots_in_use cuối cùng 0.
func TestInteractiveBusy(t *testing.T) {
	t.Parallel()
	e := newReportEnv(t)
	reg := metrics.NewRegistry()
	var calls atomic.Int32
	started := make(chan struct{})
	r := interactive(e, reporting.InteractiveOptions{Slots: 1, Timeout: 5 * time.Second, LockTimeout: time.Second, Metrics: reg},
		sleepDef(&calls, started))
	type result struct {
		v   any
		err error
	}
	first := make(chan result, 1)
	go func() {
		v, err := r.Run(t.Context(), e.user, reporting.KeyDashboard, sleepParams{MS: 300})
		first <- result{v, err}
	}()
	<-started
	t0 := time.Now()
	_, err := r.Run(t.Context(), e.user, reporting.KeyDashboard, sleepParams{MS: 0})
	if took := time.Since(t0); took >= 50*time.Millisecond {
		t.Errorf("lần thứ hai mất %v, mong < 50 ms", took)
	}
	if backgroundReason(err) != "busy" || !errors.Is(err, reporting.ErrRunInBackground) {
		t.Fatalf("lần thứ hai: %v, mong BackgroundError{busy}", err)
	}
	got := <-first
	if got.err != nil || got.v == nil {
		t.Fatalf("lần đầu: %v, %v", got.v, got.err)
	}
	if calls.Load() != 1 {
		t.Errorf("hàm báo cáo chạy %d lần, mong 1", calls.Load())
	}
	rows := reg.Drain()
	if c, ok := counter(rows, metrics.KindCounter, "report_busy", "dashboard"); !ok || c.Count != 1 {
		t.Errorf("report_busy = %+v", c)
	}
	if h, ok := counter(rows, metrics.KindHistogram, "report_ms", "dashboard"); !ok || h.Count != 1 {
		t.Errorf("report_ms = %+v", h)
	}
	if g, ok := counter(rows, metrics.KindGauge, "report_slots_in_use", ""); !ok || g.Sum != 0 || g.Max != 1 {
		t.Errorf("report_slots_in_use = %+v, mong cuối 0, cao nhất 1", g)
	}
}

// Timeout 200 ms, ngủ 1 s → BackgroundError{timeout} trong dưới 400 ms; kết nối trả về pool; report_timeout tăng 1.
func TestInteractiveTimeout(t *testing.T) {
	t.Parallel()
	e := newReportEnv(t)
	reg := metrics.NewRegistry()
	var calls atomic.Int32
	r := interactive(e, reporting.InteractiveOptions{Slots: 2, Timeout: 200 * time.Millisecond, LockTimeout: time.Second, Metrics: reg},
		sleepDef(&calls, nil))
	t0 := time.Now()
	_, err := r.Run(t.Context(), e.user, reporting.KeyDashboard, sleepParams{MS: 1000})
	if took := time.Since(t0); took >= 400*time.Millisecond {
		t.Errorf("mất %v, mong < 400 ms", took)
	}
	if backgroundReason(err) != "timeout" || !errors.Is(err, reporting.ErrRunInBackground) {
		t.Fatalf("Run: %v, mong BackgroundError{timeout}", err)
	}
	if calls.Load() != 1 {
		t.Errorf("hàm báo cáo chạy %d lần, mong 1 (57014 không chạy lại)", calls.Load())
	}
	if n := e.staff.App.Pool.Stat().AcquiredConns(); n != 0 {
		t.Errorf("còn %d kết nối đang mượn", n)
	}
	rows := reg.Drain()
	if c, ok := counter(rows, metrics.KindCounter, "report_timeout", "dashboard"); !ok || c.Count != 1 {
		t.Errorf("report_timeout = %+v", c)
	}
	// Vẫn chạy được sau khi quá hạn: chỗ đã nhả.
	if _, err := r.Run(t.Context(), e.user, reporting.KeyDashboard, sleepParams{MS: 0}); err != nil {
		t.Fatalf("chạy lại: %v", err)
	}
}

// Trong lúc chạy: pg_stat_activity có đúng một phiên application_name = crm-report; trong transaction là bốn thiết lập
// của Global Constraints và hai hạn giờ; ngoài transaction (mọi kết nối rảnh của pool) giá trị mặc định (không rò).
func TestInteractiveSettings(t *testing.T) {
	t.Parallel()
	e := newReportEnv(t)
	inside := map[string]string{}
	var sessions int
	def := reporting.Def{
		Key: reporting.KeyDashboard, Family: reporting.FamilySales, Read: "report.sales.read",
		JSON: func(ctx context.Context, tx pgx.Tx, _ reporting.DataScope, _ reporting.Params) (any, error) {
			clear(inside)
			for _, name := range []string{
				"transaction_read_only", "max_parallel_workers_per_gather", "work_mem",
				"application_name", "lock_timeout", "statement_timeout",
			} {
				var v string
				if err := tx.QueryRow(ctx, "SHOW "+name).Scan(&v); err != nil {
					return nil, fmt.Errorf("SHOW %s: %w", name, err)
				}
				inside[name] = v
			}
			// Kết nối khác của pool nhìn thấy phiên báo cáo.
			err := e.staff.App.Pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
				WHERE datname = current_database() AND application_name = 'crm-report'`).Scan(&sessions)
			if err != nil {
				return nil, fmt.Errorf("pg_stat_activity: %w", err)
			}
			return "ok", nil
		},
	}
	r := interactive(e, reporting.InteractiveOptions{Slots: 2, Timeout: 7 * time.Second, LockTimeout: time.Second}, def)
	if _, err := r.Run(t.Context(), e.user, reporting.KeyDashboard, sleepParams{}); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"transaction_read_only": "on", "max_parallel_workers_per_gather": "0", "work_mem": "32MB",
		"application_name": "crm-report", "lock_timeout": "1s", "statement_timeout": "7s",
	}
	for k, v := range want {
		if inside[k] != v {
			t.Errorf("trong transaction %s = %q, mong %q", k, inside[k], v)
		}
	}
	if sessions != 1 {
		t.Errorf("pg_stat_activity có %d phiên crm-report, mong 1", sessions)
	}
	conns := e.staff.App.Pool.AcquireAllIdle(t.Context())
	if len(conns) == 0 {
		t.Fatal("pool không có kết nối rảnh")
	}
	for _, c := range conns {
		var par, app, ro string
		err := c.QueryRow(t.Context(), `SELECT current_setting('max_parallel_workers_per_gather'),
			current_setting('application_name'), current_setting('transaction_read_only')`).Scan(&par, &app, &ro)
		c.Release()
		if err != nil {
			t.Fatal(err)
		}
		if par == "0" || app == "crm-report" || ro == "on" {
			t.Errorf("thiết lập rò ra ngoài transaction: parallel=%s app=%s read_only=%s", par, app, ro)
		}
	}
}

// Def có INSERT → lỗi 25006 (transaction chỉ đọc), hàm chạy 1 lần, không phải BackgroundError.
func TestInteractiveReadOnly(t *testing.T) {
	t.Parallel()
	e := newReportEnv(t)
	var calls atomic.Int32
	def := reporting.Def{
		Key: reporting.KeyDashboard, Family: reporting.FamilySales, Read: "report.sales.read",
		JSON: func(ctx context.Context, tx pgx.Tx, s reporting.DataScope, _ reporting.Params) (any, error) {
			calls.Add(1)
			_, err := tx.Exec(ctx, `INSERT INTO reporting.report_runs (tenant_id, seller_id, family, report, format, params,
				requested_by, expires_at) VALUES ($1, $2, 'sales', 'dashboard', 'json', '{}', $2, now())`, s.TenantID, s.SellerID)
			return nil, err //nolint:wrapcheck // test
		},
	}
	r := interactive(e, reporting.InteractiveOptions{Slots: 2, Timeout: 5 * time.Second, LockTimeout: time.Second}, def)
	_, err := r.Run(t.Context(), e.user, reporting.KeyDashboard, sleepParams{})
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok || pgErr.Code != "25006" {
		t.Fatalf("Run: %v, mong SQLSTATE 25006", err)
	}
	if errors.Is(err, reporting.ErrRunInBackground) || calls.Load() != 1 {
		t.Fatalf("BackgroundError=%v, số lần chạy %d", errors.Is(err, reporting.ErrRunInBackground), calls.Load())
	}
}

// ctx của yêu cầu bị hủy giữa chừng → context.Canceled (không phải BackgroundError); chỗ được nhả.
func TestInteractiveCanceled(t *testing.T) {
	t.Parallel()
	e := newReportEnv(t)
	var calls atomic.Int32
	started := make(chan struct{})
	r := interactive(e, reporting.InteractiveOptions{Slots: 1, Timeout: 5 * time.Second, LockTimeout: time.Second},
		sleepDef(&calls, started))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := r.Run(ctx, e.user, reporting.KeyDashboard, sleepParams{MS: 3000})
		done <- err
	}()
	<-started
	cancel()
	err := <-done
	if !errors.Is(err, context.Canceled) || errors.Is(err, reporting.ErrRunInBackground) {
		t.Fatalf("Run: %v, mong context.Canceled", err)
	}
	if _, err := r.Run(t.Context(), e.user, reporting.KeyDashboard, sleepParams{MS: 0}); err != nil {
		t.Fatalf("chỗ chưa được nhả: %v", err)
	}
}

// Kết quả của Run: người bán own và phạm vi của người gọi được dựng trong transaction; tham số sai trả lỗi trước khi
// lấy chỗ; báo cáo chưa có bản xem tại chỗ (JSON nil) → ErrNotImplemented; khóa lạ → ErrUnknownReport.
func TestInteractiveScopeAndErrors(t *testing.T) {
	t.Parallel()
	e := newReportEnv(t)
	var seen reporting.DataScope
	def := reporting.Def{
		Key: reporting.KeyDashboard, Family: reporting.FamilySales, Read: "report.sales.read",
		JSON: func(_ context.Context, _ pgx.Tx, s reporting.DataScope, _ reporting.Params) (any, error) {
			seen = s
			return "ok", nil
		},
	}
	r := reporting.NewInteractive(e.staff.App, reporting.InteractiveOptions{
		Slots: 1, Timeout: 5 * time.Second, LockTimeout: time.Second,
		Defs: map[reporting.Key]reporting.Def{
			reporting.KeyDashboard: def,
			reporting.KeyProducts:  {Key: reporting.KeyProducts, Family: reporting.FamilyInventory, Read: "report.inventory.read"},
		},
	})
	if _, err := r.Run(t.Context(), e.user, reporting.KeyDashboard, sleepParams{}); err != nil {
		t.Fatal(err)
	}
	if seen.TenantID != e.tenant || seen.SellerID == uuid.Nil || !seen.AllPages {
		t.Fatalf("phạm vi = %+v", seen)
	}
	bad := &reporting.DashboardParams{PeriodParams: reporting.PeriodParams{From: "2026-01-01", To: "2027-01-02"}}
	if _, err := r.Run(t.Context(), e.user, reporting.KeyDashboard, bad); !errors.Is(err, reporting.ErrRangeTooLong) {
		t.Fatalf("367 ngày: %v", err)
	}
	if _, err := r.Run(t.Context(), e.user, reporting.KeyProducts, sleepParams{}); !errors.Is(err, reporting.ErrNotImplemented) {
		t.Fatalf("không có JSON: %v", err)
	}
	if _, err := r.Run(t.Context(), e.user, reporting.KeyOrders, sleepParams{}); !errors.Is(err, reporting.ErrUnknownReport) {
		t.Fatalf("khóa không có trong sổ: %v", err)
	}
}

// TxReport: bốn Settings của Global Constraints và hai hạn giờ được truyền.
func TestTxReport(t *testing.T) {
	t.Parallel()
	o := reporting.TxReport(30*time.Second, time.Second)
	if o.StatementTimeout != 30*time.Second || o.LockTimeout != time.Second {
		t.Fatalf("hạn giờ: %+v", o)
	}
	got := map[string]string{}
	for _, s := range o.Settings {
		got[s.Name] = s.Value
	}
	want := map[string]string{
		"transaction_read_only": "on", "max_parallel_workers_per_gather": "0", "work_mem": "32MB", "application_name": "crm-report",
	}
	if len(got) != len(want) {
		t.Fatalf("Settings = %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, mong %q", k, got[k], v)
		}
	}
}
