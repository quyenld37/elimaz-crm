package pricing_test

import (
	"testing"

	"elimaz.com.vn/crm/internal/modules/order/internal/pricing"
)

func TestRefundFor(t *testing.T) {
	cases := []struct {
		net, qty, returnedBefore, returnNow, refundedBefore, want int64
	}{
		{500001, 2, 0, 1, 0, 250000},
		{500001, 2, 1, 1, 250000, 250001},
		{480000, 2, 0, 2, 0, 480000},
		{100, 3, 0, 1, 0, 33},
		{100, 3, 1, 1, 33, 33},
		{100, 3, 2, 1, 66, 34},
		{0, 5, 0, 5, 0, 0},
		{pricing.MaxMoney, 7, 0, 6, 0, 7720456504063706}, // floor((2^53 − 1) × 6 / 7), tích vượt int64
	}
	for _, c := range cases {
		if got := pricing.RefundFor(c.net, c.qty, c.returnedBefore, c.returnNow, c.refundedBefore); got != c.want {
			t.Errorf("RefundFor(%d, %d, %d, %d, %d) = %d, muốn %d",
				c.net, c.qty, c.returnedBefore, c.returnNow, c.refundedBefore, got, c.want)
		}
	}
}

// Đầu vào sai (bên gọi phải kiểm trước): trả 0, không bao giờ âm, không hoàn quá Net.
func TestRefundFor_InvalidReturnsZero(t *testing.T) {
	cases := [][5]int64{
		{100, 0, 0, 0, 0},   // qty 0
		{100, 2, 0, 3, 0},   // trả quá số lượng
		{100, 2, 1, 2, 50},  // trả quá số lượng còn lại
		{100, 2, 0, -1, 0},  // trả âm
		{100, 2, -1, 1, 0},  // đã trả âm
		{-100, 2, 0, 1, 0},  // Net âm
		{100, 2, 1, 1, 101}, // đã hoàn quá Net
		{100, 2, 0, 1, -1},  // đã hoàn âm
		{100, 2, 0, 0, 0},   // trả 0
	}
	for _, c := range cases {
		if got := pricing.RefundFor(c[0], c[1], c[2], c[3], c[4]); got != 0 {
			t.Errorf("RefundFor%v = %d, muốn 0", c, got)
		}
	}
}
