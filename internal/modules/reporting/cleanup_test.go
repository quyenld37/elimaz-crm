package reporting_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// Dọn reporting.cleanup: (a) bảng ready quá hạn → expired, result NULL (ready chưa hạn giữ nguyên); (b) queued quá
// hạn + 1 giờ mà lệnh đã kết thúc → failed/error, lệnh còn chờ → giữ; (c) dòng tạo 91 ngày → xóa, 89 ngày → còn.
// Lịch reporting.cleanup chạy mỗi giờ theo tenant.
func TestRuns_Cleanup(t *testing.T) {
	t.Parallel()
	e := newRunsEnv(t)
	u := e.user(t, "an@elimaz.test", salesAll(), "all", "")
	many := reporting.RunLimits{MaxPendingPerUser: 100, JSONTTL: time.Hour}
	mk := func() uuid.UUID {
		r, err := e.create(t, u, dashboardInput(), many)
		if err != nil {
			t.Fatal(err)
		}
		return r.ID
	}
	expired, fresh, staleDone, stalePending, old, young := mk(), mk(), mk(), mk(), mk(), mk()
	testkit.DrainDue(t, e.st.Owner, e.worker(e.handler(config.ReportConfig{WorkerSlots: 8}, sleepRunDef(0, nil))))
	e.ownerExec(t, `UPDATE reporting.report_runs SET expires_at = now() - interval '1 second' WHERE id = $1`, expired)
	// staleDone: lệnh đã done mà lần chạy còn queued (lưới an toàn); stalePending: lệnh còn chờ.
	e.ownerExec(t, `UPDATE reporting.report_runs SET status = 'queued', result = NULL, row_count = NULL, finished_at = NULL,
		expires_at = now() - interval '2 hours' WHERE id = ANY($1)`, []uuid.UUID{staleDone, stalePending})
	e.ownerExec(t, `UPDATE platform.jobs SET status = 'pending', run_at = now() + interval '1 day', result = NULL
		WHERE dedupe_key = $1`, stalePending.String())
	e.ownerExec(t, `UPDATE reporting.report_runs SET created_at = now() - interval '91 days' WHERE id = $1`, old)
	e.ownerExec(t, `UPDATE reporting.report_runs SET created_at = now() - interval '89 days' WHERE id = $1`, young)

	out := e.cleanup(t, e.st.App)
	if s, _, res := e.row(t, expired); s != "expired" || res != nil {
		t.Errorf("quá hạn = %s, result %s; mong expired, NULL", s, res)
	}
	if s, _, res := e.row(t, fresh); s != "ready" || res == nil {
		t.Errorf("chưa hạn = %s, result %s; mong ready", s, res)
	}
	if s, reason, _ := e.row(t, staleDone); s != "failed" || reason != "error" {
		t.Errorf("treo, lệnh xong = %s/%s; mong failed/error", s, reason)
	}
	if s, _, _ := e.row(t, stalePending); s != "queued" {
		t.Errorf("treo, lệnh còn chờ = %s; mong queued", s)
	}
	if n := e.st.CountIn(t, e.tenant, `SELECT count(*) FROM reporting.report_runs WHERE id = $1`, old); n != 0 {
		t.Errorf("dòng 91 ngày còn")
	}
	if n := e.st.CountIn(t, e.tenant, `SELECT count(*) FROM reporting.report_runs WHERE id = $1`, young); n != 1 {
		t.Errorf("dòng 89 ngày bị xóa")
	}
	if string(out) != `{"expired":1,"expiredFiles":0,"failed":1,"deleted":1}` {
		t.Errorf("kết quả dọn = %s", out)
	}

	var found bool
	for _, s := range reporting.Schedules() {
		if s.Name == "reporting.cleanup" {
			found = s.Every == time.Hour && s.PerTenant && s.Kind.Name == "reporting.cleanup"
		}
	}
	if !found {
		t.Errorf("thiếu lịch reporting.cleanup mỗi giờ theo tenant")
	}
}

// runCleanup chạy một lượt reporting.cleanup trên d (pool của crm_app, hay pool có bộ ghi câu lệnh).
func (e *runsEnv) runCleanup(t *testing.T, d *db.DB) (json.RawMessage, error) {
	var out json.RawMessage
	err := tenancy.WithTenant(t.Context(), d, e.tenant, func(tx pgx.Tx) error {
		var err error
		out, err = reporting.NewCleanupHandler(reporting.Deps{}).Handle(t.Context(), queue.Job{TenantID: &e.tenant}, tx)
		return err //nolint:wrapcheck // test
	})
	return out, err //nolint:wrapcheck // test
}

// cleanup chạy một lượt dọn; lỗi → t.Fatal.
func (e *runsEnv) cleanup(t *testing.T, d *db.DB) json.RawMessage {
	t.Helper()
	out, err := e.runCleanup(t, d)
	if err != nil {
		t.Fatalf("dọn: %v", err)
	}
	return out
}

// cleanupQuiet như cleanup nhưng chạy được trong goroutine khác (lỗi ghi bằng t.Errorf).
func (e *runsEnv) cleanupQuiet(t *testing.T, d *db.DB) json.RawMessage {
	out, err := e.runCleanup(t, d)
	if err != nil {
		t.Errorf("dọn: %v", err)
	}
	return out
}
