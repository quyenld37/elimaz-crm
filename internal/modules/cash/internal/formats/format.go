// Package formats là sổ định dạng tệp bảng kê COD (kế hoạch M3, Task M3-31; Q-16): định dạng chung elimaz_v1 của CRM
// cộng định dạng mà adapter của từng hãng tự khai (fulfillment.StatementFormatProvider). Tên cột so theo ColumnName
// (bỏ dấu, chữ thường, "_" thay cho mọi chuỗi ký tự khác chữ, số); không đoán cột ngoài danh sách đã khai.
package formats

import (
	"errors"
	"fmt"
	"strings"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/textnorm"
)

// Format là định dạng một tệp bảng kê (kiểu chung của fulfillment, Task M3-03).
type Format = fulfillment.StatementFormat

// Lỗi của sổ định dạng.
var (
	// ErrDuplicateKey: hai định dạng cùng Key (lỗi lúc khởi động).
	ErrDuplicateKey = errors.New("cash: trùng khóa định dạng bảng kê")
	// ErrInvalid: định dạng khai sai (lỗi lúc khởi động).
	ErrInvalid = errors.New("cash: định dạng bảng kê khai sai")
	// ErrUnknown: không có định dạng này cho hãng của tài khoản.
	ErrUnknown = errors.New("cash: không có định dạng bảng kê này cho hãng")
)

// ColumnName chuẩn hóa tên cột tiêu đề để so: bỏ dấu tiếng Việt, chữ thường (textnorm.Fold), mỗi chuỗi ký tự không
// phải chữ cái a–z hay chữ số thành một "_", bỏ "_" đầu cuối. "Mã vận đơn" → "ma_van_don".
func ColumnName(s string) string {
	var b strings.Builder
	under := false
	for _, r := range textnorm.Fold(s) {
		if ('a' <= r && r <= 'z') || ('0' <= r && r <= '9') {
			if under && b.Len() > 0 {
				b.WriteByte('_')
			}
			under = false
			b.WriteRune(r)
			continue
		}
		under = true
	}
	return b.String()
}

// validate kiểm một định dạng: có Key, có hãng (trừ elimaz_v1), có trường bắt buộc, trường bắt buộc nào cũng có tên
// cột, mọi tên cột chuẩn hóa khác rỗng và không thuộc hai trường. Trả bản có tên cột đã chuẩn hóa, HeaderRow ≥ 1.
func validate(f Format) (Format, error) {
	switch {
	case f.Key == "":
		return f, fmt.Errorf("%w: thiếu khóa", ErrInvalid)
	case f.CarrierKey == "" && f.Key != ElimazV1Key:
		return f, fmt.Errorf("%w: %s thiếu khóa hãng", ErrInvalid, f.Key)
	case len(f.Required) == 0:
		return f, fmt.Errorf("%w: %s không có cột bắt buộc", ErrInvalid, f.Key)
	case f.HeaderRow < 0:
		return f, fmt.Errorf("%w: %s dòng tiêu đề âm", ErrInvalid, f.Key)
	}
	if f.HeaderRow == 0 {
		f.HeaderRow = 1
	}
	owner := map[string]fulfillment.StatementField{}
	cols := make(map[fulfillment.StatementField][]string, len(f.Columns))
	for field, names := range f.Columns {
		for _, n := range names {
			c := ColumnName(n)
			if prev, ok := owner[c]; c == "" || (ok && prev != field) {
				return f, fmt.Errorf("%w: %s tên cột %q rỗng hoặc thuộc hai trường", ErrInvalid, f.Key, c)
			}
			owner[c] = field
			cols[field] = append(cols[field], c)
		}
	}
	for _, field := range f.Required {
		if len(cols[field]) == 0 {
			return f, fmt.Errorf("%w: %s cột bắt buộc %s không có tên", ErrInvalid, f.Key, field)
		}
	}
	f.Columns = cols
	return f, nil
}
