package customer_test

import (
	"errors"
	"testing"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/logging"
)

func TestNormalizePhone(t *testing.T) {
	ok := []struct{ in, want string }{
		{"0912 345 678", "+84912345678"},
		{"84912345678", "+84912345678"},
		{"+84 912.345.678", "+84912345678"},
		{"0912-345-678", "+84912345678"},
		{"02838123456", "+842838123456"},
		{"+842838123456", "+842838123456"},
		{"  0912345678  ", "+84912345678"},
	}
	for _, c := range ok {
		got, err := customer.NormalizePhone(c.in)
		if err != nil || string(got) != c.want {
			t.Errorf("NormalizePhone(%q) = %q, %v; mong %q", c.in, got, err, c.want)
		}
	}
	bad := []string{
		"", "0912", "912345678", "0912a45678", "+84", "84", "0", "091234567890", "+8409123456789",
		"+1 202 555 0100", "0912 345 67+8", "(0912)345678", "０９１２３４５６７８",
	}
	for _, in := range bad {
		got, err := customer.NormalizePhone(in)
		if !errors.Is(err, customer.ErrInvalidPhone) || got != "" {
			t.Errorf("NormalizePhone(%q) = %q, %v; mong ErrInvalidPhone", in, got, err)
		}
	}
}

func TestReverseDigits(t *testing.T) {
	cases := []struct{ in, want string }{
		{"+84912345678", "87654321948"},
		{"+842838123456", "654321838248"},
		{"", ""},
	}
	for _, c := range cases {
		if got := customer.ReverseDigits(logging.Phone(c.in)); got != c.want {
			t.Errorf("ReverseDigits(%q) = %q, mong %q", c.in, got, c.want)
		}
	}
}
