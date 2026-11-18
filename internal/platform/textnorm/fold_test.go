package textnorm_test

import (
	"testing"

	"elimaz.com.vn/crm/internal/platform/textnorm"
)

func TestFold(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Đầm Lụa  Xòe", "dam lua xoe"},
		{"Áo Thun Ơ", "ao thun o"},
		{"ĐỒNG HỒ", "dong ho"},
		{"đường  hai   khoảng", "duong hai khoang"},
		{"  Quần Jean Ống Rộng  ", "quan jean ong rong"},
		{"Ưu đãi Ỹ Ỷ Ỵ ỳ ý", "uu dai y y y y y"},
		{"Bộ Sưu Tập Thu Đông", "bo suu tap thu dong"},
		{"Chân váy xếp ly", "chan vay xep ly"},
		{"Áo khoác\tdạ\nnữ", "ao khoac da nu"},
		{"DAM01-ĐỎ-M", "dam01-do-m"},
		{"", ""},
		// Dạng tổ hợp (NFD): "Đầm" viết bằng chữ gốc cộng dấu rời.
		{"Đầm lụa", "dam lua"},
	}
	for _, c := range cases {
		if got := textnorm.Fold(c.in); got != c.want {
			t.Errorf("Fold(%q) = %q, mong %q", c.in, got, c.want)
		}
	}
}
