package textnorm

import "strings"

// Slug là phần tên của một liên kết website, đúng slugify(s, max) của website (src/features/text.ts): bỏ dấu, chữ
// thường, mỗi chuỗi ký tự ngoài [a-z0-9] thành một "-", bỏ "-" ở hai đầu, rồi cắt max byte (cắt sau khi bỏ "-", nên có
// thể kết thúc bằng "-" như website). Bỏ dấu bằng Fold (bảng tiếng Việt): chữ Latin có dấu ngoài tiếng Việt (ñ, ç…)
// thành "-" thay vì chữ gốc như NFD của website; chỉ làm liên kết khác tên, mã ở cuối liên kết vẫn tìm đúng đối tượng.
// Dùng cho liên kết chiến dịch (/uu-dai/…) và bộ phối (/bo-phoi/…).
func Slug(s string, maxLen int) string {
	var b strings.Builder
	dash := false
	for _, r := range Fold(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimPrefix(b.String(), "-")
	out = strings.TrimSuffix(out, "-")
	if len(out) > maxLen {
		out = out[:maxLen]
	}
	return out
}
