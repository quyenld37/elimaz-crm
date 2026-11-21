package httpapi

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/queries"
)

// Vùng inventory_aged (kế hoạch M3, Task M3-22; KH09, AT53): giữ hàng quá hạn, chỉ đọc.

const permAgedRead = identity.Permission("inventory.aged.read")

// ListAgedReservations (GET /api/v1/inventory/aged-reservations, inventory.aged.read theo kho): giữ hàng held quá
// AgedDays.Held ngày và in_transit quá AgedDays.InTransit ngày của cài đặt vận chuyển, cũ nhất trước. Không có
// warehouseId → mọi kho người gọi có quyền; có warehouseId ngoài phạm vi → 403. Chỉ đọc: không nhả hàng, không tạo việc.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListAgedReservations(ctx context.Context, params gen.ListAgedReservationsParams) (gen.ListAgedReservationsRes, error) {
	page := inventory.AgedPage{State: string(params.State.Value), Limit: int(params.Limit.Or(0))}
	if params.Cursor.Set {
		c, err := inventory.ParseAgedCursor(params.Cursor.Value)
		if err != nil {
			return nil, fmt.Errorf("đọc tham số cursor: %w", err)
		}
		page.After = &c
	}
	now := h.now()
	var rows []inventory.AgedRow
	var next *inventory.AgedCursor
	var orders map[uuid.UUID]queries.AgedOrderRef
	var skus map[uuid.UUID]string
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) error {
		whs, err := agedTargets(u, params)
		if err != nil {
			return err
		}
		s, _, err := fulfillment.LoadSettings(ctx, tx)
		if err != nil {
			return err //nolint:wrapcheck // fulfillment đã bọc
		}
		if rows, next, err = inventory.AgedReservations(ctx, tx, now, s.AgedDays.Held, s.AgedDays.InTransit, whs, page); err != nil {
			return err //nolint:wrapcheck // inventory đã bọc
		}
		items, variants := make([]uuid.UUID, 0, len(rows)), make([]uuid.UUID, 0, len(rows))
		for _, r := range rows {
			if r.OrderItemID != uuid.Nil {
				items = append(items, r.OrderItemID)
			}
			variants = append(variants, r.VariantID)
		}
		orders, skus, err = queries.AgedRefs(ctx, tx, items, variants)
		return err //nolint:wrapcheck // queries đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc giữ hàng quá hạn: %w", err)
	}
	return agedPageOut(rows, next, orders, skus), nil
}

func agedPageOut(rows []inventory.AgedRow, next *inventory.AgedCursor, orders map[uuid.UUID]queries.AgedOrderRef,
	skus map[uuid.UUID]string,
) *gen.AgedReservationPage {
	out := &gen.AgedReservationPage{Items: make([]gen.AgedReservation, 0, len(rows))}
	for _, r := range rows {
		o := orders[r.OrderItemID]
		out.Items = append(out.Items, gen.AgedReservation{
			ReservationId: r.ReservationID, OrderItemId: r.OrderItemID, OrderId: o.OrderID, OrderCode: o.Code,
			VariantId: r.VariantID, Sku: skus[r.VariantID], WarehouseId: r.WarehouseID,
			State: gen.AgedReservationState(r.State), Quantity: r.Quantity, Since: r.Since, AgeDays: r.AgeDays,
		})
	}
	if next != nil {
		out.NextCursor = gen.NewOptString(next.String())
	}
	return out
}

// agedTargets: kho cần đọc. warehouseId có gửi → đúng kho đó, ngoài phạm vi inventory.aged.read → errForbiddenTarget;
// không gửi → agedWarehouses.
func agedTargets(u identity.AuthUser, params gen.ListAgedReservationsParams) ([]uuid.UUID, error) {
	if !params.WarehouseId.Set {
		return agedWarehouses(u), nil
	}
	if err := canAt(u, params.WarehouseId.Value, permAgedRead); err != nil {
		return nil, err
	}
	return []uuid.UUID{params.WarehouseId.Value}, nil
}

// agedWarehouses: kho người gọi có inventory.aged.read; nil khi grant toàn shop (mọi kho), rỗng không nil khi không có
// kho nào.
func agedWarehouses(u identity.AuthUser) []uuid.UUID {
	if all, ids := readableWarehouses(u, permAgedRead); !all {
		return ids
	}
	return nil
}
