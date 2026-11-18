package logging

import (
	"log/slog"
	"regexp"
	"strings"
	"unicode"
)

// Mẫu của bộ che văn bản tự do (ScrubText). Biên dịch một lần ở cấp gói; RE2 chạy thời gian tuyến tính theo độ dài
// chuỗi nên chuỗi dài không làm treo.
var (
	// uuidPattern: mã UUID giữ nguyên (mã việc, mã yêu cầu trong lỗi giúp tra cứu); phần còn lại được che.
	uuidPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	// addressKeyPattern: như keyValuePattern cho khóa chứa "address", nhưng giá trị đi qua dấu phẩy (địa chỉ Việt Nam
	// gần như luôn có dấu phẩy: "address=12 Nguyễn Trãi, Thanh Xuân, Hà Nội").
	addressKeyPattern = regexp.MustCompile(`(?i)([a-z_]*address[a-z0-9_]*(?:\s*=\s*"?|"\s*:\s*"?|\)=\())` +
		`(?:[^;&()\n":]|:\S)*`)
	// keyValuePattern: "khóa=giá trị", JSON "khóa":"giá trị" và chi tiết PostgreSQL "(khóa)=(giá trị)" với khóa
	// chứa một từ của sensitiveKey; giữ khóa, che giá trị (tới dấu phẩy, chấm phẩy, "&", ngoặc, nháy, ": ").
	keyValuePattern = regexp.MustCompile(`(?i)([a-z_]*(?:phone|address|token|secret|password|authorization|cookie|id_number|bank_account)[a-z0-9_]*(?:\s*=\s*"?|"\s*:\s*"?|\)=\())` +
		`(?:[^,;&()\n":]|:\S)*`)
	// labelPattern: nhãn tiếng Việt đứng trước dữ liệu cá nhân ("sđt 09…", "địa chỉ: 12 …"); che tới hết đoạn
	// (dấu ";", ngoặc, xuống dòng, dấu nháy, hay ": " của lỗi bọc).
	labelPattern  = regexp.MustCompile(`(?i)(sđt|số điện thoại|điện thoại|địa chỉ|mật khẩu)(\s*:?\s*)(?:[^;()\n":]|:\S)+`)
	bearerPattern = regexp.MustCompile(`(?i)\b(bearer)\s+[A-Za-z0-9._~+/=-]+`)
	jwtPattern    = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)?`)
	emailPattern  = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	// addressPattern: địa chỉ Việt Nam: số nhà rồi từ hai từ viết hoa trở lên ("12 Nguyễn Trãi"), hoặc từ chỉ đơn vị
	// (ngõ, phố, phường, quận…) rồi một từ viết hoa hay số; kéo theo các phần sau dấu phẩy viết hoa hay số
	// (", Thanh Xuân, Hà Nội", ", quận 1"). Lỗi của Go viết thường theo quy ước nên cụm viết hoa sau số hiếm khi là
	// nội dung lỗi thật.
	addressPattern = regexp.MustCompile(
		`(?:\d+[A-Za-z]?(?:/\d+[A-Za-z]?)*\s+\p{Lu}\p{Ll}*(?:\s+\p{Lu}\p{Ll}*)+` +
			`|(?:\d+[A-Za-z]?(?:/\d+[A-Za-z]?)*\s+)?` + addressMarker + `\s*[\p{Lu}\d][\p{L}\d./]*)` +
			`(?:[\s,]+(?:` + addressMarker + `\s*)?[\p{Lu}\d][\p{L}\d./]*)*`)
	// longTokenPattern: chuỗi liền từ 20 ký tự chữ, số, "_", "-" có cả chữ lẫn số (khóa API, token ngẫu nhiên).
	longTokenPattern = regexp.MustCompile(`[A-Za-z0-9_-]{20,}`)
	// phonePattern: từ 8 chữ số trở lên, liền hoặc ngăn bằng một dấu cách, chấm, gạch ("0912 345 678",
	// "+84 912.345.678"). Che cả ngày dạng 2026-11-18, số tiền 10.000.000: che thừa an toàn hơn che thiếu.
	phonePattern = regexp.MustCompile(`\+?\d(?:[ .\-]?\d){7,}`)
)

// addressMarker: từ chỉ đơn vị hành chính hay đường phố của địa chỉ Việt Nam (không phân biệt hoa thường).
const addressMarker = `(?i:số nhà|ngõ|ngách|hẻm|kiệt|đường|phố|phường|quận|huyện|thị xã|xã|thôn|ấp|tỉnh|thành phố|tp\.)`

// ScrubText che dữ liệu cá nhân và bí mật trong văn bản tự do (nội dung lỗi): giá trị sau khóa nhạy cảm (cùng danh
// sách với SensitiveKey) và sau nhãn "sđt", "địa chỉ"; token Bearer, JWT, chuỗi token dài; email; địa chỉ Việt Nam;
// SĐT (từ 8 chữ số, liền hoặc có dấu ngăn). Phần bị che thành "[Redacted]"; mã UUID giữ nguyên. Đây là bộ che văn
// bản duy nhất của CRM: log, error_events, last_error đều đi qua nó. Tên người trong văn bản tự do không được nhận ra,
// nên mã vẫn không đưa dữ liệu khách vào chuỗi lỗi; dữ liệu cá nhân dùng kiểu tự che (Phone, Address, Secret).
func ScrubText(s string) string {
	s = strings.ToValidUTF8(s, "?")
	var b strings.Builder
	last := 0
	for _, loc := range uuidPattern.FindAllStringIndex(s, -1) {
		b.WriteString(scrubSegment(s[last:loc[0]]))
		b.WriteString(s[loc[0]:loc[1]])
		last = loc[1]
	}
	b.WriteString(scrubSegment(s[last:]))
	return b.String()
}

func scrubSegment(s string) string {
	if s == "" {
		return s
	}
	s = addressKeyPattern.ReplaceAllString(s, "${1}"+redacted)
	s = keyValuePattern.ReplaceAllString(s, "${1}"+redacted)
	s = labelPattern.ReplaceAllString(s, "${1}${2}"+redacted)
	s = bearerPattern.ReplaceAllString(s, "${1} "+redacted)
	s = jwtPattern.ReplaceAllString(s, redacted)
	s = emailPattern.ReplaceAllString(s, redacted)
	s = addressPattern.ReplaceAllString(s, redacted)
	s = longTokenPattern.ReplaceAllStringFunc(s, func(m string) string {
		if strings.ContainsFunc(m, unicode.IsDigit) && strings.ContainsFunc(m, unicode.IsLetter) {
			return redacted
		}
		return m
	})
	return phonePattern.ReplaceAllString(s, redacted)
}

// ErrorText là nội dung của err đã qua ScrubText; "" khi err nil. Mọi chỗ cần nội dung lỗi để ghi ra ngoài tiến
// trình (log, bảng lỗi, last_error) dùng hàm này thay cho err.Error().
func ErrorText(err error) string {
	if err == nil {
		return ""
	}
	return ScrubText(err.Error())
}

// Err là thuộc tính log "error" mang nội dung lỗi đã lọc; mọi lời gọi slog ghi lỗi dùng nó (test kiến trúc cấm
// err.Error() thô trong lời gọi slog).
func Err(err error) slog.Attr { return slog.String("error", ErrorText(err)) }
