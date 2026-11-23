package customer_test

import (
	"testing"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/textnorm"
)

func TestNormalizeName(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"Nguyễn Thị Hồng Nhung", "nguyen thi hong nhung"},
		{"ĐẶNG  Văn   Ơn", "dang van on"},
		{"", ""},
	}
	for _, c := range cases {
		if got := customer.NormalizeName(c.in); got != c.want {
			t.Errorf("NormalizeName(%q) = %q, mong %q", c.in, got, c.want)
		}
		if got := customer.NormalizeName(c.in); got != textnorm.Fold(c.in) {
			t.Errorf("NormalizeName(%q) phải bằng textnorm.Fold", c.in)
		}
	}
}
