package promotion_test

import (
	"regexp"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/promotion"
)

var generatedCode = regexp.MustCompile(`^[A-HJ-NP-Z2-9]{8}$`)

// Mã sinh: đúng 8 ký tự trong bảng chữ bỏ I, O, 0, 1; có tiền tố thì "<tiền tố>-" đứng trước.
func TestNewCode_Alphabet(t *testing.T) {
	if promotion.CodeAlphabet != "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" || len(promotion.CodeAlphabet) != 32 {
		t.Fatalf("bảng chữ: %q", promotion.CodeAlphabet)
	}
	rapid.Check(t, func(rt *rapid.T) {
		prefix := rapid.StringMatching(`[A-Z0-9]{0,8}`).Draw(rt, "prefix")
		code, err := promotion.NewCode(prefix)
		if err != nil {
			rt.Fatal(err)
		}
		rest := code
		if prefix != "" {
			if !strings.HasPrefix(code, prefix+"-") {
				rt.Fatalf("mã %q thiếu tiền tố %q", code, prefix)
			}
			rest = strings.TrimPrefix(code, prefix+"-")
		}
		if !generatedCode.MatchString(rest) {
			rt.Fatalf("mã %q sai bảng chữ hay độ dài", code)
		}
	})
}

// 10.000 mã liền nhau không trùng (32^8 ≈ 1,1 × 10^12 khả năng; trùng là dấu hiệu nguồn ngẫu nhiên hỏng).
func TestNewCode_Distinct(t *testing.T) {
	seen := make(map[string]bool, 10000)
	for range 10000 {
		c, err := promotion.NewCode("")
		if err != nil {
			t.Fatal(err)
		}
		if seen[c] {
			t.Fatalf("mã trùng %s", c)
		}
		seen[c] = true
	}
}

func TestNewCode_BadPrefix(t *testing.T) {
	for _, p := range []string{"abc", "ABCDEFGHI", "A-B", "Ă"} {
		if _, err := promotion.NewCode(p); err == nil {
			t.Errorf("tiền tố %q phải lỗi", p)
		}
	}
}
