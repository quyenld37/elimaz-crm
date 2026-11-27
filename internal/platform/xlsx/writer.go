package xlsx

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/xuri/excelize/v2"
)

// Kind là loại của một ô của Writer. Giá trị 0 là ô trống (Empty).
type Kind int

// Các loại ô: chữ (qua Sanitize), số nguyên, tiền (int64 đồng, định dạng "#,##0"), ngày giờ (giờ Việt Nam).
const (
	kindEmpty Kind = iota
	KindString
	KindInt
	KindMoney
	KindDate
)

// Cell là một ô của Writer; dựng bằng Str, Int, Money, Date, Empty. S dùng cho KindString, I cho KindInt và
// KindMoney, T cho KindDate.
type Cell struct {
	Kind Kind
	S    string
	I    int64
	T    time.Time
}

// Str là ô chữ; giá trị qua Sanitize khi ghi (không bao giờ thành công thức).
func Str(s string) Cell { return Cell{Kind: KindString, S: s} }

// Int là ô số nguyên.
func Int(v int64) Cell { return Cell{Kind: KindInt, I: v} }

// Money là ô tiền (đồng), ô số có định dạng "#,##0".
func Money(v int64) Cell { return Cell{Kind: KindMoney, I: v} }

// Date là ô ngày giờ, hiện theo giờ Việt Nam dạng "dd/mm/yyyy hh:mm". t.IsZero() là ô trống.
func Date(t time.Time) Cell {
	if t.IsZero() {
		return Cell{}
	}
	return Cell{Kind: KindDate, T: t}
}

// Empty là ô trống (không ghi gì vào tệp).
func Empty() Cell { return Cell{} }

// Column là một cột của Writer: tiêu đề (dòng 1, qua Sanitize), độ rộng theo số ký tự (≤ 0 là mặc định của Excel) và
// loại chính của cột (định dạng của ô trống trong cột; ô của từng dòng vẫn có thể khác loại, vd dòng tổng).
type Column struct {
	Title string
	Width int
	Kind  Kind
}

// MaxDataRows là số dòng dữ liệu tối đa của một trang (1.048.576 dòng của Excel trừ dòng tiêu đề), tính cả dòng ghi
// chú.
const MaxDataRows = excelize.TotalRows - 1

// Lỗi dùng sai Writer (lỗi lập trình của bên gọi, không phải dữ liệu người dùng).
var (
	errBadColumns = errors.New("xlsx: danh sách cột không hợp lệ")
	errBadRow     = errors.New("xlsx: dòng không hợp lệ")
	errClosed     = errors.New("xlsx: Writer đã đóng")
)

// vietnam là giờ Việt Nam (UTC+7, không đổi giờ mùa hè): ô ngày hiện theo giờ này.
//
//nolint:gochecknoglobals // hằng múi giờ
var vietnam = time.FixedZone("ICT", 7*3600)

// Khoảng ngày ghi được: từ 01/03/1900 (trước đó số ngày của Excel lệch một ngày vì lỗi năm nhuận 1900) tới hết năm 9999.
//
//nolint:gochecknoglobals // hằng thời điểm
var (
	excelEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, vietnam)
	minDate    = time.Date(1900, 3, 1, 0, 0, 0, 0, vietnam)
	endDate    = time.Date(10000, 1, 1, 0, 0, 0, 0, vietnam)
)

// Writer ghi một trang Excel theo luồng (excelize.StreamWriter: các dòng đã ghi nằm ở tệp tạm, không ở bộ nhớ): dòng 1
// là tiêu đề đậm, cố định khi cuộn; Row ghi từng dòng dữ liệu; Note ghi dòng ghi chú nghiêng; Close ghi tệp ra đích.
// Không có đường nào ghi công thức, siêu liên kết hay macro. Một Writer chỉ dùng trong một goroutine.
type Writer struct {
	dst     io.Writer
	f       *excelize.File
	sw      *excelize.StreamWriter
	cols    int
	st      styles
	rows    int // dòng dữ liệu đã ghi bằng Row
	used    int // dòng đã dùng sau tiêu đề (Row và Note)
	maxRows int
	closed  bool
}

// NewWriter mở một tệp mới có một trang tên sheet với các cột cols; tệp được ghi ra w khi Close. Lỗi khi không có cột,
// cột có loại lạ, hay tên trang không hợp lệ với Excel. Bên gọi phải gọi Close (kể cả khi bỏ dở) để xóa tệp tạm.
func NewWriter(w io.Writer, sheet string, cols []Column) (*Writer, error) {
	if len(cols) == 0 || len(cols) > excelize.MaxColumns {
		return nil, fmt.Errorf("%w: số cột %d", errBadColumns, len(cols))
	}
	for i, c := range cols {
		if !validKind(c.Kind) {
			return nil, fmt.Errorf("%w: cột %d không có loại", errBadColumns, i+1)
		}
	}
	f := excelize.NewFile()
	wr, err := newWriter(f, w, sheet, cols)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return wr, nil
}

func newWriter(f *excelize.File, w io.Writer, sheet string, cols []Column) (*Writer, error) {
	if err := f.SetSheetName(f.GetSheetName(0), sheet); err != nil {
		return nil, fmt.Errorf("%w: tên trang: %w", errBadColumns, err)
	}
	st, err := newStyles(f)
	if err != nil {
		return nil, err
	}
	sw, err := f.NewStreamWriter(sheet)
	if err != nil {
		return nil, fmt.Errorf("%w: mở trang: %w", errBadColumns, err)
	}
	// Từ cột cuối về cột đầu: excelize (flatCols) đặt cột vừa sửa lên đầu danh sách <col>, nên sửa theo thứ tự tăng
	// dần sẽ ghi <col> giảm dần (trái lược đồ OOXML, Excel đòi sửa tệp).
	for i := len(cols) - 1; i >= 0; i-- {
		if cols[i].Width > 0 {
			if err := sw.SetColWidth(i+1, i+1, float64(cols[i].Width)); err != nil {
				return nil, fmt.Errorf("%w: độ rộng cột %d: %w", errBadColumns, i+1, err)
			}
		}
		if s := st.cellStyle(cols[i].Kind); s != 0 {
			if err := sw.SetColStyle(i+1, i+1, s); err != nil {
				return nil, fmt.Errorf("kiểu cột %d: %w", i+1, err)
			}
		}
	}
	header := make([]any, len(cols))
	for i, c := range cols {
		header[i] = excelize.Cell{StyleID: st.header, Value: textValue(c.Title)}
	}
	if err := sw.SetPanes(&excelize.Panes{
		Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft",
	}); err != nil {
		return nil, fmt.Errorf("cố định dòng tiêu đề: %w", err)
	}
	if err := sw.SetRow("A1", header); err != nil {
		return nil, fmt.Errorf("ghi tiêu đề: %w", err)
	}
	return &Writer{dst: w, f: f, sw: sw, cols: len(cols), st: st, maxRows: MaxDataRows}, nil
}

func validKind(k Kind) bool { return k >= KindString && k <= KindDate }

// Row ghi một dòng dữ liệu; số ô phải bằng số cột. Lỗi khi sai số ô, ô có loại lạ, ngày ngoài khoảng Excel (trước
// 01/03/1900 hay từ năm 10000), vượt MaxDataRows (ErrTooManyRows, cùng sentinel với Read) hay Writer đã đóng; dòng lỗi không được ghi.
func (w *Writer) Row(cells ...Cell) error {
	if err := w.next(); err != nil {
		return err
	}
	if len(cells) != w.cols {
		return fmt.Errorf("%w: %d ô, mong %d", errBadRow, len(cells), w.cols)
	}
	vals := make([]any, len(cells))
	for i, c := range cells {
		v, err := w.value(c)
		if err != nil {
			return fmt.Errorf("%w: ô %d: %w", errBadRow, i+1, err)
		}
		vals[i] = v
	}
	if err := w.write(vals); err != nil {
		return err
	}
	w.rows++
	return nil
}

// Note ghi một dòng ghi chú in nghiêng ở cột A (qua Sanitize), thường ở cuối bảng; không tính vào Rows.
func (w *Writer) Note(text string) error {
	if err := w.next(); err != nil {
		return err
	}
	return w.write([]any{excelize.Cell{StyleID: w.st.note, Value: textValue(text)}})
}

// Rows trả số dòng dữ liệu đã ghi bằng Row (không kể tiêu đề, ghi chú).
func (w *Writer) Rows() int { return w.rows }

// Close kết thúc trang, ghi tệp .xlsx ra đích của NewWriter và xóa tệp tạm. Gọi lần hai trả lỗi.
func (w *Writer) Close() error {
	if w.closed {
		return errClosed
	}
	w.closed = true
	defer func() { _ = w.f.Close() }()
	if err := w.sw.Flush(); err != nil {
		return fmt.Errorf("kết thúc trang: %w", err)
	}
	if _, err := w.f.WriteTo(w.dst); err != nil {
		return fmt.Errorf("ghi tệp xlsx: %w", err)
	}
	return nil
}

// next kiểm Writer còn mở và còn chỗ cho một dòng.
func (w *Writer) next() error {
	if w.closed {
		return errClosed
	}
	if w.used >= w.maxRows {
		return ErrTooManyRows
	}
	return nil
}

func (w *Writer) write(vals []any) error {
	ref, err := excelize.CoordinatesToCellName(1, w.used+2)
	if err != nil {
		return fmt.Errorf("ô đầu dòng: %w", err)
	}
	if err := w.sw.SetRow(ref, vals); err != nil {
		return fmt.Errorf("ghi dòng: %w", err)
	}
	w.used++
	return nil
}

// value đổi một ô sang giá trị cho StreamWriter: chữ qua Sanitize (ô inlineStr), số nguyên giữ nguyên int64, ngày là
// số ngày của Excel tự tính theo giờ Việt Nam. nil là ô trống.
func (w *Writer) value(c Cell) (any, error) {
	switch c.Kind {
	case kindEmpty:
		return nil, nil
	case KindString:
		return excelize.Cell{Value: textValue(c.S)}, nil
	case KindInt:
		return excelize.Cell{Value: c.I}, nil
	case KindMoney:
		return excelize.Cell{StyleID: w.st.money, Value: c.I}, nil
	case KindDate:
		serial, err := excelSerial(c.T)
		if err != nil {
			return nil, err
		}
		return excelize.Cell{StyleID: w.st.date, Value: serial}, nil
	}
	return nil, fmt.Errorf("loại ô %d lạ", c.Kind)
}

// excelSerial là số ngày của Excel (hệ 1900) của t theo giờ Việt Nam: (t − 30/12/1899 00:00 VN) / 24 giờ. Tính từ
// giây Unix (time.Duration chỉ chứa được khoảng 292 năm), không phụ thuộc cách excelize đổi múi giờ.
func excelSerial(t time.Time) (float64, error) {
	if t.Before(minDate) || !t.Before(endDate) {
		return 0, errors.New("ngày ngoài khoảng Excel")
	}
	secs := t.Unix() - excelEpoch.Unix()
	days := secs / 86400
	rest := float64(secs%86400)*1e9 + float64(t.Nanosecond())
	v := float64(days) + rest/86400e9
	return v, nil
}

// textValue là giá trị ô chữ của s: Sanitize(s), ghi thành ô chữ inlineStr. excelize thoát các dãy "_xHHHH_" (Excel
// đọc dãy đó thành một ký tự) bằng cách tìm từng dãy không chồng nhau, nên hai dãy dùng chung một dấu "_" (vd
// "_x0056_x0041_") bị thoát thiếu và Excel hiện sai chữ. Khi gặp chỗ nối như vậy, chuỗi được tách thành nhiều đoạn rich
// text (không định dạng) ngay trước dấu "_" dùng chung: mỗi đoạn không còn dãy chồng nhau, excelize thoát đúng, Excel
// giải mã từng đoạn rồi nối lại ra đúng chữ.
func textValue(s string) any {
	s = Sanitize(s)
	cuts := xstringChains(s)
	if len(cuts) == 0 {
		return s
	}
	runs := make([]excelize.RichTextRun, 0, len(cuts)+1)
	prev := 0
	for _, c := range cuts {
		runs = append(runs, excelize.RichTextRun{Text: s[prev:c]})
		prev = c
	}
	return append(runs, excelize.RichTextRun{Text: s[prev:]})
}

// xstringChains trả các vị trí byte i mà một dãy "_xHHHH_" bắt đầu ở i và một dãy khác kết thúc ở i (dùng chung dấu
// "_" ở i), theo thứ tự tăng dần.
func xstringChains(s string) []int {
	var cuts []int
	for i := 0; i+7 <= len(s); i++ {
		if isXstringEscape(s[i:i+7]) && i+13 <= len(s) && isXstringEscape(s[i+6:i+13]) {
			cuts = append(cuts, i+6)
		}
	}
	return cuts
}

// isXstringEscape: m (7 byte) có dạng "_xHHHH_" (H là chữ số thập lục phân), như biểu thức của excelize.
func isXstringEscape(m string) bool {
	if m[0] != '_' || m[1] != 'x' || m[6] != '_' {
		return false
	}
	for _, c := range []byte(m[2:6]) {
		if !isHexDigit(c) {
			return false
		}
	}
	return true
}

func isHexDigit(c byte) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
}
