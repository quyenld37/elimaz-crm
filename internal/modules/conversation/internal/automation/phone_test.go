package automation_test

import (
	"slices"
	"testing"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/automation"
)

// Bảng của kế hoạch (Task 165, Q27) và vài ca ranh giới thêm.
func TestContainsPhone(t *testing.T) {
	t.Parallel()
	cases := []struct {
		text string
		want bool
	}{
		{"chốt size M 0912 345 678", true},
		{"09.1234.5678", true},
		{"+84 912 345 678", true},
		{"84912345678", true},
		{"0123456789", false}, // đầu số 1 không dùng
		{"size 39 cân 50kg", false},
		{"0912345", false},
		{"109123456789", false}, // dính chữ số hai đầu
		{"sdt:0987-654-321 nhé", true},
		{"09123456789", false}, // 11 chữ số
		{"mã 0912345678", true},
		{"", false},
		{"chị ơi còn hàng không", false},
	}
	for _, c := range cases {
		if got := automation.ContainsPhone(c.text); got != c.want {
			t.Errorf("ContainsPhone(%q) = %v, muốn %v", c.text, got, c.want)
		}
	}
}

func TestExtractPhones(t *testing.T) {
	t.Parallel()
	cases := []struct {
		text string
		want []string
	}{
		{"chốt size M 0912 345 678", []string{"0912345678"}},
		{"+84 912 345 678 hoặc 84.987.654.321", []string{"0912345678", "0987654321"}},
		{"09.1234.5678 và lại 0912345678", []string{"0912345678"}},
		{"0123456789", []string{}},
	}
	for _, c := range cases {
		if got := automation.ExtractPhones(c.text); !slices.Equal(got, c.want) {
			t.Errorf("ExtractPhones(%q) = %v, muốn %v", c.text, got, c.want)
		}
	}
}
