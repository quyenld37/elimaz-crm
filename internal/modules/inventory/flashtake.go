package inventory

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
)

// Lượt mua flash sale, phần của inventory (kế hoạch M5, Task 201 / M5-18; ADR-40): lấy đơn vị suất trống bằng SKIP
// LOCKED và giữ cho dòng đơn. Không đổi stock_balances: phần held của đơn vị đã cộng lúc lên lịch (ReserveForFlash).
// Chạy trong tx tenant của bên gọi (order.Writer.BuyFlash); ADR-46 nhóm 11: flash_sale_units sau flash_sale_claims.

// TakeFlashUnits khóa tối đa qty đơn vị trống của món itemID theo id tăng dần, bỏ qua đơn vị tx khác đang khóa. Đủ qty
// → (ids, true). Thiếu → (ids đã khóa, anyAvailable): anyAvailable cho biết còn đơn vị trống khác ngoài ids (đang bị tx
// khác khóa, đọc không khóa); false nghĩa là hết suất trống. qty ngoài 1–MaxInt32 → ErrInvalidInput.
func TakeFlashUnits(ctx context.Context, tx pgx.Tx, itemID uuid.UUID, qty int) (ids []uuid.UUID, anyAvailable bool, err error) {
	if qty < 1 || qty > math.MaxInt32 {
		return nil, false, fmt.Errorf("%w: số đơn vị suất", ErrInvalidInput)
	}
	q := store.New(tx)
	ids, err = q.TakeFlashUnits(ctx, store.TakeFlashUnitsParams{FlashSaleItemID: itemID, N: int32(qty)})
	if err != nil {
		return nil, false, fmt.Errorf("lấy đơn vị suất: %w", err)
	}
	if len(ids) == qty {
		return ids, true, nil
	}
	taken := ids
	if taken == nil {
		taken = []uuid.UUID{}
	}
	other, err := q.AnyOtherFlashUnitAvailable(ctx, store.AnyOtherFlashUnitAvailableParams{FlashSaleItemID: itemID, Taken: taken})
	if err != nil {
		return nil, false, fmt.Errorf("kiểm đơn vị suất còn trống: %w", err)
	}
	return ids, other, nil
}

// HoldFlashUnits giữ các đơn vị ids (đã khóa bằng TakeFlashUnits trong cùng tx) cho dòng đơn orderItemID: một dòng giữ
// hàng held số lượng qty hạn expiresAt (không đổi số dư) và đơn vị available → held. qty phải bằng len(ids); số đơn vị
// đổi khác len(ids) → ErrStateConflict (bên gọi lùi tx).
func HoldFlashUnits(ctx context.Context, tx pgx.Tx, ids []uuid.UUID, orderItemID, variantID, warehouseID uuid.UUID, qty int,
	expiresAt time.Time,
) error {
	if qty < 1 || qty > math.MaxInt32 || qty != len(ids) || orderItemID == uuid.Nil || variantID == uuid.Nil ||
		warehouseID == uuid.Nil {
		return fmt.Errorf("%w: giữ đơn vị suất", ErrInvalidInput)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("sinh mã giữ hàng: %w", err)
	}
	q := store.New(tx)
	if err := q.InsertFlashReservation(ctx, store.InsertFlashReservationParams{
		ID: id, OrderItemID: orderItemID, VariantID: variantID, WarehouseID: warehouseID, Quantity: int32(qty),
		ExpiresAt: expiresAt.UTC(),
	}); err != nil {
		return fmt.Errorf("ghi giữ hàng flash sale: %w", err)
	}
	n, err := q.HoldFlashUnits(ctx, store.HoldFlashUnitsParams{OrderItemID: orderItemID, Ids: ids})
	if err != nil {
		return fmt.Errorf("giữ đơn vị suất: %w", err)
	}
	if n != int64(len(ids)) {
		return fmt.Errorf("%w: giữ %d/%d đơn vị suất", ErrStateConflict, n, len(ids))
	}
	return nil
}
