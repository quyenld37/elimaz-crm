package order

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/events"
)

// Sự kiện công bố của order (docs/views.md mục "Sự kiện công bố"); payload chỉ mang mã và số (A16). Seq = version
// của đơn sau thay đổi.
const (
	TopicOrderCreated   = "order.created"
	TopicOrderConfirmed = "order.confirmed"
	TopicOrderDelivered = "order.delivered"
	// TopicOrderUpdated: đơn đổi trường mà không đổi trạng thái (gắn, gỡ vận đơn hiện hành, tổng đã thanh toán; Task
	// M2-18).
	TopicOrderUpdated = "order.updated"
	// TopicOrderDeliveryReversed: hãng sửa "đã giao" của vận đơn hiện hành, đơn delivered → confirmed (M3-06, TD10,
	// Q-1; Writer.ReverseDelivery). Consumer customer_stats nghe đúng tên này (customer.TopicOrderDeliveryReversed).
	TopicOrderDeliveryReversed = "order.delivery_reversed"

	aggregateOrder = "order"
)

// Changed là payload của mọi sự kiện order.*: mã đơn, người bán, version (= seq), khách và trạng thái đơn sau thay
// đổi (Task M2-16 thêm customerId, status; không dữ liệu cá nhân); partial chỉ có ở lần giao một phần (M3-06).
type Changed struct {
	OrderID    uuid.UUID `json:"orderId"`
	SellerID   uuid.UUID `json:"sellerId"`
	Version    int64     `json:"version"`
	CustomerID uuid.UUID `json:"customerId"`
	Status     string    `json:"status"`
	// Partial: order.delivered của lần giao một phần (Writer.MarkDeliveredPartial, M3-06); false thì không có khóa.
	Partial bool `json:"partial,omitempty"`
}

// emit phát topic của đơn o với payload Changed (version = seq).
func (w Writer) emit(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, o Order, topic string, seq int64) error {
	return w.emitChanged(ctx, tx, tenantID, topic, seq,
		Changed{OrderID: o.ID, SellerID: o.SellerID, Version: seq, CustomerID: o.CustomerID, Status: o.Status})
}

// emitChanged phát topic với payload p đã dựng (seq = p.Version).
func (w Writer) emitChanged(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, topic string, seq int64, p Changed,
) error {
	return w.emitPayload(ctx, tx, tenantID, topic, p.OrderID, seq, p)
}

// emitPayload phát topic của đơn orderID với payload p (Changed hay kiểu nhúng Changed), seq = version mới.
func (w Writer) emitPayload(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, topic string, orderID uuid.UUID,
	seq int64, p any,
) error {
	if _, _, err := w.Events.Emit(ctx, tx, w.Queue, events.Event{
		TenantID: tenantID, AggregateType: aggregateOrder, AggregateID: orderID.String(), Seq: seq, Topic: topic,
		Payload: p,
	}); err != nil {
		return fmt.Errorf("phát %s: %w", topic, err)
	}
	return nil
}

// AuditFields là phần sau của dòng audit cho đơn o: chỉ mã đơn, trạng thái, số tiền, số dòng (không tên khách, SĐT,
// địa chỉ). Bên gọi (lớp HTTP) ghi audit bằng identity.RecordAudit trong cùng tx, vì order không được nhập identity
// (04 mục 3, cột "Được gọi").
func (o Order) AuditFields() map[string]any {
	return map[string]any{
		"code": o.Code, "status": o.Status, "subtotal": o.Subtotal, "shippingFee": o.ShippingFee,
		"grandTotal": o.GrandTotal, "codAmount": o.CODAmount, "items": len(o.Items), "version": o.Version,
	}
}
