package inventory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Đối chiếu tồn hằng đêm (kế hoạch M1, Task M1-36; spec 05 mục 2, KH13): mỗi tenant mỗi đêm so ba điều trên mọi dòng
// số dư — (a) mỗi ngăn: tổng sổ = cột số dư; (b) held = tổng giữ hàng held + đơn vị suất flash sale còn trống của đợt
// scheduled/open (M5 Task 202, 05 mục 7.4); (c) in_transit = tổng giữ hàng in_transit — và phép so đơn vị flash sale
// (reconcile_flash.go); ghi một dòng inventory.reconcile_runs; có lệch thì ghi lệnh cảnh báo. Chỉ báo, không sửa số.

const (
	// ReconcileSchedule là tên lịch (dòng platform.schedules chèn sẵn ở migration 0122: kỳ đầu 19:30 UTC).
	ReconcileSchedule = "inventory.reconcile"
	// ReconcileEvery là chu kỳ của lịch.
	ReconcileEvery = 24 * time.Hour
	// ReconcileBatch là số dòng số dư mỗi câu đối chiếu (mỗi câu ngắn, dưới statement_timeout).
	ReconcileBatch = 2000
	// StockMismatchAlertKind là tên loại lệnh cảnh báo lệch sổ.
	StockMismatchAlertKind = "inventory.stock_mismatch_alert"
)

// Các phép so (Mismatch.Check).
const (
	CheckLedgerVsBalance       = "ledger_vs_balance"          // tổng sổ của ngăn Bucket khác cột số dư
	CheckHeldVsReservations    = "held_vs_reservations"       // held khác giữ hàng đang held + đơn vị flash còn trống
	CheckTransitVsReservations = "in_transit_vs_reservations" // in_transit khác tổng giữ hàng đang in_transit
)

// Mismatch là một mục lệch: Expected là số theo sổ (CheckLedgerVsBalance) hoặc theo giữ hàng, Actual là cột số dư.
// Bucket chỉ có ở CheckLedgerVsBalance. Mục CheckFlashUnits chỉ có FlashItemID (không biến thể, kho). Không chứa tên
// hay dữ liệu khách.
type Mismatch struct {
	VariantID   uuid.UUID `json:"variantId,omitzero"`
	WarehouseID uuid.UUID `json:"warehouseId,omitzero"`
	FlashItemID uuid.UUID `json:"flashItemId,omitzero"`
	Check       string    `json:"check"`
	Bucket      string    `json:"bucket,omitempty"`
	Expected    int64     `json:"expected"`
	Actual      int64     `json:"actual"`
}

// Run là kết quả một lần đối chiếu của một tenant.
type Run struct {
	ID         uuid.UUID
	RanAt      time.Time
	OK         bool
	Checked    int
	Mismatches []Mismatch
}

// Reconcile so mọi dòng số dư của tenant đang đặt trong tx (tenancy.WithTenant) theo (variant_id, warehouse_id) tăng
// dần, mỗi câu batch dòng (≤ 0 hay lớn hơn ReconcileBatch: ReconcileBatch), rồi ghi một dòng inventory.reconcile_runs và trả kết quả. Không khóa,
// không đổi số dư, sổ hay giữ hàng.
func Reconcile(ctx context.Context, tx pgx.Tx, batch int) (Run, error) {
	if batch <= 0 || batch > ReconcileBatch {
		batch = ReconcileBatch
	}
	q := store.New(tx)
	run := Run{RanAt: time.Now().UTC(), Mismatches: []Mismatch{}}
	var afterV, afterW uuid.UUID
	for {
		rows, err := q.ReconcileBatch(ctx, store.ReconcileBatchParams{
			AfterVariant: afterV, AfterWarehouse: afterW, BatchSize: int32(batch),
		})
		if err != nil {
			return Run{}, fmt.Errorf("đối chiếu tồn: %w", err)
		}
		for _, r := range rows {
			run.Mismatches = append(run.Mismatches, compare(r)...)
		}
		run.Checked += len(rows)
		if len(rows) < batch {
			break
		}
		afterV, afterW = rows[len(rows)-1].VariantID, rows[len(rows)-1].WarehouseID
	}
	flash, err := flashMismatches(ctx, q)
	if err != nil {
		return Run{}, err
	}
	run.Mismatches = append(run.Mismatches, flash...)
	run.OK = len(run.Mismatches) == 0
	raw, err := json.Marshal(run.Mismatches)
	if err != nil {
		return Run{}, fmt.Errorf("mã hóa mục lệch: %w", err)
	}
	if run.ID, err = q.InsertReconcileRun(ctx, store.InsertReconcileRunParams{
		RanAt: run.RanAt, Ok: run.OK, Checked: int32(run.Checked), Mismatches: raw, //nolint:gosec // số dòng số dư < 2^31
	}); err != nil {
		return Run{}, fmt.Errorf("ghi lần đối chiếu: %w", err)
	}
	return run, nil
}

// compare trả các mục lệch của một dòng số dư.
func compare(r store.ReconcileBatchRow) []Mismatch {
	var out []Mismatch
	add := func(check, bucket string, expected int64, actual int32) {
		if expected != int64(actual) {
			out = append(out, Mismatch{
				VariantID: r.VariantID, WarehouseID: r.WarehouseID, Check: check, Bucket: bucket,
				Expected: expected, Actual: int64(actual),
			})
		}
	}
	add(CheckLedgerVsBalance, string(bucketOnHand), r.LedgerOnHand, r.OnHand)
	add(CheckLedgerVsBalance, string(bucketHeld), r.LedgerHeld, r.Held)
	add(CheckLedgerVsBalance, string(bucketInTransit), r.LedgerInTransit, r.InTransit)
	add(CheckLedgerVsBalance, string(bucketDamaged), r.LedgerDamaged, r.Damaged)
	add(CheckHeldVsReservations, "", r.ReservedHeld+r.FlashAvailable, r.Held)
	add(CheckTransitVsReservations, "", r.ReservedInTransit, r.InTransit)
	return out
}

// ReconcileKind là loại việc của lịch ReconcileSchedule: việc định kỳ lỗi thì chạy ở kỳ sau (spec 04 mục 5.1) — một
// lần, quá một kỳ chưa chạy thì expired.
func ReconcileKind() queue.Kind {
	return queue.Kind{Name: "task." + ReconcileSchedule, Policy: queue.Policy{MaxAttempts: 1, ExpiresAfter: ReconcileEvery}}
}

// NewReconcileHandler trả handler (transactional, theo tenant) của ReconcileKind: Reconcile rồi, khi có lệch tồn, ghi
// lệnh StockMismatchAlertKind; khi có lệch đơn vị flash sale, ghi lệnh FlashMismatchAlertKind; cùng tx (DedupeKey = mã
// lần chạy). Kết quả {"checked": n, "mismatches": m}.
func NewReconcileHandler(q queue.JobQueue) queue.Handler { //nolint:ireturn // Worker nhận Handler
	return reconcileHandler{q: q}
}

type reconcileHandler struct{ q queue.JobQueue }

func (reconcileHandler) Kind() queue.Kind { return ReconcileKind() }

func (h reconcileHandler) Handle(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	if j.TenantID == nil {
		return nil, fmt.Errorf("đối chiếu tồn: việc không có tenant: %w", queue.ErrPermanent)
	}
	run, err := Reconcile(ctx, tx, ReconcileBatch)
	if err != nil {
		return nil, err
	}
	variants := map[uuid.UUID]struct{}{}
	for _, m := range run.Mismatches {
		if m.Check != CheckFlashUnits {
			variants[m.VariantID] = struct{}{}
		}
	}
	if len(variants) > 0 {
		if _, _, err := h.q.Enqueue(ctx, tx, stockMismatchKind(), queue.NewJob{
			TenantID: j.TenantID, DedupeKey: run.ID.String(),
			Payload: mismatchPayload{RunID: run.ID, Count: len(variants)},
		}); err != nil {
			return nil, fmt.Errorf("ghi lệnh cảnh báo lệch sổ: %w", err)
		}
	}
	if err := h.enqueueFlashAlert(ctx, tx, j, run); err != nil {
		return nil, err
	}
	out, err := json.Marshal(map[string]int{"checked": run.Checked, "mismatches": len(run.Mismatches)})
	if err != nil {
		return nil, fmt.Errorf("mã hóa kết quả đối chiếu: %w", err)
	}
	return out, nil
}

type mismatchPayload struct {
	RunID uuid.UUID `json:"runId"`
	Count int       `json:"count"`
}

func stockMismatchKind() queue.Kind {
	return commands.CommandKind(StockMismatchAlertKind, 0, shortageAlertTimeout)
}

// NewStockMismatchCommand trả lệnh của StockMismatchAlertKind: Notify(key "stock-mismatch:<tenantId>", normal,
// "<n> mã hàng lệch sổ"); việc không tenant hay payload hỏng → rejected. Lỗi gửi xử lý như lệnh thiếu hàng (not_sent,
// tự gửi lại, chờ theo Telegram 429). Đăng ký vào commands.NewRunner.
func NewStockMismatchCommand(n Notifier, logger *slog.Logger) commands.Handler { //nolint:ireturn // Runner nhận Handler
	return stockMismatchAlert{n: n, log: logger}
}

type stockMismatchAlert struct {
	n   Notifier
	log *slog.Logger
}

func (stockMismatchAlert) Kind() queue.Kind { return stockMismatchKind() }

func (stockMismatchAlert) AutoRetryNotSent() bool { return true }

func (s stockMismatchAlert) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	var p mismatchPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || j.TenantID == nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: "bad_payload"}, nil
	}
	return notify(ctx, s.n, s.log, "stock-mismatch:"+j.TenantID.String(), fmt.Sprintf("%d mã hàng lệch sổ", p.Count),
		"gửi cảnh báo lệch sổ tồn lỗi"), nil
}
