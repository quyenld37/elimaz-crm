package xlsx

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/xuri/excelize/v2"
)

// RawRow là một dòng không trống của ReadRows: Number là số dòng Excel (từ 1); Cells là giá trị từng cột theo thứ tự
// (chỉ số 0 là cột A; ô trống giữa dòng là ""; cột sau ô cuối có giá trị không có phần tử); Formulas là chỉ số cột (từ
// 0, tăng dần) của ô có công thức. Giá trị của ô công thức là giá trị Excel đã tính lúc lưu tệp ("" khi tệp không lưu):
// gói không bao giờ chạy công thức.
type RawRow struct {
	Number   int
	Cells    []string
	Formulas []int
}

// ReadRows đọc mọi dòng không trống của trang đầu tệp .xlsx (r, size byte) trong giới hạn lim, không giả định dòng
// tiêu đề (kế hoạch M3, Task M3-31: bảng kê của hãng có dòng mở đầu trước tiêu đề). Bên gọi tự tìm tiêu đề, tự quyết
// cách xử lý ô công thức (Formulas) và độ dài ô (lim.MaxCellChars không dùng ở đây; tổng dữ liệu đã bị chặn bởi
// MaxUnzipped). Dòng chỉ có khoảng trắng bị bỏ; dòng có ô công thức (kể cả không giá trị) được giữ. Dấu nháy mà Sanitize
// thêm trước ký tự công thức được bỏ như Read.
//
// Các bước kiểm trước khi excelize mở tệp giống Read. Lỗi: ErrTooLarge, ErrNotXlsx, ErrTooManyRows (quá lim.MaxRows
// dòng không trống, kể cả dòng mở đầu và tiêu đề).
func ReadRows(r io.ReaderAt, size int64, lim Limits) (rows []RawRow, err error) {
	lim = lim.withDefaults()
	err = openFirstSheet(r, size, lim, func(f *excelize.File, sheet string, formulas map[int][]int) error {
		var e error
		rows, e = readRawRows(f, sheet, lim, formulas)
		return e
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// readRawRows duyệt trang bằng bộ duyệt của excelize, dừng sớm khi quá lim.MaxRows dòng không trống.
func readRawRows(f *excelize.File, sheet string, lim Limits, formulas map[int][]int) ([]RawRow, error) {
	it, err := f.Rows(sheet)
	if err != nil {
		return nil, broken("mở trang đầu")
	}
	defer func() { _ = it.Close() }()
	var rows []RawRow
	for n := 1; it.Next(); n++ {
		cells, err := it.Columns()
		if err != nil {
			return nil, broken(fmt.Sprintf("dòng %d", n))
		}
		fcols := formulaIndexes(formulas[n])
		if len(fcols) == 0 && !slices.ContainsFunc(cells, func(c string) bool { return strings.TrimSpace(c) != "" }) {
			continue
		}
		if len(rows) == lim.MaxRows {
			return nil, ErrTooManyRows
		}
		row := RawRow{Number: n, Cells: make([]string, len(cells)), Formulas: fcols}
		for i, c := range cells {
			row.Cells[i] = unsanitize(c)
		}
		rows = append(rows, row)
	}
	if err := it.Error(); err != nil {
		return nil, broken("duyệt dòng")
	}
	return rows, nil
}

// formulaIndexes đổi cột có công thức của một dòng (từ 1, có thể trùng) thành chỉ số từ 0, tăng dần, không trùng.
func formulaIndexes(cols []int) []int {
	if len(cols) == 0 {
		return nil
	}
	out := make([]int, 0, len(cols))
	for _, c := range cols {
		out = append(out, c-1)
	}
	slices.Sort(out)
	return slices.Compact(out)
}
