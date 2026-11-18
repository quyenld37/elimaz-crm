// Package textnorm chuẩn hóa chữ tiếng Việt cho cột tìm kiếm (kế hoạch M1, Task M1-27): bỏ dấu, chữ thường, gộp khoảng
// trắng, để "dam lua" tìm được "Đầm Lụa".
//
// Khi nào không dùng: không dùng để hiển thị hay lưu chữ gốc (chỉ cho cột tìm kiếm như catalog.products.name_search);
// không dùng để so khớp định danh (SKU, email, SĐT có chuẩn hóa riêng).
package textnorm

import (
	"strings"
	"unicode"
)

// Fold trả s đã bỏ dấu tiếng Việt (bảng ánh xạ, nhận cả dạng dựng sẵn NFC lẫn dạng tổ hợp NFD), đ/Đ thành d, chữ
// thường, mọi chuỗi khoảng trắng (kể cả tab, xuống dòng) gộp thành một dấu cách, bỏ khoảng trắng đầu cuối.
// Ví dụ: "Đầm Lụa  Xòe" → "dam lua xoe".
func Fold(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) { // dấu rời của dạng tổ hợp (NFD)
			continue
		}
		b.WriteRune(foldRune(unicode.ToLower(r)))
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// foldRune đổi một chữ thường có dấu tiếng Việt về chữ gốc; chữ khác giữ nguyên.
func foldRune(r rune) rune {
	switch r {
	case 'à', 'á', 'ả', 'ã', 'ạ', 'ă', 'ằ', 'ắ', 'ẳ', 'ẵ', 'ặ', 'â', 'ầ', 'ấ', 'ẩ', 'ẫ', 'ậ':
		return 'a'
	case 'è', 'é', 'ẻ', 'ẽ', 'ẹ', 'ê', 'ề', 'ế', 'ể', 'ễ', 'ệ':
		return 'e'
	case 'ì', 'í', 'ỉ', 'ĩ', 'ị':
		return 'i'
	case 'ò', 'ó', 'ỏ', 'õ', 'ọ', 'ô', 'ồ', 'ố', 'ổ', 'ỗ', 'ộ', 'ơ', 'ờ', 'ớ', 'ở', 'ỡ', 'ợ':
		return 'o'
	case 'ù', 'ú', 'ủ', 'ũ', 'ụ', 'ư', 'ừ', 'ứ', 'ử', 'ữ', 'ự':
		return 'u'
	case 'ỳ', 'ý', 'ỷ', 'ỹ', 'ỵ':
		return 'y'
	case 'đ':
		return 'd'
	}
	return r
}
