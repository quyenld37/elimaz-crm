package xlsx_test

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/xlsx"
)

func readRows(b []byte, lim xlsx.Limits) ([]xlsx.RawRow, error) {
	return xlsx.ReadRows(bytes.NewReader(b), int64(len(b)), lim) //nolint:wrapcheck // test kiểm bằng errors.Is
}

// TestReadRowsHeaderNotFirst (Task M3-31): ReadRows trả mọi dòng không trống theo số dòng Excel (dòng mở đầu trước
// tiêu đề vẫn có), ô theo thứ tự cột kể cả ô trống giữa dòng, bỏ dấu nháy mà Sanitize thêm; dòng trống bị bỏ.
func TestReadRowsHeaderNotFirst(t *testing.T) {
	b := withSheet(t, `<row r="1"><c r="A1" t="inlineStr"><is><t>Bảng kê tháng 11</t></is></c></row>`+
		`<row r="3"><c r="A3" t="inlineStr"><is><t>ma</t></is></c><c r="C3" t="inlineStr"><is><t>tien</t></is></c></row>`+
		`<row r="4"><c r="A4" t="inlineStr"><is><t>FK1</t></is></c><c r="B4" t="inlineStr"><is><t>'=x</t></is></c>`+
		`<c r="C4"><v>630000</v></c></row>`+
		`<row r="5"><c r="A5" t="inlineStr"><is><t> </t></is></c></row>`)
	rows, err := readRows(b, xlsx.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	want := []xlsx.RawRow{
		{Number: 1, Cells: []string{"Bảng kê tháng 11"}},
		{Number: 3, Cells: []string{"ma", "", "tien"}},
		{Number: 4, Cells: []string{"FK1", "=x", "630000"}},
	}
	if fmt.Sprint(rows) != fmt.Sprint(want) {
		t.Fatalf("dòng %+v, mong %+v", rows, want)
	}
}

// TestReadRowsFormulas: ô công thức có giá trị đã tính lúc lưu trả giá trị đó (không chạy công thức) và có chỉ số cột
// trong Formulas; ô công thức không có giá trị đã lưu trả "" và vẫn có trong Formulas; dòng chỉ có ô công thức không
// giá trị vẫn được trả; công thức dùng chung cũng được đánh dấu.
func TestReadRowsFormulas(t *testing.T) {
	b := withSheet(t, headerABC+
		`<row r="2"><c r="A2" t="inlineStr"><is><t>FK1</t></is></c><c r="B2"><f>600000+30000</f><v>630000</v></c></row>`+
		`<row r="3"><c r="C3"><f>1+1</f></c></row>`+
		`<row r="4"><c r="A4"><f t="shared" si="0"/><v>5</v></c><c r="B4"><f t="shared" si="0"/><v>6</v></c></row>`)
	rows, err := readRows(b, xlsx.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("%d dòng, mong 4: %+v", len(rows), rows)
	}
	if got := rows[1]; got.Number != 2 || got.Cells[1] != "630000" || !slices.Equal(got.Formulas, []int{1}) {
		t.Errorf("dòng 2: %+v", got)
	}
	if got := rows[2]; got.Number != 3 || slices.ContainsFunc(got.Cells, func(s string) bool { return s != "" }) ||
		!slices.Equal(got.Formulas, []int{2}) {
		t.Errorf("dòng 3: %+v", got)
	}
	if got := rows[3]; got.Cells[0] != "5" || got.Cells[1] != "6" || !slices.Equal(got.Formulas, []int{0, 1}) {
		t.Errorf("dòng 4: %+v", got)
	}
}

// TestReadRowsLimits: ReadRows dùng cùng các bước kiểm của Read: quá MaxBytes, bom zip → ErrTooLarge trong dưới 2 s,
// cấp dưới 100 MB; số dòng không trống (kể cả dòng tiêu đề) quá MaxRows → ErrTooManyRows; không phải xlsx →
// ErrNotXlsx.
func TestReadRowsLimits(t *testing.T) {
	small := written(t, []string{"Tên"}, [][]any{{"Áo"}, {"Váy"}})
	if _, err := readRows(small, xlsx.Limits{MaxBytes: int64(len(small) - 1)}); !errors.Is(err, xlsx.ErrTooLarge) {
		t.Errorf("MaxBytes nhỏ hơn tệp: %v", err)
	}
	if _, err := readRows(small, xlsx.Limits{MaxRows: 2}); !errors.Is(err, xlsx.ErrTooManyRows) {
		t.Errorf("3 dòng, MaxRows 2: %v", err)
	}
	if rows, err := readRows(small, xlsx.Limits{MaxRows: 3}); err != nil || len(rows) != 3 {
		t.Errorf("3 dòng, MaxRows 3: %d dòng, %v", len(rows), err)
	}
	if _, err := readRows([]byte("ma,tien\nFK1,1\n"), xlsx.Limits{}); !errors.Is(err, xlsx.ErrNotXlsx) {
		t.Errorf("CSV: %v", err)
	}
	bomb := fixture(t, "bom-zip.xlsx")
	var err error
	d, alloc := measure(func() { _, err = readRows(bomb, xlsx.Limits{}) })
	if !errors.Is(err, xlsx.ErrTooLarge) || d > 2*time.Second || alloc > 100<<20 {
		t.Errorf("bom zip: %v sau %v, cấp %d MB", err, d, alloc>>20)
	}
}
