// Package refunds là consumer mở nghĩa vụ hoàn tiền cho khách của module cash (kế hoạch M3, Task M3-30; Q-2, TI09):
// nghe sự kiện đơn của M2 và chèn nghĩa vụ vào cash.refund_obligations (khóa unique chống trùng). Chi, miễn nghĩa vụ là
// public API của cash (refunds.go).
package refunds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// ConsumerName là tên consumer (việc event.cash_refunds); đổi tên làm mất tiến độ.
const ConsumerName = "cash_refunds"

// Nguồn nghĩa vụ consumer mở (CHECK cash.refund_obligations.source).
const (
	SourceOrderCancel    = "order_cancel"
	SourceCustomerReturn = "customer_return"
)

// topicCancelled là order.<cancelled> (applyTransition của M2 phát "order." + trạng thái mới).
const topicCancelled = "order." + order.StatusCancelled

// Consumer mở nghĩa vụ hoàn theo sự kiện đơn:
//
//   - order.cancelled: tổng phiếu của đơn trừ bù đổi hàng (exchange_credit) > 0 → nghĩa vụ order_cancel bằng số đó.
//   - order.returned, order.partially_returned của phiếu customer_return có refundAmount > 0, khi đơn đã thu tiền (đã
//     giao — COD khách trả qua hãng — hoặc có trả trước) → nghĩa vụ customer_return gắn phiếu bằng refundAmount.
//   - phiếu exchange và order.created của đơn order_type exchange → handleExchange (Task M3-25).
//   - phiếu carrier_return, partial_delivery: bỏ qua (chưa có luật, Task M3-30 Việc phát hiện thêm).
//
// Sự kiện giao lại không mở thêm (unique (đơn, nguồn, phiếu)). Payload hỏng → queue.ErrPermanent.
type Consumer struct{}

// Name trả ConsumerName.
func (Consumer) Name() string { return ConsumerName }

// Topics trả bốn sự kiện đơn consumer nghe.
func (Consumer) Topics() []string {
	return []string{topicCancelled, order.TopicOrderReturned, order.TopicOrderPartiallyReturned, order.TopicOrderCreated}
}

// Handle mở nghĩa vụ của sự kiện e trong tx của worker.
func (Consumer) Handle(ctx context.Context, e events.StoredEvent, tx pgx.Tx) error {
	var p order.ReturnReceived
	if err := json.Unmarshal(e.Payload, &p); err != nil || p.OrderID == uuid.Nil {
		return fmt.Errorf("sự kiện %s: payload không hợp lệ: %w", e.ID, queue.ErrPermanent)
	}
	q := store.New(tx)
	switch e.Topic {
	case topicCancelled:
		paid, err := q.PaidBeforeCancel(ctx, p.OrderID)
		if err != nil {
			return fmt.Errorf("đọc tiền đã thu của đơn hủy: %w", err)
		}
		if paid <= 0 {
			return nil
		}
		return open(ctx, q, p.OrderID, nil, SourceOrderCancel, paid)
	case order.TopicOrderCreated:
		o, err := orderOf(ctx, q, p.OrderID)
		if err != nil || o.OrderType != order.ReturnExchange {
			return err
		}
		return handleExchange(ctx, tx, e)
	}
	switch p.Kind {
	case order.ReturnExchange:
		return handleExchange(ctx, tx, e)
	case order.ReturnCustomer:
	default:
		return nil
	}
	if p.RefundAmount <= 0 || p.ReturnID == uuid.Nil {
		return nil
	}
	o, err := orderOf(ctx, q, p.OrderID)
	if err != nil {
		return err
	}
	if o.Status != order.StatusDelivered && o.Status != order.StatusReturned && o.PrepaidTotal <= 0 {
		return nil
	}
	return open(ctx, q, p.OrderID, &p.ReturnID, SourceCustomerReturn, p.RefundAmount)
}

// orderOf đọc đơn id qua ord.orders_v; không có → queue.ErrPermanent (sự kiện của đơn không có).
func orderOf(ctx context.Context, q *store.Queries, id uuid.UUID) (store.OrderForPaymentRow, error) {
	o, err := q.OrderForPayment(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, fmt.Errorf("đơn %s không có: %w", id, queue.ErrPermanent)
	}
	if err != nil {
		return o, fmt.Errorf("đọc đơn: %w", err)
	}
	return o, nil
}

// open chèn nghĩa vụ (đã có cùng đơn, nguồn, phiếu → không đổi gì).
func open(ctx context.Context, q *store.Queries, orderID uuid.UUID, returnID *uuid.UUID, source string, amount int64) error {
	if _, err := q.InsertRefundObligation(ctx, store.InsertRefundObligationParams{
		OrderID: orderID, ReturnID: db.NullUUID(returnID), Source: source, Amount: amount,
	}); err != nil {
		return fmt.Errorf("mở nghĩa vụ hoàn %s: %w", source, err)
	}
	return nil
}
