package fulfillment

import "elimaz.com.vn/crm/internal/modules/order"

// Status là trạng thái vận đơn (05 mục 2, phần M1). M3 thêm delivery_failed, returning, returned, cancel_requested.
type Status string

// Trạng thái vận đơn của M1.
const (
	StatusCreating       Status = "creating"
	StatusAwaitingPickup Status = "awaiting_pickup"
	StatusShipping       Status = "shipping"
	StatusDelivered      Status = "delivered"
	StatusCancelled      Status = "cancelled"
)

// rank là thứ hạng chỉ tiến (VC09 bản đầu); 3 để trống cho delivery_failed của M3. Trạng thái không có hạng (cancelled,
// lạ) không tiến, không lùi bằng Advance.
func rank(s Status) (int, bool) {
	switch s {
	case StatusCreating:
		return 0, true
	case StatusAwaitingPickup:
		return 1, true
	case StatusShipping:
		return 2, true
	case StatusDelivered:
		return 4, true
	case StatusCancelled:
	}
	return 0, false
}

// Advance trả trạng thái mới khi next có hạng cao hơn cur (ok = true); lùi, bằng, hay một trong hai không có hạng →
// (cur, false), không đổi gì.
func Advance(cur, next Status) (Status, bool) {
	rc, okc := rank(cur)
	rn, okn := rank(next)
	if !okc || !okn || rn <= rc {
		return cur, false
	}
	return next, true
}

// Step là bước áp lên đơn và tồn khi vận đơn tới trạng thái s: shipping → picked_up, delivered → delivered.
func (s Status) Step() (order.ShipmentStep, bool) {
	switch s {
	case StatusShipping:
		return order.StepPickedUp, true
	case StatusDelivered:
		return order.StepDelivered, true
	case StatusCreating, StatusAwaitingPickup, StatusCancelled:
	}
	return "", false
}
