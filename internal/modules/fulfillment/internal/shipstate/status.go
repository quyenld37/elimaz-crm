package shipstate

// Status là trạng thái vận đơn, cùng chuỗi với fulfillment.Status của M1 (`creating` là "đang gửi hãng", Q-33) và với
// CHECK của cột fulfillment.shipments.status.
type Status string

// Trạng thái vận đơn.
const (
	Creating        Status = "creating"
	AwaitingPickup  Status = "awaiting_pickup"
	Shipping        Status = "shipping"
	DeliveryFailed  Status = "delivery_failed"
	Delivered       Status = "delivered"
	Returning       Status = "returning"
	Returned        Status = "returned"
	CancelRequested Status = "cancel_requested"
	Cancelled       Status = "cancelled"
)

// Statuses trả mọi trạng thái vận đơn theo thứ tự khai báo.
func Statuses() []Status {
	return []Status{
		Creating, AwaitingPickup, Shipping, DeliveryFailed, Delivered, Returning, Returned, CancelRequested,
		Cancelled,
	}
}

// Canonicals trả mọi trạng thái chuẩn theo thứ tự khai báo.
func Canonicals() []Canonical {
	return []Canonical{
		CanonAwaitingPickup, CanonPickedUp, CanonInTransit, CanonOutForDelivery, CanonDeliveryFailed,
		CanonDelivered, CanonPartiallyDelivered, CanonReturning, CanonReturnedToShop, CanonCancelled, CanonLost,
		CanonDamaged, CanonUnknown,
	}
}

// Target là trạng thái vận đơn mà một sự kiện chuẩn chỉ tới (ghi vào cột shipment_events.status của M1). Hãng báo
// "đã hoàn về shop" vẫn chỉ tới returning (Q-9: chỉ kho nhận mới thành returned). Giao một phần, mất, hỏng, lạ không
// chỉ tới trạng thái nào → ("", false).
func Target(c Canonical) (Status, bool) {
	switch c {
	case CanonAwaitingPickup:
		return AwaitingPickup, true
	case CanonPickedUp, CanonInTransit, CanonOutForDelivery:
		return Shipping, true
	case CanonDeliveryFailed:
		return DeliveryFailed, true
	case CanonDelivered:
		return Delivered, true
	case CanonReturning, CanonReturnedToShop:
		return Returning, true
	case CanonCancelled:
		return Cancelled, true
	case CanonPartiallyDelivered, CanonLost, CanonDamaged, CanonUnknown:
	}
	return "", false
}

// Display là nhãn tiếng Việt quen với Nhanh của trạng thái vận đơn; mã lạ giữ nguyên. Trạng thái con (sub) và ý định
// hủy (cancelIntent) để dành cho nhãn chi tiết, hôm nay chưa đổi nhãn (tệp task 98, Quyết định thay chủ shop).
func Display(s Status, _ /* sub */, _ /* cancelIntent */ string) string {
	switch s {
	case Creating:
		return "Đang gửi hãng"
	case AwaitingPickup:
		return "Chờ thu gom"
	case Shipping:
		return "Đang chuyển"
	case DeliveryFailed:
		return "Thất bại"
	case Delivered:
		return "Thành công"
	case Returning:
		return "Đang chuyển hoàn"
	case Returned:
		return "Đã hoàn"
	case CancelRequested:
		return "Đang yêu cầu hủy"
	case Cancelled:
		return "Đã hủy"
	}
	return string(s)
}

// Source là nguồn của sự kiện đem ra quyết định. Webhook không bao giờ là nguồn: tin webhook chỉ là tín hiệu để tra lại
// bằng API có xác thực.
type Source string

// Nguồn quyết định (cùng chuỗi với CHECK của cột shipment_events.source).
const (
	SrcCarrierAPI Source = "carrier_api"
	SrcStaff      Source = "staff"
	SrcSystem     Source = "system"
)

// Effect là một tác động bên gọi phải chạy khi áp quyết định (Task 99 effects.go), theo đúng thứ tự trong
// Decision.Effects.
type Effect string

// Tác động.
const (
	EffPickedUp         Effect = "picked_up"
	EffDelivered        Effect = "delivered"
	EffDeliveryFailed   Effect = "delivery_failed"
	EffReturning        Effect = "returning"
	EffCarrierReturned  Effect = "carrier_returned"
	EffCancelConfirmed  Effect = "cancel_confirmed"
	EffCancelRejected   Effect = "cancel_rejected"
	EffDeliveryReversed Effect = "delivery_reversed"
	EffCloseCallbacks   Effect = "close_callbacks"
)

// Lý do bỏ qua (cùng chuỗi với CHECK của cột shipment_events.ignored_reason).
const (
	ReasonOlder          = "older"
	ReasonDuplicateState = "duplicate_state"
	ReasonNotCurrent     = "not_current"
	ReasonInvalidEdge    = "invalid_edge"
	ReasonUnmapped       = "unmapped"
	ReasonReview         = "review"
)

// Loại anomaly Decide mở (cùng chuỗi với CHECK của cột shipment_anomalies.kind).
const (
	AnomalyUnmappedStatus  = "unmapped_status"
	AnomalyPartialReported = "partial_reported"
	AnomalyLostOrDamaged   = "lost_or_damaged"
	AnomalyNotCurrentMoved = "not_current_moved"
	AnomalyInvalidEdge     = "invalid_edge"
)
