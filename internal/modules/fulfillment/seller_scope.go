package fulfillment

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
)

// ScopeKind là loại đối tượng mà API gọi theo mã và CheckSeller kiểm người bán (ADR-29).
type ScopeKind string

// Loại đối tượng của CheckSeller.
const (
	ScopeShipment      ScopeKind = "shipment"
	ScopeCallback      ScopeKind = "callback_task"
	ScopeAddressChange ScopeKind = "address_change"
	ScopeAnomaly       ScopeKind = "anomaly"
	ScopeLabelBatch    ScopeKind = "label_batch"
)

// CheckSeller kiểm đối tượng kind mã id thuộc người bán seller (vận đơn của nó có seller_id đó; lô nhãn: tài khoản hãng
// của lô), trong tx tenant của bên gọi. Không có, hay của người bán khác → sentinel "không có" của loại (vận đơn, yêu
// cầu đổi địa chỉ, lô nhãn: ErrNotFound; việc gọi lại: ErrCallbackNotFound; việc cần xem: ErrAnomalyNotFound) — bên
// ngoài không phân biệt được hai trường hợp. Lớp HTTP gọi trước mọi thao tác theo mã với người bán của API (hôm nay
// người bán mặc định, spec 04 mục 14 bước 0).
//
// Khi nào không dùng: đường nội bộ (worker, webhook hãng) không có người bán của người gọi; danh sách (lọc SellerID
// trong bộ lọc).
func CheckSeller(ctx context.Context, tx pgx.Tx, seller uuid.UUID, kind ScopeKind, id uuid.UUID) error {
	q := store.New(tx)
	var (
		read   func(context.Context, uuid.UUID) (uuid.UUID, error)
		hidden = ErrNotFound
	)
	switch kind {
	case ScopeShipment:
		read = q.SellerOfShipment
	case ScopeCallback:
		read, hidden = q.SellerOfCallbackTask, ErrCallbackNotFound
	case ScopeAddressChange:
		read = q.SellerOfAddressChange
	case ScopeAnomaly:
		read, hidden = q.SellerOfAnomaly, ErrAnomalyNotFound
	case ScopeLabelBatch:
		read = q.SellerOfLabelBatch
	default:
		return fmt.Errorf("%w: loại đối tượng %q", ErrShipInvalid, kind)
	}
	got, err := read(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return hidden
	case err != nil:
		return fmt.Errorf("đọc người bán của %s: %w", kind, err)
	case got != seller:
		return hidden
	}
	return nil
}
