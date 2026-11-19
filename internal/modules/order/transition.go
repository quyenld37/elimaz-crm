package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order/internal/state"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Đổi trạng thái đơn và ghi lịch sử (kế hoạch M2, Task M2-16): mọi câu đổi trạng thái đơn ở queries/ord/transition.sql
// (test kiến trúc order_status_sql_test) và mọi hàm Go gọi chúng ở tệp này. Task sau đổi trạng thái chỉ qua
// applyTransition.

// Báo hiệu realtime của đơn: chỉ mã đơn, người có order.read nhận (order không nhập identity, 04 mục 3).
const (
	signalTopic = "order.changed"
	permRead    = "order.read"
)

// kind là loại tác nhân ghi vào lịch sử (rỗng = staff, Q-T57-2).
func (a Actor) kind() string {
	if a.Type == "" {
		return inventory.ActorStaff
	}
	return a.Type
}

// id là mã người làm cho cột uuid có thể rỗng.
func (a Actor) id() pgtype.UUID {
	return pgtype.UUID{Bytes: a.UserID, Valid: a.UserID != uuid.Nil}
}

// recordCreated ghi phần sau của lần tạo đơn o (đã chèn): dòng lịch sử ” → status, sự kiện order.created (seq 1) và
// thêm order.confirmed (seq 2) khi tạo thẳng confirmed (version của đơn bằng seq cuối), báo hiệu order.changed.
func (w Writer) recordCreated(ctx context.Context, tx pgx.Tx, o Order, a Actor, now time.Time) error {
	if err := insertHistory(ctx, tx, o.ID, "", "", o.Status, o.PendingReason, "", a, now); err != nil {
		return err
	}
	topics := []string{TopicOrderCreated}
	if o.Status == StatusConfirmed {
		topics = append(topics, TopicOrderConfirmed)
	}
	for i, topic := range topics {
		if err := w.emit(ctx, tx, a.TenantID, o, topic, int64(i+1)); err != nil {
			return err
		}
	}
	return w.signal(ctx, tx, a.TenantID, o)
}

// applyTransition đổi đơn o (bên gọi đã khóa dòng hoặc mang version vừa đọc) sang to với lý do chờ pending: hỏi
// state.Check (sai → ErrInvalidTransition, ErrAddressIncomplete, ErrNeedsApproval), đổi dòng khi version còn khớp (không
// khớp → ErrVersionConflict), ghi lịch sử (kèm reason, có thể rỗng), phát order.<to> với seq = version mới, báo hiệu.
// Thành công thì cập nhật Status, PendingReason, Version, UpdatedAt của o. Mọi việc đổi trạng thái đơn đi qua đây.
func (w Writer) applyTransition(ctx context.Context, tx pgx.Tx, o *Order, to state.Status, pending, reason string,
	a Actor, now time.Time,
) error {
	if err := state.Check(state.Status(o.Status), to, state.Conditions{
		HasCurrentShipment: o.CurrentShipmentID != nil, AddressComplete: o.AddressComplete,
		PendingReason: o.PendingReason,
	}); err != nil {
		return stateError(err)
	}
	version, err := store.New(tx).UpdateOrderStatus(ctx, store.UpdateOrderStatusParams{
		ToStatus: string(to), PendingReason: pending, Now: now, ActorID: a.id(), SellerID: o.SellerID, ID: o.ID,
		Version: o.Version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrVersionConflict
	}
	if err != nil {
		return fmt.Errorf("đổi trạng thái đơn: %w", err)
	}
	if err := insertHistory(ctx, tx, o.ID, o.Status, o.PendingReason, string(to), pending, reason, a, now); err != nil {
		return err
	}
	o.Status, o.PendingReason, o.Version, o.UpdatedAt = string(to), pending, version, now
	if err := w.emit(ctx, tx, a.TenantID, *o, "order."+string(to), version); err != nil {
		return err
	}
	return w.signal(ctx, tx, a.TenantID, *o)
}

// stateError đổi lỗi của máy trạng thái sang sentinel của order.
func stateError(err error) error {
	switch {
	case errors.Is(err, state.ErrAddressIncomplete):
		return ErrAddressIncomplete
	case errors.Is(err, state.ErrNeedsApproval):
		return ErrNeedsApproval
	default:
		return fmt.Errorf("%w: %w", ErrInvalidTransition, err)
	}
}

// insertHistory ghi một dòng lịch sử trạng thái (from rỗng: dòng tạo đơn, from_status NULL).
func insertHistory(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, from, fromPending, to, toPending, reason string,
	a Actor, at time.Time,
) error {
	if err := store.New(tx).InsertStatusHistory(ctx, store.InsertStatusHistoryParams{
		OrderID: orderID, FromStatus: pgtype.Text{String: from, Valid: from != ""}, ToStatus: to,
		FromPending: fromPending, ToPending: toPending, ActorType: a.kind(), ActorID: a.id(),
		Reason: pgtype.Text{String: reason, Valid: reason != ""}, OccurredAt: at,
	}); err != nil {
		return fmt.Errorf("ghi lịch sử đơn: %w", err)
	}
	return nil
}

// signal báo màn hình đơn o vừa đổi (chỉ mã); Realtime nil → bỏ qua (báo hiệu được phép mất).
func (w Writer) signal(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, o Order) error {
	if w.Realtime == nil {
		return nil
	}
	sig := realtime.Signal{TenantID: tenantID, Topic: signalTopic, Requires: permRead}
	sig.Entity.Type, sig.Entity.ID = aggregateOrder, o.ID.String()
	if err := w.Realtime.Publish(ctx, tx, sig); err != nil {
		return fmt.Errorf("báo hiệu đơn %s: %w", o.Code, err)
	}
	return nil
}
