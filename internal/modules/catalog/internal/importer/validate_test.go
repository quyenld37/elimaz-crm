package importer_test

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/catalog/internal/importer"
	"elimaz.com.vn/crm/internal/platform/xlsx"
)

// fixtureRefs khớp danh mục, thuộc tính, nhãn mà testdata/xlsx/gen dùng để sinh mau-200.xlsx, loi-tung-dong.xlsx.
func fixtureRefs() importer.Refs {
	id := func(s string) uuid.UUID { return uuid.NewSHA1(uuid.NameSpaceOID, []byte(s)) }
	refs := importer.Refs{
		Categories: map[string]uuid.UUID{},
		Attributes: map[string]map[string]uuid.UUID{"Chất liệu": {}, "Kiểu dáng": {}},
		Tags:       map[string]uuid.UUID{},
	}
	for _, c := range [][]string{{"Váy", "Váy dài"}, {"Váy", "Váy ngắn"}, {"Áo", "Áo sơ mi"}, {"Áo"}, {"Váy"}, {"Đầm"}} {
		p := importer.CategoryPath(c...)
		refs.Categories[p] = id("cat:" + p)
	}
	for attr, values := range map[string][]string{"Chất liệu": {"Lụa", "Cotton", "Voan"}, "Kiểu dáng": {"Suông", "Ôm"}} {
		for _, v := range values {
			refs.Attributes[attr][v] = id("attr:" + attr + ":" + v)
		}
	}
	for _, t := range []string{"moi-ve", "sale", "ban-chay"} {
		refs.Tags[t] = id("tag:" + t)
	}
	return refs
}

func readFixture(t *testing.T, name string) []xlsx.Row {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "testdata", "xlsx", name))
	if err != nil {
		t.Fatalf("đọc tệp thử (sinh bằng go run ./testdata/xlsx/gen): %v", err)
	}
	rows, cellErrs, err := xlsx.Read(bytes.NewReader(b), int64(len(b)), xlsx.Limits{}, importer.RequiredColumns())
	if err != nil || len(cellErrs) > 0 {
		t.Fatalf("xlsx.Read: %v, lỗi ô %v", err, cellErrs)
	}
	return rows
}

func TestValidate_Template200(t *testing.T) {
	plan, errs := importer.Validate(readFixture(t, "mau-200.xlsx"), fixtureRefs())
	if len(errs) > 0 {
		t.Fatalf("%d lỗi, mong 0: %v", len(errs), errs[:min(5, len(errs))])
	}
	if len(plan.Products) != 200 {
		t.Fatalf("%d mẫu, mong 200", len(plan.Products))
	}
	creates := 0
	for _, p := range plan.Products {
		for _, v := range p.Variants {
			if v.Action == importer.ActionCreate {
				creates++
			}
		}
	}
	if creates != 1000 {
		t.Fatalf("%d biến thể create, mong 1000", creates)
	}
	refs := fixtureRefs()
	p := plan.Products[2] // M003: danh mục thứ 3, vật liệu "Voan;Lụa", không kiểu dáng, không nhãn
	wantAttrs := []uuid.UUID{refs.Attributes["Chất liệu"]["Voan"], refs.Attributes["Chất liệu"]["Lụa"]}
	slices.SortFunc(wantAttrs, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	if p.Code != "M003" || p.Status != "active" || p.CategoryID == nil ||
		*p.CategoryID != refs.Categories["Áo > Áo sơ mi"] || !slices.Equal(p.AttributeValueIDs, wantAttrs) ||
		len(p.TagIDs) != 0 || p.Description != "Chất lụa mềm, mặc mát.\nGiặt tay." {
		t.Fatalf("mẫu M003 sai: %+v", p)
	}
	v := p.Variants[1]
	if v.Row != 13 || v.SKU != "M003-2" || v.ColorHex != "#FF0000" || v.Price != 350000 || v.CostPrice != nil ||
		v.Color != "Đỏ" || v.Size != "M" {
		t.Fatalf("biến thể M003-2 sai: %+v", v)
	}
	if c := p.Variants[0].CostPrice; c == nil || *c != 200000 {
		t.Fatalf("giá nhập M003-1: %v", c)
	}
}

func TestValidate_SevenSeededErrors(t *testing.T) {
	plan, errs := importer.Validate(readFixture(t, "loi-tung-dong.xlsx"), fixtureRefs())
	type key struct {
		row          int
		column, code string
	}
	want := []key{
		{5, "Tên mẫu*", "missing_required"},
		{9, "Giá bán*", "invalid_price"},
		{12, "SKU*", "duplicate_sku_in_file"},
		{20, "Danh mục", "unknown_category"},
		{31, "Mã màu", "invalid_color_hex"},
		{44, "Trạng thái", "invalid_status"},
		{50, "Tên mẫu*", "inconsistent_product_fields"},
	}
	got := make([]key, len(errs))
	for i, e := range errs {
		got[i] = key{e.Row, e.Column, e.Code}
		if e.Message == "" || !utf8.ValidString(e.Message) {
			t.Errorf("dòng %d thiếu lời báo tiếng Việt", e.Row)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("lỗi:\n%v\nmong:\n%v\nđủ: %+v", got, want, errs)
	}
	if len(plan.Products) != 0 {
		t.Fatal("có lỗi thì Plan phải rỗng")
	}
}

// row dựng một dòng hợp lệ tối thiểu, ghi đè theo over.
func row(n int, over map[string]string) xlsx.Row {
	cells := map[string]string{}
	for _, h := range importer.RequiredColumns() {
		cells[h] = ""
	}
	maps := map[string]string{
		"Mã mẫu*": "AO01", "Tên mẫu*": "Áo sơ mi", "Màu*": "Trắng", "Size*": "M", "SKU*": fmt.Sprintf("AO01-%d", n),
		"Giá bán*": "350000",
	}
	for k, v := range maps {
		cells[k] = v
	}
	for k, v := range over {
		cells[k] = v
	}
	return xlsx.Row{Number: n, Cells: cells}
}

func codes(errs []importer.RowError) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = fmt.Sprintf("%d:%s:%s", e.Row, e.Column, e.Code)
	}
	return out
}

func TestValidatePrice(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int64
		ok   bool
	}{
		{"350000", 350000, true},
		{"350.000", 350000, true},
		{"1.350.000", 1350000, true},
		{"0", 0, true},
		{"9007199254740991", 9007199254740991, true},
		{"9.007.199.254.740.991", 9007199254740991, true},
		{"9007199254740992", 0, false},
		{"99999999999999999999", 0, false},
		{"350,5", 0, false},
		{"350,000", 0, false},
		{"-1", 0, false},
		{"350.5", 0, false},
		{"35.00", 0, false},
		{"1e5", 0, false},
		{"350 000", 0, false},
		{"abc", 0, false},
		{"350000.0", 0, false},
		{"+5", 0, false},
	} {
		plan, errs := importer.Validate([]xlsx.Row{row(2, map[string]string{"Giá bán*": c.in, "Giá nhập": c.in})}, importer.Refs{})
		if !c.ok {
			if want := []string{"2:Giá bán*:invalid_price", "2:Giá nhập:invalid_price"}; !slices.Equal(codes(errs), want) {
				t.Errorf("%q: %v, mong %v", c.in, codes(errs), want)
			}
			continue
		}
		if len(errs) > 0 {
			t.Errorf("%q: %v", c.in, codes(errs))
			continue
		}
		v := plan.Products[0].Variants[0]
		if v.Price != c.want || v.CostPrice == nil || *v.CostPrice != c.want {
			t.Errorf("%q: giá %d, nhập %v; mong %d", c.in, v.Price, v.CostPrice, c.want)
		}
	}
}

// Ô số đo (Task 516, 517): cm 0,1–1000, tối đa một chữ số thập phân, dấu phẩy hay dấu chấm (số 0 đầu bỏ); lưu mm số
// nguyên; ô trống là nil; 0, 1000,1, hai chữ số thập phân, số âm, chữ, dấu phân nghìn → invalid_measure ở đúng ô, câu
// báo không chép ô dài.
func TestValidateMeasure(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
		ok   bool
	}{
		{"", 0, true},
		{"1", 10, true},
		{"88", 880, true},
		{"1000", 10000, true},
		{"088", 880, true},
		{"0,1", 1, true},
		{"72,5", 725, true},
		{"72.5", 725, true},
		{"1000,0", 10000, true},
		{"2.0", 20, true},
		{"0", 0, false},
		{"0,0", 0, false},
		{"1001", 0, false},
		{"1000,1", 0, false},
		{"2.55", 0, false},
		{"2,", 0, false},
		{",5", 0, false},
		{"1.000", 0, false},
		{"-3", 0, false},
		{"abc", 0, false},
		{"10000", 0, false},
		{"1e2", 0, false},
		{strings.Repeat("9", 60), 0, false},
	} {
		plan, errs := importer.Validate([]xlsx.Row{row(2, map[string]string{
			importer.ColLengthCm: c.in, importer.ColWidthCm: c.in, importer.ColHeightCm: c.in,
		})}, importer.Refs{})
		if !c.ok {
			want := []string{"2:Dài (cm):invalid_measure", "2:Rộng (cm):invalid_measure", "2:Cao (cm):invalid_measure"}
			if !slices.Equal(codes(errs), want) || slices.ContainsFunc(errs, func(e importer.RowError) bool {
				return utf8.RuneCountInString(e.Message) > 120
			}) {
				t.Errorf("%q: %+v, mong %v (lời báo ngắn)", c.in, errs, want)
			}
			continue
		}
		if len(errs) > 0 {
			t.Errorf("%q: %v", c.in, codes(errs))
			continue
		}
		checkPlannedMeasure(t, c.in, plan.Products[0], c.want)
	}
}

// checkPlannedMeasure: ba số đo (mm) của p đều là want (ô in trống → đều nil).
func checkPlannedMeasure(t *testing.T, in string, p importer.PlannedProduct, want int) {
	t.Helper()
	for _, got := range []*int{p.LengthMm, p.WidthMm, p.HeightMm} {
		if in == "" && got != nil || in != "" && (got == nil || *got != want) {
			t.Errorf("%q: %v, mong %d", in, got, want)
		}
	}
}

func TestValidateFieldRules(t *testing.T) {
	long := strings.Repeat("ộ", 201)
	for name, c := range map[string]struct {
		over map[string]string
		want []string
	}{
		"thiếu mọi cột bắt buộc": {
			map[string]string{"Mã mẫu*": "", "Tên mẫu*": " ", "Màu*": "", "Size*": "", "SKU*": "", "Giá bán*": ""},
			[]string{
				"2:Mã mẫu*:missing_required", "2:Tên mẫu*:missing_required", "2:Màu*:missing_required",
				"2:Size*:missing_required", "2:SKU*:missing_required", "2:Giá bán*:missing_required",
			},
		},
		"mã mẫu sai":         {map[string]string{"Mã mẫu*": "áo 01"}, []string{"2:Mã mẫu*:invalid_code"}},
		"sku sai":            {map[string]string{"SKU*": "AO 01"}, []string{"2:SKU*:invalid_sku"}},
		"sku chữ thường":     {map[string]string{"SKU*": "ao01-x"}, nil},
		"tên dài":            {map[string]string{"Tên mẫu*": long}, []string{"2:Tên mẫu*:invalid_text"}},
		"màu dài":            {map[string]string{"Màu*": strings.Repeat("a", 51)}, []string{"2:Màu*:invalid_text"}},
		"mô tả nhiều dòng":   {map[string]string{"Mô tả": "dòng 1\r\ndòng 2"}, nil},
		"mô tả dài":          {map[string]string{"Mô tả": strings.Repeat("a", 1001)}, []string{"2:Mô tả:invalid_text"}},
		"mã màu chữ thường":  {map[string]string{"Mã màu": "#ff00aa"}, nil},
		"mã màu thiếu #":     {map[string]string{"Mã màu": "FF00AA"}, []string{"2:Mã màu:invalid_color_hex"}},
		"barcode sai":        {map[string]string{"Barcode": "12 3"}, []string{"2:Barcode:invalid_barcode"}},
		"barcode được":       {map[string]string{"Barcode": "4006381333931"}, nil},
		"trạng thái":         {map[string]string{"Trạng thái": "Ngừng bán"}, nil},
		"trạng thái lạ":      {map[string]string{"Trạng thái": "Bán"}, []string{"2:Trạng thái:invalid_status"}},
		"danh mục thừa cách": {map[string]string{"Danh mục": " Váy>Váy dài "}, nil},
		"danh mục lạ":        {map[string]string{"Danh mục": "Áo > Không có"}, []string{"2:Danh mục:unknown_category"}},
		"nhãn":               {map[string]string{"Nhãn": "Sale; moi-ve ;"}, nil},
		"nhãn lạ":            {map[string]string{"Nhãn": "sale;khong-co"}, []string{"2:Nhãn:unknown_tag"}},
		"thuộc tính":         {map[string]string{"Thuộc tính: Chất liệu": "Lụa; Voan"}, nil},
		"giá trị lạ":         {map[string]string{"Thuộc tính: Chất liệu": "Len"}, []string{"2:Thuộc tính: Chất liệu:unknown_attribute_value"}},
		"thuộc tính lạ":      {map[string]string{"Thuộc tính: Màu nền": "Đỏ"}, []string{"2:Thuộc tính: Màu nền:unknown_attribute_value"}},
		"thuộc tính lạ rỗng": {map[string]string{"Thuộc tính: Màu nền": ""}, nil},
	} {
		_, errs := importer.Validate([]xlsx.Row{row(2, c.over)}, fixtureRefs())
		if !slices.Equal(codes(errs), c.want) {
			t.Errorf("%s: %v, mong %v", name, codes(errs), c.want)
		}
		for _, e := range errs {
			if strings.Contains(e.Message, long) || utf8.RuneCountInString(e.Message) > 300 {
				t.Errorf("%s: lời báo chép nội dung ô dài: %q", name, e.Message)
			}
		}
	}
}

func TestValidateGroupRules(t *testing.T) {
	refs := fixtureRefs()
	for name, c := range map[string]struct {
		rows []xlsx.Row
		want []string
	}{
		"cùng màu size": {
			[]xlsx.Row{row(2, nil), row(3, nil)},
			[]string{"3:Size*:duplicate_option_in_file"},
		},
		"sku trùng khác hoa thường": {
			[]xlsx.Row{row(2, nil), row(3, map[string]string{"SKU*": "ao01-2", "Size*": "L", "Mã mẫu*": "AO02"})},
			[]string{"3:SKU*:duplicate_sku_in_file"},
		},
		"barcode trùng": {
			[]xlsx.Row{row(2, map[string]string{"Barcode": "ABC1"}), row(3, map[string]string{"Barcode": "ABC1", "Size*": "L"})},
			[]string{"3:Barcode:invalid_barcode"},
		},
		"nhãn khác thứ tự vẫn giống": {
			[]xlsx.Row{row(2, map[string]string{"Nhãn": "sale;moi-ve"}), row(3, map[string]string{"Nhãn": "moi-ve; SALE", "Size*": "L"})},
			nil,
		},
		"mã mẫu khác hoa thường là một mẫu": {
			[]xlsx.Row{row(2, nil), row(3, map[string]string{"Mã mẫu*": "ao01", "Size*": "L", "Mô tả": "khác"})},
			[]string{"3:Mô tả:inconsistent_product_fields"},
		},
		"nhiều cột khác": {
			[]xlsx.Row{
				row(2, map[string]string{"Danh mục": "Áo", "Thuộc tính: Kiểu dáng": "Ôm"}),
				row(3, map[string]string{"Size*": "L", "Danh mục": "Váy", "Thuộc tính: Kiểu dáng": "Suông", "Trạng thái": "Đang bán"}),
			},
			[]string{"3:Danh mục:inconsistent_product_fields", "3:Trạng thái:inconsistent_product_fields", "3:Thuộc tính: Kiểu dáng:inconsistent_product_fields"},
		},
		"thuộc tính trống ở dòng đầu, có ở dòng sau": {
			[]xlsx.Row{row(2, nil), row(3, map[string]string{"Size*": "L", "Thuộc tính: Kiểu dáng": "Ôm"})},
			[]string{"3:Thuộc tính: Kiểu dáng:inconsistent_product_fields"},
		},
		"dòng đầu thiếu tên, so với dòng có tên đầu tiên": {
			[]xlsx.Row{row(2, map[string]string{"Tên mẫu*": ""}), row(3, map[string]string{"Size*": "L"}), row(4, map[string]string{"Size*": "S", "Tên mẫu*": "Khác"})},
			[]string{"2:Tên mẫu*:missing_required", "4:Tên mẫu*:inconsistent_product_fields"},
		},
	} {
		_, errs := importer.Validate(c.rows, refs)
		if !slices.Equal(codes(errs), c.want) {
			t.Errorf("%s: %v, mong %v", name, codes(errs), c.want)
		}
	}
}

func TestValidateTooManyVariants(t *testing.T) {
	rows := make([]xlsx.Row, 201)
	for i := range rows {
		rows[i] = row(i+2, map[string]string{"Size*": fmt.Sprintf("S%d", i)})
	}
	_, errs := importer.Validate(rows, importer.Refs{})
	if want := []string{"202:SKU*:too_many_variants"}; !slices.Equal(codes(errs), want) {
		t.Fatalf("%v, mong %v", codes(errs), want)
	}
	if _, errs := importer.Validate(rows[:200], importer.Refs{}); len(errs) > 0 {
		t.Fatalf("200 biến thể: %v", codes(errs))
	}
}

func TestValidateExistingSKUs(t *testing.T) {
	cost := int64(200000)
	productID, otherID := uuid.New(), uuid.New()
	existing := func(code string, pid uuid.UUID) importer.ExistingVariant {
		return importer.ExistingVariant{
			ID: uuid.New(), ProductID: pid, ProductCode: code, Version: 3, Barcode: "2000000000015",
			Color: "Trắng", Size: "M", Price: 350000, CostPrice: &cost,
		}
	}
	refs := importer.Refs{ExistingSKUs: map[string]importer.ExistingVariant{
		"AO01-2": existing("AO01", productID), "AO01-3": existing("AO01", productID), "VAY09-1": existing("VAY09", otherID),
	}}
	refs.ExistingSKUs["AO01-3"] = func() importer.ExistingVariant { v := refs.ExistingSKUs["AO01-3"]; v.Size = "L"; return v }()
	rows := []xlsx.Row{
		row(2, map[string]string{"SKU*": "AO01-2", "Giá nhập": "200.000"}),                     // giống hệt (barcode trống: giữ)
		row(3, map[string]string{"SKU*": "AO01-3", "Size*": "L", "Giá bán*": "390000"}),        // giá khác, bỏ giá nhập
		row(4, map[string]string{"SKU*": "AO01-4", "Size*": "XL"}),                             // mới
		row(5, map[string]string{"SKU*": "vay09-1", "Size*": "S"}),                             // của mẫu khác
		row(6, map[string]string{"SKU*": "AO01-6", "Size*": "XS", "Barcode": "2000000000015"}), // barcode của biến thể khác
	}
	_, errs := importer.Validate(rows, refs)
	if want := []string{"5:SKU*:sku_belongs_to_other_product", "6:Barcode:invalid_barcode"}; !slices.Equal(codes(errs), want) {
		t.Fatalf("%v, mong %v", codes(errs), want)
	}
	plan, errs := importer.Validate(rows[:3], refs)
	if len(errs) > 0 {
		t.Fatal(codes(errs))
	}
	p := plan.Products[0]
	if p.ExistingProductID == nil || *p.ExistingProductID != productID {
		t.Fatalf("ExistingProductID = %v", p.ExistingProductID)
	}
	got := []importer.Action{p.Variants[0].Action, p.Variants[1].Action, p.Variants[2].Action}
	if want := []importer.Action{importer.ActionUnchanged, importer.ActionUpdate, importer.ActionCreate}; !slices.Equal(got, want) {
		t.Fatalf("hành động %v, mong %v", got, want)
	}
	v := p.Variants[1]
	if v.ExistingID == nil || *v.ExistingID != refs.ExistingSKUs["AO01-3"].ID || v.ExistingVersion != 3 ||
		v.Barcode != "2000000000015" || v.CostPrice != nil || v.Price != 390000 {
		t.Fatalf("biến thể cập nhật sai: %+v", v)
	}
}

func TestMergeCellErrors(t *testing.T) {
	rowErrs := []importer.RowError{
		{Row: 3, Column: "Tên mẫu*", Code: "missing_required", Message: "x"},
		{Row: 2, Column: "SKU*", Code: "invalid_sku", Message: "y"},
	}
	cellErrs := []xlsx.CellError{
		{Row: 3, Column: "Tên mẫu*", Code: xlsx.CodeFormulaNotAllowed},
		{Row: 4, Column: "Mô tả", Code: xlsx.CodeCellTooLong},
	}
	got := importer.MergeCellErrors(cellErrs, rowErrs)
	if want := []string{"2:SKU*:invalid_sku", "3:Tên mẫu*:formula_not_allowed", "4:Mô tả:cell_too_long"}; !slices.Equal(codes(got), want) {
		t.Fatalf("%v, mong %v", codes(got), want)
	}
	for _, e := range got {
		if e.Message == "" {
			t.Errorf("%d %s thiếu lời báo", e.Row, e.Code)
		}
	}
}

func TestTemplateRoundTrip(t *testing.T) {
	refs := fixtureRefs()
	b, err := importer.Template(refs)
	if err != nil {
		t.Fatalf("Template: %v", err)
	}
	rows, cellErrs, err := xlsx.Read(bytes.NewReader(b), int64(len(b)), xlsx.Limits{}, importer.Columns(refs))
	if err != nil || len(rows) != 0 || len(cellErrs) != 0 {
		t.Fatalf("đọc lại mẫu: %d dòng, lỗi ô %v, lỗi %v", len(rows), cellErrs, err)
	}
	cols := importer.Columns(refs)
	want := []string{
		"Mã mẫu*", "Tên mẫu*", "Danh mục", "Mô tả", "Trạng thái", "Nhãn", "Dài (cm)", "Rộng (cm)", "Cao (cm)",
		"Thuộc tính: Chất liệu", "Thuộc tính: Kiểu dáng",
		"Màu*", "Mã màu", "Size*", "SKU*", "Barcode", "Giá bán*", "Giá nhập",
	}
	if !slices.Equal(cols, want) {
		t.Fatalf("cột %v, mong %v", cols, want)
	}
	for _, h := range importer.RequiredColumns() {
		if !slices.Contains(cols, h) {
			t.Errorf("cột bắt buộc %q không có trong mẫu", h)
		}
	}
	// Cột số đo (Task 516) tùy chọn: tệp mẫu cũ không có chúng vẫn qua kiểm tiêu đề.
	for _, h := range []string{importer.ColLengthCm, importer.ColWidthCm, importer.ColHeightCm} {
		if slices.Contains(importer.RequiredColumns(), h) {
			t.Errorf("cột số đo %q không được bắt buộc", h)
		}
	}
}

// existingAO01 là mẫu AO01 có sẵn với hai biến thể Trắng/M (AO01-OLD-M), Trắng/L (AO01-OLD-L).
func existingAO01() (importer.Refs, uuid.UUID) {
	pid := uuid.NewSHA1(uuid.NameSpaceOID, []byte("product:AO01"))
	refs := fixtureRefs()
	refs.ExistingProducts = map[string]importer.ExistingProduct{
		"AO01": {ID: pid, Options: map[importer.Option]string{{Color: "Trắng", Size: "M"}: "AO01-OLD-M", {Color: "Trắng", Size: "L"}: "AO01-OLD-L"}},
	}
	refs.ExistingSKUs = map[string]importer.ExistingVariant{}
	for o, sku := range refs.ExistingProducts["AO01"].Options {
		refs.ExistingSKUs[sku] = importer.ExistingVariant{
			ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(sku)), ProductID: pid, ProductCode: "AO01", Version: 1,
			Barcode: "BC-" + o.Size, Color: o.Color, Size: o.Size, Price: 350000,
		}
	}
	return refs, pid
}

// Việc treo (2) của Task 38: biến thể mới trùng màu và size với biến thể có sẵn không nằm trong tệp → option_taken;
// biến thể có sẵn nằm trong tệp (đổi sang màu, size khác) thì nhường chỗ được.
func TestValidate_OptionTakenByExistingVariant(t *testing.T) {
	refs, _ := existingAO01()
	_, errs := importer.Validate([]xlsx.Row{row(2, map[string]string{"SKU*": "AO01-NEW-M"})}, refs)
	if want := []string{"2:Size*:option_taken"}; !slices.Equal(codes(errs), want) {
		t.Fatalf("trùng biến thể có sẵn: %v, mong %v", codes(errs), want)
	}
	if !strings.Contains(errs[0].Message, "AO01-OLD-M") {
		t.Errorf("lời báo không nói SKU đang giữ màu, size: %q", errs[0].Message)
	}
	// AO01-OLD-M chuyển sang size XL trong cùng tệp: AO01-NEW-M lấy được Trắng/M.
	plan, errs := importer.Validate([]xlsx.Row{
		row(2, map[string]string{"SKU*": "AO01-NEW-M"}),
		row(3, map[string]string{"SKU*": "AO01-OLD-M", "Size*": "XL"}),
	}, refs)
	if len(errs) > 0 {
		t.Fatalf("nhường chỗ trong tệp: %v", codes(errs))
	}
	if got := plan.Products[0].Variants[1].Action; got != importer.ActionUpdate {
		t.Errorf("AO01-OLD-M: %s, mong update", got)
	}
	// Mẫu khác cùng màu, size: không liên quan.
	if _, errs := importer.Validate([]xlsx.Row{row(2, map[string]string{"Mã mẫu*": "AO02", "SKU*": "AO02-M"})}, refs); len(errs) > 0 {
		t.Fatalf("mẫu khác: %v", codes(errs))
	}
}

// Mẫu có sẵn (theo mã) mà mọi SKU trong tệp đều mới vẫn có ExistingProductID: Task 323 thêm biến thể vào mẫu đó thay
// cho tạo mẫu trùng mã.
func TestValidate_ExistingProductByCode(t *testing.T) {
	refs, pid := existingAO01()
	plan, errs := importer.Validate([]xlsx.Row{row(2, map[string]string{"SKU*": "AO01-NEW-S", "Size*": "S", "Mã mẫu*": "ao01"})}, refs)
	if len(errs) > 0 {
		t.Fatal(codes(errs))
	}
	p := plan.Products[0]
	if p.ExistingProductID == nil || *p.ExistingProductID != pid || p.Variants[0].Action != importer.ActionCreate {
		t.Fatalf("mẫu có sẵn: %+v", p)
	}
}

// Quy luật đổi cm → mm (Task 517): mọi mm trong [MinMeasureMm, MaxMeasureMm] đổi ra cm (mm/10) rồi đổi lại được đúng
// mm đó; số cm có hơn một chữ số thập phân (mm + 0,5…) hay ngoài khoảng bị từ chối; NaN, ±Inf bị từ chối.
func TestMeasureMmFromCm_RoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		mm := rapid.IntRange(importer.MinMeasureMm, importer.MaxMeasureMm).Draw(t, "mm")
		got, ok := importer.MeasureMmFromCm(float64(mm) / 10)
		if !ok || got != mm {
			t.Fatalf("%d mm → %v cm → %d, %v", mm, float64(mm)/10, got, ok)
		}
		if _, ok := importer.MeasureMmFromCm((float64(mm) + 0.5) / 10); ok {
			t.Fatalf("%v cm (hai chữ số thập phân) phải bị từ chối", (float64(mm)+0.5)/10)
		}
	})
	for _, bad := range []float64{0, -0.1, 0.04, 1000.1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, ok := importer.MeasureMmFromCm(bad); ok {
			t.Errorf("%v cm phải bị từ chối", bad)
		}
	}
}
