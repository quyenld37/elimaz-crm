package fulfillment

import (
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/shipstate"
	"elimaz.com.vn/crm/internal/modules/order"
)

// Status là trạng thái vận đơn (05 mục 2), cùng chuỗi với shipstate.Status.
type Status string

// Trạng thái vận đơn của M1.
const (
	StatusCreating       Status = "creating"
	StatusAwaitingPickup Status = "awaiting_pickup"
	StatusShipping       Status = "shipping"
	StatusDelivered      Status = "delivered"
	StatusCancelled      Status = "cancelled"
)

// Trạng thái vận đơn M3 thêm (Task 98).
const (
	StatusDeliveryFailed  = Status(shipstate.DeliveryFailed)
	StatusReturning       = Status(shipstate.Returning)
	StatusReturned        = Status(shipstate.Returned)
	StatusCancelRequested = Status(shipstate.CancelRequested)
)

// Advance là đường của M1 (lệnh create_shipment, refresh_status): trả trạng thái mới khi vận đơn tiến từ cur tới next
// (ok = true); ngược lại (cur, false), không đổi gì. Luật là shipstate.Decide (một luật duy nhất), giới hạn ở phần M1
// chạy được tác động: cur và next chỉ là trạng thái của M1 có thứ hạng (hủy, hoàn, thất bại, yêu cầu hủy M1 không
// xử lý); cur = creating tới awaiting_pickup là bước "tạo ở hãng thành công", tới bước sau là tạo rồi áp tin hãng
// (như VC10).
func Advance(cur, next Status) (Status, bool) {
	var ev Canonical
	switch next {
	case StatusAwaitingPickup:
		ev = CanonAwaitingPickup
	case StatusShipping:
		ev = CanonInTransit
	case StatusDelivered:
		ev = CanonDelivered
	default:
		return cur, false
	}
	from := shipstate.Status(cur)
	switch cur {
	case StatusCreating:
		if next == StatusAwaitingPickup {
			return next, true
		}
		from = shipstate.AwaitingPickup
	case StatusAwaitingPickup, StatusShipping, StatusDelivered:
	default: // trạng thái M3 (thất bại, hoàn, yêu cầu hủy) cần tác động M1 không có: chỉ ApplyTracking của M3 đổi
		return cur, false
	}
	d := shipstate.Decide(shipstate.Input{Current: from, IsCurrent: true, Event: ev, Source: shipstate.SrcCarrierAPI})
	if !d.Apply {
		return cur, false
	}
	return Status(d.Next), true
}

// Step là bước áp lên đơn và tồn khi vận đơn tới trạng thái s: shipping → picked_up, delivered → delivered.
func (s Status) Step() (order.ShipmentStep, bool) {
	switch s {
	case StatusShipping:
		return order.StepPickedUp, true
	case StatusDelivered:
		return order.StepDelivered, true
	case StatusCreating, StatusAwaitingPickup, StatusCancelled, StatusDeliveryFailed, StatusReturning, StatusReturned,
		StatusCancelRequested:
	}
	return "", false
}
