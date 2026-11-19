package inventory

import (
	"context"
	"fmt"
	"regexp"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
)

// Trạng thái giữ hàng (05 mục 2) và bước ghi sổ của từng chuyển trạng thái.
const (
	StateHeld      = "held"
	StateInTransit = "in_transit"
	StateConsumed  = "consumed"
	StateReleased  = "released"

	stepReserve      = "reserve"
	stepRelease      = "release"
	stepStartTransit = "start_transit"
	stepConsume      = "consume"
)

var reasonRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Reserve giữ hàng cho từng dòng trong tx của bên gọi và trả mã giữ hàng theo đúng thứ tự lines. Dòng được xử lý
// theo (variant_id, warehouse_id) tăng dần (ADR-46): held + q khi có thể bán ≥ q; dòng đầu tiên không đủ →
// *InsufficientError (Is ErrInsufficient), bên gọi phải lùi transaction. Mỗi dòng ghi sổ doc_type order, doc_id = mã
// giữ hàng, step reserve, và một stock.changed.
func (w Writer) Reserve(ctx context.Context, tx pgx.Tx, lines []ReserveLine, a Actor) ([]uuid.UUID, error) {
	if !a.valid() {
		return nil, fmt.Errorf("%w: tác nhân %q", ErrInvalidInput, a.Type)
	}
	order := make([]int, len(lines))
	for i, l := range lines {
		if l.Qty <= 0 || l.OrderItemID == uuid.Nil || l.VariantID == uuid.Nil || l.WarehouseID == uuid.Nil {
			return nil, fmt.Errorf("%w: dòng giữ hàng %d", ErrInvalidInput, i)
		}
		order[i] = i
	}
	slices.SortStableFunc(order, func(i, j int) int {
		return compareKey(lines[i].VariantID, lines[i].WarehouseID, lines[j].VariantID, lines[j].WarehouseID)
	})
	ids := make([]uuid.UUID, len(lines))
	q := store.New(tx)
	for _, i := range order {
		l := lines[i]
		id, err := uuid.NewV7()
		if err != nil {
			return nil, fmt.Errorf("sinh mã giữ hàng: %w", err)
		}
		row, ok, err := applyDelta(ctx, tx, l.VariantID, l.WarehouseID, bucketHeld, l.Qty, condAvailable,
			doc{Type: docOrder, ID: id}, stepReserve, a)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, insufficient(ctx, tx, l)
		}
		exp := pgtype.Timestamptz{}
		if l.ExpiresAt != nil {
			exp = pgtype.Timestamptz{Time: l.ExpiresAt.UTC(), Valid: true}
		}
		if err := q.InsertReservation(ctx, store.InsertReservationParams{
			ID: id, TenantID: row.TenantID, OrderItemID: l.OrderItemID, VariantID: l.VariantID,
			WarehouseID: l.WarehouseID, Quantity: l.Qty, ExpiresAt: exp,
		}); err != nil {
			return nil, fmt.Errorf("ghi giữ hàng: %w", err)
		}
		if err := w.changed(ctx, tx, row); err != nil {
			return nil, err
		}
		ids[i] = id
	}
	return ids, nil
}

// insufficient đọc số có thể bán hiện tại của dòng l để dựng *InsufficientError.
func insufficient(ctx context.Context, tx pgx.Tx, l ReserveLine) error {
	bs, err := Availability(ctx, tx, l.WarehouseID, []uuid.UUID{l.VariantID})
	if err != nil {
		return err
	}
	return &InsufficientError{VariantID: l.VariantID, WarehouseID: l.WarehouseID, Requested: l.Qty, Available: bs[0].Available}
}

// Release nhả các dòng giữ hàng đang held (held → released, held −). reason: mã ngắn ^[a-z][a-z0-9_]{0,63}$ (ví dụ
// order_cancelled), lưu ở release_reason. Có dòng không ở held (hoặc không có) → ErrStateConflict.
func (w Writer) Release(ctx context.Context, tx pgx.Tx, reservationIDs []uuid.UUID, reason string, a Actor) error {
	if !reasonRe.MatchString(reason) {
		return fmt.Errorf("%w: lý do nhả", ErrInvalidInput)
	}
	return w.transition(ctx, tx, reservationIDs, release(reason), a)
}

// StartTransit chuyển các dòng giữ hàng held → in_transit (hãng lấy hàng): held −, in_transit + khi tồn thực còn đủ
// (on_hand − in_transit ≥ q). Kiểm kho đã làm tồn thực thấp hơn phần giữ (Q8) → *StockShortError (Is ErrStockShort),
// không đổi gì; giữ bất biến in_transit ≤ on_hand để Consume luôn trừ được.
func (w Writer) StartTransit(ctx context.Context, tx pgx.Tx, reservationIDs []uuid.UUID, a Actor) error {
	return w.transition(ctx, tx, reservationIDs, startTransit, a)
}

// Consume chuyển các dòng giữ hàng in_transit → consumed (giao xong): in_transit −, on_hand − khi on_hand ≥ q; không
// đủ (số dư đã lệch từ trước) → *StockShortError, không trừ âm.
func (w Writer) Consume(ctx context.Context, tx pgx.Tx, reservationIDs []uuid.UUID, a Actor) error {
	return w.transition(ctx, tx, reservationIDs, consume, a)
}

// move là một thay đổi số dư của một chuyển trạng thái (q là số lượng của dòng giữ hàng). short: điều kiện sai nghĩa là
// tồn thực không đủ (ErrStockShort), không phải số dư lệch giữ hàng.
type move struct {
	bucket bucket
	sign   int32
	cond   cond
	short  bool
}

// change là một chuyển trạng thái của giữ hàng: từ, tới, bước sổ, các thay đổi số dư theo thứ tự áp.
type change struct {
	from, to, step, reason string
	moves                  []move
}

func release(reason string) change {
	return change{
		from: StateHeld, to: StateReleased, step: stepRelease, reason: reason,
		moves: []move{{bucketHeld, -1, condHeld, false}},
	}
}

//nolint:gochecknoglobals // bảng chuyển trạng thái cố định, chỉ đọc.
var (
	startTransit = change{
		from: StateHeld, to: StateInTransit, step: stepStartTransit,
		moves: []move{{bucketHeld, -1, condHeld, false}, {bucketInTransit, 1, condOnShelf, true}},
	}
	consume = change{
		from: StateInTransit, to: StateConsumed, step: stepConsume,
		moves: []move{{bucketInTransit, -1, condInTransit, false}, {bucketOnHand, -1, condOnHand, true}},
	}
)

// transition là phần lõi của Release, StartTransit, Consume (và bản theo dòng đơn). Thứ tự khóa ADR-46 nhóm 11:
// (1) khóa các dòng số dư liên quan theo (variant_id, warehouse_id) tăng dần; (2) khóa dòng giữ hàng theo id tăng
// dần; (3) đổi trạng thái WHERE state = <cũ> — thiếu dòng nào → ErrStateConflict; (4) đổi số dư theo thứ tự
// (variant_id, warehouse_id, id), mỗi dòng giữ hàng một stock.changed. Mã trùng trong danh sách tính một lần.
func (w Writer) transition(ctx context.Context, tx pgx.Tx, reservationIDs []uuid.UUID, c change, a Actor) error {
	if !a.valid() {
		return fmt.Errorf("%w: tác nhân %q", ErrInvalidInput, a.Type)
	}
	ids := slices.Clone(reservationIDs)
	slices.SortFunc(ids, compareUUID)
	ids = slices.Compact(ids)
	if len(ids) == 0 {
		return nil
	}
	q := store.New(tx)
	res, err := lockInState(ctx, q, ids, c.from)
	if err != nil {
		return err
	}
	reason := pgtype.Text{String: c.reason, Valid: c.reason != ""}
	changed, err := q.SetReservationState(ctx, store.SetReservationStateParams{
		ToState: c.to, ReleaseReason: reason, Ids: ids, FromState: c.from,
	})
	if err != nil {
		return fmt.Errorf("đổi trạng thái giữ hàng: %w", err)
	}
	if len(changed) != len(ids) {
		return fmt.Errorf("%w: cần %s", ErrStateConflict, c.from)
	}
	slices.SortFunc(res, func(x, y store.LockReservationsRow) int {
		if k := compareKey(x.VariantID, x.WarehouseID, y.VariantID, y.WarehouseID); k != 0 {
			return k
		}
		return compareUUID(x.ID, y.ID)
	})
	for _, r := range res {
		if err := w.applyMoves(ctx, tx, r, c, a); err != nil {
			return err
		}
	}
	return nil
}

// lockInState khóa số dư của các dòng giữ hàng ids (đã sắp tăng dần, không trùng) theo (variant_id, warehouse_id)
// tăng dần rồi khóa chính các dòng đó theo id (ADR-46 nhóm 11: số dư trước giữ hàng); thiếu dòng nào hay có dòng không
// ở trạng thái from → ErrStateConflict (đã khóa: kiểm trước để sai trạng thái không ghi gì).
func lockInState(ctx context.Context, q *store.Queries, ids []uuid.UUID, from string) ([]store.LockReservationsRow, error) {
	if _, err := q.LockBalancesOfReservations(ctx, ids); err != nil {
		return nil, fmt.Errorf("khóa số dư: %w", err)
	}
	res, err := q.LockReservations(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("khóa giữ hàng: %w", err)
	}
	if len(res) != len(ids) {
		return nil, fmt.Errorf("%w: %d/%d dòng giữ hàng", ErrStateConflict, len(res), len(ids))
	}
	if slices.ContainsFunc(res, func(r store.LockReservationsRow) bool { return r.State != from }) {
		return nil, fmt.Errorf("%w: cần %s", ErrStateConflict, from)
	}
	return res, nil
}

// applyMoves áp các thay đổi số dư của c cho dòng giữ hàng r (đã khóa) rồi phát một stock.changed.
func (w Writer) applyMoves(ctx context.Context, tx pgx.Tx, r store.LockReservationsRow, c change, a Actor) error {
	var last balanceRow
	for _, m := range c.moves {
		row, ok, err := applyDelta(ctx, tx, r.VariantID, r.WarehouseID, m.bucket, m.sign*r.Quantity, m.cond,
			doc{Type: docOrder, ID: r.ID}, c.step, a)
		if err != nil {
			return err
		}
		if !ok && m.short {
			return stockShort(ctx, tx, r, m.cond)
		}
		if !ok {
			// Số dư lệch giữ hàng (bất biến hỏng): không xảy ra khi mọi đường đổi tồn đi qua gói này.
			return fmt.Errorf("%w: số dư %s không đủ cho giữ hàng %s", ErrStateConflict, m.bucket, r.ID)
		}
		last = row
	}
	return w.changed(ctx, tx, last)
}

// stockShort đọc số dư hiện tại (trong tx, sau các bước trước của cùng chuyển trạng thái) để dựng *StockShortError cho
// dòng giữ hàng r: trên kệ = on_hand − in_transit (lấy hàng, cond on_shelf) hay on_hand (giao, cond on_hand).
func stockShort(ctx context.Context, tx pgx.Tx, r store.LockReservationsRow, c cond) error {
	bs, err := Availability(ctx, tx, r.WarehouseID, []uuid.UUID{r.VariantID})
	if err != nil {
		return err
	}
	onShelf := bs[0].OnHand
	if c == condOnShelf {
		onShelf -= bs[0].InTransit
	}
	return &StockShortError{
		ReservationID: r.ID, VariantID: r.VariantID, WarehouseID: r.WarehouseID, Requested: r.Quantity, OnShelf: onShelf,
	}
}
