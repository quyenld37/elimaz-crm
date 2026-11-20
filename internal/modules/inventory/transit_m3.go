package inventory

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
)

// Hai đường tồn của vận chuyển trên giữ hàng (kế hoạch M3, Task M3-05; TD09, TD10): giao một phần và đảo phiếu xuất.
// Không đổi hàm nào của M1, M2 (A11, Q-X1). Thứ tự khóa ADR-46 nhóm 11 như transition: số dư theo (variant_id,
// warehouse_id) tăng dần, rồi giữ hàng theo id tăng dần (lockInState), rồi mới đổi.

const (
	docAdjust = "adjust"

	stepConsumePartial = "consume_partial" // giao một phần: in_transit −q, on_hand −q, giữ hàng bớt q
	stepReverseConsume = "reverse_consume" // đảo phiếu xuất: consumed → in_transit, on_hand +q, in_transit +q
)

//nolint:gochecknoglobals // bảng chuyển trạng thái cố định, chỉ đọc.
var consumePartial = change{
	from: StateInTransit, to: StateInTransit, step: stepConsumePartial,
	moves: consume.moves,
}

// PartialLine là một dòng giao một phần: dòng đơn và số cái khách nhận (> 0).
type PartialLine struct {
	OrderItemID uuid.UUID
	Qty         int32
}

// ConsumePartial ghi phần khách đã nhận của các dòng đơn đang đi giao (TD09, Q-15), trong tx của bên gọi. Mỗi dòng đơn
// có đúng một giữ hàng in_transit: Qty bằng số đang giữ → như ConsumeByItems (consumed, bước consume); 0 < Qty < số đang
// giữ → giữ hàng bớt Qty, vẫn in_transit (phần chưa giao), in_transit −Qty, on_hand −Qty, sổ doc_type order, doc_id = mã
// giữ hàng, bước consume_partial. Không tách dòng giữ hàng. Mỗi giữ hàng chỉ giao một phần một lần và giao đủ một lần.
// Lỗi (cả lệnh không đổi gì): không dòng, Qty ≤ 0, thiếu mã, trùng dòng đơn, tác nhân lạ → ErrInvalidInput; dòng đơn
// không có (hoặc có hơn một) giữ hàng in_transit, Qty lớn hơn số đang giữ, đã giao một phần (hay đã giao đủ rồi bị đảo)
// → ErrStateConflict; tồn thực không đủ → *StockShortError.
//
// Khi nào không dùng: giao đủ mọi dòng (ConsumeByItems), hàng hoàn về (ReleaseInTransitByItems).
func (w Writer) ConsumePartial(ctx context.Context, tx pgx.Tx, lines []PartialLine, a Actor) error {
	if !a.valid() || len(lines) == 0 {
		return fmt.Errorf("%w: giao một phần", ErrInvalidInput)
	}
	q := store.New(tx)
	byRes, err := partialReservations(ctx, q, lines)
	if err != nil {
		return err
	}
	ids := slices.SortedFunc(maps.Keys(byRes), compareUUID)
	res, err := lockInState(ctx, q, ids, StateInTransit)
	if err != nil {
		return err
	}
	full, part, err := splitPartial(res, byRes)
	if err != nil {
		return err
	}
	if err := checkNotDone(ctx, q, full, stepConsume); err != nil {
		return err
	}
	if err := checkNotDone(ctx, q, part, stepConsumePartial); err != nil {
		return err
	}
	if err := setConsumed(ctx, q, full); err != nil {
		return err
	}
	for _, r := range part {
		n, err := q.ConsumeReservationPart(ctx, store.ConsumeReservationPartParams{Qty: r.Quantity, ID: r.ID})
		if err != nil {
			return fmt.Errorf("bớt giữ hàng giao một phần: %w", err)
		}
		if n != 1 {
			return fmt.Errorf("%w: giữ hàng %s đổi giữa chừng", ErrStateConflict, r.ID) // đã khóa: không xảy ra
		}
	}
	steps := make([]reservationChange, 0, len(res))
	for _, r := range full {
		steps = append(steps, reservationChange{r, consume})
	}
	for _, r := range part {
		steps = append(steps, reservationChange{r, consumePartial})
	}
	slices.SortFunc(steps, func(x, y reservationChange) int {
		if k := compareKey(x.r.VariantID, x.r.WarehouseID, y.r.VariantID, y.r.WarehouseID); k != 0 {
			return k
		}
		return compareUUID(x.r.ID, y.r.ID)
	})
	for _, s := range steps {
		if err := w.applyMoves(ctx, tx, s.r, s.c, a); err != nil {
			return err
		}
	}
	return nil
}

// reservationChange là một giữ hàng đã khóa và chuyển trạng thái áp cho nó (Quantity là số áp).
type reservationChange struct {
	r store.LockReservationsRow
	c change
}

// partialReservations kiểm các dòng giao một phần và trả mã giữ hàng in_transit của từng dòng đơn → Qty. Đọc không khóa
// (bên gọi khóa theo ADR-46 rồi kiểm lại trạng thái). Dòng sai, trùng dòng đơn → ErrInvalidInput; dòng đơn không có
// hay có hơn một giữ hàng in_transit → ErrStateConflict.
func partialReservations(ctx context.Context, q *store.Queries, lines []PartialLine) (map[uuid.UUID]int32, error) {
	qty := make(map[uuid.UUID]int32, len(lines))
	items := make([]uuid.UUID, 0, len(lines))
	for i, l := range lines {
		if _, dup := qty[l.OrderItemID]; dup || l.Qty <= 0 || l.OrderItemID == uuid.Nil {
			return nil, fmt.Errorf("%w: dòng giao một phần %d", ErrInvalidInput, i)
		}
		qty[l.OrderItemID] = l.Qty
		items = append(items, l.OrderItemID)
	}
	found, err := q.InTransitReservationsByItems(ctx, items)
	if err != nil {
		return nil, fmt.Errorf("tìm giữ hàng đang đi giao: %w", err)
	}
	byRes := make(map[uuid.UUID]int32, len(found))
	seen := make(map[uuid.UUID]bool, len(found))
	for _, r := range found {
		item := uuid.UUID(r.OrderItemID.Bytes)
		if seen[item] {
			return nil, fmt.Errorf("%w: dòng đơn có hơn một giữ hàng in_transit", ErrStateConflict)
		}
		seen[item] = true
		byRes[r.ID] = qty[item]
	}
	if len(byRes) != len(items) {
		return nil, fmt.Errorf("%w: %d/%d dòng đơn có giữ hàng in_transit", ErrStateConflict, len(byRes), len(items))
	}
	return byRes, nil
}

// splitPartial chia các giữ hàng đã khóa thành giao đủ (Qty = quantity) và giao một phần (Quantity đổi thành Qty);
// Qty lớn hơn quantity → ErrStateConflict.
func splitPartial(res []store.LockReservationsRow, byRes map[uuid.UUID]int32) (full, part []store.LockReservationsRow,
	err error,
) {
	for _, r := range res {
		switch n := byRes[r.ID]; {
		case n > r.Quantity:
			return nil, nil, fmt.Errorf("%w: giao %d, giữ hàng %s chỉ còn %d", ErrStateConflict, n, r.ID, r.Quantity)
		case n == r.Quantity:
			full = append(full, r)
		default:
			r.Quantity = n
			part = append(part, r)
		}
	}
	return full, part, nil
}

// setConsumed đổi các giữ hàng đã khóa rs in_transit → consumed.
func setConsumed(ctx context.Context, q *store.Queries, rs []store.LockReservationsRow) error {
	if len(rs) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(rs))
	for i, r := range rs {
		ids[i] = r.ID
	}
	changed, err := q.SetReservationState(ctx, store.SetReservationStateParams{
		ToState: StateConsumed, Ids: ids, FromState: StateInTransit,
	})
	if err != nil {
		return fmt.Errorf("đổi trạng thái giữ hàng: %w", err)
	}
	if len(changed) != len(ids) {
		return fmt.Errorf("%w: cần %s", ErrStateConflict, StateInTransit) // đã khóa: không xảy ra
	}
	return nil
}

// checkNotDone: giữ hàng nào trong rs đã có dòng sổ ở bước step (giao một phần lần hai, giao đủ lại sau khi đảo) →
// ErrStateConflict, để không chạm khóa unique của sổ (lỗi 23505 làm hỏng transaction của bên gọi).
func checkNotDone(ctx context.Context, q *store.Queries, rs []store.LockReservationsRow, step string) error {
	if len(rs) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(rs))
	for i, r := range rs {
		ids[i] = r.ID
	}
	done, err := q.ReservationsWithStep(ctx, store.ReservationsWithStepParams{Ids: ids, Steps: []string{step}})
	if err != nil {
		return fmt.Errorf("đọc sổ giữ hàng: %w", err)
	}
	if len(done) > 0 {
		return fmt.Errorf("%w: giữ hàng %s đã có bước %s", ErrStateConflict, done[0].DocID, step)
	}
	return nil
}

// ReverseConsumption đảo phiếu xuất của các dòng đơn đã giao (TD10, Q-1: hãng sửa "đã giao" thành "hoàn"), trong tx của
// bên gọi: mọi giữ hàng consumed của orderItemIDs → in_transit; mỗi (biến thể, kho) on_hand +q rồi in_transit +q (q là
// số đang giữ của giữ hàng: phần đã giao qua ConsumePartial vẫn tính đã giao), sổ doc_type adjust, doc_id = ref (mã sự
// kiện đảo), bước reverse_consume, một stock.changed. Sau đảo hàng đi đường hoàn (ReleaseInTransitByItems); Consume lại
// giữ hàng đã đảo không được hỗ trợ. Cùng ref lần hai → nil, không đổi gì. reason rỗng → ErrReasonRequired; reason sai
// mẫu ^[a-z][a-z0-9_]{0,63}$, ref rỗng, không dòng đơn, tác nhân lạ → ErrInvalidInput; không giữ hàng consumed nào →
// ErrStateConflict. reason không lưu ở inventory (bên gọi ghi trong sự kiện của nó).
func (w Writer) ReverseConsumption(ctx context.Context, tx pgx.Tx, orderItemIDs []uuid.UUID, ref uuid.UUID,
	reason string, a Actor,
) error {
	if err := checkAdjust(ref, reason, a); err != nil {
		return err
	}
	if len(orderItemIDs) == 0 {
		return fmt.Errorf("%w: đảo phiếu xuất không dòng đơn", ErrInvalidInput)
	}
	q := store.New(tx)
	ids, err := q.ReservationIDsByItems(ctx, store.ReservationIDsByItemsParams{
		OrderItemIds: orderItemIDs, State: StateConsumed,
	})
	if err != nil {
		return fmt.Errorf("tìm giữ hàng đã giao: %w", err)
	}
	if len(ids) > 0 {
		// Khóa số dư trước khi đọc sổ: lần chạy song song cùng ref thấy dòng sổ của lần trước sau khi chờ khóa.
		if _, err := q.LockBalancesOfReservations(ctx, ids); err != nil {
			return fmt.Errorf("khóa số dư: %w", err)
		}
	}
	if done, err := q.LedgerHasDocStep(ctx, store.LedgerHasDocStepParams{
		DocType: docAdjust, DocID: ref, Step: stepReverseConsume,
	}); err != nil {
		return fmt.Errorf("đọc sổ đảo phiếu xuất: %w", err)
	} else if done {
		return nil
	}
	if len(ids) == 0 {
		return fmt.Errorf("%w: dòng đơn không có giữ hàng %s", ErrStateConflict, StateConsumed)
	}
	res, err := lockInState(ctx, q, ids, StateConsumed)
	if err != nil {
		return err
	}
	changed, err := q.SetReservationState(ctx, store.SetReservationStateParams{
		ToState: StateInTransit, Ids: ids, FromState: StateConsumed,
	})
	if err != nil {
		return fmt.Errorf("đổi trạng thái giữ hàng: %w", err)
	}
	if len(changed) != len(ids) {
		return fmt.Errorf("%w: cần %s", ErrStateConflict, StateConsumed) // đã khóa: không xảy ra
	}
	sums := make([]adjustSum, 0, len(res))
	for _, r := range res {
		sums = addSum(sums, r.VariantID, r.WarehouseID, r.Quantity)
	}
	return w.reverseSums(ctx, tx, sortSums(sums), doc{Type: docAdjust, ID: ref}, a)
}

// reverseSums cộng lại từng dòng số dư (đã sắp, đã khóa) của phần đảo: on_hand +q trước rồi in_transit +q (giữ
// in_transit ≤ on_hand), mỗi dòng một stock.changed.
func (w Writer) reverseSums(ctx context.Context, tx pgx.Tx, sums []adjustSum, d doc, a Actor) error {
	for _, s := range sums {
		var row balanceRow
		for _, b := range []bucket{bucketOnHand, bucketInTransit} {
			r, ok, err := applyDelta(ctx, tx, s.v, s.wh, b, s.qty, condNone, d, stepReverseConsume, a)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("%w: không có số dư cho giữ hàng", ErrStateConflict) // đã khóa: không xảy ra
			}
			row = r
		}
		if err := w.changed(ctx, tx, row); err != nil {
			return err
		}
	}
	return nil
}

// checkAdjust kiểm phần chung của chứng từ điều chỉnh vận chuyển (ReverseConsumption, RecordLoss).
func checkAdjust(ref uuid.UUID, reason string, a Actor) error {
	switch {
	case reason == "":
		return ErrReasonRequired
	case !reasonRe.MatchString(reason):
		return fmt.Errorf("%w: lý do", ErrInvalidInput)
	case ref == uuid.Nil || !a.valid():
		return fmt.Errorf("%w: chứng từ điều chỉnh", ErrInvalidInput)
	}
	return nil
}

// adjustSum là tổng số lượng của một dòng số dư (biến thể, kho).
type adjustSum struct {
	v, wh uuid.UUID
	qty   int32
}

// addSum cộng qty vào dòng (v, wh) của sums (thêm dòng nếu chưa có).
func addSum(sums []adjustSum, v, wh uuid.UUID, qty int32) []adjustSum {
	if i := slices.IndexFunc(sums, func(s adjustSum) bool { return s.v == v && s.wh == wh }); i >= 0 {
		sums[i].qty += qty
		return sums
	}
	return append(sums, adjustSum{v: v, wh: wh, qty: qty})
}

// sortSums sắp sums theo (variant_id, warehouse_id) tăng dần (ADR-46 nhóm 11).
func sortSums(sums []adjustSum) []adjustSum {
	slices.SortFunc(sums, func(x, y adjustSum) int { return compareKey(x.v, x.wh, y.v, y.wh) })
	return sums
}
