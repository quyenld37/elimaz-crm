package cancel_test

import (
	"testing"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/cancel"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/shipstate"
)

func TestDecide(t *testing.T) {
	oc := cancel.Request{Intent: cancel.IntentOrderCancel}
	rp := cancel.Request{Intent: cancel.IntentReplace}
	checked := cancel.Request{Intent: cancel.IntentReplace, CarrierChecked: true}
	creating := func(state string) cancel.Shipment { return cancel.Shipment{Status: "creating", CreateState: state} }
	api := cancel.Shipment{Status: "awaiting_pickup", CarrierAPI: true}
	manual := cancel.Shipment{Status: "awaiting_pickup"}
	for _, c := range []struct {
		name string
		s    cancel.Shipment
		r    cancel.Request
		want cancel.Action
	}{
		{"not_sent hủy đơn", creating("not_sent"), oc, cancel.ActCancelNow},
		{"not_sent đổi hãng", creating("not_sent"), rp, cancel.ActCancelNow},
		{"rejected đổi hãng", creating("rejected"), rp, cancel.ActCancelNow},
		{"pending hủy đơn", creating("pending"), oc, cancel.ActRecordIntent},
		{"unknown hủy đơn", creating("unknown"), oc, cancel.ActRecordIntent},
		{"created hủy đơn", creating("created"), oc, cancel.ActRecordIntent},
		{"pending đã ghi ý định", cancel.Shipment{Status: "creating", CreateState: "pending", Intent: "order_cancel"}, oc, cancel.ActNone},
		{"pending đổi hãng", creating("pending"), rp, cancel.ActCreateInProgress},
		{"unknown đổi hãng", creating("unknown"), rp, cancel.ActCreateInProgress},
		{"unknown đã tra hãng", creating("unknown"), checked, cancel.ActStaffCancel},
		{"pending đã tra hãng", creating("pending"), checked, cancel.ActStaffCancel},
		{"created đã tra hãng vẫn chờ", creating("created"), checked, cancel.ActCreateInProgress},
		{"chờ thu gom, hãng có API", api, oc, cancel.ActRequestCarrier},
		{"chờ thu gom, đổi hãng", api, rp, cancel.ActRequestCarrier},
		{"chờ thu gom, nhập tay", manual, oc, cancel.ActRequestManual},
		{"đang hủy để thay, khách hủy đơn", cancel.Shipment{Status: "cancel_requested", Intent: "replace"}, oc, cancel.ActUpgradeIntent},
		{"đang hủy đơn, giao lại", cancel.Shipment{Status: "cancel_requested", Intent: "order_cancel"}, oc, cancel.ActNone},
		{"đang hủy, bấm lần hai", cancel.Shipment{Status: "cancel_requested", Intent: "replace"}, rp, cancel.ActNotCancellable},
		{"đã hủy, giao lại", cancel.Shipment{Status: "cancelled"}, oc, cancel.ActNone},
		{"đã hủy, đổi hãng", cancel.Shipment{Status: "cancelled"}, rp, cancel.ActNotCancellable},
		{"đang giao, đổi hãng", cancel.Shipment{Status: "shipping"}, rp, cancel.ActNotCancellable},
		{"đang giao, hủy đơn", cancel.Shipment{Status: "shipping"}, oc, cancel.ActConflict},
		{"đã giao, hủy đơn", cancel.Shipment{Status: "delivered"}, oc, cancel.ActConflict},
		{"ý định lạ", api, cancel.Request{Intent: "x"}, cancel.ActInvalid},
	} {
		if got := cancel.Decide(c.s, c.r); got != c.want {
			t.Errorf("%s: Decide = %v, muốn %v", c.name, got, c.want)
		}
	}
}

func TestEffective(t *testing.T) {
	for _, c := range []struct {
		intent    string
		requested bool
		want      string
	}{
		{"replace", false, "replace"},
		{"replace", true, "order_cancel"},
		{"order_cancel", false, "order_cancel"},
		{"order_cancel", true, "order_cancel"},
	} {
		if got := cancel.Effective(c.intent, c.requested); got != c.want {
			t.Errorf("Effective(%s, %v) = %s, muốn %s", c.intent, c.requested, got, c.want)
		}
	}
}

func TestOutcome(t *testing.T) {
	for raw, want := range map[string]string{
		"done": cancel.OutcomeCancelled, "rejected_picked_up": cancel.OutcomeRejected, "pending": cancel.OutcomePending,
	} {
		if got, ok := cancel.Outcome(raw); !ok || got != want {
			t.Errorf("Outcome(%s) = %s %v, muốn %s", raw, got, ok, want)
		}
	}
	if _, ok := cancel.Outcome("lạ"); ok {
		t.Error("Outcome(lạ) phải không rõ")
	}
}

func TestLookStatus(t *testing.T) {
	for c, want := range map[shipstate.Canonical]string{
		shipstate.CanonCancelled: "done", shipstate.CanonPickedUp: "done", shipstate.CanonInTransit: "done",
		shipstate.CanonDelivered: "done", shipstate.CanonReturning: "done", shipstate.CanonAwaitingPickup: "not_sent",
		shipstate.CanonUnknown: "unknown", shipstate.CanonLost: "unknown",
	} {
		if got := cancel.LookStatus(c); got != want {
			t.Errorf("LookStatus(%s) = %s, muốn %s", c, got, want)
		}
	}
}

func TestTerminalAnomaly(t *testing.T) {
	for _, c := range []struct {
		status, job, want string
	}{
		{"cancel_requested", "in_doubt", "in_doubt_cancel"},
		{"cancel_requested", "dead", "manual_cancel"},
		{"cancel_requested", "expired", "manual_cancel"},
		{"cancelled", "dead", ""},
		{"shipping", "in_doubt", ""},
		{"cancel_requested", "done", ""},
	} {
		if got := cancel.TerminalAnomaly(c.status, c.job); got != c.want {
			t.Errorf("TerminalAnomaly(%s, %s) = %q, muốn %q", c.status, c.job, got, c.want)
		}
	}
}

func TestDedupeKey(t *testing.T) {
	if got := cancel.DedupeKey([16]byte{1}); got != "cancel:01000000-0000-0000-0000-000000000000" {
		t.Errorf("DedupeKey = %s", got)
	}
}
