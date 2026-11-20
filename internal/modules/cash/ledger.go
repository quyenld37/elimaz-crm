package cash

// Bất biến của sổ COD: mỗi bút toán có ≥ 2 dòng, tổng 0, cùng một vận đơn; không sửa, không xóa; sửa sai bằng Reverse
// hoặc adjustment có lý do. Go kiểm trước (PostWithID), database kiểm lần cuối lúc commit (trigger hoãn
// cash.cod_transfer_balanced, 0303) và chặn sửa, xóa (trigger cash.cod_append_only, REVOKE).

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/cash/internal/store"
)

var (
	// ErrAlreadyReversed báo bút toán đã bị đảo (mỗi bút toán chỉ đảo một lần). Không ghi gì.
	ErrAlreadyReversed = errors.New("cash: bút toán đã bị đảo")
	// ErrTransferNotFound báo không có bút toán với mã đã cho trong tenant.
	ErrTransferNotFound = errors.New("cash: không có bút toán")
)

// reversesKey là unique một phần "một bút toán chỉ bị đảo một lần" (0303).
const reversesKey = "cod_transfers_reverses_key"

// Balances là số dư từng ngăn sổ COD của một vận đơn (đồng, có dấu).
type Balances struct {
	ToCollect, CarrierHolding, ShopReceived, CarrierFee, SalesClearing int64
}

// BalancesFor đọc số dư từng ngăn của vận đơn shipmentID trong tx của bên gọi (tenancy.WithTenant); vận đơn chưa có
// dòng sổ → mọi ngăn 0.
func BalancesFor(ctx context.Context, tx pgx.Tx, shipmentID uuid.UUID) (Balances, error) {
	r, err := store.New(tx).ShipmentBalances(ctx, shipmentID)
	if err != nil {
		return Balances{}, fmt.Errorf("đọc số dư COD của vận đơn: %w", err)
	}
	return Balances{
		ToCollect: r.ToCollect, CarrierHolding: r.CarrierHolding, ShopReceived: r.ShopReceived,
		CarrierFee: r.CarrierFee, SalesClearing: r.SalesClearing,
	}, nil
}

// Reverse ghi bút toán reversal đảo dấu từng dòng của bút toán transferID (cùng vận đơn), lý do reason (bắt buộc),
// tác nhân actor, khóa chống trùng "reversal:<transferID>", trong tx của bên gọi. Bút toán đã bị đảo →
// ErrAlreadyReversed (kể cả khi hai lần đảo chạy cùng lúc); bút toán không có → ErrTransferNotFound; đảo một bút toán
// reversal, lý do rỗng hay tác nhân lạ → ErrInvalidTransfer. Trả mã bút toán đảo.
func Reverse(ctx context.Context, tx pgx.Tx, transferID uuid.UUID, reason string, actor Actor) (uuid.UUID, error) {
	if reason == "" {
		return uuid.Nil, fmt.Errorf("%w: đảo bút toán cần lý do", ErrInvalidTransfer)
	}
	q := store.New(tx)
	orig, err := q.GetTransfer(ctx, transferID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrTransferNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("đọc bút toán cần đảo: %w", err)
	}
	if orig.Kind == KindReversal {
		return uuid.Nil, fmt.Errorf("%w: không đảo một bút toán đảo (ghi bút toán mới)", ErrInvalidTransfer)
	}
	lines, err := q.TransferEntries(ctx, transferID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("đọc dòng của bút toán cần đảo: %w", err)
	}
	t := Transfer{
		ShipmentID: orig.ShipmentID, Kind: KindReversal, IdempotencyKey: "reversal:" + transferID.String(),
		Reason: reason, Actor: actor, ReversesTransferID: &transferID, Entries: make([]Entry, len(lines)),
	}
	for i, l := range lines {
		t.Entries[i] = Entry{Account: l.Account, Amount: -l.Amount}
	}
	id, created, err := PostWithID(ctx, tx, t)
	if err != nil {
		return uuid.Nil, err
	}
	if !created {
		return uuid.Nil, ErrAlreadyReversed
	}
	return id, nil
}

// isReversesKeyViolation báo err là vi phạm unique "một bút toán chỉ bị đảo một lần".
func isReversesKeyViolation(err error) bool {
	pg, ok := errors.AsType[*pgconn.PgError](err)
	return ok && pg.Code == "23505" && pg.ConstraintName == reversesKey
}
