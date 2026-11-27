package reporting

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// Lần chạy nền: tạo, chạy bằng lệnh có tra lại, xem, báo hiệu, dọn (BC05, vùng cao). Tệp giữ chỗ của Task 228: Task
// 235 (M6-12) viết thân RunHandler, CleanupHandler và trả lịch dọn ở cleanupSchedules, trong tệp này và
// queries/reporting/runs.sql; wiring.go đã nối sẵn.

// Tên loại lệnh, việc, lịch của lần chạy nền (Global Constraints M6).
const (
	RunCommandName  = "command.report_run"
	CleanupKindName = "reporting.cleanup"
)

// Lệnh command.report_run: hết hạn sau 1 giờ, mỗi lần chạy tối đa 80 s (REPORT_BACKGROUND_TIMEOUT dưới 75 s cộng thời
// gian ghi tệp).
const (
	runCommandExpiresAfter = time.Hour
	runCommandTimeout      = 80 * time.Second
)

// RunCommandKind là loại lệnh chạy một lần chạy nền.
func RunCommandKind() queue.Kind {
	return commands.CommandKind(RunCommandName, runCommandExpiresAfter, runCommandTimeout)
}

// RunHandler chạy một lần chạy nền (lệnh command.report_run). Bản giữ chỗ: Execute trả ErrNotImplemented (không có
// đường nào ghi lệnh này trước Task 235).
type RunHandler struct{}

// NewRunHandler dựng handler của lệnh chạy nền.
func NewRunHandler(_ Deps) *RunHandler { return &RunHandler{} }

// Kind là RunCommandKind.
func (*RunHandler) Kind() queue.Kind { return RunCommandKind() }

// AutoRetryNotSent trả true: lần chạy nền không gửi gì ra ngoài CRM, chạy lại an toàn.
func (*RunHandler) AutoRetryNotSent() bool { return true }

// Execute của bản giữ chỗ (Task 235).
func (*RunHandler) Execute(_ context.Context, _ queue.Job) (commands.Outcome, error) {
	return commands.Outcome{}, fmt.Errorf("lần chạy nền: %w", ErrNotImplemented)
}

// CleanupHandler dọn lần chạy hết hạn, tệp hết hạn (việc reporting.cleanup, theo tenant). Bản giữ chỗ (Task 235).
type CleanupHandler struct{}

// NewCleanupHandler dựng việc dọn lần chạy.
func NewCleanupHandler(_ Deps) *CleanupHandler { return &CleanupHandler{} }

// Kind là việc giao dịch reporting.cleanup.
func (*CleanupHandler) Kind() queue.Kind { return queue.Kind{Name: CleanupKindName} }

// Handle của bản giữ chỗ (Task 235): lỗi vĩnh viễn, không thử lại.
func (*CleanupHandler) Handle(_ context.Context, _ queue.Job, _ pgx.Tx) (json.RawMessage, error) {
	return nil, fmt.Errorf("dọn lần chạy: %w (%w)", ErrNotImplemented, queue.ErrPermanent)
}

// cleanupSchedules là lịch reporting.cleanup (mỗi 3.600 s, theo tenant). Bản giữ chỗ không có lịch nào, để việc giữ
// chỗ không chạy định kỳ (Task 235 trả lịch).
func cleanupSchedules() []schedules.Schedule { return nil }
