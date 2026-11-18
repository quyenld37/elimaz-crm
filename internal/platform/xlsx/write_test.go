package xlsx_test

import (
	"bytes"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"elimaz.com.vn/crm/internal/platform/xlsx"
)

// openWritten ghi sheets bằng xlsx.Write rồi mở lại bằng excelize.
func openWritten(t *testing.T, sheets []xlsx.Sheet) *excelize.File {
	t.Helper()
	var buf bytes.Buffer
	if err := xlsx.Write(&buf, sheets); err != nil {
		t.Fatalf("Write: %v", err)
	}
	f, err := excelize.OpenReader(&buf)
	if err != nil {
		t.Fatalf("mở lại tệp vừa ghi: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestWriteBlocksFormulas(t *testing.T) {
	values := []string{`=HYPERLINK("http://x")`, "+1", "@SUM(A1)", "-2"}
	row := make([]any, len(values))
	for i, v := range values {
		row[i] = v
	}
	f := openWritten(t, []xlsx.Sheet{{Name: "Dữ liệu", Header: []string{"=A", "B", "C", "D"}, Rows: [][]any{row}}})
	for i, v := range values {
		cell, _ := excelize.CoordinatesToCellName(i+1, 2)
		if formula, err := f.GetCellFormula("Dữ liệu", cell); err != nil || formula != "" {
			t.Errorf("%s: có công thức %q (lỗi %v)", cell, formula, err)
		}
		typ, err := f.GetCellType("Dữ liệu", cell)
		if err != nil || (typ != excelize.CellTypeSharedString && typ != excelize.CellTypeInlineString) {
			t.Errorf("%s: kiểu ô %v (lỗi %v), mong chữ", cell, typ, err)
		}
		got, _ := f.GetCellValue("Dữ liệu", cell)
		if got != "'"+v {
			t.Errorf("%s = %q, mong %q", cell, got, "'"+v)
		}
	}
	if got, _ := f.GetCellValue("Dữ liệu", "A1"); got != "'=A" {
		t.Errorf("tiêu đề A1 = %q, mong qua Sanitize", got)
	}
}

func TestWriteAllCellsAreText(t *testing.T) {
	f := openWritten(t, []xlsx.Sheet{{
		Name: "S", Header: []string{"Số", "Chữ", "Rỗng"},
		Rows: [][]any{{int64(350000), "Áo", nil}, {42, 3.5, true}},
	}})
	for _, c := range []struct{ cell, want string }{{"A2", "350000"}, {"B2", "Áo"}, {"A3", "42"}, {"B3", "3.5"}, {"C3", "true"}} {
		typ, _ := f.GetCellType("S", c.cell)
		got, _ := f.GetCellValue("S", c.cell)
		if typ != excelize.CellTypeSharedString && typ != excelize.CellTypeInlineString || got != c.want {
			t.Errorf("%s: kiểu %v giá trị %q, mong chữ %q", c.cell, typ, got, c.want)
		}
	}
	if got, _ := f.GetCellValue("S", "C2"); got != "" {
		t.Errorf("C2 (nil) = %q, mong rỗng", got)
	}
}

func TestWriteSheetsAndLists(t *testing.T) {
	cats := make([]string, 300) // dài quá 255 ký tự: danh sách phải nằm trên trang ẩn, không viết thẳng vào luật
	for i := range cats {
		cats[i] = "Váy > Váy dài " + strconv.Itoa(i)
	}
	status := []string{"Đang nhập", "Đang bán", "=bad"}
	f := openWritten(t, []xlsx.Sheet{
		{
			Name: "Sản phẩm", Header: []string{"Mã*", "Trạng thái", "Danh mục"},
			Lists: map[string][]string{"Trạng thái": status, "Danh mục": cats},
		},
		{Name: "Hướng dẫn", Header: []string{"Cột", "Cách điền"}, Rows: [][]any{{"Mã*", "Bắt buộc"}}},
	})
	if got := visibleSheets(f); !slices.Equal(got, []string{"Sản phẩm", "Hướng dẫn"}) || f.GetSheetList()[0] != "Sản phẩm" {
		t.Fatalf("trang hiện %v, thứ tự %v", got, f.GetSheetList())
	}
	dvs, err := f.GetDataValidations("Sản phẩm")
	if err != nil || len(dvs) != 2 {
		t.Fatalf("luật danh sách: %d (lỗi %v), mong 2", len(dvs), err)
	}
	bySqref := map[string]string{}
	for _, dv := range dvs {
		if dv.Type != "list" {
			t.Errorf("luật %s kiểu %q", dv.Sqref, dv.Type)
		}
		bySqref[strings.Fields(dv.Sqref)[0]] = dv.Formula1
	}
	checkList(t, f, bySqref["B2:B1048576"], []string{"Đang nhập", "Đang bán", "'=bad"})
	checkList(t, f, bySqref["C2:C1048576"], cats)
}

func visibleSheets(f *excelize.File) []string {
	var out []string
	for _, name := range f.GetSheetList() {
		if v, _ := f.GetSheetVisible(name); v {
			out = append(out, name)
		}
	}
	return out
}

// checkList: công thức của luật chọn (dạng 'trang'!$A$1:$A$n) trỏ tới đúng các giá trị want trên trang ẩn.
func checkList(t *testing.T, f *excelize.File, formula string, want []string) {
	t.Helper()
	ref := strings.TrimSuffix(strings.TrimPrefix(formula, "<formula1>"), "</formula1>")
	sheet, rng, ok := strings.Cut(ref, "!")
	sheet = strings.Trim(sheet, "'")
	if v, _ := f.GetSheetVisible(sheet); !ok || v {
		t.Fatalf("luật %q không trỏ tới trang ẩn", formula)
	}
	from, to, _ := strings.Cut(strings.ReplaceAll(rng, "$", ""), ":")
	var got []string
	for i := range len(want) + 1 {
		col, _, _ := excelize.SplitCellName(from)
		v, _ := f.GetCellValue(sheet, col+strconv.Itoa(i+1))
		got = append(got, v)
	}
	if wantTo := from[:1] + strconv.Itoa(len(want)); to != wantTo || !slices.Equal(got[:len(want)], want) || got[len(want)] != "" {
		t.Fatalf("luật %q: tới %s (mong %s), giá trị %v", formula, to, wantTo, got[:min(5, len(got))])
	}
}

func TestWriteRejectsBadInput(t *testing.T) {
	for name, sheets := range map[string][]xlsx.Sheet{
		"không trang":     nil,
		"tên trùng":       {{Name: "A"}, {Name: "a"}},
		"tên cấm":         {{Name: "a/b"}},
		"danh sách lạ":    {{Name: "A", Header: []string{"X"}, Lists: map[string][]string{"Y": {"1"}}}},
		"dòng dài hơn":    {{Name: "A", Header: []string{"X"}, Rows: [][]any{{"1", "2"}}}},
		"tên trang ẩn":    {{Name: "_lists"}},
		"tên dài quá 31":  {{Name: strings.Repeat("a", 32)}},
		"tiêu đề rỗng ở ": {{Name: "A", Header: []string{"X", ""}, Lists: map[string][]string{"": {"1"}}}},
	} {
		if err := xlsx.Write(&bytes.Buffer{}, sheets); err == nil {
			t.Errorf("%s: Write nhận, mong lỗi", name)
		}
	}
}
