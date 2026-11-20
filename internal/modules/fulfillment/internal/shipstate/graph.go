package shipstate

// edge là một cạnh hợp lệ của máy trạng thái: tới trạng thái nào, kéo theo tác động nào (theo thứ tự), và chỉ nhận
// từ nguồn nào (rỗng = mọi nguồn).
type edge struct {
	to      Status
	effects []Effect
	sources []Source
}

func to(s Status, effects ...Effect) (edge, bool) { return edge{to: s, effects: effects}, true }

// moved: sự kiện cho thấy hàng đã rời kho (dùng cho anomaly not_current_moved).
func moved(c Canonical) bool {
	switch c {
	case CanonPickedUp, CanonInTransit, CanonOutForDelivery, CanonDeliveryFailed, CanonDelivered, CanonReturning,
		CanonReturnedToShop:
		return true
	case CanonAwaitingPickup, CanonPartiallyDelivered, CanonCancelled, CanonLost, CanonDamaged, CanonUnknown:
	}
	return false
}

// lookup là bảng cạnh của kế hoạch M3 Task 98 (cộng năm cạnh returned_to_shop, tệp task 98 Quyết định 2). Không cạnh
// nào tới returned (chỉ kho nhận, Task 23) hay rời cancelled, returned, creating (creating → awaiting_pickup do
// AttachTrackingCode, không qua sự kiện của hãng).
func lookup(from Status, c Canonical) (edge, bool) {
	switch from {
	case AwaitingPickup:
		return fromAwaitingPickup(c)
	case CancelRequested:
		return fromCancelRequested(c)
	case Shipping:
		return fromShipping(c)
	case DeliveryFailed:
		return fromDeliveryFailed(c)
	case Delivered:
		return fromDelivered(c)
	case Returning:
		if c == CanonReturnedToShop {
			return to(Returning, EffCarrierReturned) // Q-9: chỉ ghi mốc hãng báo đã hoàn
		}
	case Creating, Returned, Cancelled:
	}
	return edge{}, false
}

func fromAwaitingPickup(c Canonical) (edge, bool) {
	switch c {
	case CanonPickedUp, CanonInTransit, CanonOutForDelivery:
		return to(Shipping, EffPickedUp)
	case CanonDelivered:
		return to(Delivered, EffPickedUp, EffDelivered)
	case CanonDeliveryFailed:
		return to(DeliveryFailed, EffPickedUp, EffDeliveryFailed)
	case CanonReturning:
		return to(Returning, EffPickedUp, EffReturning)
	case CanonReturnedToShop:
		return to(Returning, EffPickedUp, EffReturning, EffCarrierReturned)
	case CanonCancelled:
		return to(Cancelled, EffCancelConfirmed)
	case CanonAwaitingPickup, CanonPartiallyDelivered, CanonLost, CanonDamaged, CanonUnknown:
	}
	return edge{}, false
}

// fromCancelRequested: hãng đã lấy hàng trước khi hủy → hủy không thành (VC12); hoàn khi đang yêu cầu hủy → đơn về
// confirmed trước rồi mới tạo phiếu hoàn (Q-T91-2).
func fromCancelRequested(c Canonical) (edge, bool) {
	switch c {
	case CanonCancelled:
		return to(Cancelled, EffCancelConfirmed)
	case CanonPickedUp, CanonInTransit, CanonOutForDelivery:
		return to(Shipping, EffPickedUp, EffCancelRejected)
	case CanonReturning:
		return to(Returning, EffPickedUp, EffCancelRejected, EffReturning)
	case CanonReturnedToShop:
		return to(Returning, EffPickedUp, EffCancelRejected, EffReturning, EffCarrierReturned)
	case CanonAwaitingPickup, CanonDeliveryFailed, CanonDelivered, CanonPartiallyDelivered, CanonLost, CanonDamaged,
		CanonUnknown:
	}
	return edge{}, false
}

func fromShipping(c Canonical) (edge, bool) {
	switch c {
	case CanonDelivered:
		return to(Delivered, EffDelivered, EffCloseCallbacks)
	case CanonDeliveryFailed:
		return to(DeliveryFailed, EffDeliveryFailed)
	case CanonReturning:
		return to(Returning, EffReturning, EffCloseCallbacks)
	case CanonReturnedToShop:
		return to(Returning, EffReturning, EffCloseCallbacks, EffCarrierReturned)
	case CanonAwaitingPickup, CanonPickedUp, CanonInTransit, CanonOutForDelivery, CanonPartiallyDelivered,
		CanonCancelled, CanonLost, CanonDamaged, CanonUnknown:
	}
	return edge{}, false
}

func fromDeliveryFailed(c Canonical) (edge, bool) {
	switch c {
	case CanonPickedUp, CanonInTransit, CanonOutForDelivery:
		return to(Shipping) // giao lại
	case CanonDelivered:
		return to(Delivered, EffDelivered, EffCloseCallbacks)
	case CanonReturning:
		return to(Returning, EffReturning, EffCloseCallbacks)
	case CanonReturnedToShop:
		return to(Returning, EffReturning, EffCloseCallbacks, EffCarrierReturned)
	case CanonAwaitingPickup, CanonDeliveryFailed, CanonPartiallyDelivered, CanonCancelled, CanonLost, CanonDamaged,
		CanonUnknown:
	}
	return edge{}, false
}

// fromDelivered: hãng sửa "đã giao" thành "hoàn" (TD10, Q-1) chỉ nhận khi tra bằng API có xác thực hoặc nhân viên ghi.
func fromDelivered(c Canonical) (edge, bool) {
	corrected := []Source{SrcCarrierAPI, SrcStaff}
	switch c {
	case CanonReturning:
		return edge{to: Returning, effects: []Effect{EffDeliveryReversed, EffReturning}, sources: corrected}, true
	case CanonReturnedToShop:
		return edge{
			to: Returning, effects: []Effect{EffDeliveryReversed, EffReturning, EffCarrierReturned},
			sources: corrected,
		}, true
	case CanonAwaitingPickup, CanonPickedUp, CanonInTransit, CanonOutForDelivery, CanonDeliveryFailed, CanonDelivered,
		CanonPartiallyDelivered, CanonCancelled, CanonLost, CanonDamaged, CanonUnknown:
	}
	return edge{}, false
}
