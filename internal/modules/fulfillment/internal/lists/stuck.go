// Package lists là phần thuần của danh sách vận hành "Đơn kẹt", "Cần xem" (kế hoạch M3, Task M3-20): giờ gửi cảnh báo
// ngày, văn bản cảnh báo (chỉ số lượng), nhãn tiếng Việt của từng loại việc Cần xem, con trỏ trang. Không database.
package lists

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrBadCursor báo chuỗi con trỏ không do Cursor.String tạo.
var ErrBadCursor = errors.New("lists: con trỏ không hợp lệ")

const (
	alertHour = 8 // cảnh báo ngày gửi từ 08:00 giờ Việt Nam
	dayLayout = "20060102"
	cursorLen = 8 + 16
)

// vietnam là giờ Việt Nam (UTC+7, không đổi giờ theo mùa).
//
//nolint:gochecknoglobals // giá trị chỉ đọc, như hằng
var vietnam = time.FixedZone("ICT", 7*60*60)

// statusOrder là thứ tự trạng thái trong văn bản cảnh báo (creating trước, rồi năm trạng thái có ngưỡng).
func statusOrder() []string {
	return []string{"creating", "awaiting_pickup", "shipping", "delivery_failed", "returning", "cancel_requested"}
}

// AlertDay trả ngày (YYYYMMDD giờ Việt Nam) của cảnh báo đơn kẹt khi lượt lịch ở now được gửi: từ 08:00 giờ Việt Nam
// trở đi; trước 08:00 → "", false. Bên gọi chống trùng theo ngày nên chỉ lượt đầu tiên từ 08:00 gửi.
func AlertDay(now time.Time) (string, bool) {
	vn := now.In(vietnam)
	if vn.Hour() < alertHour {
		return "", false
	}
	return vn.Format(dayLayout), true
}

// AlertText là văn bản cảnh báo đơn kẹt: tổng và số lượng theo trạng thái (thứ tự statusOrder, trạng thái lạ sau, theo
// tên); bỏ trạng thái 0. Tổng 0 → "" (không gửi). Không mã đơn, mã vận đơn, tên khách.
func AlertText(counts map[string]int64) string {
	var total int64
	var parts []string
	keys := slices.Sorted(maps.Keys(counts))
	slices.SortStableFunc(keys, func(a, b string) int { return rank(a) - rank(b) })
	for _, k := range keys {
		if n := counts[k]; n > 0 {
			total += n
			parts = append(parts, fmt.Sprintf("%s: %d", k, n))
		}
	}
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("%d vận đơn kẹt (lâu không có cập nhật của hãng hoặc chưa gửi được hãng) — %s. Xem danh sách "+
		"\"Đơn kẹt\" của vận chuyển", total, strings.Join(parts, ", "))
}

func rank(status string) int {
	order := statusOrder()
	if i := slices.Index(order, status); i >= 0 {
		return i
	}
	return len(order)
}

// HoursSince là số giờ tròn xuống từ at tới now (at sau now → 0).
func HoursSince(now, at time.Time) int32 {
	h := now.Sub(at) / time.Hour
	if h < 0 {
		return 0
	}
	return int32(min(h, 1<<31-1))
}

// Cursor là vị trí keyset của một danh sách (mốc sắp, mã dòng cuối của trang trước).
type Cursor struct {
	At time.Time
	ID uuid.UUID
}

// String mã hóa con trỏ thành chuỗi mờ: base64url của 8 byte micro giây Unix và 16 byte mã.
func (c Cursor) String() string {
	b := binary.BigEndian.AppendUint64(make([]byte, 0, cursorLen), uint64(c.At.UnixMicro()))
	return base64.RawURLEncoding.EncodeToString(append(b, c.ID[:]...))
}

// ParseCursor đọc chuỗi do Cursor.String tạo; hỏng → ErrBadCursor.
func ParseCursor(s string) (Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(raw) != cursorLen {
		return Cursor{}, ErrBadCursor
	}
	c := Cursor{At: time.UnixMicro(int64(binary.BigEndian.Uint64(raw))).UTC()} //nolint:gosec // đảo của String
	copy(c.ID[:], raw[8:])
	return c, nil
}
