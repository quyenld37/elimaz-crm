package manual_test

import (
	"testing"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/manual"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/shipstate"
)

// Check: lý do trống → ReasonRequired trước mọi xét khác; returned, trạng thái không nằm trong năm trạng thái ghi tay →
// TargetNotAllowed; tài khoản tay (mọi hãng, cả self) nhận năm trạng thái; vận đơn api chỉ khi có việc hủy tay mở, và
// chỉ cancelled, shipping; api không việc → NotManual.
func TestCheck(t *testing.T) {
	tay := manual.Shipment{Mode: manual.ModeManual, CarrierKey: "best"}
	self := manual.Shipment{Mode: manual.ModeManual, CarrierKey: manual.SelfKey}
	api := manual.Shipment{Mode: "api", CarrierKey: "best"}
	apiTask := manual.Shipment{Mode: "api", CarrierKey: "best", StaffCancelOpen: true}
	cases := []struct {
		name   string
		s      manual.Shipment
		to     shipstate.Status
		reason string
		want   manual.Verdict
	}{
		{"tay shipping", tay, shipstate.Shipping, "khách gọi", manual.OK},
		{"tay delivery_failed", tay, shipstate.DeliveryFailed, "x", manual.OK},
		{"tay delivered", tay, shipstate.Delivered, "x", manual.OK},
		{"tay returning", tay, shipstate.Returning, "x", manual.OK},
		{"tay cancelled", tay, shipstate.Cancelled, "x", manual.OK},
		{"self delivered", self, shipstate.Delivered, "x", manual.OK},
		{"tay returned", tay, shipstate.Returned, "x", manual.TargetNotAllowed},
		{"tay awaiting_pickup", tay, shipstate.AwaitingPickup, "x", manual.TargetNotAllowed},
		{"tay creating", tay, shipstate.Creating, "x", manual.TargetNotAllowed},
		{"tay cancel_requested", tay, shipstate.CancelRequested, "x", manual.TargetNotAllowed},
		{"tay lạ", tay, shipstate.Status("lost"), "x", manual.TargetNotAllowed},
		{"thiếu lý do", tay, shipstate.Shipping, "", manual.ReasonRequired},
		{"lý do chỉ khoảng trắng", tay, shipstate.Shipping, " \t\n", manual.ReasonRequired},
		{"thiếu lý do trước returned", tay, shipstate.Returned, "", manual.ReasonRequired},
		{"api không việc", api, shipstate.Shipping, "x", manual.NotManual},
		{"api không việc cancelled", api, shipstate.Cancelled, "x", manual.NotManual},
		{"api việc hủy cancelled", apiTask, shipstate.Cancelled, "hãng đã hủy", manual.OK},
		{"api việc hủy shipping", apiTask, shipstate.Shipping, "hãng không cho hủy", manual.OK},
		{"api việc hủy delivered", apiTask, shipstate.Delivered, "x", manual.TargetNotAllowed},
		{"api việc hủy returning", apiTask, shipstate.Returning, "x", manual.TargetNotAllowed},
		{"api việc hủy returned", apiTask, shipstate.Returned, "x", manual.TargetNotAllowed},
	}
	for _, c := range cases {
		if got := manual.Check(c.s, c.to, c.reason); got != c.want {
			t.Errorf("%s: Check = %v, muốn %v", c.name, got, c.want)
		}
	}
}

// Canonical: mỗi trạng thái ghi tay đổi ra một sự kiện chuẩn chỉ tới đúng trạng thái đó (shipstate.Target); trạng
// thái khác → false.
func TestCanonical(t *testing.T) {
	ok := map[shipstate.Status]bool{
		shipstate.Shipping: true, shipstate.DeliveryFailed: true, shipstate.Delivered: true, shipstate.Returning: true,
		shipstate.Cancelled: true,
	}
	for _, st := range shipstate.Statuses() {
		c, got := manual.Canonical(st)
		if got != ok[st] {
			t.Errorf("Canonical(%s) ok = %v, muốn %v", st, got, ok[st])
			continue
		}
		if !got {
			continue
		}
		if target, _ := shipstate.Target(c); target != st {
			t.Errorf("Canonical(%s) = %s chỉ tới %s", st, c, target)
		}
	}
}

// Quy luật: Check trả OK thì Canonical có sự kiện; không bao giờ OK cho returned; api không việc không bao giờ OK.
func TestCheck_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := manual.Shipment{
			Mode:            rapid.SampledFrom([]string{manual.ModeManual, "api"}).Draw(t, "mode"),
			CarrierKey:      rapid.SampledFrom([]string{manual.SelfKey, "best", "vtp"}).Draw(t, "key"),
			StaffCancelOpen: rapid.Bool().Draw(t, "task"),
		}
		to := rapid.SampledFrom(shipstate.Statuses()).Draw(t, "to")
		reason := rapid.SampledFrom([]string{"", " ", "lý do"}).Draw(t, "reason")
		v := manual.Check(s, to, reason)
		if v != manual.OK {
			return
		}
		if _, ok := manual.Canonical(to); !ok {
			t.Fatalf("OK mà không có sự kiện cho %s", to)
		}
		if to == shipstate.Returned || (s.Mode != manual.ModeManual && !s.StaffCancelOpen) {
			t.Fatalf("OK sai: %+v %s", s, to)
		}
	})
}

// Label: vận đơn tự giao chờ giao có nhãn "Tự giao"; trạng thái khác, hãng khác giữ nhãn chung.
func TestLabel(t *testing.T) {
	cases := []struct {
		key  string
		st   shipstate.Status
		want string
	}{
		{manual.SelfKey, shipstate.AwaitingPickup, "Tự giao"},
		{manual.SelfKey, shipstate.Delivered, "Thành công"},
		{"best", shipstate.AwaitingPickup, "Chờ thu gom"},
	}
	for _, c := range cases {
		if got := manual.Label(c.key, c.st, shipstate.Display(c.st, "", "")); got != c.want {
			t.Errorf("Label(%s, %s) = %q, muốn %q", c.key, c.st, got, c.want)
		}
	}
}
