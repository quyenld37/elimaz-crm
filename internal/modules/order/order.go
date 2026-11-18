// Package order là đơn hàng (spec 05 mục 2). Bản M1 (kế hoạch M1, Task M1-30, lát mỏng Q-X1): lên đơn nhập tay giữ
// hàng cùng transaction và đọc đơn. Gắn vận đơn, áp bước vận đơn lên đơn và tồn: Task M1-30b. M2 mở rộng (đơn nháp,
// sửa, hủy, trả hàng), không tạo lại.
//
// Thứ tự khóa (docs/adr/0046-thu-tu-khoa.md): tạo đơn chèn khách (nhóm 5) → đơn → dòng đơn → lịch sử (nhóm 7, dòng mới)
// → số dư tồn (nhóm 11, inventory.Writer.Reserve tự sắp). Mã đơn lấy từ sequence (nextCode), không khóa dòng.
//
// Phần công khai: hàm đọc Get (nhận sellerID, ADR-29) và Writer (đường ghi: tự ghi lịch sử, sự kiện order.* trong tx
// của bên gọi). Audit do bên gọi ghi bằng Order.AuditFields: order không nhập identity (04 mục 3).
//
// Khi nào không dùng: module khác không đọc bảng ord.* (đọc view ord.orders_v); không đổi trạng thái đơn hay giữ hàng
// của đơn bằng SQL ngoài gói này (mất lịch sử, sự kiện).
package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Trạng thái đơn (05 mục 2) và kênh.
const (
	StatusConfirmed = "confirmed"

	channelManual = "manual"
)

// Actor là nhân viên lên đơn: tenant và mã nhân viên (uuid.Nil → không ghi created_by, actor_id). order không nhập
// identity (04 mục 3); lớp HTTP chép từ identity.AuthUser.
type Actor struct {
	TenantID, UserID uuid.UUID
}

// ItemInput là một dòng của đơn nhập tay: biến thể và số lượng (> 0).
type ItemInput struct {
	VariantID uuid.UUID
	Qty       int32
}

// ShipAddress là địa chỉ giao chụp lúc đặt (05: gồm SĐT người nhận). Ghi log tự che SĐT và địa chỉ; tên người nhận
// không bao giờ được đưa vào log.
type ShipAddress struct {
	Name  string          `json:"name"`
	Phone logging.Phone   `json:"phone"`
	Line  logging.Address `json:"line"`
}

// LogValue che toàn bộ địa chỉ giao khi bị ghi log.
func (ShipAddress) LogValue() slog.Value { return logging.Address("").LogValue() }

// CreateManualInput là đơn nhập tay: khóa nghiệp vụ (uuid, 04 mục 5.5), người bán của đơn, khách đã có, kho giữ hàng
// (uuid.Nil → kho chính), các dòng (giữ thứ tự nhập làm position), địa chỉ giao, phí ship báo khách (đồng).
type CreateManualInput struct {
	IdempotencyKey                    uuid.UUID
	SellerID, CustomerID, WarehouseID uuid.UUID
	Items                             []ItemInput
	ShipAddress                       ShipAddress
	ShippingFee                       int64
}

// Item là một dòng đơn; SKU, Name là bản chụp lúc đặt.
type Item struct {
	ID, VariantID        uuid.UUID
	Position, Qty        int32
	SKU, Name            string
	UnitPrice, NetAmount int64
}

// Order là một đơn kèm dòng đơn theo position.
type Order struct {
	ID, SellerID, CustomerID                                    uuid.UUID
	Code, Status, Channel                                       string
	CurrentShipmentID                                           *uuid.UUID
	ShipAddress                                                 ShipAddress
	Subtotal, DiscountTotal, ShippingFee, CODAmount, GrandTotal int64
	Version                                                     int64
	CreatedAt                                                   time.Time
	Items                                                       []Item
}

// Writer là đường ghi của order: mỗi lần ghi (trong tx của bên gọi, tenancy.WithTenant hay httpapi.WriteTx) ghi kèm
// lịch sử, sự kiện order.* (Events.Emit qua Queue) và đổi tồn qua
// inventory.Writer dựng từ cùng Events, Queue, Realtime. Dựng mới cho mỗi yêu cầu; không giữ trạng thái giữa các lần
// chạy lại (db.RetryTx).
type Writer struct {
	Events   *events.Registry
	Queue    queue.JobQueue
	Realtime realtime.Bus
}

var errWriterNotWired = errors.New("order: Writer thiếu Events hoặc Queue")

// stock trả inventory.Writer dựng từ cùng Events, Queue, Realtime; thiếu Events hoặc Queue → errWriterNotWired.
func (w Writer) stock() (inventory.Writer, error) {
	if w.Events == nil || w.Queue == nil {
		return inventory.Writer{}, errWriterNotWired
	}
	return inventory.Writer{Events: w.Events, Queue: w.Queue, Realtime: w.Realtime}, nil
}

// Get trả đơn id của người bán sellerID kèm dòng đơn; không có (hay của người bán, tenant khác) → ErrNotFound.
func Get(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID) (Order, error) {
	q := store.New(tx)
	r, err := q.GetOrder(ctx, store.GetOrderParams{SellerID: sellerID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, fmt.Errorf("đọc đơn: %w", err)
	}
	o := Order{
		ID: r.ID, SellerID: r.SellerID, CustomerID: r.CustomerID, Code: r.Code, Status: r.Status, Channel: r.Channel,
		Subtotal: r.Subtotal, DiscountTotal: r.DiscountTotal, ShippingFee: r.ShippingFeeCharged, CODAmount: r.CodAmount,
		GrandTotal: r.GrandTotal, Version: r.Version, CreatedAt: r.CreatedAt,
	}
	if r.CurrentShipmentID.Valid {
		s := uuid.UUID(r.CurrentShipmentID.Bytes)
		o.CurrentShipmentID = &s
	}
	if err := json.Unmarshal(r.ShipAddress, &o.ShipAddress); err != nil {
		return Order{}, fmt.Errorf("đọc địa chỉ giao: %w", err)
	}
	items, err := q.ListOrderItems(ctx, store.ListOrderItemsParams{SellerID: sellerID, OrderID: id})
	if err != nil {
		return Order{}, fmt.Errorf("đọc dòng đơn: %w", err)
	}
	o.Items = make([]Item, 0, len(items))
	for _, it := range items {
		o.Items = append(o.Items, Item{
			ID: it.ID, VariantID: it.VariantID, Position: it.Position, Qty: it.Quantity, SKU: it.SkuSnapshot,
			Name: it.NameSnapshot, UnitPrice: it.UnitPrice, NetAmount: it.NetAmount,
		})
	}
	return o, nil
}
