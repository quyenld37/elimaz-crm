package inventory

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
)

// Bản theo dòng đơn (kế hoạch A5): tìm dòng giữ hàng của orderItemIDs đang ở trạng thái cần rồi gọi đúng phần lõi
// (transition). Tìm không khóa: lõi khóa số dư trước rồi mới khóa giữ hàng (ADR-46 nhóm 11), và đổi trạng thái
// WHERE state = <cũ> nên dòng vừa bị transaction khác đổi cho ErrStateConflict. Không dòng nào → ErrStateConflict.

// ReleaseHeldByItems nhả mọi dòng giữ hàng held của các dòng đơn (Release).
func (w Writer) ReleaseHeldByItems(ctx context.Context, tx pgx.Tx, orderItemIDs []uuid.UUID, reason string, a Actor) error {
	if !reasonRe.MatchString(reason) {
		return fmt.Errorf("%w: lý do nhả", ErrInvalidInput)
	}
	return w.byItems(ctx, tx, orderItemIDs, release(reason), a)
}

// StartTransitByItems chuyển mọi dòng giữ hàng held của các dòng đơn sang in_transit (StartTransit).
func (w Writer) StartTransitByItems(ctx context.Context, tx pgx.Tx, orderItemIDs []uuid.UUID, a Actor) error {
	return w.byItems(ctx, tx, orderItemIDs, startTransit, a)
}

// ConsumeByItems chuyển mọi dòng giữ hàng in_transit của các dòng đơn sang consumed (Consume).
func (w Writer) ConsumeByItems(ctx context.Context, tx pgx.Tx, orderItemIDs []uuid.UUID, a Actor) error {
	return w.byItems(ctx, tx, orderItemIDs, consume, a)
}

func (w Writer) byItems(ctx context.Context, tx pgx.Tx, orderItemIDs []uuid.UUID, c change, a Actor) error {
	ids, err := store.New(tx).ReservationIDsByItems(ctx, store.ReservationIDsByItemsParams{
		OrderItemIds: orderItemIDs, State: c.from,
	})
	if err != nil {
		return fmt.Errorf("tìm giữ hàng theo dòng đơn: %w", err)
	}
	if len(ids) == 0 {
		return fmt.Errorf("%w: dòng đơn không có giữ hàng %s", ErrStateConflict, c.from)
	}
	return w.transition(ctx, tx, ids, c, a)
}

// ReservationStatesByItems đếm các dòng giữ hàng của orderItemIDs theo trạng thái (held, in_transit, consumed,
// released); trạng thái không có dòng nào không có trong map. Đọc không khóa: dùng khi bên gọi đã khóa đối tượng sở
// hữu các dòng đơn (order khóa ord.orders, ADR-46 nhóm 7) để chọn bước tiếp theo; các hàm đổi trạng thái vẫn tự kiểm
// state = <cũ> dưới khóa.
//
// Khi nào không dùng: để quyết định "đủ hàng" (dùng Availability, Reserve).
func ReservationStatesByItems(ctx context.Context, tx pgx.Tx, orderItemIDs []uuid.UUID) (map[string]int, error) {
	rows, err := store.New(tx).CountReservationsByItems(ctx, orderItemIDs)
	if err != nil {
		return nil, fmt.Errorf("đếm giữ hàng theo dòng đơn: %w", err)
	}
	m := make(map[string]int, len(rows))
	for _, r := range rows {
		m[r.State] = int(r.N)
	}
	return m, nil
}

// ResizeHeld đổi số lượng giữ của dòng đơn orderItemID (dòng giữ hàng held duy nhất của nó, M1 giữ một dòng cho mỗi
// dòng đơn) thành newQty khi sửa đơn (kế hoạch M2, Task M2-20), trong tx của bên gọi. Thứ tự khóa ADR-46 nhóm 11: số
// dư rồi dòng giữ hàng. Tăng d: held + d khi có thể bán ≥ d, thiếu → *InsufficientError (Requested = d), không đổi gì;
// giảm d: held − d; cùng số → nil, không ghi gì. Dòng sổ doc_type order, doc_id = mã giữ hàng, bước step (bên gọi đặt
// riêng cho mỗi lần sửa, ví dụ edit_v<version đơn>; mẫu ^[a-z][a-z0-9_]{0,31}$, không trùng bước của Reserve, Release,
// StartTransit, Consume), một stock.changed. newQty ≤ 0, bước sai, tác nhân lạ → ErrInvalidInput; dòng đơn không có
// giữ held → ErrStateConflict.
//
// Khi nào không dùng: bỏ hẳn dòng đơn (ReleaseHeldByItems), dòng đơn mới (Reserve).
func (w Writer) ResizeHeld(ctx context.Context, tx pgx.Tx, orderItemID uuid.UUID, newQty int32, step string, a Actor,
) error {
	switch {
	case !a.valid():
		return fmt.Errorf("%w: tác nhân %q", ErrInvalidInput, a.Type)
	case newQty <= 0 || orderItemID == uuid.Nil:
		return fmt.Errorf("%w: số lượng giữ mới %d", ErrInvalidInput, newQty)
	case !stepRe.MatchString(step) || slices.Contains(coreSteps[:], step):
		return fmt.Errorf("%w: bước sổ %q", ErrInvalidInput, step)
	}
	q := store.New(tx)
	id, err := q.HeldReservationByItem(ctx, orderItemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: dòng đơn không có giữ hàng held", ErrStateConflict)
	}
	if err != nil {
		return fmt.Errorf("tìm giữ hàng của dòng đơn: %w", err)
	}
	res, err := lockInState(ctx, q, []uuid.UUID{id}, StateHeld)
	if err != nil {
		return err
	}
	r := res[0]
	d := newQty - r.Quantity
	if d == 0 {
		return nil
	}
	c := condHeld
	if d > 0 {
		c = condAvailable
	}
	row, ok, err := applyDelta(ctx, tx, r.VariantID, r.WarehouseID, bucketHeld, d, c, doc{Type: docOrder, ID: r.ID}, step, a)
	switch {
	case err != nil:
		return err
	case !ok && d > 0:
		return insufficient(ctx, tx, ReserveLine{OrderItemID: orderItemID, VariantID: r.VariantID, WarehouseID: r.WarehouseID, Qty: d})
	case !ok:
		// Số dư lệch giữ hàng (bất biến hỏng): không xảy ra khi mọi đường đổi tồn đi qua gói này.
		return fmt.Errorf("%w: số dư held không đủ cho giữ hàng %s", ErrStateConflict, r.ID)
	}
	n, err := q.ResizeReservation(ctx, store.ResizeReservationParams{Quantity: newQty, ID: r.ID})
	if err != nil {
		return fmt.Errorf("đổi số lượng giữ hàng: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("%w: giữ hàng %s đổi trạng thái giữa chừng", ErrStateConflict, r.ID) // đã khóa: không xảy ra
	}
	return w.changed(ctx, tx, row)
}

// stepRe là mẫu của cột stock_ledger.step (CHECK của 0115).
var stepRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// coreSteps là các bước của chuyển trạng thái giữ hàng; ResizeHeld không ghi trùng (unique doc, bucket, step).
//
//nolint:gochecknoglobals // bảng cố định, chỉ đọc.
var coreSteps = [...]string{stepReserve, stepRelease, stepStartTransit, stepConsume}
