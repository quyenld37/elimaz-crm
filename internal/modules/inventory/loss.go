package inventory

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
)

const stepLoss = "loss" // hàng thất lạc: on_hand −q

// LossLine là một dòng hàng thất lạc: biến thể, kho, số lượng (> 0).
type LossLine struct {
	VariantID, WarehouseID uuid.UUID
	Qty                    int32
}

// RecordLoss ghi hàng thất lạc của phiếu trả ref (TD04, Q-27: kiện hoàn về thiếu món, hàng đã rời in_transit qua
// ReleaseInTransitByItems), trong tx của bên gọi: dòng cùng (biến thể, kho) cộng gộp; theo (variant_id, warehouse_id)
// tăng dần, on_hand −q khi có thể bán ≥ q; sổ doc_type adjust, doc_id = ref, bước loss; một stock.changed mỗi dòng số
// dư. Cùng ref lần hai → nil, không đổi gì. Thiếu hàng có thể bán hay chưa có dòng số dư → ErrStateConflict (cả lệnh
// không đổi gì: bên gọi lùi transaction). reason rỗng → ErrReasonRequired; reason sai mẫu, ref rỗng, không dòng, dòng
// sai, tác nhân lạ → ErrInvalidInput. reason không lưu ở inventory.
//
// Khi nào không dùng: hàng hỏng còn trong kho (MoveToDamaged), điều chỉnh tồn có phiếu (CreateDocument loại adjust).
func (w Writer) RecordLoss(ctx context.Context, tx pgx.Tx, lines []LossLine, ref uuid.UUID, reason string, a Actor,
) error {
	if err := checkAdjust(ref, reason, a); err != nil {
		return err
	}
	if len(lines) == 0 {
		return fmt.Errorf("%w: ghi thất lạc không dòng", ErrInvalidInput)
	}
	var sums []adjustSum
	for i, l := range lines {
		if l.Qty <= 0 || l.VariantID == uuid.Nil || l.WarehouseID == uuid.Nil {
			return fmt.Errorf("%w: dòng thất lạc %d", ErrInvalidInput, i)
		}
		sums = addSum(sums, l.VariantID, l.WarehouseID, l.Qty)
	}
	sums = sortSums(sums)
	vs, whs := make([]uuid.UUID, len(sums)), make([]uuid.UUID, len(sums))
	for i, s := range sums {
		vs[i], whs[i] = s.v, s.wh
	}
	q := store.New(tx)
	// Khóa số dư trước khi đọc sổ: lần chạy song song cùng ref thấy dòng sổ của lần trước sau khi chờ khóa.
	if _, err := q.LockBalancesByKeys(ctx, store.LockBalancesByKeysParams{VariantIds: vs, WarehouseIds: whs}); err != nil {
		return fmt.Errorf("khóa số dư: %w", err)
	}
	if done, err := q.LedgerHasDocStep(ctx, store.LedgerHasDocStepParams{
		DocType: docAdjust, DocID: ref, Step: stepLoss,
	}); err != nil {
		return fmt.Errorf("đọc sổ thất lạc: %w", err)
	} else if done {
		return nil
	}
	d := doc{Type: docAdjust, ID: ref}
	for _, s := range sums {
		row, ok, err := applyDelta(ctx, tx, s.v, s.wh, bucketOnHand, -s.qty, condAvailable, d, stepLoss, a)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: thất lạc %d cái biến thể %s kho %s vượt số có thể bán", ErrStateConflict, s.qty, s.v, s.wh)
		}
		if err := w.changed(ctx, tx, row); err != nil {
			return err
		}
	}
	return nil
}
