package vtp

import "elimaz.com.vn/crm/internal/modules/fulfillment"

// MapCanonical đổi mã trạng thái thô của VTP (ORDER_STATUS, chuỗi số) sang trạng thái chuẩn theo bảng trạng thái của
// trang Webhook (docs/carriers/vtp.md). Mã mâu thuẫn hay không rõ nghĩa (104: bảng ghi là trạng thái cuối nhưng ghi
// chú và mẫu không — core-e2e/05 I15; 503; 202 sửa phiếu) và mã lạ → (CanonUnknown, false): không ép sang trạng thái
// kết thúc, chờ người.
func (*Adapter) MapCanonical(raw string) (fulfillment.Canonical, bool) {
	switch raw {
	case "101", "107", "201":
		return fulfillment.CanonCancelled, true
	case "102", "103":
		return fulfillment.CanonAwaitingPickup, true
	case "200":
		return fulfillment.CanonPickedUp, true
	case "300", "400", "508", "509", "550":
		return fulfillment.CanonInTransit, true
	case "500":
		return fulfillment.CanonOutForDelivery, true
	case "501":
		return fulfillment.CanonDelivered, true
	case "504":
		return fulfillment.CanonReturnedToShop, true
	case "505", "506", "507":
		return fulfillment.CanonDeliveryFailed, true
	case "515":
		return fulfillment.CanonReturning, true
	}
	return fulfillment.CanonUnknown, false
}

// MapStatus đổi mã thô qua MapCanonical và fulfillment.StatusFromCanonical.
func (a *Adapter) MapStatus(raw string) (fulfillment.Status, bool) {
	c, _ := a.MapCanonical(raw)
	return fulfillment.StatusFromCanonical(c)
}
