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
	"elimaz.com.vn/crm/internal/testkit/reportseed"
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

// GET tổng quan trên kịch bản chín đơn (Task 229): 200 đúng hình dạng SalesDashboardReport, tiền là số nguyên JSON;
// người chỉ có P1 thấy số của P1; ngày không đơn → avgGoodsAmount null; kỳ 3 ngày → byDay 3 dòng.
func TestReportsDashboard_Ready(t *testing.T) {
	t.Parallel()
	staff, keys := testkit.OpenStaff(t), cfaccesstest.AccessKeyServer(t)
	tenant := staff.Tenant(t, "A")
	sc := reportseed.NineOrders(reportseed.New(t, staff.Owner, tenant), "2026-11-17")
	staff.Grant(t, tenant, staff.User(t, tenant, "banhang@elimaz.test", "active"), []string{"report.sales.read"}, "all", "")
	staff.Grant(t, tenant, staff.User(t, tenant, "p1@elimaz.test", "active"), []string{"report.sales.read"}, "page", sc.P1.String())
	srv := testServer(t, staff, keys, 0)
	const path = "/api/v1/reports/sales/dashboard"

	get := func(query, email string) map[string]any {
		t.Helper()
		rec := serveAs(srv, keys, http.MethodGet, path+query, email, "", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: mã %d (%s)", query, rec.Code, rec.Body.String())
		}
		var body map[string]any
		dec := json.NewDecoder(rec.Body)
		dec.UseNumber()
		if err := dec.Decode(&body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	num := func(v any) string {
		n, ok := v.(json.Number)
		if !ok {
			t.Fatalf("%v (%T) không phải số JSON", v, v)
		}
		return n.String()
	}
	amounts := func(v any) string {
		m, ok := v.(map[string]any)
		if !ok || len(m) != 3 {
			t.Fatalf("ReportAmounts sai hình dạng: %v", v)
		}
		return num(m["orders"]) + "/" + num(m["goodsAmount"]) + "/" + num(m["shippingFeeCharged"])
	}

	checkDashboardAll(t, get("?from=2026-11-17&to=2026-11-17", "banhang@elimaz.test"), num, amounts)

	p1 := get("?from=2026-11-17&to=2026-11-17", "p1@elimaz.test")
	if amounts(p1["placed"]) != "4/1150000/120000" || amounts(p1["active"]) != "4/1150000/120000" {
		t.Errorf("P1: placed %v, active %v", p1["placed"], p1["active"])
	}
	if pd, _ := p1["pending"].(map[string]any); num(pd["confirm"]) != "0" || num(pd["ship"]) != "0" || num(pd["cancelRequested"]) != "1" {
		t.Errorf("P1 pending = %v", p1["pending"])
	}

	empty := get("?from=2026-11-20&to=2026-11-20", "banhang@elimaz.test")
	if empty["avgGoodsAmount"] != nil || amounts(empty["placed"]) != "0/0/0" {
		t.Errorf("ngày không đơn: avg %v, placed %v", empty["avgGoodsAmount"], empty["placed"])
	}
	three := get("?from=2026-11-16&to=2026-11-18", "banhang@elimaz.test")
	days, _ := three["byDay"].([]any)
	if len(days) != 3 {
		t.Fatalf("byDay = %v", three["byDay"])
	}
	if d, _ := days[1].(map[string]any); d["day"] != "2026-11-17" || amounts(d["active"]) != "7/1910000/150000" ||
		amounts(d["delivered"]) != "2/750000/60000" {
		t.Errorf("byDay[1] = %v", days[1])
	}
}

// checkDashboardAll kiểm thân tổng quan ngày 17/11 của người phạm vi all trên kịch bản chín đơn.
func checkDashboardAll(t *testing.T, all map[string]any, num func(any) string, amounts func(any) string) {
	t.Helper()
	if len(all) != 13 || all["status"] != "ready" {
		t.Fatalf("thân: %v", all)
	}
	if p, _ := all["period"].(map[string]any); p["from"] != "2026-11-17" || p["to"] != "2026-11-17" {
		t.Errorf("period = %v", all["period"])
	}
	if _, err := time.Parse(time.RFC3339, all["generatedAt"].(string)); err != nil {
		t.Errorf("generatedAt: %v", err)
	}
	for k, want := range map[string]string{
		"placed": "8/2110000/150000", "active": "7/1910000/150000", "cancelled": "1/200000/0",
		"delivered": "2/750000/60000", "returned": "1/250000/30000",
	} {
		if got := amounts(all[k]); got != want {
			t.Errorf("%s = %s, mong %s", k, got, want)
		}
	}
	if got := num(all["avgGoodsAmount"]); got != "272857" {
		t.Errorf("avgGoodsAmount = %s", got)
	}
	cr, _ := all["customerReturns"].(map[string]any)
	pd, _ := all["pending"].(map[string]any)
	if num(cr["items"]) != "1" || num(cr["refundAmount"]) != "150000" ||
		num(pd["confirm"]) != "1" || num(pd["ship"]) != "1" || num(pd["cancelRequested"]) != "1" {
		t.Errorf("customerReturns = %v, pending = %v", cr, pd)
	}
	ch, _ := all["byChannel"].([]any)
	if len(ch) != 3 {
		t.Fatalf("byChannel = %v", all["byChannel"])
	}
	if row, _ := ch[0].(map[string]any); row["channel"] != "facebook" || amounts(row["placed"]) != "6/1500000/120000" ||
		amounts(row["active"]) != "5/1300000/120000" {
		t.Errorf("byChannel[0] = %v", ch[0])
	}
	if d, _ := all["byDay"].([]any); d == nil || len(d) != 0 {
		t.Errorf("byDay kỳ một ngày = %v", all["byDay"])
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
