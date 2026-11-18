package orderview

// Axes là ba trục của một đơn. Status là một trong 7 trạng thái đơn; PendingReason (”, awaiting_customer,
// out_of_stock, discount_approval) chỉ có nghĩa ở draft, new; PackingStatus là none, picking, packed; ShipmentStatus là
// trạng thái của vận đơn hiện hành, "" khi đơn chưa có vận đơn hiện hành.
type Axes struct {
	Status, PendingReason, PackingStatus, ShipmentStatus string
}

// LabelKey là khóa của nhãn gộp (16 giá trị, Labels).
type LabelKey string

// 16 nhãn gộp; chú thích là chữ hiển thị trên màn hình.
const (
	LabelDraft                    LabelKey = "draft"                      // Nháp
	LabelNew                      LabelKey = "new"                        // Mới
	LabelAwaitingDiscountApproval LabelKey = "awaiting_discount_approval" // Chờ duyệt giảm giá
	LabelAwaitingCustomer         LabelKey = "awaiting_customer"          // Chờ khách xác nhận
	LabelOutOfStock               LabelKey = "out_of_stock"               // Hết hàng
	LabelConfirmed                LabelKey = "confirmed"                  // Đã xác nhận
	LabelPacking                  LabelKey = "packing"                    // Đang đóng gói
	LabelPacked                   LabelKey = "packed"                     // Đã đóng gói
	LabelAwaitingPickup           LabelKey = "awaiting_pickup"            // Chờ thu gom
	LabelShipping                 LabelKey = "shipping"                   // Đang chuyển
	LabelDeliveryFailed           LabelKey = "delivery_failed"            // Thất bại
	LabelReturning                LabelKey = "returning"                  // Đang chuyển hoàn
	LabelCancelRequested          LabelKey = "cancel_requested"           // Đang yêu cầu hủy
	LabelCancelled                LabelKey = "cancelled"                  // Đã hủy
	LabelDelivered                LabelKey = "delivered"                  // Thành công
	LabelReturned                 LabelKey = "returned"                   // Đã hoàn
)

// Labels trả đủ 16 nhãn theo thứ tự thanh nhãn của màn danh sách đơn; mỗi lần gọi một slice mới.
func Labels() []LabelKey {
	return []LabelKey{
		LabelDraft, LabelNew, LabelAwaitingDiscountApproval, LabelAwaitingCustomer, LabelOutOfStock, LabelConfirmed,
		LabelPacking, LabelPacked, LabelAwaitingPickup, LabelShipping, LabelDeliveryFailed, LabelReturning,
		LabelCancelRequested, LabelCancelled, LabelDelivered, LabelReturned,
	}
}

// Label trả nhãn gộp của đơn theo bảng của kế hoạch M2, Task M2-08. Tổ hợp ngoài bảng: new với lý do lạ → new;
// confirmed có vận đơn ở trạng thái ngoài bảng (creating…) → theo trục đóng gói (picking → packing, còn lại confirmed;
// packed chỉ khi chưa có vận đơn); cancel_requested, cancelled, delivered, returned → theo trạng thái đơn, bỏ qua hai
// trục kia. Trạng thái đơn lạ → "" (không thuộc Labels(); dữ liệu sai).
func Label(a Axes) LabelKey {
	switch a.Status {
	case "draft":
		return LabelDraft
	case "new":
		return newLabel(a.PendingReason)
	case "confirmed":
		return confirmedLabel(a.PackingStatus, a.ShipmentStatus)
	case "cancel_requested":
		return LabelCancelRequested
	case "cancelled":
		return LabelCancelled
	case "delivered":
		return LabelDelivered
	case "returned":
		return LabelReturned
	}
	return ""
}

func newLabel(pendingReason string) LabelKey {
	switch pendingReason {
	case "discount_approval":
		return LabelAwaitingDiscountApproval
	case "awaiting_customer":
		return LabelAwaitingCustomer
	case "out_of_stock":
		return LabelOutOfStock
	}
	return LabelNew
}

func confirmedLabel(packing, shipment string) LabelKey {
	switch shipment {
	case "awaiting_pickup":
		return LabelAwaitingPickup
	case "shipping":
		return LabelShipping
	case "delivery_failed":
		return LabelDeliveryFailed
	case "returning", "returned":
		return LabelReturning
	}
	switch {
	case packing == "picking":
		return LabelPacking
	case packing == "packed" && shipment == "":
		return LabelPacked
	}
	return LabelConfirmed
}
