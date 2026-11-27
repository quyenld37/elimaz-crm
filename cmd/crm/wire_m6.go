package main

import (
	"time"

	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// reportLockTimeout là lock_timeout của báo cáo xem tại chỗ (Global Constraints M6).
const reportLockTimeout = time.Second

// reportInteractive dựng bộ chạy báo cáo tại chỗ của api (kế hoạch M6, Task 228): REPORT_API_SLOTS chỗ,
// REPORT_INTERACTIVE_TIMEOUT mỗi lần.
func reportInteractive(cfg config.Config, d *db.DB, reg *metrics.Registry) *reporting.Interactive {
	return reporting.NewInteractive(d, reporting.InteractiveOptions{
		Slots: cfg.Report.APISlots, Timeout: cfg.Report.InteractiveTimeout, LockTimeout: reportLockTimeout, Metrics: reg,
	})
}

// m6Parts là phần chạy nền của báo cáo M6 cho worker: lệnh chạy nền, việc chụp tồn và dọn, lịch theo tenant.
type m6Parts struct {
	Commands  []commands.Handler
	Jobs      []queue.Handler
	Schedules []schedules.Schedule
}

// wireM6 gọi một lần JobHandlers, TxHandlers, Schedules của reporting (Task 228); task sau của M6 chỉ sửa tệp của
// mình trong module.
func wireM6(d reporting.Deps) m6Parts {
	return m6Parts{
		Commands: reporting.JobHandlers(d), Jobs: reporting.TxHandlers(d), Schedules: reporting.Schedules(),
	}
}
