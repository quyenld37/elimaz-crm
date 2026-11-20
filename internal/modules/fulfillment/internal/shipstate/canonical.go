// Package shipstate là máy trạng thái vận đơn (kế hoạch M3 Task 98): thuần Go, không database. Decide là luật duy nhất
// quyết định một sự kiện chuẩn của hãng có đổi trạng thái vận đơn không và kéo theo tác động nào; bên gọi chạy tác
// động trong transaction của mình.
package shipstate

// Canonical là trạng thái chuẩn của vận đơn, chung cho mọi hãng (04 mục 4): adapter đổi mã thô của hãng sang đây qua
// MapCanonical. fulfillment.Canonical là bí danh của kiểu này.
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
