// Package cash là tiền (spec 05 mục 2 "Sổ COD"). Bản M1 (kế hoạch M1, Task M1-32, lát mỏng Q-X1): sổ COD hai vế
// (ADR-24) — mỗi lần tiền COD của một vận đơn chuyển ngăn là một bút toán (cash.cod_transfers) gồm ≥ 2 dòng
// (cash.cod_ledger_entries) cộng lại bằng 0; database kiểm lúc commit và từ chối bút toán lệch. Consumer nghe
// shipment.status_changed ghi bút toán expected, collected. M3 mở rộng (bảng kê, remitted, returned, adjustment), không
// tạo lại.
package cash

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/cash/internal/store"
)

// Loại bút toán (05 mục 2). M1 chỉ ghi expected, collected; ba loại sau có kiểu nhưng chưa có đường gọi (M3).
const (
	KindExpected   = "expected"
	KindCollected  = "collected"
	KindRemitted   = "remitted"
	KindReturned   = "returned"
	KindAdjustment = "adjustment"
)

// Ngăn tiền (05 mục 2).
const (
	// AccountToCollect: hãng đang đi giao, sẽ thu của khách.
	AccountToCollect = "to_collect"
	// AccountCarrierHolding: hãng đã thu, đang giữ, chưa chuyển cho shop.
	AccountCarrierHolding = "carrier_holding"
	// AccountShopReceived: tiền đã về shop theo bảng kê.
	AccountShopReceived = "shop_received"
	// AccountCarrierFee: phí hãng trừ lại khi chuyển tiền.
	AccountCarrierFee = "carrier_fee"
	// AccountSalesClearing: vế đối ứng, giá trị COD đã giao cho hãng đi thu.
	AccountSalesClearing = "sales_clearing"
)

// Tác nhân của bút toán.
const (
	ActorStaff   = "staff"
	ActorSystem  = "system"
	ActorCarrier = "carrier"
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

// Transfer là một bút toán cần ghi.
type Transfer struct {
	ShipmentID     uuid.UUID
	Kind           string
	IdempotencyKey string
	Reason         string
	Entries        []Entry
	Actor          Actor
}

// Post ghi bút toán t trong tx của bên gọi (tenancy.WithTenant). Kiểm trước: đủ trường, loại, ngăn, tác nhân hợp lệ,
// ≥ 2 dòng khác ngăn, mỗi dòng khác 0 và |amount| ≤ 2^53 − 1 (ErrInvalidTransfer), tổng bằng 0 (ErrUnbalanced).
// Database kiểm lại cân hai vế lúc commit (trigger hoãn). Khóa chống trùng đã có cho cùng vận đơn và loại →
// created = false, không chèn dòng; đã có cho bút toán khác → ErrIdempotencyKeyReused.
func Post(ctx context.Context, tx pgx.Tx, t Transfer) (created bool, err error) {
	if err := t.validate(); err != nil {
		return false, err
	}
	q := store.New(tx)
	id, err := q.InsertTransfer(ctx, store.InsertTransferParams{
		ShipmentID: t.ShipmentID, Kind: t.Kind, IdempotencyKey: t.IdempotencyKey, Reason: t.Reason,
		ActorType: t.Actor.Type, ActorID: pgtype.UUID{Bytes: t.Actor.ID, Valid: t.Actor.ID != uuid.Nil},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		old, err := q.GetTransferByKey(ctx, t.IdempotencyKey)
		if err != nil {
			return false, fmt.Errorf("đọc bút toán cùng khóa: %w", err)
		}
		if old.ShipmentID != t.ShipmentID || old.Kind != t.Kind {
			return false, ErrIdempotencyKeyReused
		}
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("chèn bút toán: %w", err)
	}
	for _, en := range t.Entries {
		if err := q.InsertEntry(ctx, store.InsertEntryParams{TransferID: id, Account: en.Account, Amount: en.Amount}); err != nil {
			return false, fmt.Errorf("chèn dòng sổ: %w", err)
		}
	}
	return true, nil
}

func (t Transfer) validate() error {
	switch {
	case t.ShipmentID == uuid.Nil, t.IdempotencyKey == "", len(t.IdempotencyKey) > 200, len(t.Reason) > 500:
		return fmt.Errorf("%w: thiếu vận đơn hay khóa, hoặc khóa, lý do quá dài", ErrInvalidTransfer)
	case !slices.Contains([]string{KindExpected, KindCollected, KindRemitted, KindReturned, KindAdjustment}, t.Kind):
		return fmt.Errorf("%w: loại %q", ErrInvalidTransfer, t.Kind)
	case !slices.Contains([]string{ActorStaff, ActorSystem, ActorCarrier}, t.Actor.Type):
		return fmt.Errorf("%w: tác nhân %q", ErrInvalidTransfer, t.Actor.Type)
	}
	accounts := []string{AccountToCollect, AccountCarrierHolding, AccountShopReceived, AccountCarrierFee, AccountSalesClearing}
	seen := make(map[string]bool, len(t.Entries))
	var sum int64
	for _, en := range t.Entries {
		if !slices.Contains(accounts, en.Account) || seen[en.Account] {
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
