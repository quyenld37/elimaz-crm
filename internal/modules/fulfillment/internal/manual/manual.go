// Package manual là phần quyết định thuần của chế độ nhập mã tay và hãng "tự giao" (kế hoạch M3, Task M3-15): không
// database, không gọi hãng. fulfillment (area_manual.go) khóa vận đơn, đọc việc cần xem rồi hỏi gói này nhân viên được
// ghi tay trạng thái nào, đổi trạng thái đó ra sự kiện chuẩn để đi qua đúng máy trạng thái (shipstate.Decide qua
// ApplyTracking).
//
// Khi nào không dùng: không gọi từ module khác (gói internal của fulfillment); luật cạnh của máy trạng thái nằm ở
// shipstate, không lặp ở đây.
package manual

import (
	"strings"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/shipstate"
)

const (
	// ModeManual là mode của tài khoản hãng nhập tay (cùng chuỗi với fulfillment.ModeManual).
	ModeManual = "manual"
	// SelfKey là khóa hãng "tự giao" (cùng chuỗi với fulfillment.SelfCarrierKey).
	SelfKey = "self"
	// selfLabel là nhãn của vận đơn tự giao đang chờ shop đi giao (thay "Chờ thu gom": không có hãng nào tới lấy).
	selfLabel = "Tự giao"
)

// Shipment là phần của vận đơn mà quyết định cần: mode của vận đơn, khóa hãng, có việc hủy tay mở không
// (manual_cancel hay in_doubt_cancel của Task M3-14).
type Shipment struct {
	Mode, CarrierKey string
	StaffCancelOpen  bool
}

// Verdict là kết quả của Check.
type Verdict int

// Các kết quả của Check.
const (
	// OK: được ghi tay (máy trạng thái vẫn có thể từ chối cạnh).
	OK Verdict = iota
	// ReasonRequired: lý do trống (hay chỉ khoảng trắng).
	ReasonRequired
	// NotManual: vận đơn của tài khoản api không có việc hủy tay mở.
	NotManual
	// TargetNotAllowed: trạng thái đích không ghi tay được (returned chỉ qua nhận hàng hoàn; awaiting_pickup,
	// creating, cancel_requested không phải trạng thái người ghi; vận đơn api chỉ nhận cancelled, shipping).
	TargetNotAllowed
)

// Check cho biết nhân viên được ghi tay trạng thái to lên vận đơn s với lý do reason không. Thứ tự: lý do → trạng thái
// đích → mode.
func Check(s Shipment, to shipstate.Status, reason string) Verdict {
	if strings.TrimSpace(reason) == "" {
		return ReasonRequired
	}
	if _, ok := Canonical(to); !ok {
		return TargetNotAllowed
	}
	if s.Mode == ModeManual {
		return OK
	}
	if !s.StaffCancelOpen {
		return NotManual
	}
	if to != shipstate.Cancelled && to != shipstate.Shipping { // "hãng đã hủy" / "hãng không cho hủy"
		return TargetNotAllowed
	}
	return OK
}

// Canonical đổi trạng thái ghi tay to ra sự kiện chuẩn chỉ tới đúng trạng thái đó; to không ghi tay được → false.
func Canonical(to shipstate.Status) (shipstate.Canonical, bool) {
	switch to {
	case shipstate.Shipping:
		return shipstate.CanonPickedUp, true
	case shipstate.DeliveryFailed:
		return shipstate.CanonDeliveryFailed, true
	case shipstate.Delivered:
		return shipstate.CanonDelivered, true
	case shipstate.Returning:
		return shipstate.CanonReturning, true
	case shipstate.Cancelled:
		return shipstate.CanonCancelled, true
	case shipstate.Creating, shipstate.AwaitingPickup, shipstate.Returned, shipstate.CancelRequested:
	}
	return "", false
}

// Label là nhãn hiển thị của vận đơn hãng carrierKey ở trạng thái st; def là nhãn chung (shipstate.Display). Vận đơn
// tự giao đang chờ giao: "Tự giao".
func Label(carrierKey string, st shipstate.Status, def string) string {
	if carrierKey == SelfKey && st == shipstate.AwaitingPickup {
		return selfLabel
	}
	return def
}
