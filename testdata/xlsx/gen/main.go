// Lệnh gen sinh các tệp thử của Task M1-38 vào testdata/xlsx (chạy từ gốc apps/crm):
//
//	go run ./testdata/xlsx/gen
//
// mau-200.xlsx và loi-tung-dong.xlsx ghi bằng xlsx.Write (đúng đường của mẫu thật); ba tệp còn lại dựng zip bằng tay
// vì xlsx.Write không ghi được công thức hay tệp hỏng. Danh mục, thuộc tính, nhãn dùng trong tệp khớp Refs của
// internal/modules/catalog/internal/importer/validate_test.go (fixtureRefs): đổi một bên thì đổi bên kia.
package main

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"elimaz.com.vn/crm/internal/platform/xlsx"
)

const dir = "testdata/xlsx"

// header là cột của mẫu với hai thuộc tính "Chất liệu", "Kiểu dáng" (xếp theo tên, như importer.Columns).
func header() []string {
	return []string{
		"Mã mẫu*", "Tên mẫu*", "Danh mục", "Mô tả", "Trạng thái", "Nhãn",
		"Thuộc tính: Chất liệu", "Thuộc tính: Kiểu dáng",
		"Màu*", "Mã màu", "Size*", "SKU*", "Barcode", "Giá bán*", "Giá nhập",
	}
}

var (
	categories = []string{"Váy > Váy dài", "Váy > Váy ngắn", "Áo > Áo sơ mi", "Đầm"}
	materials  = []string{"Lụa", "Cotton", "Voan;Lụa"}
	shapes     = []string{"Suông", "Ôm", ""}
	tags       = []string{"moi-ve;sale", "ban-chay", ""}
	options    = [][3]string{{"Đỏ", "#FF0000", "S"}, {"Đỏ", "#ff0000", "M"}, {"Đỏ", "#FF0000", "L"}, {"Xanh", "", "S"}, {"Xanh", "", "M"}}
)

// rows dựng n mẫu, mỗi mẫu 5 biến thể; dòng Excel của biến thể thứ k (đếm từ 0) là k+2.
func rows(n int) [][]string {
	out := make([][]string, 0, n*len(options))
	for p := range n {
		code := fmt.Sprintf("M%03d", p+1)
		for i, o := range options {
			price := "350000"
			if (p+i)%2 == 1 {
				price = "350.000"
			}
			cost := ""
			if i%2 == 0 {
				cost = "200000"
			}
			out = append(out, []string{
				code, fmt.Sprintf("Đầm lụa mẫu %d — cổ vuông", p+1), categories[p%len(categories)],
				"Chất lụa mềm, mặc mát.\nGiặt tay.", "Đang bán", tags[p%len(tags)],
				materials[p%len(materials)], shapes[p%len(shapes)],
				o[0], o[1], o[2], fmt.Sprintf("%s-%d", code, i+1), "", price, cost,
			})
		}
	}
	return out
}

func toSheet(data [][]string) xlsx.Sheet {
	s := xlsx.Sheet{Name: "Sản phẩm", Header: header()}
	for _, r := range data {
		row := make([]any, len(r))
		for i, v := range r {
			row[i] = v
		}
		s.Rows = append(s.Rows, row)
	}
	return s
}

func col(name string) int {
	for i, h := range header() {
		if h == name {
			return i
		}
	}
	panic("không có cột " + name)
}

// seededErrors cài đúng 7 lỗi của TestValidate_SevenSeededErrors (KH01) vào 12 mẫu, 60 dòng.
func seededErrors() [][]string {
	data := rows(12)
	set := func(row int, column, v string) { data[row-2][col(column)] = v }
	set(5, "Tên mẫu*", "")
	set(9, "Giá bán*", "350,5")
	set(12, "SKU*", data[3-2][col("SKU*")])
	set(20, "Danh mục", "Áo > Không có")
	set(31, "Mã màu", "red")
	set(44, "Trạng thái", "Bán")
	set(50, "Tên mẫu*", "Tên khác dòng 49")
	return data
}

func writeXlsx(name string, sheets []xlsx.Sheet) {
	var buf bytes.Buffer
	if err := xlsx.Write(&buf, sheets); err != nil {
		log.Fatalf("%s: %v", name, err)
	}
	writeFile(name, buf.Bytes())
}

func writeFile(name string, b []byte) {
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
		log.Fatal(err)
	}
}

// rawXlsx dựng tệp xlsx tối thiểu một trang từ phần sheetData; body ghi phần thân (nén deflate mức cao nhất).
func rawXlsx(body func(w io.Writer)) []byte {
	const ns = `xmlns="http://schemas.openxmlformats.org/`
	parts := []struct{ name, data string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types ` + ns + `package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
			`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships ` + ns + `package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8"?><workbook ` + ns + `spreadsheetml/2006/main" ` +
			`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>` +
			`<sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships ` + ns + `package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`},
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(w, flate.BestCompression)
	})
	for _, p := range parts {
		w, err := zw.Create(p.name)
		if err != nil {
			log.Fatal(err)
		}
		_, _ = io.WriteString(w, p.data)
	}
	w, err := zw.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		log.Fatal(err)
	}
	_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><worksheet `+ns+`spreadsheetml/2006/main"><sheetData>`)
	body(w)
	_, _ = io.WriteString(w, `</sheetData></worksheet>`)
	if err := zw.Close(); err != nil {
		log.Fatal(err)
	}
	return buf.Bytes()
}

func inline(ref, v string) string {
	return `<c r="` + ref + `" t="inlineStr"><is><t>` + v + `</t></is></c>`
}

func main() {
	writeXlsx("mau-200.xlsx", []xlsx.Sheet{toSheet(rows(200))})
	writeXlsx("loi-tung-dong.xlsx", []xlsx.Sheet{toSheet(seededErrors())})
	writeFile("khong-phai-xlsx.xlsx", []byte("<!doctype html><html><body><table><tr><td>Mã mẫu*</td></tr></table></body></html>\n"))
	// Ô B2 là công thức HYPERLINK có giá trị đã tính "bấm"; ô B3 là chữ "=HYPERLINK" (không phải công thức).
	writeFile("co-cong-thuc.xlsx", rawXlsx(func(w io.Writer) {
		_, _ = io.WriteString(w, `<row r="1">`+inline("A1", "Tên")+inline("B1", "Liên kết")+`</row>`+
			`<row r="2">`+inline("A2", "Áo")+`<c r="B2" t="str"><f>HYPERLINK("http://x","bấm")</f><v>bấm</v></c></row>`+
			`<row r="3">`+inline("A3", "Váy")+inline("B3", "=HYPERLINK")+`</row>`)
	}))
	// Bom zip: trang giải nén ra 1 GiB (một dòng tiêu đề rồi khoảng trắng), tệp nén khoảng 1 MB.
	writeFile("bom-zip.xlsx", rawXlsx(func(w io.Writer) {
		_, _ = io.WriteString(w, `<row r="1">`+inline("A1", "Tên")+`</row>`)
		chunk := []byte(strings.Repeat(" ", 1<<20))
		for range 1 << 10 {
			_, _ = w.Write(chunk)
		}
	}))
}
