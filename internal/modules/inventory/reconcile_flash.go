package inventory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Đối chiếu đêm cho đơn vị suất flash sale (kế hoạch M5, Task 202 / M5-19; 05 mục 7.4): với mọi món, không đơn vị
// available nào thuộc đợt đã đóng; số đơn vị held/sold của mỗi dòng đơn bằng số lượng của dòng. Lệch → mục
// CheckFlashUnits (chỉ mã món) và cảnh báo normal khóa reconcile:flash:<tenant>.

const (
	// CheckFlashUnits là mục kiểm đơn vị suất của món lệch (còn trống trong đợt đã đóng, hay số đơn vị của một dòng đơn khác số
	// lượng dòng). Expected là số đúng (0, hay số lượng dòng), Actual là số đơn vị.
	CheckFlashUnits = "flash_units"
	// FlashMismatchAlertKind là tên loại lệnh cảnh báo lệch đơn vị suất.
	FlashMismatchAlertKind = "inventory.flash_mismatch_alert"
	// maxFlashAlertItems: số mã món tối đa trong một cảnh báo (phần còn lại chỉ đếm).
	maxFlashAlertItems = 10
)

// flashMismatches trả các mục CheckFlashUnits của tenant trong tx (hai câu đọc, không khóa).
func flashMismatches(ctx context.Context, q *store.Queries) ([]Mismatch, error) {
	closed, err := q.FlashAvailableInClosedSales(ctx)
	if err != nil {
		return nil, fmt.Errorf("đối chiếu đơn vị suất của đợt đã đóng: %w", err)
	}
	var out []Mismatch
	for _, r := range closed {
		out = append(out, Mismatch{FlashItemID: r.FlashSaleItemID, Check: CheckFlashUnits, Expected: 0, Actual: r.Units})
	}
	lines, err := q.FlashUnitsVsOrderLines(ctx)
	if err != nil {
		return nil, fmt.Errorf("đối chiếu đơn vị suất với dòng đơn: %w", err)
	}
	for _, r := range lines {
		out = append(out, Mismatch{
			FlashItemID: r.FlashSaleItemID, Check: CheckFlashUnits, Expected: r.Quantity, Actual: r.Units,
		})
	}
	return out, nil
}

type flashMismatchPayload struct {
	RunID uuid.UUID   `json:"runId"`
	Items []uuid.UUID `json:"items"`
}

// enqueueFlashAlert ghi lệnh FlashMismatchAlertKind (mã các món lệch, không trùng, tăng dần) khi run có mục
// CheckFlashUnits.
func (h reconcileHandler) enqueueFlashAlert(ctx context.Context, tx pgx.Tx, j queue.Job, run Run) error {
	var items []uuid.UUID
	for _, m := range run.Mismatches {
		if m.Check == CheckFlashUnits {
			items = append(items, m.FlashItemID)
		}
	}
	if len(items) == 0 {
		return nil
	}
	slices.SortFunc(items, compareUUID)
	if _, _, err := h.q.Enqueue(ctx, tx, flashMismatchKind(), queue.NewJob{
		TenantID: j.TenantID, DedupeKey: run.ID.String(),
		Payload: flashMismatchPayload{RunID: run.ID, Items: slices.Compact(items)},
	}); err != nil {
		return fmt.Errorf("ghi lệnh cảnh báo lệch đơn vị suất: %w", err)
	}
	return nil
}

func flashMismatchKind() queue.Kind {
	return commands.CommandKind(FlashMismatchAlertKind, 0, shortageAlertTimeout)
}

// NewFlashMismatchCommand trả lệnh của FlashMismatchAlertKind: Notify(key "reconcile:flash:<tenantId>", normal,
// "<n> món flash sale lệch đơn vị suất: <mã món>, …" — tối đa 10 mã); việc không tenant hay payload hỏng → rejected.
// Lỗi gửi xử lý như lệnh lệch sổ. Đăng ký vào commands.NewRunner.
func NewFlashMismatchCommand(n Notifier, logger *slog.Logger) commands.Handler { //nolint:ireturn // Runner nhận Handler
	return flashMismatchAlert{n: n, log: logger}
}

type flashMismatchAlert struct {
	n   Notifier
	log *slog.Logger
}

func (flashMismatchAlert) Kind() queue.Kind { return flashMismatchKind() }

func (flashMismatchAlert) AutoRetryNotSent() bool { return true }

func (s flashMismatchAlert) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	var p flashMismatchPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || j.TenantID == nil || len(p.Items) == 0 {
		return commands.Outcome{Status: commands.StatusRejected, Reason: "bad_payload"}, nil
	}
	ids := make([]string, 0, min(len(p.Items), maxFlashAlertItems))
	for _, id := range p.Items[:min(len(p.Items), maxFlashAlertItems)] {
		ids = append(ids, id.String())
	}
	text := fmt.Sprintf("%d món flash sale lệch đơn vị suất: %s", len(p.Items), strings.Join(ids, ", "))
	return notify(ctx, s.n, s.log, "reconcile:flash:"+j.TenantID.String(), text, "gửi cảnh báo lệch đơn vị suất lỗi"), nil
}
