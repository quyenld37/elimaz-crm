package create_test

import (
	"testing"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/create"
)

func TestDedupeKey(t *testing.T) {
	id := uuid.MustParse("0190e6a0-0000-7000-8000-000000000001")
	if got := create.DedupeKey(id, 1); got != "create:"+id.String()+":1" {
		t.Fatalf("lần 1 = %s", got)
	}
	if create.DedupeKey(id, 2) == create.DedupeKey(id, 1) {
		t.Fatal("hai lần gửi cùng khóa chống trùng")
	}
}

func TestExecuteGate(t *testing.T) {
	for _, c := range []struct {
		name string
		s    create.Shipment
		want create.Gate
	}{
		{"chờ gửi", create.Shipment{Creating: true, State: create.StatePending, Current: true}, create.GateSend},
		{"không rõ: tra trước", create.Shipment{Creating: true, State: create.StateUnknown, Current: true}, create.GateLookup},
		{"chưa gửi: chờ người", create.Shipment{Creating: true, State: create.StateNotSent, Current: true}, create.GateStale},
		{"đã từ chối", create.Shipment{Creating: true, State: create.StateRejected, Current: true}, create.GateStale},
		{"đã qua creating", create.Shipment{State: create.StatePending, Current: true}, create.GateStale},
		{"lệnh của lần gửi cũ", create.Shipment{Creating: true, State: create.StatePending}, create.GateStale},
	} {
		if got := create.ExecuteGate(c.s); got != c.want {
			t.Errorf("%s: %v, muốn %v", c.name, got, c.want)
		}
	}
}

func TestCanResendResolve(t *testing.T) {
	if !create.CanResend(true, create.StateNotSent) || create.CanResend(true, create.StateUnknown) ||
		create.CanResend(false, create.StateNotSent) || create.CanResend(true, create.StatePending) {
		t.Fatal("CanResend sai")
	}
	if !create.CanResolve(true, create.StateUnknown) || create.CanResolve(true, create.StateNotSent) ||
		create.CanResolve(false, create.StateUnknown) {
		t.Fatal("CanResolve sai")
	}
}

func TestValidResolution(t *testing.T) {
	for _, c := range []struct {
		result, code, reason string
		want                 bool
	}{
		{create.ResultCreated, "FK7777", "đã thấy trên trang hãng", true},
		{create.ResultCreated, "", "x", false},
		{create.ResultNotCreated, "", "không có trên trang hãng", true},
		{create.ResultNotCreated, "FK1", "x", false},
		{create.ResultCreated, "FK1", "", false},
		{"maybe", "", "x", false},
	} {
		if got := create.ValidResolution(c.result, c.code, c.reason); got != c.want {
			t.Errorf("%+v: %v", c, got)
		}
	}
}

// ManualTrackingCode (Task M3-15): tài khoản tay của hãng thật bắt buộc mã nhân viên dán (cắt khoảng trắng hai đầu);
// self không nhận mã nhập, CRM dùng mã vận đơn nội bộ.
func TestManualTrackingCode(t *testing.T) {
	for _, c := range []struct {
		name        string
		self        bool
		code, given string
		want        string
		ok          bool
	}{
		{"hãng thật có mã", false, "SH1", "BEST123456", "BEST123456", true},
		{"hãng thật cắt khoảng trắng", false, "SH1", "  BEST123456 \t", "BEST123456", true},
		{"hãng thật thiếu mã", false, "SH1", "", "", false},
		{"hãng thật mã toàn khoảng trắng", false, "SH1", "   ", "", false},
		{"self dùng mã nội bộ", true, "SH1", "", "SH1", true},
		{"self không nhận mã nhập", true, "SH1", "X9", "", false},
	} {
		got, ok := create.ManualTrackingCode(c.self, c.code, c.given)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: (%q, %v), muốn (%q, %v)", c.name, got, ok, c.want, c.ok)
		}
	}
}
