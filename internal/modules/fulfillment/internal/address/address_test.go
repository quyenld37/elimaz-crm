package address_test

import (
	"testing"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/address"
)

// Vận đơn đổi được địa chỉ: chỉ khi hàng còn ở hãng mà chưa giao xong (awaiting_pickup, shipping, delivery_failed).
func TestChangeable(t *testing.T) {
	for _, c := range []struct {
		status string
		want   bool
	}{
		{"awaiting_pickup", true},
		{"shipping", true},
		{"delivery_failed", true},
		{"creating", false},
		{"cancel_requested", false},
		{"cancelled", false},
		{"delivered", false},
		{"returning", false},
		{"returned", false},
		{"", false},
	} {
		if got := address.Changeable(c.status); got != c.want {
			t.Errorf("Changeable(%q) = %v, muốn %v", c.status, got, c.want)
		}
	}
}

// Nhân viên xác nhận tay được: yêu cầu manual còn chờ, yêu cầu api đã gửi (sent) hay không rõ (unknown); yêu cầu api
// còn chờ lệnh, đã xong (confirmed, rejected) → không.
func TestResolvable(t *testing.T) {
	for _, c := range []struct {
		mode, status string
		want         bool
	}{
		{"manual", "pending", true},
		{"api", "sent", true},
		{"api", "unknown", true},
		{"api", "pending", false},
		{"manual", "confirmed", false},
		{"manual", "rejected", false},
		{"api", "confirmed", false},
		{"api", "rejected", false},
	} {
		if got := address.Resolvable(c.mode, c.status); got != c.want {
			t.Errorf("Resolvable(%q, %q) = %v, muốn %v", c.mode, c.status, got, c.want)
		}
	}
}

// Kết quả nhân viên chọn: chỉ confirmed, rejected.
func TestResolution(t *testing.T) {
	for _, c := range []struct {
		in string
		ok bool
	}{{"confirmed", true}, {"rejected", true}, {"sent", false}, {"unknown", false}, {"pending", false}, {"", false}} {
		if _, ok := address.Resolution(c.in); ok != c.ok {
			t.Errorf("Resolution(%q) ok = %v, muốn %v", c.in, ok, c.ok)
		}
	}
	if got, _ := address.Resolution("confirmed"); got != address.StatusConfirmed {
		t.Errorf("Resolution(confirmed) = %q", got)
	}
}

// Kết quả hãng (UpdateResult) → trạng thái yêu cầu: done → confirmed, rejected → rejected, pending → sent; lạ → false.
func TestFromCarrier(t *testing.T) {
	for _, c := range []struct {
		in, want string
		ok       bool
	}{
		{"done", address.StatusConfirmed, true},
		{"rejected", address.StatusRejected, true},
		{"pending", address.StatusSent, true},
		{"", "", false},
		{"ok", "", false},
	} {
		got, ok := address.FromCarrier(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("FromCarrier(%q) = %q %v, muốn %q %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// Lệnh kết thúc không có kết quả hãng: in_doubt → unknown (nhân viên xác nhận); dead, expired (chắc hãng chưa nhận:
// từ chối, chưa gửi được) → rejected; trạng thái khác → không đổi.
func TestTerminal(t *testing.T) {
	for _, c := range []struct {
		in, want string
	}{
		{"in_doubt", address.StatusUnknown},
		{"dead", address.StatusRejected},
		{"expired", address.StatusRejected},
		{"done", ""},
		{"pending", ""},
	} {
		if got := address.Terminal(c.in); got != c.want {
			t.Errorf("Terminal(%q) = %q, muốn %q", c.in, got, c.want)
		}
	}
}

// Yêu cầu còn mở (chặn yêu cầu thứ hai, khớp index address_change_requests_open): pending, sent, unknown.
func TestOpen(t *testing.T) {
	for _, c := range []struct {
		in   string
		want bool
	}{
		{"pending", true}, {"sent", true}, {"unknown", true}, {"confirmed", false}, {"rejected", false},
	} {
		if got := address.Open(c.in); got != c.want {
			t.Errorf("Open(%q) = %v, muốn %v", c.in, got, c.want)
		}
	}
}

func TestDedupeKey(t *testing.T) {
	id := uuid.MustParse("01930000-0000-7000-8000-000000000001")
	if got := address.DedupeKey(id); got != "address:"+id.String() {
		t.Fatalf("DedupeKey = %q", got)
	}
}
