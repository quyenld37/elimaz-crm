package automation

import (
	"regexp"
	"slices"
	"strings"
)

// phoneAt là mẫu SĐT di động Việt Nam bắt đầu đúng ở đầu chuỗi (Q27): 0 hoặc (+)84 (sau 84 được một dấu cách, chấm,
// gạch), đầu số 3, 5, 7, 8, 9, rồi đủ 8 chữ số, giữa hai chữ số được một dấu cách, chấm hay gạch. Ranh giới chữ số hai
// đầu kiểm riêng (RE2 không có nhìn trước, nhìn sau).
var phoneAt = regexp.MustCompile(`^(?:\+?84[\s.\-]?|0)[35789](?:[\s.\-]?\d){8}`)

// ContainsPhone cho biết text có ít nhất một SĐT di động Việt Nam (mẫu phoneAt, không dính chữ số trước hay sau).
func ContainsPhone(text string) bool {
	return len(findPhones(text, 1)) > 0
}

// ExtractPhones trả các SĐT trong text ở dạng 0xxxxxxxxx (10 chữ số), theo thứ tự xuất hiện, không trùng.
func ExtractPhones(text string) []string {
	out := []string{}
	for _, m := range findPhones(text, -1) {
		digits := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, m)
		if strings.HasPrefix(digits, "84") {
			digits = "0" + digits[2:]
		}
		if !slices.Contains(out, digits) {
			out = append(out, digits)
		}
	}
	return out
}

// findPhones dò từng vị trí bắt đầu có thể (chữ số 0, 8 hay dấu +) không đứng sau một chữ số, khớp phoneAt rồi đòi ký
// tự sau không phải chữ số; tối đa limit kết quả (âm: không giới hạn). Khớp xong nhảy qua đoạn đã khớp.
func findPhones(text string, limit int) []string {
	var out []string
	for i := 0; i < len(text) && (limit < 0 || len(out) < limit); i++ {
		c := text[i]
		if (c != '0' && c != '8' && c != '+') || (i > 0 && isDigit(text[i-1])) {
			continue
		}
		loc := phoneAt.FindStringIndex(text[i:])
		if loc == nil {
			continue
		}
		end := i + loc[1]
		if end < len(text) && isDigit(text[end]) {
			continue
		}
		out = append(out, text[i:end])
		i = end - 1
	}
	return out
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
