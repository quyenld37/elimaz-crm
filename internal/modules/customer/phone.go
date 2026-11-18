package customer

import (
	"strings"

	"elimaz.com.vn/crm/internal/platform/logging"
)

// NormalizePhone đưa SĐT Việt Nam về dạng chuẩn +84… (khóa của danh tính phone, 05 mục 1): bỏ khoảng trắng, chấm, gạch;
// nhận "0" + 9 hoặc 10 chữ số, "84…" hay "+84…" cùng độ dài phần sau (phần sau không bắt đầu bằng 0). Còn lại
// ErrInvalidPhone (lỗi không mang giá trị).
func NormalizePhone(raw string) (logging.Phone, error) {
	s := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '.', '-':
			return -1
		}
		return r
	}, raw)
	var rest string
	switch {
	case strings.HasPrefix(s, "+84"):
		rest = s[3:]
	case strings.HasPrefix(s, "84"):
		rest = s[2:]
	case strings.HasPrefix(s, "0"):
		rest = s[1:]
	default:
		return "", ErrInvalidPhone
	}
	if (len(rest) != 9 && len(rest) != 10) || rest[0] == '0' || strings.ContainsFunc(rest, notDigit) {
		return "", ErrInvalidPhone
	}
	return logging.Phone("+84" + rest), nil
}

// ReverseDigits trả các chữ số của p theo thứ tự ngược (bỏ "+"), cho cột value_reversed: tìm theo đuôi số bằng câu
// khoảng trên index thay cho LIKE '%…'.
func ReverseDigits(p logging.Phone) string {
	s := string(p)
	b := make([]byte, 0, len(s))
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] >= '0' && s[i] <= '9' {
			b = append(b, s[i])
		}
	}
	return string(b)
}

func notDigit(r rune) bool { return r < '0' || r > '9' }
