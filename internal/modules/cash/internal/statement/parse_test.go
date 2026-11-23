package statement_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/cash/internal/formats"
	"elimaz.com.vn/crm/internal/modules/cash/internal/statement"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// testdata đọc tệp mẫu ở testdata/statements của repo.
func testdata(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "testdata", "statements", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// formulaCell trong book: ô là công thức (phần sau dấu "|" là giá trị đã tính lưu kèm; rỗng là không lưu).
const formulaCell = "\x00f="

// book dựng tệp .xlsx một trang từ rows (dòng 1 là rows[0]): ô chữ inlineStr; ô bắt đầu bằng formulaCell là công thức
// "<f>…</f><v>…</v>". Phần khung (workbook, quan hệ) lấy từ testdata/xlsx/co-cong-thuc.xlsx của gói xlsx.
func book(t testing.TB, rows [][]string) []byte {
	t.Helper()
	return withSheet(t, sheetData(rows))
}

// sheetData dựng phần <sheetData> của rows (xem book).
func sheetData(rows [][]string) string {
	var sd strings.Builder
	for i, r := range rows {
		fmt.Fprintf(&sd, `<row r="%d">`, i+1)
		for j, c := range r {
			ref := fmt.Sprintf("%c%d", 'A'+j, i+1)
			if j >= 26 {
				ref = fmt.Sprintf("A%c%d", 'A'+j-26, i+1)
			}
			if f, ok := strings.CutPrefix(c, formulaCell); ok {
				expr, val, _ := strings.Cut(f, "|")
				v := ""
				if val != "" {
					v = "<v>" + val + "</v>"
				}
				fmt.Fprintf(&sd, `<c r="%s"><f>%s</f>%s</c>`, ref, expr, v)
				continue
			}
			var esc bytes.Buffer
			_ = xmlEscape(&esc, c)
			fmt.Fprintf(&sd, `<c r="%s" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, esc.String())
		}
		sd.WriteString(`</row>`)
	}
	return sd.String()
}

// withSheet thay trang đầu của testdata/xlsx/co-cong-thuc.xlsx bằng sheetData.
func withSheet(t testing.TB, data string) []byte {
	t.Helper()
	sheet := `<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
		`<sheetData>` + data + `</sheetData></worksheet>`
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "testdata", "xlsx", "co-cong-thuc.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		w, err := zw.Create(f.Name)
		if err != nil {
			t.Fatal(err)
		}
		if f.Name == "xl/worksheets/sheet1.xml" {
			_, _ = io.WriteString(w, sheet)
			continue
		}
		r, _ := f.Open()
		_, _ = io.Copy(w, r) //nolint:gosec // khung của tệp mẫu trong repo, vài KB
		_ = r.Close()
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func xmlEscape(w io.Writer, s string) error {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	_, err := io.WriteString(w, r.Replace(s))
	return err //nolint:wrapcheck // test
}

func elimaz() formats.Format {
	r, err := formats.NewRegistry(nil)
	if err != nil {
		panic(err)
	}
	f, _ := r.Get(formats.ElimazV1Key)
	return f
}

func parse(t testing.TB, content []byte) ([]statement.Line, error) {
	t.Helper()
	return statement.Parse(content, elimaz(), statement.DefaultLimits()) //nolint:wrapcheck // test
}

// parseErr đòi err là *statement.ParseError và trả nó.
func parseErr(t testing.TB, err error) *statement.ParseError {
	t.Helper()
	pe, ok := errors.AsType[*statement.ParseError](err)
	if !ok {
		t.Fatalf("lỗi %v, mong *ParseError", err)
	}
	return pe
}

func day(s string) time.Time {
	d, _ := time.Parse("2006-01-02", s)
	return d
}

// csvLines dựng CSV elimaz_v1 có tiêu đề và n dòng FKxxxxxx 1.000/0/0/1.000.
func csvLines(n int) []byte {
	var b bytes.Buffer
	b.WriteString("ma_van_don,tien_cod,phi_van_chuyen,thuc_nhan\n")
	for i := range n {
		fmt.Fprintf(&b, "FK%06d,1000,0,1000\n", i)
	}
	return b.Bytes()
}

// formatCase là một tệp sai định dạng và lỗi mong đợi.
type formatCase struct {
	name    string
	content []byte
	want    statement.ParseError
}

// planFormatCases là ba ca lỗi định dạng của kế hoạch (Task M3-31): thiếu cột tien_cod, "12,5" ở dòng 17, 50.001
// dòng; dùng cho cả test đọc tệp và test lệnh.
func planFormatCases(t testing.TB) []formatCase {
	t.Helper()
	line17 := append(csvLines(15), []byte("FK999999,\"12,5\",0,0\n")...)
	return []formatCase{
		{"thiếu tien_cod", testdata(t, "elimaz_v1_thieu_tien_cod.csv"), statement.ParseError{Code: "missing_column", Column: "tien_cod"}},
		{"12,5 dòng 17", line17, statement.ParseError{Code: "bad_amount", Column: "tien_cod", Line: 17}},
		{"50.001 dòng", csvLines(50001), statement.ParseError{Code: "too_many_rows"}},
	}
}

// TI02 (phần đọc tệp): elimaz_v1.csv 3 dòng → 3 dòng đúng số nguyên, số dòng là dòng trong tệp, ngày chuyển đọc theo
// hai dạng; bản .xlsx cùng dữ liệu (ô tiền FK0001 là công thức =600000+30000 có giá trị đã tính 630.000) → đọc
// 630.000, không chạy công thức.
func TestParse_ElimazThreeLines(t *testing.T) {
	t.Parallel()
	want := []statement.Line{
		{LineNo: 2, TrackingCode: "FK0001", COD: 630000, Fee: 32000, OtherFee: 0, Net: 598000, PaidOn: day("2026-11-05")},
		{LineNo: 3, TrackingCode: "FK0002", COD: 0, Fee: 25000, OtherFee: 0, Net: -25000, PaidOn: day("2026-11-05")},
		{LineNo: 4, TrackingCode: "NHANH9999", COD: 450000, Fee: 30000, OtherFee: 0, Net: 420000, PaidOn: day("2026-11-06")},
	}
	csvGot, err := parse(t, testdata(t, "elimaz_v1_ba_dong.csv"))
	if err != nil || fmt.Sprint(csvGot) != fmt.Sprint(want) {
		t.Fatalf("CSV: %v, %v; mong %v", csvGot, err, want)
	}
	x := book(t, [][]string{
		{"Mã vận đơn", "Tiền COD", "Phí vận chuyển", "Phí khác", "Thực nhận", "Ngày chuyển"},
		{"FK0001", formulaCell + "600000+30000|630000", "32000", "0", "598000", "46331"},
		{"FK0002", "0", "25.000", "0", "-25.000", "05/11/2026"},
		{"NHANH9999", "450000", "30000", "", "420000", "2026-11-06"},
	})
	xGot, err := parse(t, x)
	if err != nil || fmt.Sprint(xGot) != fmt.Sprint(want) {
		t.Fatalf("xlsx: %v, %v; mong %v", xGot, err, want)
	}
}

// Lỗi định dạng có mã, cột, số dòng (không giá trị ô): thiếu cột tien_cod, "12,5" ở dòng 17, 50.001 dòng (50.000 thì
// được), tệp 11 MiB, công thức không lưu giá trị, cột trùng, quá 60 cột, ô quá 1.000 ký tự, COD/phí âm, mã vận đơn
// trống ở dòng có số, ngày sai, không dòng nào, tệp không đọc được, CSV không phải UTF-8.
func TestParse_FormatErrors(t *testing.T) {
	t.Parallel()
	wide := "ma_van_don,tien_cod,phi_van_chuyen,thuc_nhan" + strings.Repeat(",x", 57) + "\nFK1,1,0,1\n"
	cases := append(planFormatCases(t), []formatCase{
		{"11 MiB", bytes.Repeat([]byte("x"), 11<<20), statement.ParseError{Code: "too_large"}},
		{"công thức không giá trị", book(t, [][]string{{"ma_van_don", "tien_cod", "phi_van_chuyen", "thuc_nhan"}, {"FK1", formulaCell + "1+1|", "0", "1"}}), statement.ParseError{Code: "formula_not_allowed", Column: "tien_cod", Line: 2}},
		{"cột trùng", []byte("ma_van_don,tien_cod,Tiền COD,phi_van_chuyen,thuc_nhan\nFK1,1,1,0,1\n"), statement.ParseError{Code: "duplicate_column", Column: "tien_cod"}},
		{"61 cột", []byte(wide), statement.ParseError{Code: "too_many_columns", Line: 1}},
		{"ô quá dài", []byte("ma_van_don,tien_cod,phi_van_chuyen,thuc_nhan\n" + strings.Repeat("A", 1001) + ",1,0,1\n"), statement.ParseError{Code: "cell_too_long", Column: "ma_van_don", Line: 2}},
		{"COD âm", []byte("ma_van_don,tien_cod,phi_van_chuyen,thuc_nhan\nFK1,-1,0,1\n"), statement.ParseError{Code: "bad_amount", Column: "tien_cod", Line: 2}},
		{"phí âm", []byte("ma_van_don,tien_cod,phi_van_chuyen,thuc_nhan\nFK1,1,-5,1\n"), statement.ParseError{Code: "bad_amount", Column: "phi_van_chuyen", Line: 2}},
		{"trống mã vận đơn", []byte("ma_van_don,tien_cod,phi_van_chuyen,thuc_nhan\nFK1,1,0,1\n,630000,0,630000\n"), statement.ParseError{Code: "missing_value", Column: "ma_van_don", Line: 3}},
		{"trống tiền bắt buộc", []byte("ma_van_don,tien_cod,phi_van_chuyen,thuc_nhan\nFK1,,0,1\n"), statement.ParseError{Code: "missing_value", Column: "tien_cod", Line: 2}},
		{"ngày sai", []byte("ma_van_don,tien_cod,phi_van_chuyen,thuc_nhan,ngay_chuyen\nFK1,1,0,1,31/02/2026\n"), statement.ParseError{Code: "bad_date", Column: "ngay_chuyen", Line: 2}},
		{"không dòng", []byte("ma_van_don,tien_cod,phi_van_chuyen,thuc_nhan\n"), statement.ParseError{Code: "no_lines"}},
		{"CSV hỏng", []byte("ma_van_don,tien_cod,phi_van_chuyen,thuc_nhan\nFK1,\"1,0,1\n"), statement.ParseError{Code: "bad_file", Line: 2}},
		{"không UTF-8", []byte("ma_van_don,tien_cod,phi_van_chuyen,thuc_nhan\nFK\xff,1,0,1\n"), statement.ParseError{Code: "bad_encoding"}},
		{"xlsx hỏng", []byte("PK\x03\x04hỏng"), statement.ParseError{Code: "bad_file"}},
	}...)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lines, err := parse(t, c.content)
			if pe := parseErr(t, err); *pe != c.want || lines != nil {
				t.Errorf("lỗi %+v, %d dòng; mong %+v", *pe, len(lines), c.want)
			}
		})
	}
	if lines, err := parse(t, csvLines(50000)); err != nil || len(lines) != 50000 {
		t.Errorf("50.000 dòng: %d dòng, %v", len(lines), err)
	}
}

// Định dạng hãng có dòng mở đầu (HeaderRow 3) và tên cột có dấu: dòng trước tiêu đề bị bỏ; cột thực nhận không khai →
// tính COD − phí − phí khác; dòng trống hoàn toàn bị bỏ; cột ngoài định dạng (tên, SĐT người nhận) không đọc.
func TestParse_CarrierHeaderRow(t *testing.T) {
	t.Parallel()
	f := fulfillment.StatementFormat{
		Key: "x_v1", CarrierKey: "x", Version: 1, HeaderRow: 3,
		Columns: map[fulfillment.StatementField][]string{
			fulfillment.FieldTrackingCode: {"Mã đơn hàng"}, fulfillment.FieldCOD: {"Tiền thu hộ"},
			fulfillment.FieldFee: {"Cước phí"}, fulfillment.FieldOtherFee: {"Phụ phí"},
		},
		Required: []fulfillment.StatementField{fulfillment.FieldTrackingCode, fulfillment.FieldCOD},
	}
	r, err := formats.NewRegistry([]fulfillment.StatementFormat{f})
	if err != nil {
		t.Fatal(err)
	}
	fx, _ := r.Get("x_v1")
	x := book(t, [][]string{
		{"BẢNG KÊ ĐỐI SOÁT"},
		{"Kỳ 01/11 - 07/11"},
		{"STT", "Mã đơn hàng", "Tên người nhận", "SĐT", "Tiền thu hộ", "Cước phí", "Phụ phí"},
		{"1", "X123", "Nguyễn Văn Nhận", "0912345678", "630.000", "32.000", "-2.000"},
		{"", "", "", "", "", "", ""},
		{"2", "X124", "Trần B", "0987654321", "100000", "", ""},
	})
	lines, err := statement.Parse(x, fx, statement.DefaultLimits())
	want := []statement.Line{
		{LineNo: 4, TrackingCode: "X123", COD: 630000, Fee: 32000, OtherFee: -2000, Net: 600000},
		{LineNo: 6, TrackingCode: "X124", COD: 100000, Net: 100000},
	}
	if err != nil || fmt.Sprint(lines) != fmt.Sprint(want) {
		t.Fatalf("%v, %v; mong %v", lines, err, want)
	}
	if _, err := statement.Parse(testdata(t, "elimaz_v1_ba_dong.csv"), fx, statement.DefaultLimits()); parseErr(t, err).Code != "missing_column" {
		t.Errorf("tệp elimaz với định dạng hãng: %v", err)
	}
}

// ParseAmount: các dạng nhận và từ chối của kế hoạch (Task M3-31).
func TestParseAmount(t *testing.T) {
	t.Parallel()
	ok := map[string]int64{
		"1.234.567": 1234567, "1,234,567": 1234567, "1234567": 1234567, "1234567.0": 1234567, "\t630.000\t": 630000,
		"0": 0, "-25.000": -25000, "1,234,567.00": 1234567, "1.234.567,0": 1234567, "9007199254740992": 1 << 53,
		"-9007199254740992": -(1 << 53), "007": 7,
	}
	for s, want := range ok {
		if got, err := statement.ParseAmount(s); err != nil || got != want {
			t.Errorf("ParseAmount(%q) = %d, %v; mong %d", s, got, err, want)
		}
	}
	for _, s := range []string{
		"", "12,5", "1.5", "1234.50", "1.23", "1,23", "12.34.567", "1,234.567", "+5", "--5", "5-", "1e6", "١٢", "9007199254740993",
		"99999999999999999999", "1 000", "0x10", ".0", "1.", "-", "1,234,567,", "₫100",
	} {
		if got, err := statement.ParseAmount(s); !errors.Is(err, statement.ErrBadAmount) {
			t.Errorf("ParseAmount(%q) = %d, %v; mong ErrBadAmount", s, got, err)
		}
	}
}

// Quy luật: mọi số |n| ≤ 2^53 viết theo bốn dạng (liền, chấm nghìn, phẩy nghìn, thêm ".0") đọc lại đúng n.
func TestParseAmount_RoundTrip(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.Int64Range(-(1<<53), 1<<53).Draw(t, "n")
		sep := rapid.SampledFrom([]string{"", ".", ","}).Draw(t, "sep")
		abs := n
		if abs < 0 {
			abs = -abs
		}
		s := group(fmt.Sprint(abs), sep)
		if rapid.Bool().Draw(t, "zero") && sep != "." {
			s += ".0"
		}
		if n < 0 {
			s = "-" + s
		}
		got, err := statement.ParseAmount(s)
		if err != nil || got != n {
			t.Fatalf("ParseAmount(%q) = %d, %v; mong %d", s, got, err, n)
		}
	})
}

// group chèn sep mỗi ba chữ số từ phải (sep rỗng: giữ nguyên).
func group(d, sep string) string {
	if sep == "" {
		return d
	}
	var parts []string
	for len(d) > 3 {
		parts = append([]string{d[len(d)-3:]}, parts...)
		d = d[:len(d)-3]
	}
	return strings.Join(append([]string{d}, parts...), sep)
}
