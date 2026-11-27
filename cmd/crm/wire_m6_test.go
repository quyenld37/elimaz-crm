package main

import (
	"slices"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/modules/reporting"
)

// wireM6 nối đúng một lần phần chạy nền của M6 (Task 228): lệnh command.report_run, việc reporting.stock_snapshot và
// reporting.cleanup. Lịch còn rỗng tới khi Task 233, 235 làm thân việc (bản giữ chỗ không chạy định kỳ).
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
	if len(m.Schedules) != 0 {
		t.Errorf("lịch = %+v, mong rỗng tới Task 233, 235", m.Schedules)
	}
	if k := reporting.RunCommandKind(); k.Policy.Timeout != 80*time.Second || k.Policy.ExpiresAfter != time.Hour {
		t.Errorf("chính sách lệnh chạy nền = %+v", k.Policy)
	}
}
