package xlsx_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"elimaz.com.vn/crm/internal/platform/xlsx"
)

func fixture(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "xlsx", name))
	if err != nil {
		t.Fatalf("đọc tệp thử (sinh bằng go run ./testdata/xlsx/gen): %v", err)
	}
	return b
}

func read(b []byte, lim xlsx.Limits, header ...string) ([]xlsx.Row, []xlsx.CellError, error) {
	return xlsx.Read(bytes.NewReader(b), int64(len(b)), lim, header) //nolint:wrapcheck // test kiểm bằng errors.Is
}

// written ghi một trang bằng xlsx.Write và trả byte của tệp.
func written(t testing.TB, header []string, rows [][]any) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := xlsx.Write(&buf, []xlsx.Sheet{{Name: "Dữ liệu", Header: header, Rows: rows}}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return buf.Bytes()
}

func TestReadWriteRoundTripVietnamese(t *testing.T) {
	header := []string{"Tên*", "Ghi chú", "Công thức"}
	var rows [][]any
	for i := range 1000 {
		rows = append(rows, []any{
			fmt.Sprintf("Đầm lụa Hà Nội số %d — ưu đãi", i), "Áo sơ mi\nnhiều dòng: ỗ ữ ặ ĩ", []string{"=1+1", "-giảm", "@x", "+84"}[i%4],
		})
	}
	got, cellErrs, err := read(written(t, header, rows), xlsx.Limits{}, header...)
	if err != nil || len(cellErrs) > 0 {
		t.Fatalf("Read: %v, lỗi ô %v", err, cellErrs)
	}
	if len(got) != 1000 {
		t.Fatalf("%d dòng, mong 1000", len(got))
	}
	for i, r := range got {
		if r.Number != i+2 {
			t.Fatalf("dòng %d có Number %d", i, r.Number)
		}
		for j, h := range header {
			if r.Cells[h] != rows[i][j] {
				t.Fatalf("dòng %d cột %s = %q, mong %q", r.Number, h, r.Cells[h], rows[i][j])
			}
		}
	}
}

func TestReadFormulaNotAllowed(t *testing.T) {
	rows, cellErrs, err := read(fixture(t, "co-cong-thuc.xlsx"), xlsx.Limits{}, "Tên", "Liên kết")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := []xlsx.CellError{{Row: 2, Column: "Liên kết", Code: xlsx.CodeFormulaNotAllowed}}
	if !slices.Equal(cellErrs, want) {
		t.Fatalf("lỗi ô %v, mong %v", cellErrs, want)
	}
	if len(rows) != 2 || rows[0].Cells["Liên kết"] != "" || rows[0].Cells["Tên"] != "Áo" {
		t.Fatalf("dòng có công thức phải bỏ giá trị ô đó: %v", rows)
	}
	if rows[1].Cells["Liên kết"] != "=HYPERLINK" {
		t.Fatalf("ô chữ mở bằng = (không phải công thức) phải giữ: %q", rows[1].Cells["Liên kết"])
	}
}

func TestReadNotXlsx(t *testing.T) {
	var zipNoWorkbook bytes.Buffer
	zw := zip.NewWriter(&zipNoWorkbook)
	w, _ := zw.Create("a.txt")
	_, _ = io.WriteString(w, "xin chào")
	_ = zw.Close()
	for name, b := range map[string][]byte{
		"html đổi đuôi":    fixture(t, "khong-phai-xlsx.xlsx"),
		"rỗng":             {},
		"zip không sổ":     zipNoWorkbook.Bytes(),
		"đầu zip rồi rác":  append([]byte("PK\x03\x04"), bytes.Repeat([]byte{0xff}, 100)...),
		"tệp ole (có mật)": append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, make([]byte, 504)...),
	} {
		if _, _, err := read(b, xlsx.Limits{}, "Tên"); !errors.Is(err, xlsx.ErrNotXlsx) {
			t.Errorf("%s: lỗi %v, mong ErrNotXlsx", name, err)
		}
	}
}

// measure chạy f, trả thời gian và số byte cấp phát thêm (TotalAlloc, kể cả phần đã được thu gom).
func measure(f func()) (time.Duration, uint64) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	f()
	d := time.Since(start)
	runtime.ReadMemStats(&after)
	return d, after.TotalAlloc - before.TotalAlloc
}

func TestReadZipBomb(t *testing.T) {
	b := fixture(t, "bom-zip.xlsx")
	var err error
	d, alloc := measure(func() { _, _, err = read(b, xlsx.Limits{}, "Tên") })
	if !errors.Is(err, xlsx.ErrTooLarge) {
		t.Fatalf("lỗi %v, mong ErrTooLarge", err)
	}
	if d > 2*time.Second || alloc > 100<<20 {
		t.Fatalf("mất %v, cấp %d MB; mong dưới 2 s, 100 MB", d, alloc>>20)
	}
	t.Logf("bom zip %d KB: từ chối sau %v, cấp thêm %d KB", len(b)>>10, d, alloc>>10)
}

// TestReadZipBombLyingHeader: header zip khai trang chỉ 1 KB nhưng dữ liệu nén giải ra 1 GiB. archive/zip dừng ở số
// byte đã khai, nên Read báo lỗi mà không giải nén hết.
func TestReadZipBombLyingHeader(t *testing.T) {
	src, err := zip.NewReader(bytes.NewReader(fixture(t, "bom-zip.xlsx")), int64(len(fixture(t, "bom-zip.xlsx"))))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range src.File {
		h := f.FileHeader
		if strings.HasSuffix(h.Name, "sheet1.xml") {
			h.UncompressedSize64 = 1 << 10
		}
		w, err := zw.CreateRaw(&h)
		if err != nil {
			t.Fatal(err)
		}
		r, _ := f.OpenRaw()
		if _, err := io.Copy(w, r); err != nil {
			t.Fatal(err)
		}
	}
	_ = zw.Close()
	d, alloc := measure(func() { _, _, err = read(buf.Bytes(), xlsx.Limits{}, "Tên") })
	if !errors.Is(err, xlsx.ErrNotXlsx) && !errors.Is(err, xlsx.ErrTooLarge) {
		t.Fatalf("lỗi %v, mong ErrNotXlsx hoặc ErrTooLarge", err)
	}
	if d > 2*time.Second || alloc > 100<<20 {
		t.Fatalf("mất %v, cấp %d MB; mong dưới 2 s, 100 MB", d, alloc>>20)
	}
}

func TestReadSizeLimits(t *testing.T) {
	header := []string{"Tên"}
	if _, _, err := xlsx.Read(bytes.NewReader(nil), 5<<20+1, xlsx.Limits{}, header); !errors.Is(err, xlsx.ErrTooLarge) {
		t.Errorf("tệp 5 MiB + 1: %v, mong ErrTooLarge", err)
	}
	small := written(t, header, [][]any{{"Áo"}})
	if _, _, err := read(small, xlsx.Limits{MaxBytes: int64(len(small) - 1)}, header...); !errors.Is(err, xlsx.ErrTooLarge) {
		t.Errorf("MaxBytes nhỏ hơn tệp: %v, mong ErrTooLarge", err)
	}
	if _, _, err := read(small, xlsx.Limits{MaxUnzipped: 1 << 10}, header...); !errors.Is(err, xlsx.ErrTooLarge) {
		t.Errorf("MaxUnzipped 1 KB: %v, mong ErrTooLarge", err)
	}
}

func TestReadTooManyRows(t *testing.T) {
	header := []string{"SKU"}
	rows := make([][]any, 20001)
	for i := range rows {
		rows[i] = []any{fmt.Sprintf("SKU%05d", i)}
	}
	b := written(t, header, rows)
	if _, _, err := read(b, xlsx.Limits{}, header...); !errors.Is(err, xlsx.ErrTooManyRows) {
		t.Fatalf("20.001 dòng: %v, mong ErrTooManyRows", err)
	}
	got, _, err := read(written(t, header, rows[:20000]), xlsx.Limits{}, header...)
	if err != nil || len(got) != 20000 {
		t.Fatalf("20.000 dòng: %d dòng, lỗi %v", len(got), err)
	}
}

func TestReadHeader(t *testing.T) {
	b := written(t, []string{" Tên* ", "Giá", "", "Thêm"}, [][]any{{"Áo", "1", "bỏ", "x"}, {"", "", "", ""}, {"Váy", "", "", ""}})
	rows, _, err := read(b, xlsx.Limits{}, "Tên*", "Giá")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// Cột thừa có tiêu đề vẫn được đọc; cột không tiêu đề bị bỏ; dòng trống bị bỏ, số dòng giữ đúng dòng Excel.
	if len(rows) != 2 || rows[0].Number != 2 || rows[1].Number != 4 {
		t.Fatalf("dòng: %+v", rows)
	}
	if want := map[string]string{"Tên*": "Áo", "Giá": "1", "Thêm": "x"}; !maps.Equal(rows[0].Cells, want) {
		t.Fatalf("ô dòng 2 = %v, mong %v", rows[0].Cells, want)
	}
	if want := map[string]string{"Tên*": "Váy"}; !maps.Equal(rows[1].Cells, want) {
		t.Fatalf("ô dòng 4 = %v, mong %v", rows[1].Cells, want)
	}
	for name, c := range map[string]struct {
		file   []byte
		header []string
	}{
		"thiếu cột":    {b, []string{"Tên*", "Giá", "SKU*"}},
		"cột trùng":    {written(t, []string{"Tên*", "Tên*"}, nil), []string{"Tên*"}},
		"không dòng":   {written(t, nil, nil), []string{"Tên*"}},
		"sai tên cột":  {b, []string{"Tên"}},
		"tiêu đề rỗng": {b, []string{""}},
	} {
		if _, _, err := read(c.file, xlsx.Limits{}, c.header...); !errors.Is(err, xlsx.ErrHeaderMismatch) {
			t.Errorf("%s: %v, mong ErrHeaderMismatch", name, err)
		}
	}
}

func TestReadCellTooLong(t *testing.T) {
	b := written(t, []string{"Mô tả", "Tên"}, [][]any{{strings.Repeat("ộ", 2001), "Áo"}, {strings.Repeat("ộ", 2000), "Váy"}})
	rows, cellErrs, err := read(b, xlsx.Limits{}, "Mô tả", "Tên")
	if err != nil {
		t.Fatal(err)
	}
	if want := []xlsx.CellError{{Row: 2, Column: "Mô tả", Code: xlsx.CodeCellTooLong}}; !slices.Equal(cellErrs, want) {
		t.Fatalf("lỗi ô %v, mong %v", cellErrs, want)
	}
	if rows[0].Cells["Mô tả"] != "" || rows[0].Cells["Tên"] != "Áo" || len([]rune(rows[1].Cells["Mô tả"])) != 2000 {
		t.Fatal("ô quá dài phải bị bỏ giá trị, ô đủ dài phải giữ")
	}
}

// TestReadOnlyFirstSheetRawNumbers: chỉ đọc trang đầu; ô số đọc giá trị thô, không theo định dạng hiển thị (350000
// định dạng "#,##0" vẫn ra "350000", không ra "350,000").
func TestReadOnlyFirstSheetRawNumbers(t *testing.T) {
	f := excelize.NewFile()
	_ = f.SetSheetName("Sheet1", "Đầu")
	_ = f.SetSheetRow("Đầu", "A1", &[]any{"Giá", "Ngày"})
	_ = f.SetCellValue("Đầu", "A2", 350000)
	style, _ := f.NewStyle(&excelize.Style{NumFmt: 3}) // #,##0
	_ = f.SetCellStyle("Đầu", "A2", "A2", style)
	_, _ = f.NewSheet("Sau")
	_ = f.SetSheetRow("Sau", "A1", &[]any{"Giá"})
	_ = f.SetCellValue("Sau", "A2", "từ trang sau")
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	rows, _, err := read(buf.Bytes(), xlsx.Limits{}, "Giá")
	if err != nil || len(rows) != 1 || rows[0].Cells["Giá"] != "350000" {
		t.Fatalf("dòng %+v, lỗi %v; mong một dòng Giá = 350000", rows, err)
	}
}

// part là một phần của tệp zip.
type part struct{ name, data string }

// fixtureParts trả các phần của co-cong-thuc.xlsx (một trang, B2 là công thức).
func fixtureParts(t *testing.T) []part {
	t.Helper()
	src := fixture(t, "co-cong-thuc.xlsx")
	zr, err := zip.NewReader(bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatal(err)
	}
	var out []part
	for _, f := range zr.File {
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		out = append(out, part{f.Name, string(b)})
	}
	return out
}

// zipParts ghi parts (theo thứ tự, được trùng tên) thành tệp zip.
func zipParts(t *testing.T, parts []part) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range parts {
		w, err := zw.Create(p.name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, p.data)
	}
	_ = zw.Close()
	return buf.Bytes()
}

func sheetXML(sheetData string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><worksheet ` +
		`xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` + sheetData + `</sheetData></worksheet>`
}

// replaced trả parts với phần name đổi thành data.
func replaced(parts []part, name, data string) []part {
	out := slices.Clone(parts)
	for i := range out {
		if out[i].name == name {
			out[i].data = data
		}
	}
	return out
}

// withSheet thay phần trang đầu của co-cong-thuc.xlsx bằng sheetData (phần trong thẻ <sheetData>).
func withSheet(t *testing.T, sheetData string) []byte {
	t.Helper()
	return zipParts(t, replaced(fixtureParts(t), "xl/worksheets/sheet1.xml", sheetXML(sheetData)))
}

const headerABC = `<row r="1"><c r="A1" t="inlineStr"><is><t>A</t></is></c><c r="B1" t="inlineStr"><is><t>B</t></is></c>` +
	`<c r="C1" t="inlineStr"><is><t>C</t></is></c></row>`

// TestReadFormulaKinds: công thức không giá trị đã tính, công thức dùng chung, công thức mảng, ô không có thuộc tính
// r (đánh số theo thứ tự) đều bị báo.
func TestReadFormulaKinds(t *testing.T) {
	b := withSheet(t, headerABC+
		`<row r="2"><c r="A2"><f>1+1</f></c><c r="B2" t="inlineStr"><is><t>ok</t></is></c></row>`+
		`<row r="3"><c r="B3"><f t="shared" si="0"/><v>2</v></c></row>`+
		`<row><c t="inlineStr"><is><t>x</t></is></c><c/><c><f t="array" ref="C4">SUM(A1)</f><v>3</v></c></row>`)
	rows, cellErrs, err := read(b, xlsx.Limits{}, "A", "B", "C")
	if err != nil {
		t.Fatal(err)
	}
	want := []xlsx.CellError{
		{Row: 2, Column: "A", Code: xlsx.CodeFormulaNotAllowed},
		{Row: 3, Column: "B", Code: xlsx.CodeFormulaNotAllowed},
		{Row: 4, Column: "C", Code: xlsx.CodeFormulaNotAllowed},
	}
	if !slices.Equal(cellErrs, want) {
		t.Fatalf("lỗi ô %v, mong %v", cellErrs, want)
	}
	if len(rows) != 3 || rows[0].Cells["B"] != "ok" || rows[1].Cells["B"] != "" || rows[2].Cells["A"] != "x" ||
		rows[2].Cells["C"] != "" {
		t.Fatalf("dòng %+v", rows)
	}
}

// sharedFormulas dựng trang có n dòng, mỗi dòng 3 ô công thức dùng chung không có ô gốc.
func sharedFormulas(t *testing.T, n int) []byte {
	t.Helper()
	var sb strings.Builder
	sb.WriteString(headerABC)
	for i := 2; i <= n+1; i++ {
		fmt.Fprintf(&sb, `<row r="%d"><c r="A%d"><f t="shared" si="0"/><v>1</v></c><c r="B%d"><f t="shared" si="0"/>`+
			`<v>1</v></c><c r="C%d"><f t="shared" si="0"/><v>1</v></c></row>`, i, i, i, i)
	}
	return withSheet(t, sb.String())
}

// TestReadManySharedFormulasLinear: ô công thức dùng chung không có ô gốc (excelize.GetCellFormula quét lại cả trang
// cho mỗi ô như vậy) được báo với thời gian tuyến tính: gấp 4 số ô thì thời gian gấp dưới 10 (bình phương là 16; -race làm tỉ lệ lệch lên khoảng 6).
// So tỉ lệ, không so số giây, để máy bận không làm test đỏ.
func TestReadManySharedFormulasLinear(t *testing.T) {
	small, large := sharedFormulas(t, 2500), sharedFormulas(t, 10000)
	best := func(b []byte, wantErrs int) time.Duration {
		var d time.Duration
		for k := range 3 {
			var (
				cellErrs []xlsx.CellError
				err      error
			)
			got, _ := measure(func() { _, cellErrs, err = read(b, xlsx.Limits{}, "A") })
			if err != nil || len(cellErrs) != wantErrs {
				t.Fatalf("%d lỗi ô, lỗi %v; mong %d", len(cellErrs), err, wantErrs)
			}
			if k == 0 || got < d {
				d = got
			}
		}
		return d
	}
	ds, dl := best(small, 7500), best(large, 30000)
	if dl > 10*ds {
		t.Fatalf("7.500 ô mất %v, 30.000 ô mất %v: tăng quá tuyến tính", ds, dl)
	}
	t.Logf("7.500 ô: %v; 30.000 ô: %v", ds, dl)
}

// TestReadFarColumnRows: dòng chỉ có ô ở cột XFD (không tiêu đề, Read coi là trống, không đếm vào MaxRows) làm excelize
// dựng mảng 16.384 ô cho mỗi dòng; 700 dòng như vậy (11,5 triệu ô) bị từ chối ngay ở lượt quét, trong khi 20.000 dòng
// trống có định dạng (ô rỗng ở cột A–Z) vẫn đọc được.
func TestReadFarColumnRows(t *testing.T) {
	var err error
	d, alloc := measure(func() { _, _, err = read(withSheet(t, farRows(700)), xlsx.Limits{}, "A") })
	if !errors.Is(err, xlsx.ErrTooLarge) {
		t.Fatalf("lỗi %v, mong ErrTooLarge", err)
	}
	if d > 2*time.Second || alloc > 100<<20 {
		t.Fatalf("mất %v, cấp %d MB", d, alloc>>20)
	}
	var styled strings.Builder
	styled.WriteString(headerABC)
	for i := 2; i <= 20001; i++ {
		fmt.Fprintf(&styled, `<row r="%d"><c r="A%d" s="1"/><c r="Z%d" s="1"/></row>`, i, i, i)
	}
	fmt.Fprintf(&styled, `<row r="20002"><c r="A20002" t="inlineStr"><is><t>cuối</t></is></c></row>`)
	rows, _, err := read(withSheet(t, styled.String()), xlsx.Limits{}, "A")
	if err != nil || len(rows) != 1 || rows[0].Number != 20002 {
		t.Fatalf("dòng trống có định dạng: %d dòng, lỗi %v", len(rows), err)
	}
}

// farRows: n dòng chỉ có ô ở cột XFD (16.384 ô phải dựng mỗi dòng).
func farRows(n int) string {
	var sb strings.Builder
	sb.WriteString(headerABC)
	for i := 2; i <= n+1; i++ {
		fmt.Fprintf(&sb, `<row r="%d"><c r="XFD%d"><v>1</v></c></row>`, i, i)
	}
	return sb.String()
}

// TestReadStructureMismatch (reviewer lần 2): những cách làm cho các bước kiểm của gói và excelize đọc hai trang khác
// nhau đều bị từ chối, hoặc vẫn bị trần số ô chặn.
func TestReadStructureMismatch(t *testing.T) {
	base := fixtureParts(t)
	far := sheetXML(farRows(700))
	// evil: trang nhỏ có công thức, để thử riêng phép kiểm cấu trúc (không bị trần số ô chặn trước).
	evil := sheetXML(headerABC + `<row r="2"><c r="A2"><f>1+1</f><v>2</v></c></row>`)
	const relsHead = `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
	const wbHead = `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`
	const sheetRel = `Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet"`
	const officeDoc = `Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"`
	// evilBook: _rels/.rels thay bằng rels (phải trỏ sổ thật về evil/workbook.xml); sổ evil có trang "Sheet1" là evil.
	evilBook := func(rels string) []part {
		return append(replaced(base, "_rels/.rels", relsHead+rels+`</Relationships>`),
			part{"evil/workbook.xml", wbHead + `<sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets></workbook>`},
			part{"evil/_rels/workbook.xml.rels", relsHead + `<Relationship Id="rId1" ` + sheetRel + ` Target="big.xml"/></Relationships>`},
			part{"evil/big.xml", evil})
	}
	for name, c := range map[string]struct {
		parts []part
		want  error
	}{
		"trang trùng tên, trang lớn sau": {append(slices.Clone(base), part{"xl/worksheets/sheet1.xml", far}), xlsx.ErrNotXlsx},
		"trang trùng tên kiểu Windows":   {append(slices.Clone(base), part{`xl\worksheets\sheet1.xml`, far}), xlsx.ErrNotXlsx},
		"trùng tên khác hoa thường":      {append(slices.Clone(base), part{"XL/Worksheets/Sheet1.xml", far}), xlsx.ErrNotXlsx},
		"sổ thật ở chỗ khác":             {evilBook(`<Relationship Id="rId1" ` + officeDoc + ` Target="evil/workbook.xml"/>`), xlsx.ErrNotXlsx},
		"kiểu quan hệ giả cùng đuôi": {replaced(base, "_rels/.rels", relsHead+
			`<Relationship Id="rId0" Type="http://evil/relationships/officeDocument" Target="xl/workbook.xml"/>`+
			`<Relationship Id="rId1" `+officeDoc+` Target="evil/workbook.xml"/></Relationships>`), xlsx.ErrNotXlsx},
		"Id quan hệ trùng (excelize lấy cái cuối)": {append(replaced(base, "xl/_rels/workbook.xml.rels", relsHead+
			`<Relationship Id="rId1" `+sheetRel+` Target="worksheets/sheet1.xml"/>`+
			`<Relationship Id="rId1" `+sheetRel+` Target="worksheets/sheet2.xml"/></Relationships>`),
			part{"xl/worksheets/sheet2.xml", evil}), xlsx.ErrNotXlsx},
		"tên trang trùng": {
			replaced(base, "xl/workbook.xml", wbHead+
				`<sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/><sheet name="sheet1" sheetId="2" r:id="rId1"/></sheets></workbook>`),
			xlsx.ErrNotXlsx,
		},
		"trang lớn không ai trỏ tới": {append(slices.Clone(base), part{"xl/worksheets/sheet9.xml", far}), xlsx.ErrTooLarge},
		// Reviewer lần 3: thẻ quan hệ lồng (excelize bỏ, chỉ đọc con trực tiếp) trỏ sổ giả; sổ thật trỏ trang nhỏ có
		// công thức.
		"quan hệ officeDocument lồng": {evilBook(`<X><Relationship Id="rId0" ` + officeDoc + ` Target="xl/workbook.xml"/></X>` +
			`<Relationship Id="rId1" ` + officeDoc + ` Target="evil/workbook.xml"/>`), xlsx.ErrNotXlsx},
		"id của hai namespace quan hệ": {append(replaced(base, "xl/workbook.xml", wbHead[:len(wbHead)-1]+
			` xmlns:s="http://purl.oclc.org/ooxml/officeDocument/relationships">`+
			`<sheets><sheet name="Sheet1" sheetId="1" r:id="rId1" s:id="rId2"/></sheets></workbook>`),
			part{"xl/worksheets/sheet2.xml", evil}), xlsx.ErrNotXlsx},
		"trang lớn không đuôi .xml": {append(slices.Clone(base), part{"xl/worksheets/data.bin", far}), xlsx.ErrTooLarge},
	} {
		var err error
		d, alloc := measure(func() { _, _, err = read(zipParts(t, c.parts), xlsx.Limits{}, "Tên") })
		if !errors.Is(err, c.want) {
			t.Errorf("%s: lỗi %v, mong %v", name, err, c.want)
		}
		if d > 2*time.Second || alloc > 100<<20 {
			t.Errorf("%s: mất %v, cấp %d MB", name, d, alloc>>20)
		}
	}
}

// TestReadDecoySheetElements: thẻ <sheet> mồi ngoài <sheets>, thuộc tính id của namespace lạ không làm lệch trang
// đầu: ô công thức B2 vẫn bị báo.
func TestReadDecoySheetElements(t *testing.T) {
	parts := replaced(fixtureParts(t), "xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8"?><workbook `+
		`xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" `+
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:x="urn:x">`+
		`<x:sheet name="Giả" x:id="rId9"/><sheets><sheet x:id="rId9" name="Sheet1" sheetId="1" r:id="rId1"/></sheets></workbook>`)
	_, cellErrs, err := read(zipParts(t, parts), xlsx.Limits{}, "Tên", "Liên kết")
	want := []xlsx.CellError{{Row: 2, Column: "Liên kết", Code: xlsx.CodeFormulaNotAllowed}}
	if err != nil || !slices.Equal(cellErrs, want) {
		t.Fatalf("lỗi ô %v, lỗi %v; mong %v", cellErrs, err, want)
	}
}

func FuzzRead(f *testing.F) {
	for _, name := range []string{"co-cong-thuc.xlsx", "khong-phai-xlsx.xlsx", "loi-tung-dong.xlsx"} {
		f.Add(fixture(f, name))
	}
	f.Add(written(f, []string{"A", "B"}, [][]any{{"1", "2"}}))
	f.Fuzz(func(_ *testing.T, b []byte) {
		_, _, _ = read(b, xlsx.Limits{}, "A")
	})
}
