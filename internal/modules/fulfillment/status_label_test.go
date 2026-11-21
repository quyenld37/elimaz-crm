package fulfillment_test

import (
	"testing"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// StatusLabel là nhãn hiển thị của getShipment (Task M3-28 dùng cho cột vận đơn của danh sách đơn): nhãn chung theo
// trạng thái, vận đơn tự giao chờ giao là "Tự giao", mã lạ giữ nguyên.
func TestStatusLabel(t *testing.T) {
	t.Parallel()
	cases := []struct{ key, status, want string }{
		{"fake_a", "creating", "Đang gửi hãng"},
		{"fake_a", "awaiting_pickup", "Chờ thu gom"},
		{"fake_b", "shipping", "Đang chuyển"},
		{"ghn", "delivery_failed", "Thất bại"},
		{"ghn", "returning", "Đang chuyển hoàn"},
		{fulfillment.SelfCarrierKey, "awaiting_pickup", "Tự giao"},
		{fulfillment.SelfCarrierKey, "shipping", "Đang chuyển"},
		{"ghn", "la", "la"},
	}
	for _, c := range cases {
		if got := fulfillment.StatusLabel(c.key, c.status, ""); got != c.want {
			t.Errorf("StatusLabel(%q, %q) = %q, muốn %q", c.key, c.status, got, c.want)
		}
	}
}
