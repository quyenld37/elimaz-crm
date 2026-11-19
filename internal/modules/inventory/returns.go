package inventory

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
)

// Ba đường tồn của trả hàng (kế hoạch M2, Task M2-22; Q-X1, A5): order gọi trong tx của nó, sau khi đã khóa đơn và
// phiếu trả (ADR-46 nhóm 7 trước nhóm 11). Cùng cách ghi applyDelta (một câu có điều kiện + một dòng sổ, mỗi dòng số dư
// một stock.changed); nhiều biến thể thì theo (variant_id, warehouse_id) tăng dần.

const (
	docReturn = "return"
	docDamage = "damage"

	stepCarrierReturn = "carrier_return" // giữ hàng in_transit → released: hàng hoàn của hãng về kho
	stepReturnReceive = "return_receive" // on_hand + q: hàng khách trả nhập lại kho
	stepReturnDamaged = "return_damaged" // damaged + q: hàng trả về bị hỏng

	reasonCarrierReturn = "carrier_return"
)

//nolint:gochecknoglobals // bảng chuyển trạng thái cố định, chỉ đọc.
var releaseInTransit = change{
	from: StateInTransit, to: StateReleased, step: stepCarrierReturn, reason: reasonCarrierReturn,
	moves: []move{{bucketInTransit, -1, condInTransit, false}},
}

// ReturnLine là một dòng hàng khách trả về kho: biến thể, kho, số lượng (> 0); Damaged thì món đó vào ngăn hỏng.
type ReturnLine struct {
	VariantID, WarehouseID uuid.UUID
	Qty                    int32
	Damaged                bool
}

// ReleaseInTransitByItems nhả mọi dòng giữ hàng in_transit của các dòng đơn (hàng hoàn của hãng về kho): in_transit →
// released, in_transit −, on_hand không đổi (hàng chưa từng rời sổ). Sổ: doc_type order, doc_id = mã giữ hàng, bước
// carrier_return. Không có dòng giữ hàng in_transit nào → ErrStateConflict, không đổi gì (tìm và khóa như byItems).
func (w Writer) ReleaseInTransitByItems(ctx context.Context, tx pgx.Tx, orderItemIDs []uuid.UUID, a Actor) error {
	return w.byItems(ctx, tx, orderItemIDs, releaseInTransit, a)
}

// ReceiveCustomerReturn nhập lại kho hàng khách trả của phiếu docID: mỗi dòng on_hand + q; Damaged thì thêm damaged + q
// (có thể bán không đổi). Dòng cùng (biến thể, kho) được cộng gộp. Sổ doc_type return, doc_id = docID. Đầu vào sai
// (không dòng, số ≤ 0, thiếu mã, tác nhân lạ, biến thể không có) → ErrInvalidInput; bên gọi lùi transaction.
func (w Writer) ReceiveCustomerReturn(ctx context.Context, tx pgx.Tx, docID uuid.UUID, lines []ReturnLine, a Actor) error {
	if !a.valid() || docID == uuid.Nil || len(lines) == 0 {
		return fmt.Errorf("%w: phiếu trả nhập kho", ErrInvalidInput)
	}
	sums, err := sumReturnLines(lines)
	if err != nil {
		return err
	}
	q := store.New(tx)
	d := doc{Type: docReturn, ID: docID}
	for _, s := range sums {
		k := s.key
		if err := q.EnsureBalance(ctx, store.EnsureBalanceParams{VariantID: k.v, WarehouseID: k.wh}); err != nil {
			return fmt.Errorf("tạo số dư cho hàng trả: %w", err)
		}
		row, ok, err := applyDelta(ctx, tx, k.v, k.wh, bucketOnHand, s.all, condNone, d, stepReturnReceive, a)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: biến thể %s không có", ErrInvalidInput, k.v)
		}
		if s.damaged > 0 {
			if row, _, err = applyDelta(ctx, tx, k.v, k.wh, bucketDamaged, s.damaged, condNone, d, stepReturnDamaged, a); err != nil {
				return err
			}
		}
		if err := w.changed(ctx, tx, row); err != nil {
			return err
		}
	}
	return nil
}

// MoveToDamaged chuyển qty cái (> 0) của biến thể ở kho sang ngăn hỏng theo phiếu docID (hàng hoàn của hãng về bị hỏng):
// damaged + q khi có thể bán ≥ q; không đủ → *InsufficientError (Is ErrInsufficient), không đổi gì. Sổ doc_type
// damage, doc_id = docID.
func (w Writer) MoveToDamaged(ctx context.Context, tx pgx.Tx, docID, variantID, warehouseID uuid.UUID, qty int32,
	a Actor,
) error {
	if !a.valid() || docID == uuid.Nil || variantID == uuid.Nil || warehouseID == uuid.Nil || qty <= 0 {
		return fmt.Errorf("%w: chuyển hàng hỏng", ErrInvalidInput)
	}
	row, ok, err := applyDelta(ctx, tx, variantID, warehouseID, bucketDamaged, qty, condAvailable,
		doc{Type: docDamage, ID: docID}, stepReturnDamaged, a)
	if err != nil {
		return err
	}
	if !ok {
		return insufficient(ctx, tx, ReserveLine{VariantID: variantID, WarehouseID: warehouseID, Qty: qty})
	}
	return w.changed(ctx, tx, row)
}

// returnKey là một dòng số dư (biến thể, kho).
type returnKey struct{ v, wh uuid.UUID }

// returnSum là tổng hàng trả của một dòng số dư: mọi món (on_hand +) và món hỏng (damaged +).
type returnSum struct {
	key          returnKey
	all, damaged int32
}

// sumReturnLines kiểm từng dòng và cộng gộp theo (biến thể, kho), sắp tăng dần (ADR-46 nhóm 11).
func sumReturnLines(lines []ReturnLine) ([]returnSum, error) {
	idx := make(map[returnKey]int, len(lines))
	var out []returnSum
	for i, l := range lines {
		if l.Qty <= 0 || l.VariantID == uuid.Nil || l.WarehouseID == uuid.Nil {
			return nil, fmt.Errorf("%w: dòng trả %d", ErrInvalidInput, i)
		}
		k := returnKey{l.VariantID, l.WarehouseID}
		j, ok := idx[k]
		if !ok {
			j = len(out)
			idx[k] = j
			out = append(out, returnSum{key: k})
		}
		out[j].all += l.Qty
		if l.Damaged {
			out[j].damaged += l.Qty
		}
	}
	slices.SortFunc(out, func(x, y returnSum) int { return compareKey(x.key.v, x.key.wh, y.key.v, y.key.wh) })
	return out, nil
}
