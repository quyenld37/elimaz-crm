package statement

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"elimaz.com.vn/crm/internal/modules/cash/internal/formats"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/xlsx"
)

// Mã lỗi đọc tệp (parse_error.code của bảng kê; màn hình có câu cho từng mã).
const (
	CodeMissingColumn   = "missing_column"      // thiếu cột bắt buộc (column)
	CodeDuplicateColumn = "duplicate_column"    // hai cột cùng là một trường (column)
	CodeMissingValue    = "missing_value"       // ô bắt buộc trống ở dòng có dữ liệu (column, line)
	CodeBadAmount       = "bad_amount"          // số tiền sai dạng, âm ở cột không được âm (column, line)
	CodeBadDate         = "bad_date"            // ngày sai (column, line)
	CodeTooManyRows     = "too_many_rows"       // quá Limits.MaxRows dòng
	CodeTooManyColumns  = "too_many_columns"    // dòng quá Limits.MaxCols cột (line)
	CodeCellTooLong     = "cell_too_long"       // ô quá Limits.MaxCellLen ký tự (column?, line)
	CodeTooLarge        = "too_large"           // tệp quá Limits.MaxBytes hay giải nén quá 50 MiB
	CodeFormula         = "formula_not_allowed" // ô công thức không có giá trị đã tính (column, line)
	CodeBadFile         = "bad_file"            // không đọc được tệp (.xlsx hỏng, CSV sai dấu nháy)
	CodeBadEncoding     = "bad_encoding"        // CSV không phải UTF-8
	CodeNoLines         = "no_lines"            // không có dòng dữ liệu nào
)

// ErrBadAmount là lỗi khi chuỗi không phải số tiền nguyên mà ParseAmount nhận.
var ErrBadAmount = errors.New("cash: số tiền sai dạng")

// ParseError là lỗi định dạng của tệp bảng kê, lưu ở parse_error dạng {"code","column"?,"line"?}. Column là tên cột
// của định dạng (không phải chữ trong tệp), Line là số dòng trong tệp. Không bao giờ chứa giá trị ô.
type ParseError struct {
	Code   string `json:"code"`
	Column string `json:"column,omitempty"`
	Line   int    `json:"line,omitempty"`
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("cash: bảng kê sai định dạng: %s (cột %q, dòng %d)", e.Code, e.Column, e.Line)
}

// Line là một dòng bảng kê đã đọc: LineNo là số dòng trong tệp; tiền là đồng; PaidOn là ngày chuyển (UTC 00:00, zero
// là không có).
type Line struct {
	LineNo       int32
	TrackingCode string
	COD, Fee     int64
	OtherFee     int64
	Net          int64
	PaidOn       time.Time
}

// maxAmount: số tiền lớn nhất (theo trị tuyệt đối) nhận được, 2^53 (số nguyên chính xác của JSON phía màn hình).
const maxAmount = 1 << 53

// Các dạng số tiền nhận được (sau khi bỏ dấu trừ đầu): liền; chấm hay phẩy phân cách nghìn; phần lẻ toàn số 0.
var (
	amountPlain     = regexp.MustCompile(`^[0-9]+$`)
	amountDots      = regexp.MustCompile(`^[0-9]{1,3}(\.[0-9]{3})+$`)
	amountCommas    = regexp.MustCompile(`^[0-9]{1,3}(,[0-9]{3})+$`)
	amountZeroFrac  = regexp.MustCompile(`^([0-9]+)\.0+$`)
	amountCommaFrac = regexp.MustCompile(`^([0-9]{1,3}(?:,[0-9]{3})+)\.0+$`)
	amountDotFrac   = regexp.MustCompile(`^([0-9]{1,3}(?:\.[0-9]{3})+),0+$`)
	excelSerial     = regexp.MustCompile(`^[0-9]{1,7}(\.[0-9]+)?$`)
)

// ParseAmount đọc số tiền nguyên đồng: "1.234.567", "1,234,567", "1234567", "1234567.0" (và "1,234,567.00",
// "1.234.567,0"), dấu trừ đầu được nhận (bên gọi chặn âm ở cột không được âm). Số lẻ khác 0 ("12,5", "1.5"), dấu cách
// giữa số, ký hiệu tiền, vượt 2^53 → ErrBadAmount.
func ParseAmount(s string) (int64, error) {
	s = strings.TrimSpace(s)
	neg := false
	if rest, ok := strings.CutPrefix(s, "-"); ok {
		neg, s = true, rest
	}
	var digits string
	switch {
	case amountPlain.MatchString(s):
		digits = s
	case amountDots.MatchString(s):
		digits = strings.ReplaceAll(s, ".", "")
	case amountCommas.MatchString(s):
		digits = strings.ReplaceAll(s, ",", "")
	case amountZeroFrac.MatchString(s):
		digits = amountZeroFrac.FindStringSubmatch(s)[1]
	case amountCommaFrac.MatchString(s):
		digits = strings.ReplaceAll(amountCommaFrac.FindStringSubmatch(s)[1], ",", "")
	case amountDotFrac.MatchString(s):
		digits = strings.ReplaceAll(amountDotFrac.FindStringSubmatch(s)[1], ".", "")
	default:
		return 0, ErrBadAmount
	}
	if len(digits) > 18 {
		return 0, ErrBadAmount
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || n > maxAmount {
		return 0, ErrBadAmount
	}
	if neg {
		n = -n
	}
	return n, nil
}

// rawRow là một dòng không trống của tệp: number là số dòng trong tệp; formulas là chỉ số cột có công thức (.xlsx).
type rawRow struct {
	number   int
	cells    []string
	formulas []int
}

// Parse đọc toàn bộ content (tệp .xlsx nhận theo chữ ký zip, còn lại là CSV UTF-8 phân cách bằng dấu phẩy) theo định
// dạng f (đã qua formats.Registry) trong giới hạn lim: dòng f.HeaderRow là tiêu đề, dòng trước nó bị bỏ, dòng sau là
// dữ liệu (dòng trống bị bỏ). Lỗi định dạng là *ParseError; dữ liệu đúng trả mọi dòng, không bao giờ trả một phần.
func Parse(content []byte, f formats.Format, lim Limits) ([]Line, error) {
	if int64(len(content)) > lim.MaxBytes {
		return nil, &ParseError{Code: CodeTooLarge}
	}
	isXlsx := bytes.HasPrefix(content, []byte("PK\x03\x04"))
	var rows []rawRow
	var err error
	if isXlsx {
		rows, err = xlsxRows(content, lim, f.HeaderRow)
	} else {
		rows, err = csvRows(content, lim, f.HeaderRow)
	}
	if err != nil {
		return nil, err
	}
	var header *rawRow
	data := rows[:0:0]
	for i := range rows {
		switch r := &rows[i]; {
		case r.number == f.HeaderRow:
			header = r
		case r.number > f.HeaderRow:
			data = append(data, *r)
		}
	}
	if len(data) > lim.MaxRows {
		return nil, &ParseError{Code: CodeTooManyRows}
	}
	if header == nil {
		header = &rawRow{number: f.HeaderRow}
	}
	cols, err := mapHeader(*header, f, lim)
	if err != nil {
		return nil, err
	}
	lines := make([]Line, 0, len(data))
	for _, r := range data {
		l, err := parseRow(r, cols, f, lim, isXlsx)
		if err != nil {
			return nil, err
		}
		lines = append(lines, l)
	}
	if len(lines) == 0 {
		return nil, &ParseError{Code: CodeNoLines}
	}
	return lines, nil
}

// xlsxRows đọc dòng không trống của trang đầu bằng xlsx.ReadRows (giới hạn kích thước, giải nén, số dòng).
func xlsxRows(content []byte, lim Limits, headerRow int) ([]rawRow, error) {
	rs, err := xlsx.ReadRows(bytes.NewReader(content), int64(len(content)), lim.xlsxLimits(headerRow))
	switch {
	case errors.Is(err, xlsx.ErrTooLarge):
		return nil, &ParseError{Code: CodeTooLarge}
	case errors.Is(err, xlsx.ErrTooManyRows):
		return nil, &ParseError{Code: CodeTooManyRows}
	case err != nil:
		return nil, &ParseError{Code: CodeBadFile}
	}
	out := make([]rawRow, 0, len(rs))
	for _, r := range rs {
		row := rawRow{number: r.Number, cells: r.Cells, formulas: r.Formulas}
		if width(row.cells) > lim.MaxCols {
			return nil, &ParseError{Code: CodeTooManyColumns, Line: r.Number}
		}
		out = append(out, row)
	}
	return out, nil
}

// csvRows đọc bản ghi CSV không trống (bỏ BOM UTF-8), số dòng là dòng bắt đầu bản ghi; dừng sớm khi quá số dòng hay
// số cột.
func csvRows(content []byte, lim Limits, headerRow int) ([]rawRow, error) {
	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(content) {
		return nil, &ParseError{Code: CodeBadEncoding}
	}
	r := csv.NewReader(bytes.NewReader(content))
	r.FieldsPerRecord = -1
	var out []rawRow
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if pe, ok := errors.AsType[*csv.ParseError](err); ok {
			return nil, &ParseError{Code: CodeBadFile, Line: pe.StartLine}
		}
		if err != nil {
			return nil, &ParseError{Code: CodeBadFile}
		}
		line, _ := r.FieldPos(0)
		if width(rec) == 0 {
			continue
		}
		if width(rec) > lim.MaxCols {
			return nil, &ParseError{Code: CodeTooManyColumns, Line: line}
		}
		if len(out) == lim.MaxRows+headerRow {
			return nil, &ParseError{Code: CodeTooManyRows}
		}
		out = append(out, rawRow{number: line, cells: rec})
	}
}

// width là số cột tới ô cuối có giá trị (0: dòng trống).
func width(cells []string) int {
	for i := len(cells) - 1; i >= 0; i-- {
		if strings.TrimSpace(cells[i]) != "" {
			return i + 1
		}
	}
	return 0
}

// mapHeader tìm cột của từng trường trong dòng tiêu đề theo tên chuẩn hóa (formats.ColumnName); trường bắt buộc
// thiếu → missing_column, hai cột cùng một trường → duplicate_column (cột báo là tên đầu tiên của trường).
func mapHeader(h rawRow, f formats.Format, lim Limits) (map[fulfillment.StatementField]int, error) {
	field := map[string]fulfillment.StatementField{}
	for fl, names := range f.Columns {
		for _, n := range names {
			field[n] = fl
		}
	}
	cols := map[fulfillment.StatementField]int{}
	for i, c := range h.cells {
		if utf8.RuneCountInString(c) > lim.MaxCellLen {
			return nil, &ParseError{Code: CodeCellTooLong, Line: h.number}
		}
		fl, ok := field[formats.ColumnName(c)]
		if !ok {
			continue
		}
		if _, dup := cols[fl]; dup {
			return nil, &ParseError{Code: CodeDuplicateColumn, Column: f.Columns[fl][0]}
		}
		cols[fl] = i
	}
	for _, fl := range f.Required {
		if _, ok := cols[fl]; !ok {
			return nil, &ParseError{Code: CodeMissingColumn, Column: f.Columns[fl][0]}
		}
	}
	return cols, nil
}

// rowReader đọc ô của một dòng dữ liệu theo cột đã tìm ở tiêu đề.
type rowReader struct {
	r    rawRow
	cols map[fulfillment.StatementField]int
	f    formats.Format
	lim  Limits
}

func (rr rowReader) fail(code string, fl fulfillment.StatementField) error {
	return &ParseError{Code: code, Column: rr.f.Columns[fl][0], Line: rr.r.number}
}

// cell trả ô của trường fl đã bỏ khoảng trắng đầu cuối ("" khi định dạng có mà tệp không có cột, hay ô trống). Ô quá
// dài → cell_too_long; ô công thức không có giá trị đã tính → formula_not_allowed.
func (rr rowReader) cell(fl fulfillment.StatementField) (string, error) {
	i, ok := rr.cols[fl]
	if !ok {
		return "", nil
	}
	v := ""
	if i < len(rr.r.cells) {
		v = rr.r.cells[i]
	}
	switch {
	case utf8.RuneCountInString(v) > rr.lim.MaxCellLen:
		return "", rr.fail(CodeCellTooLong, fl)
	case strings.TrimSpace(v) == "" && slices.Contains(rr.r.formulas, i):
		return "", rr.fail(CodeFormula, fl)
	}
	return strings.TrimSpace(v), nil
}

// amount đọc số tiền của trường fl: trống ở cột bắt buộc → missing_value, ở cột tùy chọn → 0; sai dạng hay âm khi
// không được âm → bad_amount.
func (rr rowReader) amount(fl fulfillment.StatementField, allowNeg bool) (int64, error) {
	v, err := rr.cell(fl)
	switch {
	case err != nil:
		return 0, err
	case v == "" && slices.Contains(rr.f.Required, fl):
		return 0, rr.fail(CodeMissingValue, fl)
	case v == "":
		return 0, nil
	}
	n, err := ParseAmount(v)
	if err != nil || (n < 0 && !allowNeg) {
		return 0, rr.fail(CodeBadAmount, fl)
	}
	return n, nil
}

// parseRow đọc một dòng dữ liệu r theo cột đã tìm. Ô công thức của .xlsx dùng giá trị đã tính lúc lưu; không có giá
// trị → formula_not_allowed. Mã vận đơn bắt buộc; COD, phí không âm; phí khác, thực nhận được âm; ô tiền trống ở cột
// bắt buộc → missing_value, ở cột tùy chọn → 0; không có cột thực nhận → COD − phí − phí khác.
func parseRow(r rawRow, cols map[fulfillment.StatementField]int, f formats.Format, lim Limits, isXlsx bool) (Line, error) {
	rr := rowReader{r: r, cols: cols, f: f, lim: lim}
	l := Line{LineNo: int32(min(r.number, 1<<31-1))} //nolint:gosec // đã chặn trong int32
	var err error
	if l.TrackingCode, err = rr.cell(fulfillment.FieldTrackingCode); err != nil {
		return Line{}, err
	}
	if l.TrackingCode == "" {
		return Line{}, rr.fail(CodeMissingValue, fulfillment.FieldTrackingCode)
	}
	for _, a := range []struct {
		dst      *int64
		field    fulfillment.StatementField
		allowNeg bool
	}{
		{&l.COD, fulfillment.FieldCOD, false},
		{&l.Fee, fulfillment.FieldFee, false},
		{&l.OtherFee, fulfillment.FieldOtherFee, true},
	} {
		if *a.dst, err = rr.amount(a.field, a.allowNeg); err != nil {
			return Line{}, err
		}
	}
	l.Net = l.COD - l.Fee - l.OtherFee
	if _, ok := cols[fulfillment.FieldNet]; ok {
		if l.Net, err = rr.amount(fulfillment.FieldNet, true); err != nil {
			return Line{}, err
		}
	}
	v, err := rr.cell(fulfillment.FieldPaidOn)
	if err != nil || v == "" {
		return l, err
	}
	if l.PaidOn, err = parseDate(v, f.DateLayouts, isXlsx); err != nil {
		return Line{}, rr.fail(CodeBadDate, fulfillment.FieldPaidOn)
	}
	return l, nil
}

// errBadDate: ngày không theo dạng nào của định dạng.
var errBadDate = errors.New("cash: ngày sai dạng")

// parseDate đọc ngày theo layouts; ô của .xlsx còn nhận số ngày Excel (ô ngày lưu dạng số, phần giờ bị bỏ; ngày 0 là
// 30/12/1899 của hệ 1900, đã tính lỗi năm nhuận 1900 cho ngày sau 01/03/1900) từ 1 tới 2.958.465 (31/12/9999).
func parseDate(v string, layouts []string, isXlsx bool) (time.Time, error) {
	if isXlsx && excelSerial.MatchString(v) {
		whole, _, _ := strings.Cut(v, ".")
		n, err := strconv.Atoi(whole)
		if err != nil || n < 1 || n > 2_958_465 {
			return time.Time{}, errBadDate
		}
		return time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n), nil
	}
	for _, l := range layouts {
		if d, err := time.ParseInLocation(l, v, time.UTC); err == nil {
			return d, nil
		}
	}
	return time.Time{}, errBadDate
}
