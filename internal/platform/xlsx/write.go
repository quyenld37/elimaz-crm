package xlsx

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Lỗi của Write (lỗi lập trình của bên gọi, không phải dữ liệu người dùng).
var errBadSheets = errors.New("xlsx: danh sách trang không hợp lệ")

// listSheet là trang ẩn chứa giá trị của các danh sách chọn (luật danh sách viết thẳng chỉ chứa được 255 ký tự).
const listSheet = "_lists"

// maxRow là dòng cuối của một trang Excel; danh sách chọn áp từ dòng 2 tới dòng này.
const maxRow = excelize.TotalRows

// Sheet là một trang để ghi: Header ở dòng 1, Rows từ dòng 2 (mỗi dòng không dài hơn Header). Ô nil là ô trống;
// giá trị khác ghi thành chữ (fmt.Sprint) qua Sanitize. Lists gắn danh sách chọn cho cột theo tiêu đề (giá trị cũng
// qua Sanitize; danh sách rỗng bị bỏ qua).
type Sheet struct {
	Name   string
	Header []string
	Rows   [][]any
	Lists  map[string][]string
}

// Write ghi sheets (theo thứ tự, trang đầu là trang mở ra) thành một tệp .xlsx vào w. Mọi ô là chữ, qua Sanitize:
// tệp không bao giờ có công thức. Tên trang phải khác nhau (không phân biệt hoa thường), hợp lệ với Excel và khác
// "_lists".
func Write(w io.Writer, sheets []Sheet) error {
	if err := checkSheets(sheets); err != nil {
		return err
	}
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	var lists []listRef
	for i, s := range sheets {
		if i == 0 {
			if err := f.SetSheetName(f.GetSheetName(0), s.Name); err != nil {
				return fmt.Errorf("%w: %w", errBadSheets, err)
			}
		} else if _, err := f.NewSheet(s.Name); err != nil {
			return fmt.Errorf("%w: %w", errBadSheets, err)
		}
		if err := writeSheet(f, s); err != nil {
			return err
		}
		lists = append(lists, sheetLists(s)...)
	}
	if err := writeLists(f, lists); err != nil {
		return err
	}
	f.SetActiveSheet(0)
	if _, err := f.WriteTo(w); err != nil {
		return fmt.Errorf("ghi tệp xlsx: %w", err)
	}
	return nil
}

func checkSheets(sheets []Sheet) error {
	if len(sheets) == 0 {
		return fmt.Errorf("%w: không có trang", errBadSheets)
	}
	for i, s := range sheets {
		if strings.EqualFold(s.Name, listSheet) || slices.ContainsFunc(sheets[:i], func(o Sheet) bool {
			return strings.EqualFold(o.Name, s.Name)
		}) {
			return fmt.Errorf("%w: tên trang %q trùng hoặc dành riêng", errBadSheets, s.Name)
		}
		for _, r := range s.Rows {
			if len(r) > len(s.Header) {
				return fmt.Errorf("%w: trang %q có dòng dài hơn tiêu đề", errBadSheets, s.Name)
			}
		}
		for col := range s.Lists {
			if col == "" || !slices.Contains(s.Header, col) {
				return fmt.Errorf("%w: danh sách chọn cho cột %q không có trong tiêu đề", errBadSheets, col)
			}
		}
	}
	return nil
}

func writeSheet(f *excelize.File, s Sheet) error {
	write := func(row int, values []any) error {
		for i, v := range values {
			if v == nil {
				continue
			}
			ref, err := excelize.CoordinatesToCellName(i+1, row)
			if err != nil {
				return fmt.Errorf("ghi trang %q: %w", s.Name, err)
			}
			if err := f.SetCellStr(s.Name, ref, Sanitize(fmt.Sprint(v))); err != nil {
				return fmt.Errorf("ghi trang %q: %w", s.Name, err)
			}
		}
		return nil
	}
	header := make([]any, len(s.Header))
	for i, h := range s.Header {
		header[i] = h
	}
	if err := write(1, header); err != nil {
		return err
	}
	for i, r := range s.Rows {
		if err := write(i+2, r); err != nil {
			return err
		}
	}
	return nil
}

// listRef là một danh sách chọn: trang, cột (chỉ số từ 1) và giá trị.
type listRef struct {
	sheet  string
	column int
	values []string
}

// sheetLists trả danh sách chọn của s theo thứ tự cột.
func sheetLists(s Sheet) []listRef {
	var out []listRef
	for i, h := range s.Header {
		if vs := s.Lists[h]; len(vs) > 0 {
			out = append(out, listRef{sheet: s.Name, column: i + 1, values: vs})
		}
	}
	return out
}

// writeLists ghi giá trị mỗi danh sách vào một cột của trang ẩn listSheet và gắn luật chọn từ dòng 2 tới cuối cột.
func writeLists(f *excelize.File, lists []listRef) error {
	if len(lists) == 0 {
		return nil
	}
	if _, err := f.NewSheet(listSheet); err != nil {
		return fmt.Errorf("tạo trang danh sách: %w", err)
	}
	for k, l := range lists {
		listCol, err := excelize.ColumnNumberToName(k + 1)
		if err != nil {
			return fmt.Errorf("cột danh sách: %w", err)
		}
		for i, v := range l.values {
			if err := f.SetCellStr(listSheet, fmt.Sprintf("%s%d", listCol, i+1), Sanitize(v)); err != nil {
				return fmt.Errorf("ghi danh sách: %w", err)
			}
		}
		col, err := excelize.ColumnNumberToName(l.column)
		if err != nil {
			return fmt.Errorf("cột có danh sách: %w", err)
		}
		dv := excelize.NewDataValidation(true)
		dv.Sqref = fmt.Sprintf("%s2:%s%d", col, col, maxRow)
		dv.SetSqrefDropList(fmt.Sprintf("'%s'!$%s$1:$%s$%d", listSheet, listCol, listCol, len(l.values)))
		if err := f.AddDataValidation(l.sheet, dv); err != nil {
			return fmt.Errorf("gắn danh sách chọn: %w", err)
		}
	}
	if err := f.SetSheetVisible(listSheet, false); err != nil {
		return fmt.Errorf("ẩn trang danh sách: %w", err)
	}
	return nil
}
