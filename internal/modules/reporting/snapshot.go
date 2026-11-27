package reporting

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// Chụp tồn hằng ngày (BC03). Tệp giữ chỗ của Task 228: Task 233 (M6-10) viết thân SnapshotHandler và trả lịch ở
// snapshotSchedules, trong tệp này và queries/reporting/snapshot.sql; wiring.go đã nối sẵn.

// SnapshotKindName là tên việc giao dịch chụp tồn (theo tenant).
const SnapshotKindName = "reporting.stock_snapshot"

// SnapshotHandler chụp tồn những ngày chưa chụp (việc reporting.stock_snapshot). Bản giữ chỗ (Task 233).
type SnapshotHandler struct{}

// NewSnapshotHandler dựng việc chụp tồn.
func NewSnapshotHandler(_ Deps) *SnapshotHandler { return &SnapshotHandler{} }

// Kind là việc giao dịch reporting.stock_snapshot.
func (*SnapshotHandler) Kind() queue.Kind { return queue.Kind{Name: SnapshotKindName} }

// Handle của bản giữ chỗ (Task 233): lỗi vĩnh viễn, không thử lại.
func (*SnapshotHandler) Handle(_ context.Context, _ queue.Job, _ pgx.Tx) (json.RawMessage, error) {
	return nil, fmt.Errorf("chụp tồn: %w (%w)", ErrNotImplemented, queue.ErrPermanent)
}

// snapshotSchedules là lịch reporting.stock_snapshot (mỗi 900 s, theo tenant). Bản giữ chỗ không có lịch nào (Task 233
// trả lịch).
func snapshotSchedules() []schedules.Schedule { return nil }
