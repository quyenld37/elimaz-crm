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

	aggregateOrder = "order"
)

// Changed là payload của mọi sự kiện order.*: mã đơn, người bán, version (= seq), khách và trạng thái đơn sau thay
// đổi (Task M2-16 thêm customerId, status; không dữ liệu cá nhân).
type Changed struct {
	OrderID    uuid.UUID `json:"orderId"`
	SellerID   uuid.UUID `json:"sellerId"`
	Version    int64     `json:"version"`
	CustomerID uuid.UUID `json:"customerId"`
	Status     string    `json:"status"`
}

func (w Writer) emit(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, o Order, topic string, seq int64) error {
	if _, _, err := w.Events.Emit(ctx, tx, w.Queue, events.Event{
		TenantID: tenantID, AggregateType: aggregateOrder, AggregateID: o.ID.String(), Seq: seq, Topic: topic,
		Payload: Changed{OrderID: o.ID, SellerID: o.SellerID, Version: seq, CustomerID: o.CustomerID, Status: o.Status},
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
