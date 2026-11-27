package reporting

import (
	"slices"
	"time"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// Deps là phụ thuộc của phần chạy nền của báo cáo (worker), cmd/crm dựng một lần.
type Deps struct {
	DB      *db.DB
	Files   files.FileStore
	Bus     realtime.Bus
	Queue   queue.JobQueue
	Cfg     config.Config
	Metrics *metrics.Registry
	Now     func() time.Time
}

// JobHandlers là lệnh của M6 cho Runner của worker: command.report_run (runs.go).
func JobHandlers(d Deps) []commands.Handler {
	return []commands.Handler{NewRunHandler(d)}
}

// TxHandlers là việc giao dịch của M6 cho hàng đợi của worker: reporting.stock_snapshot (snapshot.go),
// reporting.cleanup (runs.go).
func TxHandlers(d Deps) []queue.Handler {
	return []queue.Handler{NewSnapshotHandler(d), NewCleanupHandler(d)}
}

// Schedules là lịch theo tenant của M6: reporting.stock_snapshot (snapshot.go), reporting.cleanup (runs.go). Mỗi tệp tự
// trả lịch của mình khi task của nó làm xong thân việc.
func Schedules() []schedules.Schedule {
	return slices.Concat(snapshotSchedules(), cleanupSchedules())
}
