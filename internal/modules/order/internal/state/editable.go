package state

import "elimaz.com.vn/crm/internal/queries/orderview"

// EditScope là các nhóm trường của đơn được sửa.
type EditScope struct {
	Lines, Discounts, Coupon, Address, ShippingFee, CarrierNote, CareBy, TrafficSource bool
}

// Editable trả các nhóm trường được sửa của đơn có ba trục a (Q25 của kế hoạch M2):
//
//   - draft, new, confirmed chưa có vận đơn hiện hành: mọi trường;
//   - draft, new, confirmed có vận đơn hiện hành (ShipmentStatus khác ""): chỉ người phụ trách và nguồn đơn — hãng đã
//     giữ hàng, địa chỉ, phí; sửa qua hãng là việc của M3;
//   - cancel_requested, cancelled, delivered, returned: chỉ người phụ trách;
//   - trạng thái lạ: không trường nào.
//
// Ghi chú nội bộ là bảng riêng, luôn thêm được, không nằm trong EditScope.
func Editable(a orderview.Axes) EditScope {
	switch Status(a.Status) {
	case Draft, New, Confirmed:
		if a.ShipmentStatus != "" {
			return EditScope{CareBy: true, TrafficSource: true}
		}
		return EditScope{
			Lines: true, Discounts: true, Coupon: true, Address: true, ShippingFee: true,
			CarrierNote: true, CareBy: true, TrafficSource: true,
		}
	case CancelRequested, Cancelled, Delivered, Returned:
		return EditScope{CareBy: true}
	}
	return EditScope{}
}
