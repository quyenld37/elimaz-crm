package barcode

import "testing"

func TestValidEAN13(t *testing.T) {
	for _, c := range []struct {
		s    string
		want bool
	}{
		{"4006381333931", true},  // mã EAN-13 có thật (số kiểm 1)
		{"2000000000015", true},  // tiền tố 2, số thứ tự 1
		{"4006381333932", false}, // sai số kiểm
		{"400638133393", false},  // 12 chữ số
		{"40063813339311", false},
		{"400638133393a", false},
		{"", false},
	} {
		if got := ValidEAN13(c.s); got != c.want {
			t.Errorf("ValidEAN13(%q) = %v, mong %v", c.s, got, c.want)
		}
	}
}

func TestFormatInternal(t *testing.T) {
	for _, c := range []struct {
		n    int64
		want string
	}{
		{1, "2000000000015"},
		{42, "2000000000428"},
		{99999999999, "2999999999991"},
	} {
		got, err := formatInternal(c.n)
		if err != nil || got != c.want {
			t.Errorf("formatInternal(%d) = %q, %v; mong %q", c.n, got, err, c.want)
		}
		if !ValidEAN13(got) {
			t.Errorf("formatInternal(%d) = %q không phải EAN-13 hợp lệ", c.n, got)
		}
	}
	for _, n := range []int64{0, -1, 100000000000} {
		if _, err := formatInternal(n); err == nil {
			t.Errorf("formatInternal(%d) phải lỗi (ngoài dải 11 chữ số)", n)
		}
	}
}
