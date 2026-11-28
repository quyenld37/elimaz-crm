package cash

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash/internal/store"
)

// ScopeKind là loại đối tượng tiền mà API gọi theo mã và CheckSeller kiểm người bán (ADR-29, Task 531).
type ScopeKind string

// Loại đối tượng của CheckSeller.
const (
	ScopeStatement        ScopeKind = "cod_statement"
	ScopeCarrierAccount   ScopeKind = "carrier_account"
	ScopeRefundObligation ScopeKind = "refund_obligation"
)

// errScopeKind: loại đối tượng lạ (lỗi lập trình của bên gọi).
var errScopeKind = errors.New("cash: loại đối tượng lạ")

// CheckSeller kiểm đối tượng kind mã id thuộc người bán seller (bảng kê: người bán của tài khoản hãng của nó; nghĩa vụ
// hoàn: người bán của đơn), trong tx tenant của bên gọi. Không có, hay của người bán khác → sentinel "không có" của
// loại (ErrStatementNotFound, ErrCarrierAccountNotFound, ErrObligationNotFound) — bên ngoài không phân biệt được hai
// trường hợp. Lớp HTTP gọi trước mọi thao tác theo mã với người bán của API (hôm nay người bán mặc định, spec 04 mục 14
// bước 0).
//
// Khi nào không dùng: đường nội bộ (worker, lệnh đọc tệp, lô ghi tiền) không có người bán của người gọi; danh sách
// (tham số seller của hàm List*); đơn (order.Get với người bán).
func CheckSeller(ctx context.Context, tx pgx.Tx, seller uuid.UUID, kind ScopeKind, id uuid.UUID) error {
	q := store.New(tx)
	var (
		owner  uuid.UUID
		err    error
		hidden error
	)
	switch kind {
	case ScopeStatement:
		owner, err = q.SellerOfStatement(ctx, id)
		hidden = ErrStatementNotFound
	case ScopeCarrierAccount:
		owner, err = q.SellerOfCarrierAccount(ctx, id)
		hidden = ErrCarrierAccountNotFound
	case ScopeRefundObligation:
		owner, err = q.SellerOfRefundObligation(ctx, id)
		hidden = ErrObligationNotFound
	default:
		return fmt.Errorf("%w: %q", errScopeKind, kind)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("kiểm người bán của %s %s: %w", kind, id, err)
	}
	if err != nil || owner != seller {
		return hidden
	}
	return nil
}
