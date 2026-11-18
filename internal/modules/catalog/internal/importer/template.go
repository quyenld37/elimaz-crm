// Package importer là phần đọc và kiểm của nhập sản phẩm bằng Excel (kế hoạch M1, Task M1-38; KH01): mẫu Excel
// (Template, Columns), kiểm từng dòng ra lỗi theo ô (Validate, MergeCellErrors) và kế hoạch ghi (Plan) cho Task M1-47.
// Không đọc, không ghi database: Refs do bên gọi đọc qua catalog. Đọc tệp bằng xlsx.Read với RequiredColumns.
package importer

import (
	"bytes"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"elimaz.com.vn/crm/internal/platform/xlsx"
)

// Tiêu đề cột của mẫu (cột có * là bắt buộc).
const (
	ColCode         = "Mã mẫu*"
	ColName         = "Tên mẫu*"
	ColCategory     = "Danh mục"
	ColDescription  = "Mô tả"
	ColStatus       = "Trạng thái"
	ColTags         = "Nhãn"
	AttributePrefix = "Thuộc tính: " // một cột "Thuộc tính: <tên>" cho mỗi thuộc tính
	ColColor        = "Màu*"
	ColColorHex     = "Mã màu"
	ColSize         = "Size*"
	ColSKU          = "SKU*"
	ColBarcode      = "Barcode"
	ColPrice        = "Giá bán*"
	ColCostPrice    = "Giá nhập"
)

// Luật dữ liệu: bản sao của luật trong gói catalog (importer không nhập ngược catalog); test importrules_test.go
// của catalog so từng giá trị, lệch là đỏ.
const (
	MaxName        = 200
	MaxDescription = 1000
	MaxOption      = 50 // màu, size
	MaxVariants    = 200
	MaxMoney       = 9007199254740991

	StatusDraft        = "draft"
	StatusActive       = "active"
	StatusDiscontinued = "discontinued"
)

// Biểu thức luật: bản sao của catalog (xem trên).
var (
	CodePattern     = regexp.MustCompile(`^[A-Z0-9][A-Z0-9._-]{0,63}$`)
	BarcodePattern  = regexp.MustCompile(`^[0-9A-Za-z-]{4,32}$`)
	ColorHexPattern = regexp.MustCompile(`^#[0-9A-F]{6}$`)
)

// statusLabels: chữ của cột Trạng thái → trạng thái mẫu (thứ tự là thứ tự trong danh sách chọn).
func statusLabels() []struct{ label, status string } {
	return []struct{ label, status string }{
		{"Đang nhập", StatusDraft}, {"Đang bán", StatusActive}, {"Ngừng bán", StatusDiscontinued},
	}
}

func productColumns() []string {
	return []string{ColCode, ColName, ColCategory, ColDescription, ColStatus, ColTags}
}

func variantColumns() []string {
	return []string{ColColor, ColColorHex, ColSize, ColSKU, ColBarcode, ColPrice, ColCostPrice}
}

// RequiredColumns là các cột cố định mà tệp nhập phải có (tham số header của xlsx.Read). Cột thuộc tính không bắt
// buộc có: thuộc tính thêm sau khi tải mẫu không làm tệp cũ hỏng.
func RequiredColumns() []string { return slices.Concat(productColumns(), variantColumns()) }

// Columns là tiêu đề của mẫu: cột cố định, xen một cột thuộc tính cho mỗi thuộc tính trong refs (xếp theo tên).
func Columns(refs Refs) []string {
	attrs := slices.Sorted(maps.Keys(refs.Attributes))
	for i, a := range attrs {
		attrs[i] = AttributePrefix + a
	}
	return slices.Concat(productColumns(), attrs, variantColumns())
}

// CategoryPath là khóa của Refs.Categories cho danh mục có đường dẫn names (từ gốc): "Váy > Váy dài".
func CategoryPath(names ...string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = strings.TrimSpace(n)
	}
	return strings.Join(parts, " > ")
}

// Template sinh tệp mẫu sản phẩm: trang "Sản phẩm" (tiêu đề Columns(refs), danh sách chọn cho Trạng thái và Danh mục)
// và trang "Hướng dẫn".
func Template(refs Refs) ([]byte, error) {
	labels := make([]string, 0, 3)
	for _, s := range statusLabels() {
		labels = append(labels, s.label)
	}
	guide := [][]any{
		{ColCode, "Có", "Mã của mẫu: chữ không dấu, số, dấu . _ - (tối đa 64 ký tự). Mỗi dòng là một biến thể; " +
			"các dòng cùng mã mẫu là một mẫu."},
		{ColName, "Có", fmt.Sprintf("Tên mẫu, tối đa %d ký tự.", MaxName)},
		{ColCategory, "Không", "Chọn trong danh sách, dạng \"Váy > Váy dài\"."},
		{ColDescription, "Không", fmt.Sprintf("Mô tả ngắn, tối đa %d ký tự, được xuống dòng.", MaxDescription)},
		{ColStatus, "Không", "Đang nhập (ẩn với khách), Đang bán hoặc Ngừng bán. Để trống: mẫu mới là Đang nhập."},
		{ColTags, "Không", "Mã nhãn kệ hàng, cách nhau bởi dấu ; (ví dụ: moi-ve;sale)."},
		{AttributePrefix + "<tên>", "Không", "Giá trị của thuộc tính, nhiều giá trị cách nhau bởi dấu ;."},
		{ColColor, "Có", fmt.Sprintf("Tên màu, tối đa %d ký tự.", MaxOption)},
		{ColColorHex, "Không", "Mã màu dạng #RRGGBB, ví dụ #FF0000."},
		{ColSize, "Có", fmt.Sprintf("Size, tối đa %d ký tự. Một mẫu không có hai dòng cùng màu và size.", MaxOption)},
		{ColSKU, "Có", "Mã biến thể, không trùng trong cả cửa hàng; chữ không dấu, số, dấu . _ -. SKU đã có thì dòng " +
			"này sửa biến thể đó."},
		{ColBarcode, "Không", "Mã vạch 4–32 chữ, số. Để trống: CRM tự sinh mã EAN-13."},
		{ColPrice, "Có", "Số nguyên đồng, ví dụ 350000 hoặc 350.000 (không có số lẻ)."},
		{ColCostPrice, "Không", "Như giá bán."},
		{"", "", "Các cột của mẫu (tên, danh mục, mô tả, trạng thái, nhãn, thuộc tính) phải giống nhau ở mọi dòng " +
			"cùng mã mẫu."},
	}
	var buf bytes.Buffer
	err := xlsx.Write(&buf, []xlsx.Sheet{
		{
			Name: "Sản phẩm", Header: Columns(refs),
			Lists: map[string][]string{ColStatus: labels, ColCategory: slices.Sorted(maps.Keys(refs.Categories))},
		},
		{Name: "Hướng dẫn", Header: []string{"Cột", "Bắt buộc", "Cách điền"}, Rows: guide},
	})
	if err != nil {
		return nil, fmt.Errorf("ghi mẫu Excel sản phẩm: %w", err)
	}
	return buf.Bytes(), nil
}
