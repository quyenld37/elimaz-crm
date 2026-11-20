package fulfillment

// Canonical là trạng thái chuẩn của vận đơn, chung cho mọi hãng (04 mục 4): adapter đổi mã thô của hãng sang đây qua
// MapCanonical.
type Canonical string

// Trạng thái chuẩn.
const (
	CanonAwaitingPickup     Canonical = "awaiting_pickup"
	CanonPickedUp           Canonical = "picked_up"
	CanonInTransit          Canonical = "in_transit"
	CanonOutForDelivery     Canonical = "out_for_delivery"
	CanonDeliveryFailed     Canonical = "delivery_failed"
	CanonDelivered          Canonical = "delivered"
	CanonPartiallyDelivered Canonical = "partially_delivered"
	CanonReturning          Canonical = "returning"
	CanonReturnedToShop     Canonical = "returned_to_shop"
	CanonCancelled          Canonical = "cancelled"
	CanonLost               Canonical = "lost"
	CanonDamaged            Canonical = "damaged"
	CanonUnknown            Canonical = "unknown"
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
