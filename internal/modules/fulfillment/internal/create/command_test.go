package create_test

import (
	"testing"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/create"
)

func TestTerminal(t *testing.T) {
	cur := func(state string) create.Shipment {
		return create.Shipment{Creating: true, State: state, Current: true}
	}
	for _, c := range []struct {
		name        string
		s           create.Shipment
		job, intent string
		want        create.Action
	}{
		{"quá giờ sau khi gửi", cur(create.StatePending), create.JobInDoubt, "", create.ActUnknown},
		{"từ chối chưa ghi xong rồi in_doubt", cur(create.StateRejected), create.JobInDoubt, "", create.ActUnknown},
		{"đã không rõ", cur(create.StateUnknown), create.JobInDoubt, "", create.ActNone},
		{"hãng từ chối", cur(create.StateRejected), create.JobDead, "", create.ActCancel},
		{"chưa gửi được", cur(create.StatePending), create.JobDead, "", create.ActNotSent},
		{"tra lại xác nhận chưa có", cur(create.StateUnknown), create.JobDead, "", create.ActNotSent},
		{"quá hạn (lệnh M1)", cur(create.StatePending), create.JobExpired, "", create.ActNotSent},
		{"chưa gửi mà đơn đã yêu cầu hủy", cur(create.StatePending), create.JobDead, "order_cancel", create.ActCancel},
		{"không rõ, xác nhận chưa có, đổi hãng", cur(create.StateUnknown), create.JobDead, "replace", create.ActCancel},
		{"in_doubt có ý định hủy vẫn không hủy", cur(create.StatePending), create.JobInDoubt, "order_cancel", create.ActUnknown},
		{"đã not_sent, dead lần hai", cur(create.StateNotSent), create.JobDead, "", create.ActNone},
		{"đã tạo", cur(create.StateCreated), create.JobDead, "", create.ActNone},
		{"vận đơn đã hủy", create.Shipment{State: create.StateRejected, Current: true}, create.JobDead, "", create.ActNone},
		{"lệnh của lần gửi cũ", create.Shipment{Creating: true, State: create.StatePending}, create.JobDead, "", create.ActNone},
		{"done không phải kết thúc hỏng", cur(create.StatePending), "done", "", create.ActNone},
	} {
		if got := create.Terminal(c.s, c.job, c.intent); got != c.want {
			t.Errorf("%s: %v, muốn %v", c.name, got, c.want)
		}
	}
}

func TestCancelKindState(t *testing.T) {
	if create.CancelKind("") != "create_rejected" || create.CancelKind("order_cancel") != "order_cancel" {
		t.Fatal("CancelKind sai")
	}
	if create.CancelState(create.Shipment{State: create.StateRejected}) != create.StateRejected ||
		create.CancelState(create.Shipment{State: create.StateUnknown}) != create.StateNotSent {
		t.Fatal("CancelState sai")
	}
}

// Đơn đã yêu cầu hủy (cancel_requested) mà vận đơn chưa có ý định hủy: chắc chưa tạo → hủy như order_cancel; hãng từ
// chối giữ create_rejected (đơn vẫn được hủy qua DetachShipment); ý định đã ghi giữ nguyên.
func TestEffectiveIntent(t *testing.T) {
	pending, rejected := create.Shipment{State: create.StatePending}, create.Shipment{State: create.StateRejected}
	for _, c := range []struct {
		name      string
		s         create.Shipment
		intent    string
		requested bool
		want      string
	}{
		{"không yêu cầu hủy", pending, "", false, ""},
		{"đơn yêu cầu hủy, chưa tạo", pending, "", true, "order_cancel"},
		{"đơn yêu cầu hủy, hãng từ chối", rejected, "", true, ""},
		{"ý định đã ghi", pending, "replace", true, "replace"},
	} {
		if got := create.EffectiveIntent(c.s, c.intent, c.requested); got != c.want {
			t.Errorf("%s: %q, muốn %q", c.name, got, c.want)
		}
	}
}
