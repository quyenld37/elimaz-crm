package cancel_test

import (
	"testing"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/cancel"
)

// Quy luật của Decide trên mọi trạng thái vận đơn, ý định, cờ: chỉ hủy ngay khi hãng chắc không có kiện; chỉ gọi hãng
// khi vận đơn chờ thu gom; đổi hãng không bao giờ ghi ý định hay mở cancel_conflict; hủy đơn (M2 đã đưa đơn sang
// cancel_requested, không trả 409 được) không bao giờ bị từ chối; áp kết quả rồi quyết lại cùng yêu cầu hủy đơn → không
// làm gì (sự kiện giao lại không ghi lệnh thứ hai).
func TestDecideProperties(t *testing.T) {
	statuses := []string{
		"creating", "awaiting_pickup", "cancel_requested", "cancelled", "shipping", "delivery_failed",
		"delivered", "returning", "returned",
	}
	states := []string{"pending", "not_sent", "unknown", "created", "rejected", "manual"}
	rapid.Check(t, func(t *rapid.T) {
		s := cancel.Shipment{
			Status: rapid.SampledFrom(statuses).Draw(t, "status"), CreateState: rapid.SampledFrom(states).Draw(t, "state"),
			Intent:     rapid.SampledFrom([]string{"", "order_cancel", "replace"}).Draw(t, "intent"),
			CarrierAPI: rapid.Bool().Draw(t, "api"),
		}
		r := cancel.Request{
			Intent:         rapid.SampledFrom([]string{"order_cancel", "replace"}).Draw(t, "req"),
			CarrierChecked: rapid.Bool().Draw(t, "checked"),
		}
		a := cancel.Decide(s, r)
		checkAction(t, s, r, a)
		if r.Intent != cancel.IntentOrderCancel {
			return
		}
		after, ok := applied(s, r, a)
		if !ok {
			return
		}
		if again := cancel.Decide(after, r); again != cancel.ActNone {
			t.Fatalf("giao lại sự kiện hủy đơn: %v sau %v (%+v)", again, a, after)
		}
	})
}

// checkAction kiểm việc a mà Decide trả cho vận đơn s, yêu cầu r.
func checkAction(t *rapid.T, s cancel.Shipment, r cancel.Request, a cancel.Action) {
	switch a {
	case cancel.ActCancelNow:
		if s.Status != "creating" || (s.CreateState != "not_sent" && s.CreateState != "rejected") {
			t.Fatalf("hủy ngay khi hãng có thể có kiện: %+v", s)
		}
	case cancel.ActStaffCancel:
		if !r.CarrierChecked || r.Intent != cancel.IntentReplace || s.Status != "creating" {
			t.Fatalf("hủy sau khi tra mà không ai tra: %+v %+v", s, r)
		}
	case cancel.ActRequestCarrier:
		if s.Status != "awaiting_pickup" || !s.CarrierAPI {
			t.Fatalf("gọi hãng hủy khi %+v", s)
		}
	case cancel.ActRecordIntent, cancel.ActConflict, cancel.ActUpgradeIntent:
		if r.Intent != cancel.IntentOrderCancel {
			t.Fatalf("đổi hãng ra %v", a)
		}
	case cancel.ActCreateInProgress, cancel.ActNotCancellable:
		if r.Intent != cancel.IntentReplace {
			t.Fatalf("hủy đơn bị từ chối %v ở %+v", a, s)
		}
	case cancel.ActNone, cancel.ActRequestManual, cancel.ActInvalid:
	}
}

// applied là vận đơn sau khi làm việc a của yêu cầu r; ok = false khi a không đổi vận đơn.
func applied(s cancel.Shipment, r cancel.Request, a cancel.Action) (cancel.Shipment, bool) {
	after := s
	switch a {
	case cancel.ActCancelNow, cancel.ActStaffCancel:
		after.Status = "cancelled"
	case cancel.ActRequestCarrier, cancel.ActRequestManual:
		after.Status, after.Intent = "cancel_requested", r.Intent
	case cancel.ActRecordIntent, cancel.ActUpgradeIntent:
		after.Intent = r.Intent
	case cancel.ActNone, cancel.ActCreateInProgress, cancel.ActNotCancellable, cancel.ActConflict, cancel.ActInvalid:
		return s, false
	}
	return after, true
}
