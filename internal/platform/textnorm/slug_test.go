package textnorm_test

import (
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/platform/textnorm"
)

// Slug đúng slugify của website (bảng đầy đủ do website sinh: testdata/contracts/web-campaign-links.json, web-look-links.json,
// kiểm ở promotion và content).
func TestSlug(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"Sơ mi cổ đức", 80, "so-mi-co-duc"},
		{"  Siêu ưu đãi!!!  ", 60, "sieu-uu-dai"},
		{"ĐỒ BỘ MẶC NHÀ", 60, "do-bo-mac-nha"},
		{"!!!", 60, ""},
		{"", 60, ""},
		{"Đi làm ngày thường", 5, "di-la"},
		{strings.Repeat("x", 59) + " yz", 60, strings.Repeat("x", 59) + "-"}, // cắt sau khi bỏ "-": giữ "-" cuối như website
	}
	for _, c := range cases {
		if got := textnorm.Slug(c.in, c.max); got != c.want {
			t.Errorf("Slug(%q, %d) = %q, muốn %q", c.in, c.max, got, c.want)
		}
	}
}
