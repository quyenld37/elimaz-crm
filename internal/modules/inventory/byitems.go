package inventory

import (
	"context"
	"fmt"

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
