package xlsx

import (
	"fmt"

	"github.com/xuri/excelize/v2"
)

// Mã định dạng số của bộ ghi theo luồng: tiền nghìn có dấu phẩy (mã dựng sẵn 3 của Excel), ngày giờ kiểu Việt Nam.
const (
	moneyNumFmt = 3 // "#,##0"
	dateNumFmt  = "dd/mm/yyyy hh:mm"
)

// styles là chỉ số kiểu (cellXfs) mà Writer gắn cho ô; 0 là kiểu mặc định.
type styles struct {
	header, note, money, date int
}

// newStyles tạo các kiểu của Writer trong f: tiêu đề đậm, ghi chú nghiêng, tiền "#,##0", ngày "dd/mm/yyyy hh:mm".
func newStyles(f *excelize.File) (styles, error) {
	var s styles
	dateFmt := dateNumFmt
	for _, c := range []struct {
		dst *int
		st  *excelize.Style
	}{
		{&s.header, &excelize.Style{Font: &excelize.Font{Bold: true}}},
		{&s.note, &excelize.Style{Font: &excelize.Font{Italic: true}}},
		{&s.money, &excelize.Style{NumFmt: moneyNumFmt}},
		{&s.date, &excelize.Style{CustomNumFmt: &dateFmt}},
	} {
		id, err := f.NewStyle(c.st)
		if err != nil {
			return styles{}, fmt.Errorf("tạo kiểu ô: %w", err)
		}
		*c.dst = id
	}
	return s, nil
}

// cellStyle là kiểu của một ô dữ liệu theo loại.
func (s styles) cellStyle(k Kind) int {
	switch k {
	case KindMoney:
		return s.money
	case KindDate:
		return s.date
	case kindEmpty, KindString, KindInt:
	}
	return 0
}
