package fulfillment

import (
	"encoding/base32"
	"io"
	"time"
)

// crockford là bảng chữ base32 Crockford viết hoa (không I, L, O, U).
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// codeRandBytes: 4 byte (32 bit) đủ cho 6 ký tự base32 (30 bit).
const codeRandBytes = 4

// NewShipmentCode là chỗ sinh mã vận đơn duy nhất của CRM (M3 dùng lại): "E" + yyMMdd theo giờ Việt Nam + 6 ký tự
// base32 Crockford viết hoa đọc từ rnd (crypto/rand.Reader khi chạy thật). rnd lỗi hay thiếu byte thì phần thiếu là
// số 0 (mã vẫn đúng mẫu; trùng thì unique của database bắt, bên gọi sinh lại).
func NewShipmentCode(now time.Time, rnd io.Reader) string {
	b := make([]byte, codeRandBytes)
	_, _ = io.ReadFull(rnd, b)
	enc := base32.NewEncoding(crockford).WithPadding(base32.NoPadding).EncodeToString(b)
	return "E" + now.In(vietnam).Format("060102") + enc[:6]
}

// vietnam là giờ Việt Nam (UTC+7, không đổi giờ mùa hè); cố định để không phụ thuộc tzdata của máy.
//
//nolint:gochecknoglobals // giá trị chỉ đọc, như hằng
var vietnam = time.FixedZone("ICT", 7*60*60)
