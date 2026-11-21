package cash

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash/internal/adjust"
	"elimaz.com.vn/crm/internal/modules/cash/internal/postings"
	"elimaz.com.vn/crm/internal/modules/cash/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng adjustments (kế hoạch M3, Task M3-33): điều chỉnh COD có xem trước (TI08). Xem trước không đổi gì; ghi khóa sổ
// của vận đơn, tính lại bản xem trước, so token rồi ghi bút toán đảo (Reverse) hay chuyển ngăn (adjustment) — không xóa
// dòng cũ. Luật dựng bút toán ở internal/adjust.

func (*Module) adjustmentsHandlers() []commands.Handler       { return nil }
func (*Module) adjustmentsInbound() []webhooks.InboundHandler { return nil }
func (*Module) adjustmentsConsumers() []events.Consumer       { return nil }
func (*Module) adjustmentsSchedules() []schedules.Schedule    { return nil }
func (*Module) adjustmentsCleanups() []schedules.CleanupFunc  { return nil }

// Chế độ điều chỉnh.
const (
	AdjustReverse = adjust.ModeReverse
	AdjustMove    = adjust.ModeMove
)

var (
	// ErrAdjustmentInvalid báo yêu cầu điều chỉnh sai (chế độ, ngăn, số tiền, trường thừa, lý do quá 500 ký tự).
	ErrAdjustmentInvalid = adjust.ErrInvalid
	// ErrAdjustReasonRequired báo thiếu lý do điều chỉnh.
	ErrAdjustReasonRequired = adjust.ErrReasonRequired
	// ErrAdjustTargetNotFound báo bút toán cần đảo không thuộc sổ của vận đơn.
	ErrAdjustTargetNotFound = adjust.ErrTargetNotFound
	// ErrAdjustmentBlocked báo bút toán không được đảo (đã đảo, là bút toán đảo, expected của vận đơn chưa kết thúc).
	ErrAdjustmentBlocked = adjust.ErrBlocked
	// ErrPreviewStale báo token khác bản xem trước tính lại lúc ghi (sổ đã đổi). Không ghi gì.
	ErrPreviewStale = adjust.ErrStale
)

// CODAdjustmentInput là yêu cầu điều chỉnh sổ COD của vận đơn ShipmentID: reverse cần ReversesTransferID; move cần
// From, To, Amount; Reason bắt buộc; Actor là nhân viên.
type CODAdjustmentInput struct {
	ShipmentID         uuid.UUID
	Mode               string
	ReversesTransferID *uuid.UUID
	From, To           string
	Amount             int64
	Reason             string
	Actor              uuid.UUID
}

// CODAdjustmentPreview là bản xem trước: số dư trước, sau, bút toán sẽ ghi, cảnh báo, token.
type CODAdjustmentPreview struct {
	Before, After Balances
	Kind          string
	Entries       []Entry
	Warnings      []string
	Token         string
}

// PreviewCODAdjustment dựng bản xem trước của in trong tx của bên gọi (tenancy.WithTenant), không ghi, không khóa.
// Vận đơn không có hay của tenant khác → ErrShipmentNotFound; yêu cầu sai → ErrAdjustmentInvalid,
// ErrAdjustReasonRequired; bút toán cần đảo không thuộc vận đơn → ErrAdjustTargetNotFound; không được đảo →
// ErrAdjustmentBlocked.
func (*Service) PreviewCODAdjustment(ctx context.Context, tx pgx.Tx, in CODAdjustmentInput) (CODAdjustmentPreview, error) {
	p, err := planAdjustment(ctx, tx, in, false)
	if err != nil {
		return CODAdjustmentPreview{}, err
	}
	out := CODAdjustmentPreview{
		Before: Balances(p.Before), After: Balances(p.After), Kind: p.Kind, Warnings: p.Warnings, Token: p.Token,
		Entries: make([]Entry, len(p.Entries)),
	}
	for i, e := range p.Entries {
		out.Entries[i] = Entry(e)
	}
	return out, nil
}

// CreateCODAdjustment ghi điều chỉnh in theo bản xem trước có token, trong tx của bên gọi (WriteTx): khóa sổ của vận
// đơn (LockShipmentCOD, cùng khóa với consumer sổ và lô ghi bảng kê), tính lại bản xem trước, token khác →
// ErrPreviewStale; reverse → Reverse (khóa "reversal:<bút toán>"), move → PostWithID kind adjustment (khóa
// "adjustment:<token>"); tác nhân là nhân viên in.Actor. Không sửa, không xóa dòng cũ. Lỗi như PreviewCODAdjustment;
// ErrAdjustmentBlocked, ErrAdjustTargetNotFound lúc ghi bọc thêm ErrPreviewStale (errors.Is đúng với cả hai).
// Trả bút toán vừa ghi. Bên gọi ghi audit.
func (s *Service) CreateCODAdjustment(ctx context.Context, tx pgx.Tx, in CODAdjustmentInput, token string) (CODTransfer, error) {
	p, err := planAdjustment(ctx, tx, in, true)
	if errors.Is(err, ErrAdjustmentBlocked) || errors.Is(err, ErrAdjustTargetNotFound) {
		// Cùng yêu cầu trên cùng sổ thì xem trước đã từ chối: sổ đã đổi từ lúc xem trước (hay token không từ bản xem
		// trước nào).
		return CODTransfer{}, fmt.Errorf("%w: %w", ErrPreviewStale, err)
	}
	if err != nil {
		return CODTransfer{}, err
	}
	if err := p.Check(token); err != nil {
		return CODTransfer{}, err //nolint:wrapcheck // sentinel của cash (ErrPreviewStale)
	}
	actor := Actor{Type: ActorStaff, ID: in.Actor}
	var id uuid.UUID
	if p.Kind == KindReversal {
		if id, err = Reverse(ctx, tx, *p.ReversesTransferID, in.Reason, actor); err != nil {
			return CODTransfer{}, err
		}
	} else {
		t := Transfer{
			ShipmentID: in.ShipmentID, Kind: KindAdjustment, IdempotencyKey: p.IdempotencyKey(), Reason: in.Reason,
			Actor: actor, Entries: make([]Entry, len(p.Entries)),
		}
		for i, e := range p.Entries {
			t.Entries[i] = Entry(e)
		}
		var created bool
		if id, created, err = PostWithID(ctx, tx, t); err != nil {
			return CODTransfer{}, err
		}
		if !created {
			return CODTransfer{}, ErrPreviewStale
		}
	}
	c, err := s.ShipmentCOD(ctx, tx, in.ShipmentID)
	if err != nil {
		return CODTransfer{}, err
	}
	for _, t := range c.Transfers {
		if t.ID == id {
			return t, nil
		}
	}
	return CODTransfer{}, fmt.Errorf("đọc bút toán điều chỉnh vừa ghi %s: %w", id, ErrTransferNotFound)
}

// planAdjustment đọc sổ của vận đơn (lock: sau khóa sổ của vận đơn) và dựng bản xem trước của in.
func planAdjustment(ctx context.Context, tx pgx.Tx, in CODAdjustmentInput, lock bool) (adjust.Plan, error) {
	q := store.New(tx)
	if lock {
		if err := q.LockShipmentCOD(ctx, in.ShipmentID); err != nil {
			return adjust.Plan{}, fmt.Errorf("khóa sổ COD của vận đơn: %w", err)
		}
	}
	status, err := q.AdjustShipmentStatus(ctx, in.ShipmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return adjust.Plan{}, ErrShipmentNotFound
	}
	if err != nil {
		return adjust.Plan{}, fmt.Errorf("đọc vận đơn cần điều chỉnh: %w", err)
	}
	b, err := BalancesFor(ctx, tx, in.ShipmentID)
	if err != nil {
		return adjust.Plan{}, err
	}
	rows, err := q.AdjustTransfers(ctx, in.ShipmentID)
	if err != nil {
		return adjust.Plan{}, fmt.Errorf("đọc bút toán của vận đơn: %w", err)
	}
	lines, err := q.ShipmentEntries(ctx, in.ShipmentID)
	if err != nil {
		return adjust.Plan{}, fmt.Errorf("đọc dòng sổ của vận đơn: %w", err)
	}
	entries := make(map[uuid.UUID][]postings.Entry, len(rows))
	for _, l := range lines {
		entries[l.TransferID] = append(entries[l.TransferID], postings.Entry{Account: l.Account, Amount: l.Amount})
	}
	st := adjust.State{
		ShipmentID: in.ShipmentID, Status: status, Balances: postings.Balances(b),
		Transfers: make([]adjust.Transfer, len(rows)),
	}
	for i, r := range rows {
		st.Transfers[i] = adjust.Transfer{ID: r.ID, Kind: r.Kind, Reversed: r.Reversed, Entries: entries[r.ID]}
		if r.ConfirmedAt.Valid {
			st.Transfers[i].ConfirmedAt = new(r.ConfirmedAt.Time)
		}
	}
	return adjust.Preview(st, adjust.Request{ //nolint:wrapcheck // sentinel của cash (Err* = adjust.Err*)
		Mode: in.Mode, ReversesTransferID: in.ReversesTransferID, From: in.From, To: in.To, Amount: in.Amount,
		Reason: in.Reason,
	})
}
