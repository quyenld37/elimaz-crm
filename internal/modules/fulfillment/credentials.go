package fulfillment

import (
	"crypto/sha256"
	"fmt"
	"log/slog"
	"strings"
)

// redacted là chuỗi thay cho bí mật khi in hay ghi log.
const redacted = "[Redacted]"

// Credentials là bộ khóa của một tài khoản hãng (token, mã khách hàng, mật khẩu…), giải mã từ cột mã hóa bằng
// fieldcrypt (Task 9 của M3). Ghi log, %v, %+v, %#v, %s đều ra "[Redacted]"; giá trị chỉ đọc bằng chỉ mục map.
//
// Khi nào không dùng: không cho bí mật một giá trị của cấu hình (dùng logging.Secret).
type Credentials map[string]string

// LogValue che toàn bộ bộ khóa.
func (Credentials) LogValue() slog.Value { return slog.StringValue(redacted) }

// String che toàn bộ bộ khóa (%v, %+v, %s).
func (Credentials) String() string { return redacted }

// GoString che toàn bộ bộ khóa (%#v).
func (Credentials) GoString() string { return redacted }

// InputHash là sha256 của dữ liệu báo phí đã chuẩn hóa (địa chỉ, dịch vụ, tùy chọn bỏ khoảng trắng thừa, chữ thường);
// Task 12, 13 dùng để biết một báo phí còn khớp đơn hay không. Tên, SĐT người nhận và ghi chú không ảnh hưởng phí nên
// không vào băm. Chuỗi ghi bằng %q nên ranh giới giữa các trường không mơ hồ.
func InputHash(to Address, p Parcel, cod int64, service string, o Options) []byte {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "v1|%q|%q|%q|%d|%d|%d|%d|%d|%d|%d|%q|%q|%q|%d|%t",
		norm(to.Line), norm(to.ProvinceCode), norm(to.WardCode),
		p.WeightGrams, p.LengthCm, p.WidthCm, p.HeightCm, p.DeclaredValue, p.ItemCount,
		cod, norm(service), norm(o.Inspection), norm(o.Payer), o.FeeOnRefuse, o.Pickup)
	return h.Sum(nil)
}

// norm bỏ khoảng trắng thừa, đổi chữ thường.
func norm(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
