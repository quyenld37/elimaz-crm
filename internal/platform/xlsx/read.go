package xlsx

import (
	"archive/zip"
	"cmp"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

// Lỗi của Read. Lỗi trả về bọc một trong các sentinel này (kiểm bằng errors.Is); chuỗi lỗi không chứa nội dung ô.
var (
	ErrNotXlsx        = errors.New("xlsx: không phải tệp Excel .xlsx đọc được")
	ErrTooLarge       = errors.New("xlsx: tệp quá lớn")
	ErrTooManyRows    = errors.New("xlsx: tệp quá nhiều dòng")
	ErrHeaderMismatch = errors.New("xlsx: dòng tiêu đề không khớp mẫu")
)

// Mã lỗi ô của Read.
const (
	CodeFormulaNotAllowed = "formula_not_allowed" // ô là công thức
	CodeCellTooLong       = "cell_too_long"       // ô dài quá Limits.MaxCellChars
)

// Row là một dòng dữ liệu không trống: Number là số dòng trong Excel (tiêu đề là dòng 1); Cells theo tiêu đề cột,
// chỉ có ô có giá trị (ô trống, ô có CellError không có khóa: đọc ra "").
type Row struct {
	Number int
	Cells  map[string]string
}

// CellError là một ô không nhận được: Row là số dòng Excel, Column là tiêu đề cột, Code là CodeFormulaNotAllowed
// hoặc CodeCellTooLong.
type CellError struct {
	Row          int
	Column, Code string
}

// Read đọc trang đầu của tệp .xlsx (r, size byte) trong giới hạn lim. Dòng 1 là tiêu đề: mỗi tên trong header phải
// có đúng một cột (so sau khi bỏ khoảng trắng đầu cuối), không thì ErrHeaderMismatch; cột có tiêu đề khác header vẫn
// được đọc, cột không tiêu đề bị bỏ. Dòng trống bị bỏ. Ô số đọc giá trị thô, không theo định dạng hiển thị. Ô có
// công thức (kể cả công thức dùng chung, mảng, công thức chưa có giá trị) hay dài quá lim.MaxCellChars thành CellError
// và không vào Cells. Dấu nháy mà Sanitize thêm trước ký tự công thức được bỏ để tệp CRM xuất ra nhập lại đúng nguyên
// văn.
//
// Trước khi excelize mở tệp: kích thước tệp, tổng giải nén theo header zip và giải nén thử từng phần (đúng số byte
// khai, đúng CRC). Lỗi: ErrTooLarge, ErrNotXlsx (không phải zip, thiếu phần sổ, phần nén hỏng, excelize không đọc
// được), ErrTooManyRows (quá lim.MaxRows dòng không trống), ErrHeaderMismatch.
func Read(r io.ReaderAt, size int64, lim Limits, header []string) (rows []Row, cellErrs []CellError, err error) {
	lim = lim.withDefaults()
	err = openFirstSheet(r, size, lim, func(f *excelize.File, sheet string, formulas map[int][]int) error {
		var e error
		rows, cellErrs, e = readSheet(f, sheet, lim, header, formulas)
		return e
	})
	if err != nil {
		return nil, nil, err
	}
	return rows, cellErrs, nil
}

// openFirstSheet kiểm tệp (kích thước, tổng giải nén, từng phần nén, trần số ô, vị trí công thức của scanBook) trước
// khi excelize mở, rồi gọi read với sổ đã mở, tên trang đầu và vị trí ô công thức (dòng → cột, từ 1). lim đã qua
// withDefaults. Panic của excelize trong lúc mở hay trong read thành ErrNotXlsx.
func openFirstSheet(r io.ReaderAt, size int64, lim Limits,
	read func(f *excelize.File, sheet string, formulas map[int][]int) error,
) (err error) {
	if size > lim.MaxBytes {
		return ErrTooLarge
	}
	zr, err := checkZip(r, size, lim)
	if err != nil {
		return err
	}
	first, formulas, err := scanBook(zr)
	if err != nil {
		return err
	}
	// excelize đọc dữ liệu người dùng: lỗi lập trình của nó (đã có nhiều báo cáo panic với tệp hỏng) không được làm
	// sập tiến trình gọi.
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("%w: excelize lỗi khi đọc", ErrNotXlsx)
		}
	}()
	f, err := excelize.OpenReader(io.NewSectionReader(r, 0, size), excelize.Options{
		UnzipSizeLimit: lim.MaxUnzipped, UnzipXMLSizeLimit: lim.MaxUnzipped, RawCellValue: true,
	})
	if err != nil {
		// Tổng giải nén đã kiểm ở checkZip, nên lỗi ở đây là tệp hỏng.
		return broken("mở sổ")
	}
	defer func() { _ = f.Close() }()
	// Trang đầu của excelize phải là trang scanBook đã quét (trần số ô, công thức).
	if sheets := f.GetSheetList(); len(sheets) == 0 || sheets[0] != first.name {
		return fmt.Errorf("%w: trang đầu không khớp", ErrNotXlsx)
	}
	return read(f, first.name, formulas)
}

// checkZip từ chối tệp không phải zip, thiếu phần sổ của xlsx, hoặc tổng giải nén theo header vượt giới hạn.
// archive/zip báo lỗi khi dữ liệu thật dài hơn số byte header khai, nên tổng này là chặn trên của phần giải nén.
func checkZip(r io.ReaderAt, size int64, lim Limits) (*zip.Reader, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotXlsx, err)
	}
	var total uint64
	contentTypes, workbook := false, false
	seen := make(map[string]bool, len(zr.File))
	for _, f := range zr.File {
		// excelize lấy phần sau khi hai phần trùng tên (sau khi đổi "\" thành "/"), các bước kiểm của gói lấy phần đầu:
		// tên trùng (không phân biệt hoa thường) bị từ chối để hai bên luôn đọc cùng một nội dung.
		key := strings.ToLower(partName(f.Name))
		if seen[key] {
			return nil, fmt.Errorf("%w: hai phần trùng tên", ErrNotXlsx)
		}
		seen[key] = true
		if f.UncompressedSize64 > uint64(lim.MaxUnzipped)-total { //nolint:gosec // withDefaults: MaxUnzipped > 0
			return nil, ErrTooLarge
		}
		total += f.UncompressedSize64
		contentTypes = contentTypes || f.Name == "[Content_Types].xml"
		workbook = workbook || f.Name == workbookPart
	}
	if !contentTypes || !workbook {
		return nil, fmt.Errorf("%w: thiếu [Content_Types].xml hoặc xl/workbook.xml", ErrNotXlsx)
	}
	// excelize đọc mỗi phần tới đúng số byte header khai và bỏ qua lỗi cụt, nên tệp khai sai bị đọc thiếu dòng mà
	// không báo. Giải nén thử từng phần (tổng ≤ MaxUnzipped, bộ nhớ cố định): archive/zip báo lỗi khi dữ liệu dài hơn
	// số khai hoặc sai CRC.
	for _, f := range zr.File {
		if err := drain(f); err != nil {
			return nil, fmt.Errorf("%w: phần nén hỏng: %w", ErrNotXlsx, err)
		}
	}
	return zr, nil
}

func drain(f *zip.File) error {
	rc, err := f.Open()
	if err != nil {
		return err //nolint:wrapcheck // bọc ở checkZip
	}
	defer func() { _ = rc.Close() }()
	// Đọc tối đa số byte khai cộng 1 (archive/zip đã báo lỗi khi dữ liệu dài hơn số khai).
	n, err := io.CopyN(io.Discard, rc, int64(f.UncompressedSize64)+1) //nolint:gosec // ≤ MaxUnzipped (đã kiểm)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	if err == nil && uint64(n) != f.UncompressedSize64 { //nolint:gosec // n ≥ 0
		err = zip.ErrFormat
	}
	return err
}

// readSheet đọc từng dòng bằng bộ duyệt của excelize (dừng sớm khi quá số dòng).
func readSheet(f *excelize.File, sheet string, lim Limits, header []string, formulas map[int][]int,
) ([]Row, []CellError, error) {
	it, err := f.Rows(sheet)
	if err != nil {
		return nil, nil, broken("mở trang đầu")
	}
	defer func() { _ = it.Close() }()
	var (
		cols     []string // tiêu đề theo chỉ số cột; "" là cột bỏ
		rows     []Row
		cellErrs []CellError
	)
	for n := 1; it.Next(); n++ {
		cells, err := it.Columns()
		if err != nil {
			return nil, nil, broken(fmt.Sprintf("dòng %d", n))
		}
		if n == 1 {
			if cols, err = headerColumns(cells, header); err != nil {
				return nil, nil, err
			}
			continue
		}
		row, errs := dataRow(n, cols, cells, formulas[n], lim)
		if row.Cells == nil {
			continue
		}
		if len(rows) == lim.MaxRows {
			return nil, nil, ErrTooManyRows
		}
		rows = append(rows, row)
		cellErrs = append(cellErrs, errs...)
	}
	if err := it.Error(); err != nil {
		return nil, nil, broken("duyệt dòng")
	}
	if cols == nil {
		if _, err := headerColumns(nil, header); err != nil {
			return nil, nil, err
		}
	}
	return rows, cellErrs, nil
}

// headerColumns kiểm dòng tiêu đề: mỗi tên của want có đúng một cột, không cột nào trùng tên.
func headerColumns(cells, want []string) ([]string, error) {
	cols := make([]string, len(cells))
	seen := make(map[string]bool, len(cells))
	for i, c := range cells {
		c = strings.TrimSpace(unsanitize(c))
		if c == "" {
			continue
		}
		if seen[c] {
			return nil, fmt.Errorf("%w: cột %d trùng tiêu đề", ErrHeaderMismatch, i+1)
		}
		seen[c] = true
		cols[i] = c
	}
	for _, w := range want {
		if w == "" || !seen[w] {
			return nil, fmt.Errorf("%w: thiếu cột %q", ErrHeaderMismatch, w)
		}
	}
	return cols, nil
}

// dataRow dựng Row n từ cells (formulaCols: cột có công thức của dòng); Row.Cells nil là dòng trống. Chỉ ô có giá
// trị vào Cells (bộ nhớ theo số ô thật, không theo số cột × số dòng). Ô có công thức hay quá dài thành CellError.
func dataRow(n int, cols, cells []string, formulaCols []int, lim Limits) (Row, []CellError) {
	row := Row{Number: n, Cells: map[string]string{}}
	var errs []CellError
	named := func(i int) string { // tiêu đề của cột chỉ số i (từ 0); "" là cột bỏ
		if i < len(cols) {
			return cols[i]
		}
		return ""
	}
	formula := map[int]bool{}
	for _, c := range formulaCols {
		if col := named(c - 1); col != "" && !formula[c-1] {
			formula[c-1] = true
			errs = append(errs, CellError{Row: n, Column: col, Code: CodeFormulaNotAllowed})
		}
	}
	for i, v := range cells[:min(len(cells), len(cols))] { // cột sau tiêu đề cuối bị bỏ
		col := named(i)
		if col == "" || formula[i] || strings.TrimSpace(v) == "" {
			continue
		}
		v = unsanitize(v)
		if utf8.RuneCountInString(v) > lim.MaxCellChars {
			errs = append(errs, CellError{Row: n, Column: col, Code: CodeCellTooLong})
			continue
		}
		row.Cells[col] = v
	}
	if len(row.Cells) == 0 && len(errs) == 0 {
		return Row{Number: n}, nil
	}
	slices.SortFunc(errs, func(a, b CellError) int {
		return cmp.Compare(slices.Index(cols, a.Column), slices.Index(cols, b.Column))
	})
	return row, errs
}

// broken báo tệp hỏng ở bước where. Không bọc lỗi của excelize: chuỗi lỗi của nó có thể chép giá trị trong tệp
// (tệp khách có dữ liệu cá nhân, 04 mục 8).
func broken(where string) error {
	return fmt.Errorf("%w: excelize không đọc được (%s)", ErrNotXlsx, where)
}
