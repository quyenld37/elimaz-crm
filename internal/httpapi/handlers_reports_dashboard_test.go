package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/testkit"
)

// Khung chạy tại chỗ của GET /api/v1/reports/sales/dashboard (kế hoạch M6, Task 228; phần tính số là Task 229): hết
// chỗ → 200 run_in_background/busy; quá hạn → 200 run_in_background/timeout; ngày sai dạng, thiếu, from sau to → 400
// bad_date; 367 ngày → 400 range_too_long; người chỉ có report.inventory.read → 403.
func TestReportsDashboard_RunInBackground(t *testing.T) {
	t.Parallel()
	staff, keys := testkit.OpenStaff(t), cfaccesstest.AccessKeyServer(t)
	tenant := staff.Tenant(t, "A")
	staff.ChannelAccount(t, tenant, "facebook", "page-1", "Page 1") // người bán own của tenant
	staff.Grant(t, tenant, staff.User(t, tenant, "banhang@elimaz.test", "active"), []string{"report.sales.read"}, "all", "")
	staff.Grant(t, tenant, staff.User(t, tenant, "kho@elimaz.test", "active"), []string{"report.inventory.read"}, "all", "")

	started, release := make(chan struct{}), make(chan struct{})
	block := reporting.Def{
		Key: reporting.KeyDashboard, Family: reporting.FamilySales, Read: "report.sales.read",
		NewParams: func() reporting.Params { return &reporting.DashboardParams{} },
		JSON: func(ctx context.Context, _ pgx.Tx, _ reporting.DataScope, p reporting.Params) (any, error) {
			if dp, ok := p.(*reporting.DashboardParams); ok && dp.From == "2026-11-05" {
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
				}
			} else {
				<-ctx.Done() // ca quá hạn: chờ tới hết hạn
			}
			return nil, ctx.Err()
		},
	}
	busyDeps := testServerDeps(t, staff, keys, 0)
	busyDeps.Reports = reporting.NewInteractive(staff.App, reporting.InteractiveOptions{
		Slots: 1, Timeout: 5 * time.Second, LockTimeout: time.Second, Defs: map[reporting.Key]reporting.Def{block.Key: block},
	})
	srv := NewServer(busyDeps)
	const path = "/api/v1/reports/sales/dashboard"

	first := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		first <- serveAs(srv, keys, http.MethodGet, path+"?from=2026-11-05&to=2026-11-06", "banhang@elimaz.test", "", "")
	}()
	<-started
	rec := serveAs(srv, keys, http.MethodGet, path+"?from=2026-11-01&to=2026-11-02", "banhang@elimaz.test", "", "")
	expectRunInBackground(t, "hết chỗ", rec, "busy")
	close(release)
	<-first

	timeoutDeps := testServerDeps(t, staff, keys, 0)
	timeoutDeps.Reports = reporting.NewInteractive(staff.App, reporting.InteractiveOptions{
		Slots: 1, Timeout: 150 * time.Millisecond, LockTimeout: time.Second, Defs: map[reporting.Key]reporting.Def{block.Key: block},
	})
	rec = serveAs(NewServer(timeoutDeps), keys, http.MethodGet, path+"?from=2026-11-01&to=2026-11-02", "banhang@elimaz.test", "", "")
	expectRunInBackground(t, "quá hạn", rec, "timeout")

	for _, c := range []struct {
		name, query, email string
		status             int
		code               string
	}{
		{"from sai dạng", "?from=2026-13-01&to=2026-11-02", "banhang@elimaz.test", http.StatusBadRequest, "bad_date"},
		{"to không có trên lịch", "?from=2026-02-01&to=2026-02-30", "banhang@elimaz.test", http.StatusBadRequest, "bad_date"},
		{"thiếu from", "?to=2026-11-02", "banhang@elimaz.test", http.StatusBadRequest, "bad_date"},
		{"from sau to", "?from=2026-11-03&to=2026-11-02", "banhang@elimaz.test", http.StatusBadRequest, "bad_date"},
		{"367 ngày", "?from=2026-01-01&to=2027-01-02", "banhang@elimaz.test", http.StatusBadRequest, "range_too_long"},
		{"chỉ có quyền tồn", "?from=2026-11-01&to=2026-11-02", "kho@elimaz.test", http.StatusForbidden, "forbidden"},
	} {
		expect(t, c.name, serveAs(srv, keys, http.MethodGet, path+c.query, c.email, "", ""), c.status, c.code)
	}
}

func expectRunInBackground(t *testing.T, name string, rec *httptest.ResponseRecorder, reason string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: mã %d (%s)", name, rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 2 || body["status"] != "run_in_background" || body["reason"] != reason {
		t.Fatalf("%s: thân %s, mong run_in_background/%s", name, rec.Body.String(), reason)
	}
}
