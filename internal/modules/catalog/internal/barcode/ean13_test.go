package barcode

import (
	"errors"
	"testing"
)

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

// Range.Next đi hết dải theo thứ tự rồi báo hết; dải vượt 11 chữ số → ErrExhausted.
func TestRangeNext(t *testing.T) {
	r := Range{From: 41, To: 42}
	var got []string
	for {
		code, ok, err := r.Next()
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		got = append(got, code)
	}
	if len(got) != 2 || got[0] != "2000000000411" || got[1] != "2000000000428" {
		t.Fatalf("dải 41–42: %v", got)
	}
	if _, ok, err := (&Range{From: 1, To: 0}).Next(); ok || err != nil {
		t.Fatalf("dải rỗng: ok=%v err=%v", ok, err)
	}
	if _, _, err := (&Range{From: maxSerial + 1, To: maxSerial + 1}).Next(); !errors.Is(err, ErrExhausted) {
		t.Fatalf("vượt dải: %v", err)
	}
}
