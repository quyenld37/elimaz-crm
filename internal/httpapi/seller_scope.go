package httpapi

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
)

// Người bán của API (ADR-29, Task 527): API của console chạy với người bán mặc định (spec 04 mục 14 bước 0), như đường
// đơn (order.Get(seller, …)). Mọi route vận đơn theo mã kiểm đối tượng thuộc người bán đó trước khi đọc, ghi; khác → cùng
// 404 như mã lạ. Khi có tài khoản người bán (bước 2), chỉ đổi chỗ lấy người bán ở đây.

// sellerOwns: đối tượng kind mã id thuộc người bán mặc định (fulfillment.CheckSeller), trong tx của bên gọi.
func sellerOwns(ctx context.Context, tx pgx.Tx, kind fulfillment.ScopeKind, id uuid.UUID) error {
	seller, err := catalog.DefaultSeller(ctx, tx)
	if err != nil {
		return err //nolint:wrapcheck // sentinel của catalog
	}
	return fulfillment.CheckSeller(ctx, tx, seller, kind, id) //nolint:wrapcheck // sentinel của fulfillment
}

// sellerShipment: vận đơn id thuộc người bán mặc định; khác → fulfillment.ErrNotFound.
func sellerShipment(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	return sellerOwns(ctx, tx, fulfillment.ScopeShipment, id)
}

// sellerShipmentCOD như sellerShipment cho đường của cash: khác → cash.ErrShipmentNotFound (404 của cash).
func sellerShipmentCOD(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	err := sellerShipment(ctx, tx, id)
	if errors.Is(err, fulfillment.ErrNotFound) {
		return cash.ErrShipmentNotFound
	}
	return err
}

// sellerOrder: đơn id thuộc người bán mặc định (order.Get); không có hay của người bán khác → hidden.
func sellerOrder(ctx context.Context, tx pgx.Tx, id uuid.UUID, hidden error) error {
	seller, err := catalog.DefaultSeller(ctx, tx)
	if err != nil {
		return err //nolint:wrapcheck // sentinel của catalog
	}
	_, err = order.Get(ctx, tx, seller, id)
	if errors.Is(err, order.ErrNotFound) {
		return hidden
	}
	return err //nolint:wrapcheck // order đã bọc
}

// sellerShipmentWrite như refusableWrite cho đường ghi theo mã vận đơn id: trong cùng transaction, kiểm vận đơn thuộc
// người bán mặc định (sellerShipment) trước change; khác → fulfillment.ErrNotFound (404, không lưu khóa chống trùng).
func (h *handler) sellerShipmentWrite(ctx context.Context, status int, refusal func(error) bool, what string, id uuid.UUID,
	change func(tx pgx.Tx, u identity.AuthUser) (any, error),
) (any, error) {
	return h.refusableWrite(ctx, status, refusal, what, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		if err := sellerShipment(ctx, tx, id); err != nil {
			return nil, err
		}
		return change(tx, u)
	})
}

// readableShipment đọc vận đơn id cho đường đọc theo mã (fulfillment.shipment.read ở kho của vận đơn): người bán mặc
// định và phạm vi kho; thiếu một trong hai, hay mã lạ → fulfillment.ErrNotFound.
func readableShipment(ctx context.Context, tx pgx.Tx, u identity.AuthUser, id uuid.UUID) (fulfillment.Shipment, error) {
	if err := sellerShipment(ctx, tx, id); err != nil {
		return fulfillment.Shipment{}, err
	}
	s, err := fulfillment.Get(ctx, tx, id)
	if err != nil {
		return fulfillment.Shipment{}, err //nolint:wrapcheck // sentinel của fulfillment
	}
	if canAt(u, s.WarehouseID, permShipRead) != nil {
		return fulfillment.Shipment{}, fulfillment.ErrNotFound
	}
	return s, nil
}

// listScope là phạm vi của danh sách vận hành vận đơn (đơn kẹt, việc cần xem): người bán mặc định và các kho người gọi
// có fulfillment.shipment.read (readableWarehouses).
func listScope(ctx context.Context, tx pgx.Tx, u identity.AuthUser) (uuid.UUID, bool, []uuid.UUID, error) {
	seller, err := catalog.DefaultSeller(ctx, tx)
	if err != nil {
		return uuid.Nil, false, nil, err //nolint:wrapcheck // sentinel của catalog
	}
	all, ids := readableWarehouses(u, permShipRead)
	return seller, all, ids, nil
}
