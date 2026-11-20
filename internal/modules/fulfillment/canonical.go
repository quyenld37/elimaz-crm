package fulfillment

import "elimaz.com.vn/crm/internal/modules/fulfillment/internal/shipstate"

// Canonical là trạng thái chuẩn của vận đơn, chung cho mọi hãng (04 mục 4): adapter đổi mã thô của hãng sang đây qua
// MapCanonical. Kiểu nằm ở máy trạng thái (shipstate) để Decide dùng được mà không nhập ngược fulfillment.
type Canonical = shipstate.Canonical

// Trạng thái chuẩn.
const (
	CanonAwaitingPickup     = shipstate.CanonAwaitingPickup
	CanonPickedUp           = shipstate.CanonPickedUp
	CanonInTransit          = shipstate.CanonInTransit
	CanonOutForDelivery     = shipstate.CanonOutForDelivery
	CanonDeliveryFailed     = shipstate.CanonDeliveryFailed
	CanonDelivered          = shipstate.CanonDelivered
	CanonPartiallyDelivered = shipstate.CanonPartiallyDelivered
	CanonReturning          = shipstate.CanonReturning
	CanonReturnedToShop     = shipstate.CanonReturnedToShop
	CanonCancelled          = shipstate.CanonCancelled
	CanonLost               = shipstate.CanonLost
	CanonDamaged            = shipstate.CanonDamaged
	CanonUnknown            = shipstate.CanonUnknown
)

// StatusFromCanonical đổi trạng thái chuẩn sang trạng thái vận đơn của M1, dùng chung để adapter hiện thực MapStatus
// qua MapCanonical. Trạng thái chuẩn chưa có trạng thái M1 tương ứng (giao thất bại, hoàn, mất, hỏng, lạ; M3 Task 7
// thêm) → ("", false): M1 không đổi gì.
func StatusFromCanonical(c Canonical) (Status, bool) {
	switch c {
	case CanonAwaitingPickup:
		return StatusAwaitingPickup, true
	case CanonPickedUp, CanonInTransit, CanonOutForDelivery:
		return StatusShipping, true
	case CanonDelivered:
		return StatusDelivered, true
	case CanonCancelled:
		return StatusCancelled, true
	case CanonDeliveryFailed, CanonPartiallyDelivered, CanonReturning, CanonReturnedToShop, CanonLost, CanonDamaged,
		CanonUnknown:
	}
	return "", false
}
