package inventory

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
)

// Chứng từ giữ cho đợt flash sale (kế hoạch M5, Task 200 / M5-17; ADR-40, QĐ31, A17): lên lịch và thêm suất cộng
// held của số dư (sổ doc_type flash_hold, doc_id = mã món) và trải từng đơn vị suất trống; đóng đợt trả đơn vị còn
// trống (flash_release, held −). Không tạo phiếu kho. Thứ tự khóa ADR-46 nhóm 11: stock_balances theo (variant_id,
// warehouse_id) tăng dần → flash_sale_units.

// Loại chứng từ của sổ cho đợt flash sale (CHECK doc_type, migration 0540).
const (
	docFlashHold    = "flash_hold"
	docFlashRelease = "flash_release"
	stepFlashClose  = "close"
)

// FlashItemRef là một món của đợt: mã món (doc_id của sổ), biến thể, kho, số suất (lên lịch) và bước sổ (schedule,
// add_<version>; mỗi bước của một món chỉ áp một lần nhờ unique của sổ).
type FlashItemRef struct {
	ItemID, VariantID, WarehouseID uuid.UUID
	Quantity                       int
	Step                           string
}

// FlashShortage là một món thiếu hàng: cần Need suất, có thể bán Sellable.
type FlashShortage struct {
	ItemID, VariantID, WarehouseID uuid.UUID
	Need, Sellable                 int
}

// LockFlashStock khóa dòng số dư của mọi món refs theo (variant_id, warehouse_id) tăng dần (ADR-46 nhóm 11) và trả
// các món có số có thể bán nhỏ hơn Quantity (theo thứ tự refs; chưa có dòng số dư: có thể bán 0). Bên gọi kiểm mọi món
// trước rồi mới ghi, để báo đủ các món thiếu mà không ghi gì; khóa giữ tới hết transaction nên số đã đọc vẫn đúng lúc
// ghi.
func LockFlashStock(ctx context.Context, tx pgx.Tx, refs []FlashItemRef) ([]FlashShortage, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	vs, ws := make([]uuid.UUID, 0, len(refs)), make([]uuid.UUID, 0, len(refs))
	for _, r := range refs {
		vs, ws = append(vs, r.VariantID), append(ws, r.WarehouseID)
	}
	slices.SortFunc(vs, compareUUID)
	slices.SortFunc(ws, compareUUID)
	rows, err := store.New(tx).LockFlashBalances(ctx, store.LockFlashBalancesParams{
		VariantIds: slices.Compact(vs), WarehouseIds: slices.Compact(ws),
	})
	if err != nil {
		return nil, fmt.Errorf("khóa số dư của đợt flash sale: %w", err)
	}
	var out []FlashShortage
	for _, r := range refs {
		avail := 0
		if i := slices.IndexFunc(rows, func(b store.LockFlashBalancesRow) bool {
			return b.VariantID == r.VariantID && b.WarehouseID == r.WarehouseID
		}); i >= 0 {
			avail = int(rows[i].Available)
		}
		if avail < r.Quantity {
			out = append(out, FlashShortage{
				ItemID: r.ItemID, VariantID: r.VariantID, WarehouseID: r.WarehouseID, Need: r.Quantity, Sellable: avail,
			})
		}
	}
	return out, nil
}

// ReserveForFlash giữ r.Quantity suất của món cho đợt: held + n khi có thể bán ≥ n (applyDelta), dòng sổ flash_hold
// bước r.Step, n đơn vị suất trống, một stock.changed. Không đủ → *InsufficientError (Is ErrInsufficient), bên gọi lùi
// transaction.
func (w Writer) ReserveForFlash(ctx context.Context, tx pgx.Tx, r FlashItemRef, a Actor) error {
	return w.AddFlashUnits(ctx, tx, r, r.Quantity, a)
}

// AddFlashUnits như ReserveForFlash với add suất (thêm suất cho đợt đã lên lịch hay đang mở; r.Step = add_<version>).
func (w Writer) AddFlashUnits(ctx context.Context, tx pgx.Tx, r FlashItemRef, add int, a Actor) error {
	if !a.valid() {
		return fmt.Errorf("%w: tác nhân %q", ErrInvalidInput, a.Type)
	}
	if add <= 0 || add > math.MaxInt32 || r.ItemID == uuid.Nil || !reasonRe.MatchString(r.Step) {
		return fmt.Errorf("%w: suất flash sale", ErrInvalidInput)
	}
	n := int32(add)
	row, ok, err := applyDelta(ctx, tx, r.VariantID, r.WarehouseID, bucketHeld, n, condAvailable,
		doc{Type: docFlashHold, ID: r.ItemID}, r.Step, a)
	if err != nil {
		return err
	}
	if !ok {
		return insufficient(ctx, tx, ReserveLine{VariantID: r.VariantID, WarehouseID: r.WarehouseID, Qty: n})
	}
	if _, err := store.New(tx).InsertFlashUnits(ctx, store.InsertFlashUnitsParams{FlashSaleItemID: r.ItemID, N: n}); err != nil {
		return fmt.Errorf("trải đơn vị suất: %w", err)
	}
	return w.changed(ctx, tx, row)
}

// CloseFlashItem đóng món: khóa dòng số dư của món, mọi đơn vị còn trống → released, held − số đã trả (điều kiện
// held ≥ q), dòng sổ flash_release bước close, một stock.changed; trả số đơn vị đã trả. Không còn đơn vị trống → 0, không
// ghi sổ. Đơn vị đang giữ cho đơn (held, sold) giữ nguyên: phần held đó nhả theo đơn (Task 202).
func (w Writer) CloseFlashItem(ctx context.Context, tx pgx.Tx, r FlashItemRef, a Actor) (int, error) {
	if !a.valid() {
		return 0, fmt.Errorf("%w: tác nhân %q", ErrInvalidInput, a.Type)
	}
	if _, err := LockFlashStock(ctx, tx, []FlashItemRef{r}); err != nil {
		return 0, err
	}
	q := store.New(tx)
	released, err := q.ReleaseAvailableFlashUnits(ctx, r.ItemID)
	if err != nil {
		return 0, fmt.Errorf("trả đơn vị suất: %w", err)
	}
	if released == 0 {
		return 0, nil
	}
	if released > math.MaxInt32 {
		return 0, fmt.Errorf("%w: %d đơn vị suất", ErrInvalidInput, released)
	}
	row, ok, err := applyDelta(ctx, tx, r.VariantID, r.WarehouseID, bucketHeld, -int32(released), condHeld, //nolint:gosec // G115: đã kiểm ≤ MaxInt32 ở trên
		doc{Type: docFlashRelease, ID: r.ItemID}, stepFlashClose, a)
	if err != nil {
		return 0, err
	}
	if !ok {
		// Số dư lệch đơn vị (bất biến hỏng): không xảy ra khi mọi đường đổi tồn đi qua gói này.
		return 0, fmt.Errorf("%w: held không đủ để trả %d suất của món %s", ErrStateConflict, released, r.ItemID)
	}
	if err := w.changed(ctx, tx, row); err != nil {
		return 0, err
	}
	return int(released), nil
}
