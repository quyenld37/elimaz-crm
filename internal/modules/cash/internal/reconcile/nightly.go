package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/cash/internal/postings"
	"elimaz.com.vn/crm/internal/modules/cash/internal/store"
)

// Loại phát hiện của lần chạy (chuỗi mở của hợp đồng ReconcileFinding.kind).
const (
	// KindLedgerNonzero: tổng mọi dòng sổ của tenant khác 0.
	KindLedgerNonzero = "ledger_nonzero"
	// KindTransferUnbalanced: bút toán ít hơn hai dòng, tổng khác 0 hay có dòng khác vận đơn (trigger đã chặn; kiểm lại).
	KindTransferUnbalanced = "transfer_unbalanced"
	// KindBalanceMismatch: số dư của vận đơn không khớp Target của trạng thái.
	KindBalanceMismatch = "balance_mismatch"
	// KindSkipped: vận đơn không đối chiếu ở lần này (detail.reason), không tính là lệch.
	KindSkipped = "skipped"
)

// Lý do bỏ qua (detail.reason của KindSkipped).
const (
	// SkipReturnedFinding: có phát hiện remitted_then_returned còn mở (đã báo; chờ kế toán điều chỉnh).
	SkipReturnedFinding = "remitted_then_returned"
	// SkipAdjusted: có điều chỉnh của nhân viên sau lần chốt bảng kê gần nhất (số dư lệch đích có chủ đích).
	SkipAdjusted = "adjusted"
	// SkipRecentStatus: vận đơn vừa đổi trạng thái (trong Options.Recent): sự kiện có thể chưa vào sổ.
	SkipRecentStatus = "recent_status"
)

const (
	// DefaultBatch là số bút toán, vận đơn mỗi câu đối chiếu (mỗi câu ngắn, dưới statement_timeout).
	DefaultBatch = 2000
	// DefaultRecent là khoảng "vừa đổi trạng thái" bỏ qua ở lần chạy.
	DefaultRecent = 15 * time.Minute
)

// Options là tham số một lần chạy: Slot là kỳ của lịch (khóa một lần chạy mỗi kỳ), Now là lúc chạy (đồng hồ của
// module), Recent ≤ 0 → DefaultRecent, Batch ≤ 0 hay lớn hơn DefaultBatch → DefaultBatch.
type Options struct {
	Slot, Now time.Time
	Recent    time.Duration
	Batch     int
}

// Finding là một phát hiện: vận đơn (rỗng với ledger_nonzero) và chi tiết chỉ mang mã, số tiền, trạng thái (chuỗi).
type Finding struct {
	Kind       string
	ShipmentID *uuid.UUID
	Detail     map[string]string
}

// Result là kết quả Run: Created = false khi kỳ đã có lần chạy (không ghi gì, RunID là lần chạy đó, phần còn lại rỗng).
// Problems là số phát hiện không phải skipped; OK khi Problems = 0.
type Result struct {
	RunID    uuid.UUID
	Created  bool
	OK       bool
	Problems int
	Findings []Finding
}

// RunForSlot trả mã lần chạy của kỳ slot (ok = false khi chưa có), trong tx của bên gọi (tenancy.WithTenant).
func RunForSlot(ctx context.Context, tx pgx.Tx, slot time.Time) (uuid.UUID, bool, error) {
	id, err := store.New(tx).ReconcileRunBySlot(ctx, pgtype.Timestamptz{Time: slot.UTC(), Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("đọc lần đối chiếu của kỳ: %w", err)
	}
	return id, true, nil
}

// Run đối chiếu sổ COD của tenant đang đặt trong tx (tenancy.WithTenant) rồi ghi một dòng cash.reconcile_runs của kỳ
// o.Slot và các phát hiện: (1) tổng mọi dòng = 0; (2) không bút toán lệch; (3) mỗi vận đơn có COD hay có dòng sổ: số dư
// khớp Target (bỏ qua — ghi skipped — vận đơn có remitted_then_returned còn mở, có điều chỉnh sau lần chốt gần nhất, hay
// vừa đổi trạng thái). Kỳ đã có lần chạy → Created = false, không ghi. Không khóa, không đổi sổ.
func Run(ctx context.Context, tx pgx.Tx, o Options) (Result, error) {
	if o.Batch <= 0 || o.Batch > DefaultBatch {
		o.Batch = DefaultBatch
	}
	if o.Recent <= 0 {
		o.Recent = DefaultRecent
	}
	if id, ok, err := RunForSlot(ctx, tx, o.Slot); err != nil || ok {
		return Result{RunID: id}, err
	}
	q := store.New(tx)
	var fs []Finding
	total, err := q.LedgerTotal(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("cộng sổ COD: %w", err)
	}
	if total != 0 {
		fs = append(fs, Finding{Kind: KindLedgerNonzero, Detail: map[string]string{"total": money(total)}})
	}
	unbalanced, err := transferFindings(ctx, q, o.Batch)
	if err != nil {
		return Result{}, err
	}
	shipments, err := shipmentFindings(ctx, q, o)
	if err != nil {
		return Result{}, err
	}
	fs = append(append(fs, unbalanced...), shipments...)
	return record(ctx, tx, o, fs)
}

func transferFindings(ctx context.Context, q *store.Queries, batch int) ([]Finding, error) {
	var out []Finding
	var after uuid.UUID
	for {
		rows, err := q.TransferChecks(ctx, store.TransferChecksParams{After: after, Lim: int32(batch)}) //nolint:gosec // ≤ DefaultBatch
		if err != nil {
			return nil, fmt.Errorf("kiểm cân hai vế bút toán: %w", err)
		}
		for _, r := range rows {
			if r.Lines < 2 || r.Total != 0 || r.OtherShipment > 0 {
				out = append(out, Finding{Kind: KindTransferUnbalanced, ShipmentID: new(r.ShipmentID), Detail: map[string]string{
					"transferId": r.ID.String(), "lines": strconv.FormatInt(r.Lines, 10), "total": money(r.Total),
					"otherShipmentLines": strconv.FormatInt(r.OtherShipment, 10),
				}})
			}
		}
		if len(rows) < batch {
			return out, nil
		}
		after = rows[len(rows)-1].ID
	}
}

func shipmentFindings(ctx context.Context, q *store.Queries, o Options) ([]Finding, error) {
	var out []Finding
	recent := o.Now.Add(-o.Recent)
	var after uuid.UUID
	for {
		rows, err := q.ReconcileShipments(ctx, store.ReconcileShipmentsParams{After: after, Lim: int32(o.Batch)}) //nolint:gosec // ≤ DefaultBatch
		if err != nil {
			return nil, fmt.Errorf("đọc số dư vận đơn để đối chiếu: %w", err)
		}
		for _, r := range rows {
			if f, ok := shipmentFinding(r, recent); ok {
				out = append(out, f)
			}
		}
		if len(rows) < o.Batch {
			return out, nil
		}
		after = rows[len(rows)-1].ID
	}
}

// shipmentFinding trả phát hiện của một vận đơn (ok = false khi số dư khớp đích).
func shipmentFinding(r store.ReconcileShipmentsRow, recent time.Time) (Finding, bool) {
	b := postings.Balances{
		ToCollect: r.ToCollect, CarrierHolding: r.CarrierHolding, ShopReceived: r.ShopReceived, CarrierFee: r.CarrierFee,
		SalesClearing: r.SalesClearing,
	}
	x := Target(r.Status, r.Partial, r.CodAmount, r.CodCollected)
	if x.Match(b) {
		return Finding{}, false
	}
	id := new(r.ID)
	skip := ""
	switch {
	case r.OpenReturnedFinding:
		skip = SkipReturnedFinding
	case r.AdjustedSinceConfirm:
		skip = SkipAdjusted
	case r.LastStatusAt.After(recent):
		skip = SkipRecentStatus
	}
	if skip != "" {
		return Finding{Kind: KindSkipped, ShipmentID: id, Detail: map[string]string{"reason": skip, "status": r.Status}}, true
	}
	return Finding{Kind: KindBalanceMismatch, ShipmentID: id, Detail: map[string]string{
		"status": r.Status, "toCollect": money(b.ToCollect), "carrierHolding": money(b.CarrierHolding),
		"shopReceived": money(b.ShopReceived), "carrierFee": money(b.CarrierFee), "salesClearing": money(b.SalesClearing),
		"expectedToCollect": money(x.ToCollect), "expectedSettled": money(x.Settled),
		"expectedSalesClearing": money(x.SalesClearing),
	}}, true
}

// record ghi lần chạy và phát hiện; kỳ đã có lần chạy (chèn trùng) → Created = false, không ghi phát hiện.
func record(ctx context.Context, tx pgx.Tx, o Options, fs []Finding) (Result, error) {
	q := store.New(tx)
	res := Result{Created: true, Findings: fs}
	for _, f := range fs {
		if f.Kind != KindSkipped {
			res.Problems++
		}
	}
	res.OK = res.Problems == 0
	id, err := q.InsertReconcileRun(ctx, store.InsertReconcileRunParams{
		RanAt: o.Now.UTC(), Ok: res.OK, Findings: int32(res.Problems), //nolint:gosec // số phát hiện < 2^31
		Slot: pgtype.Timestamptz{Time: o.Slot.UTC(), Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		id, _, err := RunForSlot(ctx, tx, o.Slot)
		return Result{RunID: id}, err
	}
	if err != nil {
		return Result{}, fmt.Errorf("ghi lần đối chiếu: %w", err)
	}
	res.RunID = id
	for _, f := range fs {
		detail, err := json.Marshal(f.Detail)
		if err != nil {
			return Result{}, fmt.Errorf("mã hóa chi tiết phát hiện: %w", err)
		}
		var ship pgtype.UUID
		if f.ShipmentID != nil {
			ship = pgtype.UUID{Bytes: *f.ShipmentID, Valid: true}
		}
		if err := q.InsertRunFinding(ctx, store.InsertRunFindingParams{
			RunID: pgtype.UUID{Bytes: id, Valid: true}, Kind: f.Kind, ShipmentID: ship, Detail: detail,
		}); err != nil {
			return Result{}, fmt.Errorf("ghi phát hiện đối chiếu: %w", err)
		}
	}
	return res, nil
}

func money(v int64) string { return strconv.FormatInt(v, 10) }
