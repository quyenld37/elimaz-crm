// Package order là đơn hàng (spec 05 mục 2). Bản M1 (kế hoạch M1, Task M1-30, lát mỏng Q-X1): lên đơn nhập tay giữ
// hàng cùng transaction và đọc đơn; Task M1-30b: gắn vận đơn hiện hành (AttachShipment), áp bước vận đơn lên đơn và
// tồn (Writer.ApplyShipmentProgress; chỉ vận đơn hiện hành đổi được). M2 mở rộng (đơn nháp, sửa, hủy, trả hàng),
// không tạo lại.
//
// Thứ tự khóa (docs/adr/0046-thu-tu-khoa.md): tạo đơn chèn khách (nhóm 5) → đơn → dòng đơn (nhóm 7, dòng mới) → mã
// giảm giá (nhóm 10, promotion.Writer.Redeem) → số dư tồn (nhóm 11, inventory.Writer.Reserve tự sắp) → lịch sử (dòng
// mới). Bước vận đơn: khóa đơn FOR UPDATE trước rồi số dư, giữ
// hàng. Mã đơn lấy từ sequence (nextCode), không khóa dòng.
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
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Trạng thái đơn (05 mục 2; bảy giá trị như order/internal/state) và kênh (CHECK của ord.orders.channel).
const (
	StatusDraft           = "draft"
	StatusNew             = "new"
	StatusConfirmed       = "confirmed"
	StatusCancelRequested = "cancel_requested"
	StatusCancelled       = "cancelled"
	StatusDelivered       = "delivered"
	StatusReturned        = "returned"

	ChannelManual   = "manual"
	ChannelFacebook = "facebook"
	ChannelWeb      = "web"
)

// Actor là người làm một việc trên đơn: tenant, mã người (uuid.Nil → không ghi created_by, actor_id) và loại (Q-T57-2:
// rỗng = staff; system, carrier, customer, service). order không nhập identity (04 mục 3); lớp HTTP chép từ
// identity.AuthUser. CanApproveDiscount (Task M2-17): người làm có quyền order.discount.approve — lớp HTTP tính từ grant
// của người dùng (identity.Can), không bao giờ lấy từ thân yêu cầu.
type Actor struct {
	TenantID, UserID   uuid.UUID
	Type               string
	CanApproveDiscount bool
}

// ItemInput là một dòng của đơn nhập tay: biến thể và số lượng (> 0).
type ItemInput struct {
	VariantID uuid.UUID
	Qty       int32
}

// ShipAddress là địa chỉ giao chụp lúc đặt (05: gồm SĐT người nhận). Ghi log tự che SĐT và địa chỉ; tên người nhận
// không bao giờ được đưa vào log.
//
// ProvinceCode, WardCode (M2-05): mã tỉnh, xã theo danh mục hai cấp (Task M2-04); rỗng ở đơn M1 (JSON bỏ khóa rỗng nên
// địa chỉ của đơn M1 không đổi).
type ShipAddress struct {
	Name         string          `json:"name"`
	Phone        logging.Phone   `json:"phone"`
	Line         logging.Address `json:"line"`
	ProvinceCode string          `json:"provinceCode,omitempty"`
	WardCode     string          `json:"wardCode,omitempty"`
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

// Item là một dòng đơn; SKU, Name là bản chụp lúc đặt. NetAmount = Qty × UnitPrice − LineDiscount −
// AllocatedOrderDiscount.
type Item struct {
	ID, VariantID                        uuid.UUID
	Position, Qty                        int32
	SKU, Name                            string
	UnitPrice, NetAmount                 int64
	LineDiscount, AllocatedOrderDiscount int64
}

// Order là một đơn kèm dòng đơn theo position. Trường M2 (Task M2-05; 05 mục 2): DiscountTotal = LineDiscountTotal +
// OrderDiscount + CouponDiscount; CODAmount = max(GrandTotal − PrepaidTotal, 0); ShipPhone là SĐT giao đã chuẩn hóa
// (rỗng khi không chuẩn hóa được); con trỏ nil là chưa có.
type Order struct {
	ID, SellerID, CustomerID                                    uuid.UUID
	Code, Status, Channel                                       string
	CurrentShipmentID                                           *uuid.UUID
	ShipAddress                                                 ShipAddress
	Subtotal, DiscountTotal, ShippingFee, CODAmount, GrandTotal int64
	Version                                                     int64
	CreatedAt                                                   time.Time
	Items                                                       []Item

	PendingReason, PackingStatus, PaymentStatus, OrderType       string
	WarehouseID                                                  uuid.UUID
	CheckoutGroupID, ChannelAccountID, TrafficSourceID           *uuid.UUID
	ConversationID, ReplacedReturnID                             *uuid.UUID
	ExternalRef, CancelReason, CancelNote                        *string
	ShipPhone                                                    logging.Phone
	AddressComplete, DuplicateSuspect                            bool
	LineDiscountTotal, OrderDiscount, CouponDiscount             int64
	PrepaidTotal                                                 int64
	CarrierNote                                                  string
	WebShipping                                                  *WebShipChoice // hãng khách chọn trên website (Task 490); nil: không có
	CreatedBy, CareBy, ConfirmedBy, DiscountApprovedBy           *uuid.UUID
	DraftExpiresAt, ConfirmedAt, DiscountApprovedAt, CancelledAt *time.Time
	UpdatedAt                                                    time.Time
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
	r, err := q.GetOrderBase(ctx, store.GetOrderBaseParams{SellerID: sellerID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, fmt.Errorf("đọc đơn: %w", err)
	}
	o := orderFromBase(r)
	if err := json.Unmarshal(r.ShipAddress, &o.ShipAddress); err != nil {
		return Order{}, fmt.Errorf("đọc địa chỉ giao: %w", err)
	}
	if o.Channel == ChannelWeb {
		raw, err := q.OrderWebShipping(ctx, store.OrderWebShippingParams{SellerID: sellerID, ID: id})
		if err != nil {
			return Order{}, fmt.Errorf("đọc hãng khách chọn: %w", err)
		}
		if o.WebShipping, err = shipChoiceOf(raw); err != nil {
			return Order{}, err
		}
	}
	items, err := q.ListOrderItems(ctx, store.ListOrderItemsParams{SellerID: sellerID, OrderID: id})
	if err != nil {
		return Order{}, fmt.Errorf("đọc dòng đơn: %w", err)
	}
	o.Items = make([]Item, 0, len(items))
	for _, it := range items {
		o.Items = append(o.Items, Item{
			ID: it.ID, VariantID: it.VariantID, Position: it.Position, Qty: it.Quantity, SKU: it.SkuSnapshot,
			Name: it.NameSnapshot, UnitPrice: it.UnitPrice, NetAmount: it.NetAmount, LineDiscount: it.LineDiscount,
			AllocatedOrderDiscount: it.AllocatedOrderDiscount,
		})
	}
	return o, nil
}

// orderFromBase chép một dòng GetOrderBase sang Order (chưa có địa chỉ giao, dòng đơn).
func orderFromBase(r store.GetOrderBaseRow) Order {
	return Order{
		ID: r.ID, SellerID: r.SellerID, CustomerID: r.CustomerID, Code: r.Code, Status: r.Status, Channel: r.Channel,
		CurrentShipmentID: uuidPtr(r.CurrentShipmentID), Subtotal: r.Subtotal, DiscountTotal: r.DiscountTotal,
		ShippingFee: r.ShippingFeeCharged, CODAmount: r.CodAmount, GrandTotal: r.GrandTotal, Version: r.Version,
		CreatedAt: r.CreatedAt,

		PendingReason: r.PendingReason, PackingStatus: r.PackingStatus, PaymentStatus: r.PaymentStatus,
		OrderType: r.OrderType, WarehouseID: r.WarehouseID, CheckoutGroupID: uuidPtr(r.CheckoutGroupID),
		ChannelAccountID: uuidPtr(r.ChannelAccountID), TrafficSourceID: uuidPtr(r.TrafficSourceID),
		ConversationID: uuidPtr(r.ConversationID), ReplacedReturnID: uuidPtr(r.ReplacedReturnID),
		ExternalRef: textPtr(r.ExternalRef), CancelReason: textPtr(r.CancelReason), CancelNote: textPtr(r.CancelNote),
		ShipPhone: logging.Phone(r.ShipPhone), AddressComplete: r.AddressComplete, DuplicateSuspect: r.DuplicateSuspect,
		LineDiscountTotal: r.LineDiscountTotal, OrderDiscount: r.OrderDiscount, CouponDiscount: r.CouponDiscount,
		PrepaidTotal: r.PrepaidTotal, CarrierNote: r.CarrierNote, CreatedBy: uuidPtr(r.CreatedBy),
		CareBy: uuidPtr(r.CareBy), ConfirmedBy: uuidPtr(r.ConfirmedBy), DiscountApprovedBy: uuidPtr(r.DiscountApprovedBy),
		DraftExpiresAt: timePtr(r.DraftExpiresAt), ConfirmedAt: timePtr(r.ConfirmedAt),
		DiscountApprovedAt: timePtr(r.DiscountApprovedAt), CancelledAt: timePtr(r.CancelledAt), UpdatedAt: r.UpdatedAt,
	}
}

// ptr trả &v khi ok (cột có giá trị), nil khi cột rỗng.
func ptr[T any](v T, ok bool) (out *T) {
	if ok {
		out = &v
	}
	return out
}

func uuidPtr(u pgtype.UUID) *uuid.UUID { return ptr(uuid.UUID(u.Bytes), u.Valid) }

func textPtr(t pgtype.Text) *string { return ptr(t.String, t.Valid) }

func timePtr(t pgtype.Timestamptz) *time.Time { return ptr(t.Time, t.Valid) }
