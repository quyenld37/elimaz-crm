// Package cancel là phần quyết định thuần của hủy vận đơn, đổi hãng, hủy đơn đã gửi hãng (kế hoạch M3, Task M3-14):
// không database, không gọi hãng. fulfillment (area_cancel.go, cancel_command.go) khóa, đọc, gọi hãng, ghi; gói này chỉ
// nói một yêu cầu hủy được làm gì với vận đơn ở trạng thái nào, kết quả của hãng ra kết quả lệnh nào, để mọi đường
// (nút hủy, consumer order.cancel_requested, lệnh tạo xong) dùng một bảng quyết định có test bảng và test quy luật.
//
// Khi nào không dùng: không gọi từ module khác (gói internal của fulfillment); phân lớp lỗi gọi hãng nằm ở fulfillment.
package cancel

import (
	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/shipstate"
)

// Ý định hủy (shipments.cancel_intent) và loại hủy riêng của vùng này (shipments.cancel_kind).
const (
	// IntentOrderCancel: khách hủy đơn (M2 đã đưa đơn sang cancel_requested); hãng xác nhận → đơn cancelled, nhả hàng.
	IntentOrderCancel = "order_cancel"
	// IntentReplace: hủy vận đơn để gửi lại hãng khác; hãng xác nhận → đơn giữ confirmed, giữ hàng.
	IntentReplace = "replace"
	// KindStaffAfterUnknown: vận đơn creating kẹt (không rõ hãng đã tạo chưa, hay lệnh tạo bị bỏ) mà nhân viên xác nhận đã
	// tra trang hãng, hãng không có kiện (Q-T91-12).
	KindStaffAfterUnknown = "staff_after_unknown"
)

// Kết quả của lệnh hủy (result.outcome khi done).
const (
	OutcomeCancelled = "cancelled"
	OutcomeRejected  = "rejected"
	OutcomePending   = "pending"
)

// Loại bất thường mở khi lệnh hủy kết thúc không có kết quả.
const (
	AnomalyInDoubt = "in_doubt_cancel"
	AnomalyManual  = "manual_cancel"
)

// Shipment là phần của vận đơn (đã khóa) mà quyết định cần: trạng thái, create_state, ý định hủy đã ghi, và hãng có
// hủy qua API được không (tài khoản mode api, adapter khai Cancel).
type Shipment struct {
	Status, CreateState, Intent string
	CarrierAPI                  bool
}

// Request là một yêu cầu hủy: ý định, và nhân viên đã tra trang hãng thấy hãng không có kiện (chỉ có nghĩa khi đổi
// hãng một vận đơn creating kẹt).
type Request struct {
	Intent         string
	CarrierChecked bool
}

// Action là việc RequestCancel làm.
type Action int

// Các việc của RequestCancel.
const (
	// ActNone: không đổi gì (yêu cầu lặp: sự kiện giao lại, vận đơn đã hủy).
	ActNone Action = iota
	// ActCancelNow: vận đơn creating mà hãng chắc không có kiện (not_sent, rejected) → cancelled ngay, gỡ khỏi đơn.
	ActCancelNow
	// ActStaffCancel: vận đơn creating kẹt, nhân viên đã tra hãng → bỏ lệnh tạo, cancelled staff_after_unknown, gỡ.
	ActStaffCancel
	// ActRecordIntent: lệnh tạo còn chờ, có thể đã tạo → ghi cancel_intent; lệnh tạo kết thúc tự hủy (hay ghi lệnh hủy).
	ActRecordIntent
	// ActCreateInProgress: đổi hãng khi lệnh tạo chưa có kết quả → 409 create_in_progress.
	ActCreateInProgress
	// ActRequestCarrier: chờ thu gom, hãng hủy qua API → cancel_requested, lệnh hủy.
	ActRequestCarrier
	// ActRequestManual: chờ thu gom, hãng không hủy qua API → cancel_requested, việc manual_cancel cho nhân viên.
	ActRequestManual
	// ActUpgradeIntent: đang hủy để đổi hãng mà khách hủy đơn → cancel_intent order_cancel, không ghi lệnh thứ hai.
	ActUpgradeIntent
	// ActNotCancellable: đổi hãng khi vận đơn đã đang hủy, đã hủy, hay hàng đã đi → 409 shipment_not_cancellable.
	ActNotCancellable
	// ActConflict: khách hủy đơn mà hàng đã đi → việc cancel_conflict cho người xem, không gọi hãng.
	ActConflict
	// ActInvalid: ý định lạ.
	ActInvalid
)

// Decide trả việc làm với vận đơn s cho yêu cầu r.
func Decide(s Shipment, r Request) Action {
	if r.Intent != IntentOrderCancel && r.Intent != IntentReplace {
		return ActInvalid
	}
	replace := r.Intent == IntentReplace
	switch s.Status {
	case "creating":
		return decideCreating(s, r)
	case "awaiting_pickup":
		if s.CarrierAPI {
			return ActRequestCarrier
		}
		return ActRequestManual
	case "cancel_requested":
		switch {
		case replace:
			return ActNotCancellable
		case s.Intent == IntentReplace:
			return ActUpgradeIntent
		}
		return ActNone
	case "cancelled":
		if replace {
			return ActNotCancellable
		}
		return ActNone
	}
	if replace { // shipping trở đi: phải chờ hoàn
		return ActNotCancellable
	}
	return ActConflict
}

func decideCreating(s Shipment, r Request) Action {
	switch s.CreateState {
	case "not_sent", "rejected":
		return ActCancelNow
	}
	if r.Intent == IntentReplace {
		if r.CarrierChecked && (s.CreateState == "pending" || s.CreateState == "unknown") {
			return ActStaffCancel
		}
		return ActCreateInProgress
	}
	if s.Intent == IntentOrderCancel {
		return ActNone
	}
	return ActRecordIntent
}

// Effective là ý định ghi lên vận đơn: đơn đã cancel_requested (khách hủy) thì luôn order_cancel, kể cả khi nhân viên
// bấm đổi hãng (hãng xác nhận phải hủy đơn, nhả hàng; hãng từ chối phải đưa đơn về confirmed).
func Effective(intent string, orderCancelRequested bool) string {
	if orderCancelRequested {
		intent = IntentOrderCancel
	}
	return intent
}

// DedupeKey là khóa chống trùng của lệnh hủy vận đơn id: mỗi vận đơn tối đa một lệnh hủy.
func DedupeKey(id uuid.UUID) string { return "cancel:" + id.String() }

// Outcome đổi kết quả hủy của adapter (fulfillment.CancelResult) sang outcome của lệnh; lạ → ok = false.
func Outcome(raw string) (string, bool) {
	switch raw {
	case "done":
		return OutcomeCancelled, true
	case "rejected_picked_up":
		return OutcomeRejected, true
	case "pending":
		return OutcomePending, true
	}
	return "", false
}

// LookStatus là kết quả tra lại (commands.Looker) của lệnh hủy theo trạng thái chuẩn mới nhất của kiện: đã hủy hay
// hàng đã đi → done (lệnh tra trạng thái áp kết quả thật); còn chờ thu gom → not_sent (lệnh hủy chưa có tác dụng);
// khác → unknown.
func LookStatus(c shipstate.Canonical) string {
	switch c {
	case shipstate.CanonCancelled, shipstate.CanonPickedUp, shipstate.CanonInTransit, shipstate.CanonOutForDelivery,
		shipstate.CanonDeliveryFailed, shipstate.CanonDelivered, shipstate.CanonReturning, shipstate.CanonReturnedToShop:
		return "done"
	case shipstate.CanonAwaitingPickup:
		return "not_sent"
	case shipstate.CanonPartiallyDelivered, shipstate.CanonLost, shipstate.CanonDamaged, shipstate.CanonUnknown:
	}
	return "unknown"
}

// TerminalAnomaly là bất thường mở khi lệnh hủy kết thúc ở jobStatus mà vận đơn vẫn status: chỉ khi vận đơn còn
// cancel_requested; in_doubt → in_doubt_cancel (không rõ hãng đã hủy chưa); dead, expired → manual_cancel (hãng chưa
// hủy, người hủy trên trang hãng rồi ghi tay). Khác → "".
func TerminalAnomaly(status, jobStatus string) string {
	if status != "cancel_requested" {
		return ""
	}
	switch jobStatus {
	case "in_doubt":
		return AnomalyInDoubt
	case "dead", "expired":
		return AnomalyManual
	}
	return ""
}
