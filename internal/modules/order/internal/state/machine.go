// Package state là máy trạng thái của đơn (05 mục 2: đơn và vận đơn là hai trục riêng) và phạm vi được sửa theo
// trạng thái (kế hoạch M2, Task M2-08). Thuần Go, không database: bên gọi khóa dòng đơn, kiểm version, rồi hỏi Check
// trước khi đổi trạng thái và Editable trước khi sửa trường.
package state

import "errors"

// Status là trạng thái đơn (7 giá trị, CHECK của ord.orders).
type Status string

// Bảy trạng thái đơn.
const (
	Draft           Status = "draft"
	New             Status = "new"
	Confirmed       Status = "confirmed"
	CancelRequested Status = "cancel_requested"
	Cancelled       Status = "cancelled"
	Delivered       Status = "delivered"
	Returned        Status = "returned"
)

// Lý do chờ của đơn draft, new (CHECK của ord.orders.pending_reason) mà Check cần biết.
const (
	PendingNone             = ""
	PendingAwaitingCustomer = "awaiting_customer"
	PendingOutOfStock       = "out_of_stock"
	PendingDiscountApproval = "discount_approval"
)

// Lỗi của Check. Gói order bọc thành sentinel của mình ở order/errors.go.
var (
	// ErrInvalidTransition: cặp (từ, tới) không có trong sơ đồ 05 mục 2, hoặc có nhưng sai điều kiện vận đơn.
	ErrInvalidTransition = errors.New("state: không chuyển được trạng thái đơn")
	// ErrNeedsApproval: đơn đang chờ duyệt giảm giá, chưa xác nhận được.
	ErrNeedsApproval = errors.New("state: đơn chờ duyệt giảm giá")
	// ErrAddressIncomplete: đơn nháp chưa đủ địa chỉ giao, chưa chốt được.
	ErrAddressIncomplete = errors.New("state: địa chỉ giao chưa đủ")
)

// Conditions là các điều kiện của đơn mà một số cạnh cần.
type Conditions struct {
	HasCurrentShipment, AddressComplete bool
	PendingReason                       string
	// CarrierCorrection: hãng sửa "đã giao" của vận đơn hiện hành (M3-06, TD10, Q-1); chỉ order.ReverseDelivery đặt.
	CarrierCorrection bool
}

// Check trả nil khi đơn được chuyển from → to với điều kiện c. Chỉ đúng các cạnh của sơ đồ đơn ở 05 mục 2, cộng
// new → new và draft → draft khi chỉ đổi lý do chờ (draft → draft: duyệt giảm giá đơn nháp, Task M2-19):
//
//   - draft → confirmed: cần AddressComplete (không thì ErrAddressIncomplete) và không lý do chờ (discount_approval →
//     ErrNeedsApproval, lý do khác → ErrInvalidTransition);
//   - draft → cancelled, new → cancelled, new → new, draft → draft, cancel_requested → cancelled, cancel_requested → confirmed,
//     confirmed → delivered, confirmed → returned: không điều kiện;
//   - new → confirmed: lý do chờ khác discount_approval (không thì ErrNeedsApproval);
//   - confirmed → cancelled chỉ khi chưa có vận đơn hiện hành, confirmed → cancel_requested chỉ khi có;
//   - delivered → confirmed chỉ khi CarrierCorrection (M3-06: hãng sửa "đã giao" thành "hoàn").
//
// Mọi cặp khác, kể cả draft → new, trả ErrInvalidTransition.
func Check(from, to Status, c Conditions) error {
	switch [2]Status{from, to} {
	case [2]Status{Draft, Confirmed}:
		switch {
		case !c.AddressComplete:
			return ErrAddressIncomplete
		case c.PendingReason == PendingDiscountApproval:
			return ErrNeedsApproval
		case c.PendingReason != PendingNone:
			return ErrInvalidTransition
		}
		return nil
	case [2]Status{New, Confirmed}:
		if c.PendingReason == PendingDiscountApproval {
			return ErrNeedsApproval
		}
		return nil
	case [2]Status{Confirmed, Cancelled}:
		if c.HasCurrentShipment {
			return ErrInvalidTransition
		}
		return nil
	case [2]Status{Confirmed, CancelRequested}:
		if !c.HasCurrentShipment {
			return ErrInvalidTransition
		}
		return nil
	case [2]Status{Delivered, Confirmed}:
		if !c.CarrierCorrection {
			return ErrInvalidTransition
		}
		return nil
	case [2]Status{Draft, Cancelled}, [2]Status{New, Cancelled}, [2]Status{New, New}, [2]Status{Draft, Draft},
		[2]Status{CancelRequested, Cancelled}, [2]Status{CancelRequested, Confirmed},
		[2]Status{Confirmed, Delivered}, [2]Status{Confirmed, Returned}:
		return nil
	}
	return ErrInvalidTransition
}
