package xlsx

import (
	"strings"
)

// MaxCellLength là số ký tự tối đa của một ô Excel.
const MaxCellLength = 32_767

// formulaLead là các ký tự mở đầu khiến Excel (và LibreOffice, Google Sheets) hiểu ô là công thức.
const formulaLead = "=+-@\t\r\n"

// Sanitize làm sạch một chuỗi trước khi ghi vào ô: bỏ byte UTF-8 hỏng (gồm nửa cặp UTF-16 lẻ) và ký tự không hợp lệ
// trong XML 1.0 (dưới 0x20 trừ tab, LF, CR; U+FFFE, U+FFFF); chuỗi mở đầu bằng =, +, -, @, tab, CR, LF được thêm "'"
// phía trước để không thành công thức; cắt còn tối đa MaxCellLength ký tự (tính cả "'"), không cắt giữa một ký tự.
// Sanitize(Sanitize(s)) == Sanitize(s).
func Sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		if !xmlChar(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, ""))
	if s != "" && strings.IndexByte(formulaLead, s[0]) >= 0 {
		s = "'" + s
	}
	n := 0
	for i := range s {
		if n == MaxCellLength {
			return s[:i]
		}
		n++
	}
	return s
}

// xmlChar: ký tự được phép trong XML 1.0 (Char, mục 2.2).
func xmlChar(r rune) bool {
	return r == '\t' || r == '\n' || r == '\r' ||
		(r >= 0x20 && r <= 0xD7FF) || (r >= 0xE000 && r <= 0xFFFD) || (r >= 0x10000 && r <= 0x10FFFF)
}

// unsanitize bỏ "'" mà Sanitize đã thêm trước ký tự công thức, để tệp CRM xuất rồi nhập lại ra đúng chữ gốc.
func unsanitize(s string) string {
	if len(s) >= 2 && s[0] == '\'' && strings.IndexByte(formulaLead, s[1]) >= 0 {
		return s[1:]
	}
	return s
}
