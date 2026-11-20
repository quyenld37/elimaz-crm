package order

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order/internal/state"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
)

// Hàm đơn hàng cho vận chuyển (kế hoạch M3, Task M3-06): khóa đơn trả ảnh chụp, đảo "Thành công" (TD10), giao một phần
// (partial.go, TD09), đóng gói chỉ đi tới (packing.go, VC04), đổi địa chỉ khi hãng đã nhận (address_change.go, DH15).
// Móc nối vận đơn của M2 (shiphooks.go) dùng nguyên; các hàm ở đây chỉ thêm. Mọi hàm khóa đơn trước (ADR-46 nhóm 7).

// reasonRe là mẫu lý do mã máy (CHECK của ord.order_status_history.reason, inventory reasonRe).
var reasonRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Snapshot là phần của đơn đã khóa mà vận chuyển cần để quyết định (M3: gửi hãng, hủy, thu tiền).
type Snapshot struct {
	ID, SellerID                                    uuid.UUID
	Status, PackingStatus, PaymentStatus, OrderType string
	CurrentShipmentID                               *uuid.UUID
	CODAmount, GrandTotal                           int64
	WarehouseID                                     uuid.UUID
	Version                                         int64
}

// LockSnapshot khóa đơn id của người bán sellerID (FOR UPDATE, ADR-46 nhóm 7: trước mọi dòng nhóm 8 trở đi) trong tx
// của bên gọi và trả ảnh chụp dưới khóa. Không có (hay của người bán, tenant khác) → ErrNotFound. Lock là bản chỉ khóa.
func LockSnapshot(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID) (Snapshot, error) {
	o, err := lockOrder(ctx, store.New(tx), sellerID, id)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		ID: o.ID, SellerID: o.SellerID, Status: o.Status, PackingStatus: o.PackingStatus,
		PaymentStatus: o.PaymentStatus, OrderType: o.OrderType, CurrentShipmentID: uuidPtr(o.CurrentShipmentID),
		CODAmount: o.CodAmount, GrandTotal: o.GrandTotal, WarehouseID: o.WarehouseID, Version: o.Version,
	}, nil
}

// ReverseDelivery đảo "đã giao" của vận đơn shipmentID trên đơn orderID của người bán sellerID (TD10, Q-1: hãng sửa
// "đã giao" thành "hoàn", kết quả tra bằng API có xác thực), trong tx của bên gọi. Chỉ khi shipmentID là vận đơn hiện
// hành và đơn delivered; khác (kể cả gọi lại sau khi đã đảo) → applied=false, không đổi gì. Thứ tự: khóa đơn (nhóm 7) →
// inventory.ReverseConsumption(dòng đơn, ref, reason) (giữ hàng consumed → in_transit, on_hand +, in_transit +; nhóm
// 11) → delivered → confirmed qua máy trạng thái với CarrierCorrection (lịch sử lý do reason, người làm a, giờ now) →
// order.delivery_reversed (payload Changed, seq = version mới), báo hiệu. Tiền (grand_total, cod_amount) không đổi: cash
// nghe sự kiện. Sau đảo đơn đi đường hoàn của M2 (OnShipmentReturning).
//
// Lỗi (bên gọi lùi cả transaction): reason rỗng hay sai mẫu ^[a-z][a-z0-9_]{0,63}$, ref rỗng → ErrInvalidInput; không
// có đơn → ErrNotFound; lỗi của inventory trả nguyên (inventory.ErrStateConflict khi không còn giữ hàng consumed).
func (w Writer) ReverseDelivery(ctx context.Context, tx pgx.Tx, sellerID, orderID, shipmentID, ref uuid.UUID,
	reason string, a Actor, now time.Time,
) (applied bool, err error) {
	if !reasonRe.MatchString(reason) || ref == uuid.Nil {
		return false, fmt.Errorf("%w: lý do, mã chứng từ đảo giao", ErrInvalidInput)
	}
	stock, err := w.stock()
	if err != nil {
		return false, err
	}
	q, o, current, err := lockForShipment(ctx, tx, sellerID, orderID, shipmentID)
	if err != nil || !current || o.Status != StatusDelivered {
		return false, err
	}
	items, err := q.OrderItemIDs(ctx, store.OrderItemIDsParams{SellerID: sellerID, OrderID: orderID})
	if err != nil {
		return false, fmt.Errorf("đọc dòng đơn: %w", err)
	}
	if err := stock.ReverseConsumption(ctx, tx, items, ref, reason, inventory.Actor{Type: a.kind(), ID: a.UserID}); err != nil {
		return false, fmt.Errorf("đảo phiếu xuất: %w", err)
	}
	cur := lockedOrder(o)
	a.TenantID = o.TenantID
	if err := w.applyTransitionWith(ctx, tx, &cur, state.Confirmed, "", reason, transitionOpt{
		topic: TopicOrderDeliveryReversed, carrierCorrection: true,
	}, a, now); err != nil {
		return false, fmt.Errorf("đơn đảo về confirmed: %w", err)
	}
	return true, nil
}
