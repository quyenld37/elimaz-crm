package inbound

import "testing"

// Task 265 (M7-20): mã vận đơn của CRM đúng mẫu NewShipmentCode ("E" + yyMMdd + 6 ký tự base32 Crockford viết hoa);
// mã tham chiếu của đơn cũ (Nhanh), mã hãng, mã sai độ dài hay có chữ ngoài bảng Crockford không phải mã CRM.
func TestIsCRMShipmentCode(t *testing.T) {
	t.Parallel()
	for code, want := range map[string]bool{
		"E261128ABCDEF":     true,
		"E2611280Z9XYV":     true,
		"":                  false,
		"TUANPAD3024822076": false,
		"1587818000200":     false,
		"E261118ABC200":     true,
		"E26112ABCDEF":      false,
		"E261128ABCDEFG":    false,
		"E261128ABCDEI":     false, // I không có trong bảng Crockford
		"E261128abcdef":     false,
		"e261128ABCDEF":     false,
		"X261128ABCDEF":     false,
	} {
		if got := isCRMShipmentCode(code); got != want {
			t.Errorf("isCRMShipmentCode(%q) = %v, muốn %v", code, got, want)
		}
	}
}
