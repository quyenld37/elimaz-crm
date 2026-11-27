package xlsx_test

import (
	"bytes"
	"encoding/xml"
	"errors"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"

	"elimaz.com.vn/crm/internal/platform/xlsx"
	"elimaz.com.vn/crm/internal/platform/xlsx/xlsxtest"
)

const sheetPart = "xl/worksheets/sheet1.xml"

func vn() *time.Location { return time.FixedZone("ICT", 7*3600) }

// writeBook ghi các dòng rồi đọc lại bằng xlsxtest.
func writeBook(t *testing.T, cols []xlsx.Column, rows [][]xlsx.Cell, notes ...string) ([]byte, *xlsxtest.Book) {
	t.Helper()
	var buf bytes.Buffer
	w, err := xlsx.NewWriter(&buf, "Báo cáo", cols)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	for _, r := range rows {
		if err := w.Row(r...); err != nil {
			t.Fatalf("Row: %v", err)
		}
	}
	for _, n := range notes {
		if err := w.Note(n); err != nil {
			t.Fatalf("Note: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	b, err := xlsxtest.Read(buf.Bytes())
	if err != nil {
		t.Fatalf("xlsxtest.Read: %v", err)
	}
	return buf.Bytes(), b
}

func threeCols() []xlsx.Column {
	return []xlsx.Column{
		{Title: "Tên", Width: 30, Kind: xlsx.KindString},
		{Title: "Tiền", Width: 12, Kind: xlsx.KindMoney},
		{Title: "Ngày", Width: 18, Kind: xlsx.KindDate},
	}
}

func style(t *testing.T, b *xlsxtest.Book, i int) xlsxtest.Style {
	t.Helper()
	s, err := b.Style(i)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestWriterBlocksFormulas(t *testing.T) {
	t.Parallel()
	texts := []string{`=HYPERLINK("http://x","bấm")`, "+84912345678", "-5", "@SUM(A1)", "\tX", "\rY", "\nZ", "Bình thường"}
	want := []string{`'=HYPERLINK("http://x","bấm")`, "'+84912345678", "'-5", "'@SUM(A1)", "'\tX", "'\rY", "'\nZ", "Bình thường"}
	at := time.Date(2026, 11, 18, 10, 30, 0, 0, vn())
	var rows [][]xlsx.Cell
	for _, s := range texts {
		rows = append(rows, []xlsx.Cell{xlsx.Str(s), xlsx.Money(150000), xlsx.Date(at)})
	}
	_, b := writeBook(t, threeCols(), rows)
	if fp := b.FormulaParts(); len(fp) != 0 {
		t.Fatalf("có phần tử <f> ở: %v", fp)
	}
	if slices.ContainsFunc(b.Parts(), func(p string) bool {
		return strings.Contains(p, "hyperlink") || strings.Contains(p, "externalLink")
	}) {
		t.Fatal("tệp có phần siêu liên kết")
	}
	sheet, _ := b.Part(sheetPart)
	if bytes.Contains(sheet, []byte("<hyperlink")) {
		t.Fatal("trang có <hyperlink>")
	}
	got, err := b.Rows(sheetPart)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(texts)+1 {
		t.Fatalf("%d dòng, mong %d", len(got), len(texts)+1)
	}
	for i, r := range got[1:] {
		checkFormulaRow(t, b, r, want[i])
	}
}

// checkFormulaRow: dòng (chữ, tiền 150000, ngày 18/11/2026 10:30 VN) đọc lại đúng kiểu ô và định dạng.
func checkFormulaRow(t *testing.T, b *xlsxtest.Book, r xlsxtest.Row, want string) {
	t.Helper()
	if len(r.Cells) != 3 {
		t.Fatalf("dòng %d có %d ô", r.N, len(r.Cells))
	}
	s, m, d := r.Cells[0], r.Cells[1], r.Cells[2]
	if s.Type != "inlineStr" || s.Value != want || s.Formula {
		t.Errorf("ô chữ %s = %+v, mong chữ %q", s.Ref, s, want)
	}
	isNumber := func(c xlsxtest.Cell) bool { return c.Type == "n" || c.Type == "" }
	if !isNumber(m) || m.Value != "150000" || style(t, b, m.Style).NumFmt != "#,##0" {
		t.Errorf("ô tiền %s = %+v (định dạng %q)", m.Ref, m, style(t, b, m.Style).NumFmt)
	}
	if !isNumber(d) || d.Value != "46344.4375" || style(t, b, d.Style).NumFmt != "dd/mm/yyyy hh:mm" {
		t.Errorf("ô ngày %s = %+v (định dạng %q)", d.Ref, d, style(t, b, d.Style).NumFmt)
	}
}

func TestWriterHeaderBoldFrozenAndNoteItalic(t *testing.T) {
	t.Parallel()
	_, b := writeBook(t, threeCols(), [][]xlsx.Cell{{xlsx.Str("a"), xlsx.Empty(), xlsx.Date(time.Time{})}},
		"Đã cắt ở 200.000 dòng")
	rows, err := b.Rows(sheetPart)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("%d dòng, mong 3 (tiêu đề, dữ liệu, ghi chú)", len(rows))
	}
	for i, c := range rows[0].Cells {
		if c.Value != threeCols()[i].Title || c.Type != "inlineStr" || !style(t, b, c.Style).Bold {
			t.Errorf("tiêu đề %s = %+v", c.Ref, c)
		}
	}
	if len(rows[1].Cells) != 1 {
		t.Errorf("ô trống vẫn được ghi: %+v", rows[1].Cells)
	}
	note := rows[2]
	if note.N != 3 || len(note.Cells) != 1 || note.Cells[0].Value != "Đã cắt ở 200.000 dòng" ||
		!style(t, b, note.Cells[0].Style).Italic {
		t.Errorf("dòng ghi chú = %+v", note)
	}
	sheet, _ := b.Part(sheetPart)
	var ws struct {
		Pane struct {
			YSplit string `xml:"ySplit,attr"`
			State  string `xml:"state,attr"`
			Top    string `xml:"topLeftCell,attr"`
		} `xml:"sheetViews>sheetView>pane"`
		Cols []struct {
			Min   string `xml:"min,attr"`
			Width string `xml:"width,attr"`
		} `xml:"cols>col"`
	}
	if err := xml.Unmarshal(sheet, &ws); err != nil {
		t.Fatal(err)
	}
	if ws.Pane.YSplit != "1" || ws.Pane.State != "frozen" || ws.Pane.Top != "A2" {
		t.Errorf("dòng tiêu đề không cố định: %+v", ws.Pane)
	}
	if len(ws.Cols) != 3 || ws.Cols[0].Min != "1" || ws.Cols[0].Width != "30" || ws.Cols[1].Min != "2" ||
		ws.Cols[2].Min != "3" || ws.Cols[2].Width != "18" {
		t.Errorf("độ rộng cột: %+v", ws.Cols)
	}
	wb, _ := b.Part("xl/workbook.xml")
	if !bytes.Contains(wb, []byte(`name="Báo cáo"`)) {
		t.Errorf("tên trang sai: %s", wb)
	}
}

func TestWriterIntAndBigMoneyExact(t *testing.T) {
	t.Parallel()
	cols := []xlsx.Column{{Title: "Số", Kind: xlsx.KindInt}, {Title: "Tiền", Kind: xlsx.KindMoney}}
	_, b := writeBook(t, cols, [][]xlsx.Cell{{xlsx.Int(-42), xlsx.Money(9_007_199_254_740_993)}})
	rows, err := b.Rows(sheetPart)
	if err != nil {
		t.Fatal(err)
	}
	c := rows[1].Cells
	if c[0].Value != "-42" || c[1].Value != "9007199254740993" {
		t.Fatalf("ô số = %+v", c)
	}
	if style(t, b, c[0].Style).NumFmt == "#,##0" {
		t.Error("ô số nguyên không được mang định dạng tiền")
	}
}

func TestWriterDateSerial(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, 11, 18, 10, 30, 0, 0, vn()), "46344.4375"},
		{time.Date(2026, 11, 18, 3, 30, 0, 0, time.UTC), "46344.4375"}, // cùng thời điểm, múi giờ khác
		{time.Date(1900, 3, 1, 0, 0, 0, 0, vn()), "61"},
		{time.Date(2026, 1, 1, 0, 0, 0, 0, vn()), "46023"},
	} {
		_, b := writeBook(t, []xlsx.Column{{Title: "Ngày", Kind: xlsx.KindDate}}, [][]xlsx.Cell{{xlsx.Date(c.at)}})
		rows, err := b.Rows(sheetPart)
		if err != nil {
			t.Fatal(err)
		}
		if got := rows[1].Cells[0].Value; got != c.want {
			t.Errorf("Date(%v) = %s, mong %s", c.at, got, c.want)
		}
	}
	var buf bytes.Buffer
	w, err := xlsx.NewWriter(&buf, "S", []xlsx.Column{{Title: "Ngày", Kind: xlsx.KindDate}})
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []time.Time{
		time.Date(1900, 2, 28, 23, 59, 0, 0, vn()),
		time.Date(10000, 1, 1, 0, 0, 0, 0, vn()),
	} {
		if err := w.Row(xlsx.Date(bad)); err == nil {
			t.Errorf("Date(%v) ngoài khoảng Excel mà không lỗi", bad)
		}
	}
}

func TestWriterStripsControlAndTruncates(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("ệ", 40000)
	emoji := strings.Repeat("\U0001F600", 20000)
	_, b := writeBook(t, []xlsx.Column{{Title: "A", Kind: xlsx.KindString}},
		[][]xlsx.Cell{{xlsx.Str("a\x01b")}, {xlsx.Str(long)}, {xlsx.Str(emoji)}, {xlsx.Str("_x0041_ và _x005F_")}})
	rows, err := b.Rows(sheetPart)
	if err != nil {
		t.Fatalf("XML không đọc được: %v", err)
	}
	if got := rows[1].Cells[0].Value; got != "ab" {
		t.Errorf("ký tự điều khiển: %q", got)
	}
	got := rows[2].Cells[0].Value
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != xlsx.MaxCellLength {
		t.Errorf("chuỗi dài: %d ký tự, UTF-8 hợp lệ %v", utf8.RuneCountInString(got), utf8.ValidString(got))
	}
	if got := rows[3].Cells[0].Value; got != xlsx.Sanitize(emoji) || utf8.RuneCountInString(got) != xlsx.MaxCellLength/2 {
		t.Errorf("chuỗi emoji: %d ký tự", utf8.RuneCountInString(got))
	}
	if got := rows[4].Cells[0].Value; got != "_x0041_ và _x005F_" {
		t.Errorf("dãy _xHHHH_ đọc lại = %q", got)
	}
}

// TestWriterChainedXstringEscapes: hai dãy "_xHHHH_" dùng chung dấu "_" (fuzz tìm ra: excelize thoát thiếu) vẫn đọc
// lại đúng chữ bằng xlsxtest (giải mã từng phần tử <t> như ECMA-376). Không so bằng excelize: bộ đọc của excelize
// giải mã chuỗi đã nối của các đoạn rich text nên tự đọc sai chính dạng này (ghi ở "Việc phát hiện thêm").
func TestWriterChainedXstringEscapes(t *testing.T) {
	t.Parallel()
	texts := []string{"_x0056_x0041_", "_x005F_x0041_x0042_", "a_x0041_x0042_x0043_b", "_x0041_ _x0042_"}
	var rows [][]xlsx.Cell
	for _, s := range texts {
		rows = append(rows, []xlsx.Cell{xlsx.Str(s)})
	}
	_, b := writeBook(t, []xlsx.Column{{Title: "_x0041_x0042_", Kind: xlsx.KindString}}, rows)
	got, err := b.Rows(sheetPart)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Cells[0].Value != "_x0041_x0042_" {
		t.Errorf("tiêu đề đọc lại %q", got[0].Cells[0].Value)
	}
	for i, s := range texts {
		if v := got[i+1].Cells[0].Value; v != s {
			t.Errorf("xlsxtest đọc lại %q, mong %q", v, s)
		}
	}
	sheet, _ := b.Part(sheetPart)
	if !bytes.Contains(sheet, []byte("<r><t>_x0056</t></r>")) {
		t.Errorf("chuỗi nối không được tách thành đoạn rich text: %s", sheet)
	}
}

func TestWriterRejectsBadUse(t *testing.T) {
	t.Parallel()
	if _, err := xlsx.NewWriter(&bytes.Buffer{}, "S", nil); err == nil {
		t.Error("không có cột mà không lỗi")
	}
	if _, err := xlsx.NewWriter(&bytes.Buffer{}, "S", []xlsx.Column{{Title: "A"}}); err == nil {
		t.Error("cột không có kiểu mà không lỗi")
	}
	if _, err := xlsx.NewWriter(&bytes.Buffer{}, "a/b", threeCols()); err == nil {
		t.Error("tên trang không hợp lệ mà không lỗi")
	}
	var buf bytes.Buffer
	w, err := xlsx.NewWriter(&buf, "S", threeCols())
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Row(xlsx.Str("a"), xlsx.Money(1)); err == nil {
		t.Error("thiếu ô mà không lỗi")
	}
	if err := w.Row(xlsx.Str("a"), xlsx.Money(1), xlsx.Empty(), xlsx.Empty()); err == nil {
		t.Error("thừa ô mà không lỗi")
	}
	if err := w.Row(xlsx.Cell{Kind: xlsx.Kind(99)}, xlsx.Empty(), xlsx.Empty()); err == nil {
		t.Error("kiểu ô lạ mà không lỗi")
	}
	if err := w.Row(xlsx.Str("ok"), xlsx.Money(1), xlsx.Empty()); err != nil {
		t.Fatal(err)
	}
	if w.Rows() != 1 {
		t.Errorf("Rows() = %d, mong 1 (dòng sai không đếm)", w.Rows())
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Row(xlsx.Str("a"), xlsx.Empty(), xlsx.Empty()); err == nil {
		t.Error("Row sau Close mà không lỗi")
	}
	if err := w.Close(); err == nil {
		t.Error("Close hai lần mà không lỗi")
	}
	f, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("excelize mở tệp: %v", err)
	}
	defer func() { _ = f.Close() }()
	if v, _ := f.GetCellValue("S", "A2"); v != "ok" {
		t.Errorf("excelize đọc A2 = %q", v)
	}
	b, err := xlsxtest.Read(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range b.Parts() {
		if p == "xl/calcChain.xml" || p == "xl/vbaProject.bin" || strings.HasPrefix(p, "xl/externalLinks/") {
			t.Errorf("tệp có phần cấm %s", p)
		}
	}
}

func TestWriterTooManyRows(t *testing.T) {
	t.Parallel()
	w, err := xlsx.NewWriter(&bytes.Buffer{}, "S", []xlsx.Column{{Title: "A", Kind: xlsx.KindInt}})
	if err != nil {
		t.Fatal(err)
	}
	xlsx.SetMaxDataRowsForTest(w, 3)
	for i := range 3 {
		if err := w.Row(xlsx.Int(int64(i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Row(xlsx.Int(9)); !errors.Is(err, xlsx.ErrTooManyRows) {
		t.Fatalf("dòng thứ 4: %v, mong ErrTooManyRows", err)
	}
	if err := w.Note("x"); !errors.Is(err, xlsx.ErrTooManyRows) {
		t.Fatalf("ghi chú vượt trang: %v, mong ErrTooManyRows", err)
	}
	if xlsx.MaxDataRows != 1_048_575 {
		t.Fatalf("MaxDataRows = %d", xlsx.MaxDataRows)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestWriterStreamsLargeTable: 200.000 dòng × 10 cột không giữ cả bảng trong bộ nhớ (kế hoạch: HeapInuse tăng
// ≤ 50 MB; dưới 10 s trên máy dev — thời gian chỉ in ra, không làm tiêu chí, Task 400). HeapInuse đo sau
// runtime.GC() (bộ nhớ còn giữ, không lẫn rác chưa dọn) ở mỗi 20.000 dòng và đúng lúc tệp được đẩy ra đích (khi đó
// excelize đang giữ cả tệp nén trong bộ nhớ: đỉnh thật của Close).
func TestWriterStreamsLargeTable(t *testing.T) {
	cols := make([]xlsx.Column, 10)
	kinds := []xlsx.Kind{xlsx.KindString, xlsx.KindInt, xlsx.KindMoney, xlsx.KindDate}
	for i := range cols {
		cols[i] = xlsx.Column{Title: "Cột", Kind: kinds[i%4]}
	}
	at := time.Date(2026, 11, 18, 10, 30, 0, 0, vn())
	before := liveHeap()
	dst := &countWriter{}
	start := time.Now()
	w, err := xlsx.NewWriter(dst, "S", cols)
	if err != nil {
		t.Fatal(err)
	}
	row := make([]xlsx.Cell, 10)
	var peak uint64
	for i := range 200_000 {
		for j := range row {
			switch j % 4 {
			case 0:
				row[j] = xlsx.Str("Đầm lụa hoa nhí mã " + strings.Repeat("x", j))
			case 1:
				row[j] = xlsx.Int(int64(i))
			case 2:
				row[j] = xlsx.Money(int64(i) * 1000)
			default:
				row[j] = xlsx.Date(at)
			}
		}
		if err := w.Row(row...); err != nil {
			t.Fatal(err)
		}
		if i%20_000 == 0 {
			peak = max(peak, liveHeap())
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	peak = max(peak, dst.heapAtFirstWrite)
	t.Logf("200.000 dòng: %v, tệp %d byte, HeapInuse trước %d MB, đỉnh %d MB, lúc đẩy tệp %d MB",
		time.Since(start), dst.n, before>>20, peak>>20, dst.heapAtFirstWrite>>20)
	if w.Rows() != 200_000 || dst.heapAtFirstWrite == 0 {
		t.Fatalf("Rows() = %d, đã đo lúc đẩy tệp: %v", w.Rows(), dst.heapAtFirstWrite != 0)
	}
	if peak > before && peak-before > 50<<20 {
		t.Fatalf("HeapInuse tăng %d MB, quá 50 MB", (peak-before)>>20)
	}
}

// liveHeap là HeapInuse sau một lượt GC.
func liveHeap() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapInuse
}

// countWriter đếm byte và đo liveHeap ở lần ghi đầu tiên.
type countWriter struct {
	n                int64
	heapAtFirstWrite uint64
}

func (c *countWriter) Write(p []byte) (int, error) {
	if c.n == 0 {
		c.heapAtFirstWrite = liveHeap()
	}
	c.n += int64(len(p))
	return len(p), nil
}
