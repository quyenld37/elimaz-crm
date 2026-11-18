package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
)

// Đổi trạng thái đơn và ghi lịch sử (kế hoạch M2, Task M2-16): mọi câu đổi trạng thái đơn ở queries/ord/transition.sql
// và mọi hàm Go gọi chúng ở tệp này.

// markDelivered đổi đơn o (đã khóa, confirmed) sang delivered, ghi lịch sử và phát order.delivered.
func (w Writer) markDelivered(ctx context.Context, tx pgx.Tx, o store.LockOrderRow) error {
	q := store.New(tx)
	version, err := q.SetOrderStatus(ctx, store.SetOrderStatusParams{
		ToStatus: StatusDelivered, SellerID: o.SellerID, ID: o.ID, FromStatus: StatusConfirmed,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: đơn không còn confirmed", ErrStatusConflict)
	}
	if err != nil {
		return fmt.Errorf("đổi trạng thái đơn: %w", err)
	}
	if err := q.InsertStatusHistory(ctx, store.InsertStatusHistoryParams{
		OrderID: o.ID, FromStatus: pgtype.Text{String: StatusConfirmed, Valid: true}, ToStatus: StatusDelivered,
		ActorType: inventory.ActorCarrier, OccurredAt: time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("ghi lịch sử đơn: %w", err)
	}
	return w.emit(ctx, tx, o.TenantID, Order{ID: o.ID, SellerID: o.SellerID}, TopicOrderDelivered, version)
}
