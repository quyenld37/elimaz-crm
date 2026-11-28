package main

import (
	"slices"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/modules/reporting"
)

// wireM6 nối đúng một lần phần chạy nền của M6 (Task 228): lệnh command.report_run, việc reporting.stock_snapshot và
// reporting.cleanup. Lịch theo tenant: reporting.stock_snapshot (Task 233), reporting.cleanup (Task 235).
func TestWireM6(t *testing.T) {
	t.Parallel()
	m := wireM6(reporting.Deps{})
	var cmds, jobs []string
	for _, c := range m.Commands {
		cmds = append(cmds, c.Kind().Name)
	}
	for _, j := range m.Jobs {
		jobs = append(jobs, j.Kind().Name)
	}
	if !slices.Equal(cmds, []string{"command.report_run"}) {
		t.Errorf("lệnh = %v", cmds)
	}
	if !slices.Equal(jobs, []string{"reporting.stock_snapshot", "reporting.cleanup"}) {
		t.Errorf("việc = %v", jobs)
	}
	var scheds []string
	for _, s := range m.Schedules {
		scheds = append(scheds, s.Name)
	}
	if !slices.Equal(scheds, []string{"reporting.stock_snapshot", "reporting.cleanup"}) {
		t.Errorf("lịch = %v, mong reporting.stock_snapshot, reporting.cleanup", scheds)
	}
	if k := reporting.RunCommandKind(); k.Policy.Timeout != 80*time.Second || k.Policy.ExpiresAfter != time.Hour {
		t.Errorf("chính sách lệnh chạy nền = %+v", k.Policy)
	}
}
