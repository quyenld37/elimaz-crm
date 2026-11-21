package fulfillment

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
)

// ShipAccount là tài khoản hãng người gửi chọn được cho một đơn (Task 449): chỉ phần cần để chọn, không khóa API, địa
// chỉ lấy hàng hay cấu hình (khác AccountView của màn kết nối hãng, cần fulfillment.carrier_accounts.read).
type ShipAccount struct {
	ID         uuid.UUID
	Name       string
	CarrierKey string
	Mode       string
	Status     string
}

// ShipAccounts trả tài khoản hãng gửi được cho đơn o (xếp web_sort, id): cùng người bán và kho của đơn, active, mode
// api mà hãng có adapter (khi Service có bảng hãng) hoặc manual (kể cả tự giao) — đúng điều kiện Ship chấp nhận
// (shareAccount và kiểm adapter trong Ship). Không kiểm trạng thái đơn; bên gọi đã đọc đơn và kiểm quyền ở kho của đơn.
func (s *Service) ShipAccounts(ctx context.Context, tx pgx.Tx, o order.Order) ([]ShipAccount, error) {
	// Ids rỗng (không nil: nil thành NULL, cardinality(NULL) không chọn nhánh "mọi tài khoản của kho").
	rows, err := store.New(tx).QuoteAccounts(ctx, store.QuoteAccountsParams{
		SellerID: o.SellerID, Ids: []uuid.UUID{}, WarehouseID: o.WarehouseID,
	})
	if err != nil {
		return nil, fmt.Errorf("đọc tài khoản hãng của kho: %w", err)
	}
	out := make([]ShipAccount, 0, len(rows))
	for _, r := range rows {
		if r.Status != AccountActive || r.WarehouseID != o.WarehouseID {
			continue
		}
		switch r.Mode {
		case ModeManual:
		case ModeAPI:
			if _, ok := s.adapter(r.CarrierKey); s.d.Carriers != nil && !ok {
				continue
			}
		default:
			continue
		}
		out = append(out, ShipAccount{ID: r.ID, Name: r.Name, CarrierKey: r.CarrierKey, Mode: r.Mode, Status: r.Status})
	}
	return out, nil
}
