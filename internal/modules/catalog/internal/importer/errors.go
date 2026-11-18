package importer

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"elimaz.com.vn/crm/internal/platform/xlsx"
)

// Mã lỗi dòng (kế hoạch M1, Task M1-38; màn hình đổi mã thành câu cho nhân viên).
const (
	CodeMissingRequired           = "missing_required"
	CodeInvalidPrice              = "invalid_price"
	CodeInvalidStatus             = "invalid_status"
	CodeInvalidColorHex           = "invalid_color_hex"
	CodeInvalidSKU                = "invalid_sku"
	CodeInvalidCode               = "invalid_code" // Mã mẫu sai mẫu (thêm so với kế hoạch)
	CodeInvalidText               = "invalid_text" // tên, màu, size, mô tả quá dài hay có ký tự điều khiển (thêm)
	CodeTooManyVariants           = "too_many_variants"
	CodeDuplicateSKUInFile        = "duplicate_sku_in_file"
	CodeSKUBelongsToOtherProduct  = "sku_belongs_to_other_product"
	CodeDuplicateOptionInFile     = "duplicate_option_in_file"
	CodeOptionTaken               = "option_taken" // cùng màu, size với biến thể có sẵn không có trong tệp (thêm)
	CodeUnknownCategory           = "unknown_category"
	CodeUnknownAttributeValue     = "unknown_attribute_value"
	CodeUnknownTag                = "unknown_tag"
	CodeInconsistentProductFields = "inconsistent_product_fields"
	CodeFormulaNotAllowed         = xlsx.CodeFormulaNotAllowed
	CodeCellTooLong               = xlsx.CodeCellTooLong
	CodeInvalidBarcode            = "invalid_barcode"
)

// maxQuoted: lời báo chỉ chép nội dung ô khi ô không dài quá số ký tự này.
const maxQuoted = 50

// RowError là một lỗi của một ô: Row là số dòng Excel, Column là tiêu đề cột, Message là câu tiếng Việt (không chép
// ô dài quá 50 ký tự).
type RowError struct {
	Row     int    `json:"row"`
	Column  string `json:"column"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// quoted trả ` "v"` khi v ngắn, rỗng khi dài (để lời báo không chép nội dung dài).
func quoted(v string) string {
	if v == "" || utf8.RuneCountInString(v) > maxQuoted {
		return ""
	}
	return fmt.Sprintf(" %q", v)
}

// MergeCellErrors gộp lỗi ô của xlsx.Read với lỗi của Validate: lỗi Validate ở đúng ô đã có lỗi ô bị bỏ (ô đó đã
// bị đọc thành rỗng); kết quả xếp theo dòng, rồi theo thứ tự cột của mẫu.
func MergeCellErrors(cellErrs []xlsx.CellError, rowErrs []RowError) []RowError {
	type cell struct {
		row int
		col string
	}
	bad := make(map[cell]bool, len(cellErrs))
	out := make([]RowError, 0, len(cellErrs)+len(rowErrs))
	for _, e := range cellErrs {
		bad[cell{e.Row, e.Column}] = true
		msg := fmt.Sprintf("Ô %s là công thức: chỉ nhận chữ hoặc số, hãy dán giá trị", e.Column)
		if e.Code == xlsx.CodeCellTooLong {
			msg = fmt.Sprintf("Ô %s quá dài", e.Column)
		}
		out = append(out, RowError{Row: e.Row, Column: e.Column, Code: e.Code, Message: msg})
	}
	for _, e := range rowErrs {
		if !bad[cell{e.Row, e.Column}] {
			out = append(out, e)
		}
	}
	sortErrors(out)
	return out
}

// sortErrors xếp theo dòng, thứ tự cột của mẫu, mã.
func sortErrors(errs []RowError) {
	slices.SortStableFunc(errs, func(a, b RowError) int {
		return cmp.Or(cmp.Compare(a.Row, b.Row), cmp.Compare(columnRank(a.Column), columnRank(b.Column)),
			strings.Compare(a.Column, b.Column), strings.Compare(a.Code, b.Code))
	})
}

// columnRank: cột của mẫu theo thứ tự trong mẫu; cột thuộc tính nằm giữa cột mẫu và cột biến thể; cột lạ cuối cùng.
func columnRank(col string) int {
	if i := slices.Index(productColumns(), col); i >= 0 {
		return i
	}
	if strings.HasPrefix(col, AttributePrefix) {
		return len(productColumns())
	}
	if i := slices.Index(variantColumns(), col); i >= 0 {
		return len(productColumns()) + 1 + i
	}
	return 1 << 20
}
