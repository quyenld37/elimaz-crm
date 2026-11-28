package cash

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/cash/internal/reconcile"
	"elimaz.com.vn/crm/internal/modules/cash/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng reconcile (kế hoạch M3, Task M3-33): đối chiếu sổ COD hằng đêm. Lịch cash.reconcile (theo tenant, mỗi 24 giờ,
// kỳ đầu 02:30 giờ Việt Nam — dòng platform.schedules chèn ở 0321) ghi lệnh cùng tên; lệnh chạy reconcile.Run trong
// một transaction của tenant, có phát hiện thì ghi lệnh cảnh báo ReconcileAlertKind cùng transaction.

const (
	// ReconcileScheduleName là tên lịch và tên loại lệnh đối chiếu.
	ReconcileScheduleName = "cash.reconcile"
	// ReconcileAlertKind là tên loại lệnh cảnh báo thường "cod-reconcile:<ngày>" (chỉ mang số lượng).
	ReconcileAlertKind = "cash.cod_reconcile_alert"

	reconcileEvery   = 24 * time.Hour
	reconcileTimeout = 2 * time.Minute
	reasonRunFailed  = "reconcile_failed"
	// defaultFindingLimit: số phát hiện mỗi trang khi không gửi limit; maxFindingLimit: trần.
	defaultFindingLimit = 50
	maxFindingLimit     = 200
)

// Loại phát hiện, lý do bỏ qua của lần đối chiếu (ReconcileFinding.Kind, Detail["reason"]).
const (
	FindingLedgerNonzero      = reconcile.KindLedgerNonzero
	FindingTransferUnbalanced = reconcile.KindTransferUnbalanced
	FindingBalanceMismatch    = reconcile.KindBalanceMismatch
	FindingSkipped            = reconcile.KindSkipped
)

// ErrReconcileRunNotFound báo lần đối chiếu không có hay của tenant khác.
var ErrReconcileRunNotFound = errors.New("cash: không có lần đối chiếu")

// vnZone là giờ Việt Nam (UTC+7) cho ngày của cảnh báo đối chiếu.
var vnZone = time.FixedZone("ICT", 7*3600) //nolint:gochecknoglobals // hằng múi giờ

func (*Module) reconcileInbound() []webhooks.InboundHandler { return nil }
func (*Module) reconcileConsumers() []events.Consumer       { return nil }
func (*Module) reconcileCleanups() []schedules.CleanupFunc  { return nil }

// reconcileHandlers: lệnh đối chiếu của lịch khi có database (cùng điều kiện với lịch).
func (m *Module) reconcileHandlers() []commands.Handler {
	return withDB(m.d.DB, commands.Handler(reconcileCommand{s: m.Service}))
}

// reconcileSchedules: lịch cash.reconcile (86.400 s, theo tenant) khi có database.
func (m *Module) reconcileSchedules() []schedules.Schedule {
	return withDB(m.d.DB, schedules.Schedule{
		Name: ReconcileScheduleName, Every: reconcileEvery, PerTenant: true, Kind: reconcileKind(),
	})
}

// withDB trả items khi có database, nil khi không (vai trò không nối database).
func withDB[T any](d *db.DB, items ...T) []T {
	if d == nil {
		return nil
	}
	return items
}

func reconcileKind() queue.Kind {
	return commands.CommandKind(ReconcileScheduleName, reconcileEvery, reconcileTimeout)
}

func reconcileAlertKind() queue.Kind {
	return commands.CommandKind(ReconcileAlertKind, 0, alertTimeout)
}

// reconcileCommand là lệnh của lịch: không gọi ra ngoài; một lần chạy mỗi (tenant, kỳ) nhờ unique của
// cash.reconcile_runs, nên chạy lại an toàn.
type reconcileCommand struct{ s *Service }

type slotPayload struct {
	Slot time.Time `json:"slot"`
}

type reconcileAlertPayload struct {
	Date     string `json:"date"`
	Findings int    `json:"findings"`
}

func (reconcileCommand) Kind() queue.Kind       { return reconcileKind() }
func (reconcileCommand) AutoRetryNotSent() bool { return true }

// Lookup: kỳ đã có lần chạy → done; chưa → not_sent (chạy lại); lỗi đọc → unknown.
func (c reconcileCommand) Lookup(ctx context.Context, j queue.Job) (string, error) {
	p, ok := decodeSlot(j)
	if !ok {
		return commands.StatusNotSent, nil
	}
	var found bool
	err := tenancy.WithTenant(ctx, c.s.d.DB, *j.TenantID, func(tx pgx.Tx) error {
		var err error
		_, found, err = reconcile.RunForSlot(ctx, tx, p.Slot)
		return err //nolint:wrapcheck // reconcile đã bọc
	}, db.TxWorker)
	switch {
	case err != nil:
		return commands.StatusUnknown, fmt.Errorf("tra lần đối chiếu của kỳ: %w", err)
	case found:
		return commands.StatusDone, nil
	}
	return commands.StatusNotSent, nil
}

// Execute chạy reconcile.Run của kỳ trong một transaction của tenant; lần chạy mới có phát hiện (không tính skipped) →
// ghi lệnh ReconcileAlertKind (DedupeKey = mã lần chạy) cùng transaction. Kết quả {"runId", "created", "findings"}.
// Payload hỏng → rejected; lỗi database → not_sent (chạy lại trong kỳ).
func (c reconcileCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	p, ok := decodeSlot(j)
	if !ok {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil
	}
	var res reconcile.Result
	err := tenancy.WithTenant(ctx, c.s.d.DB, *j.TenantID, func(tx pgx.Tx) error {
		var err error
		res, err = reconcile.Run(ctx, tx, reconcile.Options{Slot: p.Slot, Now: c.s.d.Now()})
		if err != nil || !res.Created || res.OK {
			return err //nolint:wrapcheck // reconcile đã bọc
		}
		_, _, err = c.s.d.Queue.Enqueue(ctx, tx, reconcileAlertKind(), queue.NewJob{
			TenantID: j.TenantID, DedupeKey: "cod-reconcile:" + res.RunID.String(),
			Payload: reconcileAlertPayload{Date: p.Slot.In(vnZone).Format(time.DateOnly), Findings: res.Problems},
		})
		if err != nil {
			return fmt.Errorf("ghi lệnh cảnh báo đối chiếu: %w", err)
		}
		return nil
	}, db.TxWorker)
	if err != nil {
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonRunFailed}, nil //nolint:nilerr // chạy lại trong kỳ
	}
	b, err := json.Marshal(map[string]any{"runId": res.RunID, "created": res.Created, "findings": res.Problems})
	if err != nil {
		return commands.Outcome{}, fmt.Errorf("mã hóa kết quả đối chiếu: %w", err)
	}
	return commands.Outcome{Status: commands.StatusDone, Result: b}, nil
}

func decodeSlot(j queue.Job) (slotPayload, bool) {
	var p slotPayload
	err := json.Unmarshal(j.Payload, &p)
	return p, err == nil && !p.Slot.IsZero() && j.TenantID != nil
}

// reconcileAlert là lệnh ReconcileAlertKind: Notify(key "cod-reconcile:<ngày>", normal, "<n> lệch sổ COD") — không mã
// đơn, không dữ liệu khách. Kết quả theo notifyOutcome.
type reconcileAlert struct{ n observability.Alerter }

func (reconcileAlert) Kind() queue.Kind       { return reconcileAlertKind() }
func (reconcileAlert) AutoRetryNotSent() bool { return true }

func (a reconcileAlert) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	var p reconcileAlertPayload
	ok := json.Unmarshal(j.Payload, &p) == nil && p.Date != "" && p.Findings > 0
	text := fmt.Sprintf("Đối chiếu sổ COD đêm %s: %d phát hiện lệch. Kế toán xem ở Kết quả đối chiếu.", p.Date, p.Findings)
	return notifyOutcome(ctx, a.n, ok, "cod-reconcile:"+p.Date, text), nil
}

// NewReconcileAlertCommand trả lệnh ReconcileAlertKind gửi qua n. cmd/crm đăng ký cùng các lệnh cảnh báo khác (dùng
// chung Alerter của worker), không qua Module.CommandHandlers.
func NewReconcileAlertCommand(n observability.Alerter) commands.Handler { //nolint:ireturn // Runner nhận Handler
	return reconcileAlert{n: n}
}

// ReconcileFinding là một phát hiện của lần đối chiếu: Detail chỉ mang mã, số tiền (chuỗi), trạng thái.
type ReconcileFinding struct {
	ID, RunID  uuid.UUID
	Kind       string
	ShipmentID *uuid.UUID
	Detail     map[string]string
	CreatedAt  time.Time
}

// ReconcileFindingPage là một trang phát hiện của lần chạy RunID (nil: chưa chạy lần nào); Next là con trỏ trang sau.
type ReconcileFindingPage struct {
	RunID *uuid.UUID
	RunAt time.Time
	Items []ReconcileFinding
	Next  *uuid.UUID
}

// ListReconcileFindings đọc trong tx của bên gọi (tenancy.WithTenant) các phát hiện của lần chạy runID (nil: lần gần
// nhất) gắn vận đơn của người bán seller hay không gắn vận đơn (cấp tenant; ADR-29, QĐ-531-2) theo mã tăng dần, sau mã after (nil: từ đầu), tối đa limit (≤ 0: 50; trần 200). Lần chạy không có →
// ErrReconcileRunNotFound; chưa chạy lần nào (runID nil) → trang rỗng, RunID nil.
func (*Service) ListReconcileFindings(ctx context.Context, tx pgx.Tx, seller uuid.UUID, runID, after *uuid.UUID, limit int,
) (ReconcileFindingPage, error) {
	if limit <= 0 {
		limit = defaultFindingLimit
	}
	limit = min(limit, maxFindingLimit)
	q := store.New(tx)
	run, err := q.GetReconcileRun(ctx, db.NullUUID(runID))
	switch {
	case errors.Is(err, pgx.ErrNoRows) && runID == nil:
		return ReconcileFindingPage{Items: []ReconcileFinding{}}, nil
	case errors.Is(err, pgx.ErrNoRows):
		return ReconcileFindingPage{}, ErrReconcileRunNotFound
	case err != nil:
		return ReconcileFindingPage{}, fmt.Errorf("đọc lần đối chiếu: %w", err)
	}
	var from uuid.UUID
	if after != nil {
		from = *after
	}
	rows, err := q.ListRunFindings(ctx, store.ListRunFindingsParams{
		RunID: pgtype.UUID{Bytes: run.ID, Valid: true}, SellerID: seller, After: from, Lim: int32(limit + 1),
	})
	if err != nil {
		return ReconcileFindingPage{}, fmt.Errorf("đọc phát hiện đối chiếu: %w", err)
	}
	page := ReconcileFindingPage{RunID: new(run.ID), RunAt: run.RanAt, Items: make([]ReconcileFinding, 0, len(rows))}
	if len(rows) > limit {
		rows = rows[:limit]
		page.Next = new(rows[limit-1].ID)
	}
	for _, r := range rows {
		f := ReconcileFinding{ID: r.ID, RunID: run.ID, Kind: r.Kind, CreatedAt: r.CreatedAt, Detail: detailStrings(r.Detail)}
		if r.ShipmentID.Valid {
			f.ShipmentID = new(uuid.UUID(r.ShipmentID.Bytes))
		}
		page.Items = append(page.Items, f)
	}
	return page, nil
}

// detailStrings đọc detail (jsonb object) thành map chuỗi: giá trị không phải chuỗi lấy dạng JSON của nó.
func detailStrings(raw []byte) map[string]string {
	var m map[string]json.RawMessage
	out := map[string]string{}
	if json.Unmarshal(raw, &m) != nil {
		return out
	}
	for k, v := range m {
		var str string
		if json.Unmarshal(v, &str) == nil {
			out[k] = str
			continue
		}
		out[k] = string(v)
	}
	return out
}
