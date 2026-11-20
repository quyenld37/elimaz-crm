// Package cash là tiền (spec 05 mục 2 "Sổ COD"). Bản M1 (kế hoạch M1, Task M1-32, lát mỏng Q-X1): sổ COD hai vế
// (ADR-24) — mỗi lần tiền COD của một vận đơn chuyển ngăn là một bút toán (cash.cod_transfers) gồm ≥ 2 dòng
// (cash.cod_ledger_entries) cộng lại bằng 0; database kiểm lúc commit và từ chối bút toán lệch. Consumer nghe
// shipment.status_changed ghi bút toán expected, collected. M3 (Task M3-02) mở rộng: loại fee, reversal, dòng sổ mang
// vận đơn, Reverse, BalancesFor (ledger.go); bảng kê, phiếu thu, nghĩa vụ hoàn, đối chiếu ở các task sau.
package cash

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/cash/internal/store"
	"elimaz.com.vn/crm/internal/platform/db"
)

// maxAmount là 2^53 − 1: số đồng lớn nhất một dòng sổ mang (khớp CHECK của bảng, JSON không mất chính xác).
const maxAmount = 1<<53 - 1

var (
	// ErrInvalidTransfer báo bút toán thiếu trường, loại, ngăn hay tác nhân lạ, dòng 0 đồng, vượt 2^53 − 1 hay hai
	// dòng cùng ngăn. Không ghi gì.
	ErrInvalidTransfer = errors.New("cash: bút toán không hợp lệ")
	// ErrUnbalanced báo bút toán ít hơn hai dòng hay tổng khác 0. Không ghi gì.
	ErrUnbalanced = errors.New("cash: bút toán lệch hai vế")
	// ErrIdempotencyKeyReused báo khóa chống trùng đã dùng cho bút toán khác vận đơn hay khác loại.
	ErrIdempotencyKeyReused = errors.New("cash: khóa chống trùng đã dùng cho bút toán khác")
)

// Actor là người hay hệ thống gây ra bút toán. Type: staff | system | carrier; ID = uuid.Nil → NULL.
type Actor struct {
	Type string
	ID   uuid.UUID
}

// Entry là một vế của bút toán: ngăn và số đồng có dấu.
type Entry struct {
	Account string
	Amount  int64
}

// Transfer là một bút toán cần ghi. Reason bắt buộc với adjustment, reversal và phải rỗng với loại khác;
// ReversesTransferID có khi và chỉ khi Kind = reversal (chỉ ghi reversal qua Reverse: PostWithID không kiểm đích cùng
// vận đơn, vế đảo dấu); StatementLineID là dòng bảng kê sinh ra bút toán
// (M3); OccurredAt là lúc việc xảy ra (rỗng = giờ của transaction).
type Transfer struct {
	ShipmentID         uuid.UUID
	Kind               string
	IdempotencyKey     string
	Reason             string
	Entries            []Entry
	Actor              Actor
	StatementLineID    *uuid.UUID
	ReversesTransferID *uuid.UUID
	OccurredAt         time.Time
}

// Post ghi bút toán t trong tx của bên gọi (tenancy.WithTenant). Kiểm trước: đủ trường, loại, ngăn, tác nhân hợp lệ,
// ≥ 2 dòng khác ngăn, mỗi dòng khác 0 và |amount| ≤ 2^53 − 1 (ErrInvalidTransfer), tổng bằng 0 (ErrUnbalanced).
// Database kiểm lại cân hai vế lúc commit (trigger hoãn). Khóa chống trùng đã có cho cùng vận đơn và loại →
// created = false, không chèn dòng; đã có cho bút toán khác → ErrIdempotencyKeyReused.
func Post(ctx context.Context, tx pgx.Tx, t Transfer) (created bool, err error) {
	_, created, err = PostWithID(ctx, tx, t)
	return created, err
}

// PostWithID như Post và trả thêm mã bút toán: bút toán mới, hay bút toán đã có cùng khóa chống trùng (created = false).
// Kiểm thêm lý do theo loại, bút toán bị đảo theo loại (ErrInvalidTransfer); bút toán đảo cho bút toán đã bị đảo (khóa
// khác) → ErrAlreadyReversed. Mọi dòng sổ mang vận đơn của bút toán.
func PostWithID(ctx context.Context, tx pgx.Tx, t Transfer) (uuid.UUID, bool, error) {
	if err := t.validate(); err != nil {
		return uuid.Nil, false, err
	}
	q := store.New(tx)
	id, err := q.InsertTransfer(ctx, store.InsertTransferParams{
		ShipmentID: t.ShipmentID, Kind: t.Kind, IdempotencyKey: t.IdempotencyKey, Reason: t.Reason,
		ActorType: t.Actor.Type, ActorID: pgtype.UUID{Bytes: t.Actor.ID, Valid: t.Actor.ID != uuid.Nil},
		StatementLineID: db.NullUUID(t.StatementLineID), ReversesTransferID: db.NullUUID(t.ReversesTransferID),
		OccurredAt: pgtype.Timestamptz{Time: t.OccurredAt.UTC(), Valid: !t.OccurredAt.IsZero()},
	})
	if isReversesKeyViolation(err) {
		return uuid.Nil, false, ErrAlreadyReversed
	}
	if errors.Is(err, pgx.ErrNoRows) {
		old, err := q.GetTransferByKey(ctx, t.IdempotencyKey)
		if err != nil {
			return uuid.Nil, false, fmt.Errorf("đọc bút toán cùng khóa: %w", err)
		}
		if old.ShipmentID != t.ShipmentID || old.Kind != t.Kind {
			return uuid.Nil, false, ErrIdempotencyKeyReused
		}
		return old.ID, false, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("chèn bút toán: %w", err)
	}
	for _, en := range t.Entries {
		if err := q.InsertEntry(ctx, store.InsertEntryParams{
			TransferID: id, ShipmentID: t.ShipmentID, Account: en.Account, Amount: en.Amount,
		}); err != nil {
			return uuid.Nil, false, fmt.Errorf("chèn dòng sổ: %w", err)
		}
	}
	return id, true, nil
}

func (t Transfer) validate() error {
	switch {
	case t.ShipmentID == uuid.Nil, t.IdempotencyKey == "", len(t.IdempotencyKey) > 200, len(t.Reason) > 500:
		return fmt.Errorf("%w: thiếu vận đơn hay khóa, hoặc khóa, lý do quá dài", ErrInvalidTransfer)
	case !isKind(t.Kind):
		return fmt.Errorf("%w: loại %q", ErrInvalidTransfer, t.Kind)
	case needsReason(t.Kind) != (t.Reason != ""):
		return fmt.Errorf("%w: lý do chỉ có (và bắt buộc) ở adjustment, reversal; loại %q", ErrInvalidTransfer, t.Kind)
	case (t.Kind == KindReversal) != (t.ReversesTransferID != nil):
		return fmt.Errorf("%w: bút toán bị đảo chỉ có (và bắt buộc) ở reversal", ErrInvalidTransfer)
	case t.ReversesTransferID != nil && *t.ReversesTransferID == uuid.Nil,
		t.StatementLineID != nil && *t.StatementLineID == uuid.Nil:
		return fmt.Errorf("%w: mã bút toán bị đảo hay dòng bảng kê rỗng", ErrInvalidTransfer)
	case !slices.Contains([]string{ActorStaff, ActorSystem, ActorCarrier}, t.Actor.Type):
		return fmt.Errorf("%w: tác nhân %q", ErrInvalidTransfer, t.Actor.Type)
	}
	seen := make(map[string]bool, len(t.Entries))
	var sum int64
	for _, en := range t.Entries {
		if !isAccount(en.Account) || seen[en.Account] {
			return fmt.Errorf("%w: ngăn %q lạ hay lặp", ErrInvalidTransfer, en.Account)
		}
		if en.Amount == 0 || en.Amount > maxAmount || en.Amount < -maxAmount {
			return fmt.Errorf("%w: số tiền %d", ErrInvalidTransfer, en.Amount)
		}
		seen[en.Account] = true
		sum += en.Amount // ≤ 5 dòng, mỗi dòng ≤ 2^53: không tràn int64
	}
	if len(t.Entries) < 2 || sum != 0 {
		return fmt.Errorf("%w: %d dòng, tổng %d", ErrUnbalanced, len(t.Entries), sum)
	}
	return nil
}
