package xlsx_test

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/platform/xlsx"
)

func TestSanitizeExamples(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", ""},
		{"Đầm lụa", "Đầm lụa"},
		{`=HYPERLINK("http://x")`, `'=HYPERLINK("http://x")`},
		{"+1", "'+1"},
		{"-5", "'-5"},
		{"@SUM(A1)", "'@SUM(A1)"},
		{"\tA", "'\tA"},
		{"\rA", "'\rA"},
		{"\nA", "'\nA"},
		{"'=x", "'=x"},
		{"a\x00b\x1fc\td\ne\rf", "ab" + "c\td\ne\rf"},
		{"\x01=x", "'=x"},            // bỏ ký tự cấm trước rồi mới xét ký tự đầu
		{"a￾b￿c", "abc"},             // hai ký tự không phải ký tự của XML 1.0
		{"a\xed\xa0\x80b", "ab"},     // nửa cặp UTF-16 (U+D800 mã hóa kiểu CESU) không phải UTF-8 hợp lệ
		{"a\xffb", "ab"},             // byte UTF-8 hỏng
		{"\U0001F600", "\U0001F600"}, // ký tự ngoài mặt phẳng cơ bản giữ nguyên
	} {
		if got := xlsx.Sanitize(c.in); got != c.want {
			t.Errorf("Sanitize(%q) = %q, mong %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeTruncatesOnRuneBoundary(t *testing.T) {
	got := xlsx.Sanitize(strings.Repeat("ộ", 40000))
	if !utf8.ValidString(got) {
		t.Fatal("kết quả không còn là UTF-8 hợp lệ")
	}
	if n := utf8.RuneCountInString(got); n != xlsx.MaxCellLength {
		t.Fatalf("độ dài %d ký tự, mong %d", n, xlsx.MaxCellLength)
	}
	if got != strings.Repeat("ộ", xlsx.MaxCellLength) {
		t.Fatal("cắt sai nội dung")
	}
	// Dấu nháy thêm vào tính trong giới hạn.
	got = xlsx.Sanitize("=" + strings.Repeat("a", 40000))
	if n := utf8.RuneCountInString(got); n != xlsx.MaxCellLength || !strings.HasPrefix(got, "'=") {
		t.Fatalf("độ dài %d, đầu %q", n, got[:2])
	}
}

// forbiddenXML: ký tự XML 1.0 cấm (điều khiển trừ tab, LF, CR; nửa cặp UTF-16; U+FFFE, U+FFFF; ngoài Unicode).
func forbiddenXML(r rune) bool {
	switch {
	case r < 0x20:
		return !strings.ContainsRune("\t\n\r", r)
	case r >= 0xD800 && r < 0xE000, r == 0xFFFE, r == 0xFFFF:
		return true
	}
	return r > unicode.MaxRune
}

func TestSanitizeProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Chuỗi bất kỳ: byte ngẫu nhiên (cả UTF-8 hỏng) hoặc ký tự ngẫu nhiên, có lúc rất dài, có lúc mở bằng ký tự
		// công thức.
		var s string
		switch rapid.IntRange(0, 2).Draw(t, "kind") {
		case 0:
			s = string(rapid.SliceOf(rapid.Byte()).Draw(t, "bytes"))
		case 1:
			s = rapid.String().Draw(t, "str")
		default:
			s = rapid.SampledFrom([]string{"=", "+", "-", "@", "\t", "\r", "\n", "\x00", "￾"}).Draw(t, "head") +
				strings.Repeat(rapid.SampledFrom([]string{"a", "ộ", "\U0001F600"}).Draw(t, "unit"),
					rapid.IntRange(0, 33000).Draw(t, "n"))
		}
		got := xlsx.Sanitize(s)
		if got != "" && strings.ContainsRune("=+-@\t\r\n", []rune(got)[0]) {
			t.Fatalf("mở đầu bằng ký tự công thức: %q", got[:1])
		}
		if !utf8.ValidString(got) {
			t.Fatal("không phải UTF-8 hợp lệ")
		}
		for _, r := range got {
			if forbiddenXML(r) {
				t.Fatalf("ký tự cấm trong XML 1.0: %U", r)
			}
		}
		if utf8.RuneCountInString(got) > xlsx.MaxCellLength {
			t.Fatalf("dài %d ký tự", utf8.RuneCountInString(got))
		}
		if n := len(utf16.Encode([]rune(got))); n > xlsx.MaxCellLength {
			t.Fatalf("dài %d đơn vị UTF-16", n)
		}
		if again := xlsx.Sanitize(got); again != got {
			t.Fatal("Sanitize(Sanitize(s)) khác Sanitize(s)")
		}
	})
}

// TestSanitizeTruncatesUTF16Units: Excel đếm giới hạn ô theo đơn vị UTF-16 (Task M6-13, QĐ-236-2): ký tự ngoài BMP tính
// 2, không cắt giữa ký tự.
func TestSanitizeTruncatesUTF16Units(t *testing.T) {
	got := xlsx.Sanitize(strings.Repeat("\U0001F600", 20000))
	if got != strings.Repeat("\U0001F600", xlsx.MaxCellLength/2) {
		t.Fatalf("cắt còn %d ký tự, mong %d", utf8.RuneCountInString(got), xlsx.MaxCellLength/2)
	}
	got = xlsx.Sanitize("a" + strings.Repeat("\U0001F600", 20000))
	if n := len(utf16.Encode([]rune(got))); n != xlsx.MaxCellLength {
		t.Fatalf("%d đơn vị UTF-16, mong %d", n, xlsx.MaxCellLength)
	}
}
