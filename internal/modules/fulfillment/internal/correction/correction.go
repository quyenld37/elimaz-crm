// Package correction là luật thuần của việc đảo "Thành công" (kế hoạch M3, Task M3-27; TI08): câu tác động, câu chặn
// và token của bản xem trước. Không database: bên gọi (fulfillment, correction.go) đọc dữ kiện dưới khóa rồi gọi Decide.
package correction

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// Line là một dòng đơn sẽ được đảo phiếu xuất: SKU và số cái.
type Line struct {
	SKU string
	Qty int32
}

// Facts là dữ kiện của một lần đảo: vận đơn (mã, version, trạng thái trước, sau), các dòng đơn, tiền hãng đã thu
// sẽ được cash đảo (0: không có), tiền đã về shop (số dư shop_received) và còn bút toán remitted chưa bị đảo không.
type Facts struct {
	ShipmentID   uuid.UUID
	Version      int64
	From, To     string
	Lines        []Line
	Collected    int64
	ShopReceived int64
	Remitted     bool
}

// Plan là bản xem trước: câu tác động, câu chặn (khác rỗng thì không áp được), token.
type Plan struct {
	Effects, Blocking []string
	Token             string
}

// Decide dựng bản xem trước của f. Token là sha256 (hex) của mã vận đơn, version, trạng thái trước, sau và các câu
// tác động — đổi khi vận đơn đổi version hay tác động đổi; không gồm câu chặn (chặn kiểm riêng lúc áp).
func Decide(f Facts) Plan {
	effects := make([]string, 0, len(f.Lines)+2)
	effects = append(effects,
		"Vận đơn từ Thành công sang Đang chuyển hoàn; đơn bỏ Thành công, mở phiếu Hãng hoàn chờ kho nhận")
	for _, l := range f.Lines {
		effects = append(effects, fmt.Sprintf(
			"Đảo phiếu xuất %s ×%d: hàng về Đang chuyển, kho nhận lại khi hàng hoàn về", l.SKU, l.Qty))
	}
	if f.Collected > 0 {
		effects = append(effects, "Đảo bút toán tiền hãng đã thu "+VND(f.Collected))
	}
	h := sha256.New()
	for _, part := range append([]string{f.ShipmentID.String(), strconv.FormatInt(f.Version, 10), f.From, f.To},
		effects...) {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return Plan{Effects: effects, Blocking: Blocking(f.ShopReceived, f.Remitted), Token: hex.EncodeToString(h.Sum(nil))}
}

// Blocking trả câu chặn theo luật của cash (postings/rules.go, delivery_reversed): tiền đã về shop (shop_received ≠ 0)
// hoặc còn bút toán remitted chưa bị đảo → kế toán điều chỉnh sổ COD trước (AT40). nil: không chặn.
func Blocking(shopReceived int64, remitted bool) []string {
	const tail = " trong bảng kê đã chốt — kế toán điều chỉnh sổ COD trước"
	switch {
	case shopReceived != 0:
		return []string{"Vận đơn đã có tiền về shop " + VND(shopReceived) + tail}
	case remitted:
		return []string{"Vận đơn đã có bút toán tiền về shop" + tail}
	}
	return nil
}

// VND viết số đồng n kiểu "630.000 đ" (dấu chấm nhóm nghìn).
func VND(n int64) string {
	digits := strconv.FormatInt(n, 10)
	sign := ""
	if n < 0 {
		sign, digits = "-", digits[1:]
	}
	var b strings.Builder
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	return sign + b.String() + " đ"
}
