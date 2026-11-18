package importer

// Mã lỗi dòng chỉ có lúc áp (kế hoạch M1, Task M1-47b): dữ liệu đổi giữa lúc kiểm và lúc áp.
const (
	CodeCodeTaken              = "code_taken"               // mã mẫu mới vừa được tạo (hay thuộc mẫu đã xóa)
	CodeBarcodeTaken           = "barcode_taken"            // barcode vừa bị biến thể khác lấy
	CodeChangedSinceValidation = "changed_since_validation" // mẫu, biến thể, danh mục vừa bị sửa hay xóa
)

// Recheck so plan (đã kiểm từ trước) với dữ liệu lúc áp now (ExistingSKUs theo mọi SKU của plan, ExistingProducts
// theo mọi mã mẫu của plan, đọc sau khi đã khóa) và trả lỗi theo dòng: mã mẫu mới vừa có người tạo; mẫu có sẵn vừa bị
// xóa; SKU mới vừa bị lấy; biến thể sửa hay giữ nguyên vừa đổi version, bị xóa hay sang mẫu khác. Rỗng là áp được
// (unique của database vẫn chặn phần Recheck không thấy: barcode, màu+size).
func Recheck(p Plan, now Refs) []RowError {
	var errs []RowError
	for _, pp := range p.Products {
		if len(pp.Variants) == 0 {
			continue
		}
		first := pp.Variants[0].Row
		ex, has := now.ExistingProducts[pp.Code]
		switch {
		case pp.ExistingProductID == nil && has:
			errs = append(errs, RowError{Row: first, Column: ColCode, Code: CodeCodeTaken, Message: "Mã mẫu" +
				quoted(pp.Code) + " vừa được tạo sau khi kiểm tệp (hoặc thuộc mẫu đã xóa): hãy tải tệp lên lại để kiểm"})
		case pp.ExistingProductID != nil && (!has || ex.ID != *pp.ExistingProductID):
			errs = append(errs, changed(first, ColCode, "Mẫu"+quoted(pp.Code)+" vừa bị xóa hay đổi"))
		}
		for _, v := range pp.Variants {
			if e, bad := recheckVariant(pp.Code, v, now); bad {
				errs = append(errs, e)
			}
		}
	}
	sortErrors(errs)
	return errs
}

func recheckVariant(code string, v PlannedVariant, now Refs) (RowError, bool) {
	cur, has := now.ExistingSKUs[v.SKU]
	switch {
	case has && cur.ProductCode != code:
		return RowError{Row: v.Row, Column: ColSKU, Code: CodeSKUBelongsToOtherProduct, Message: "SKU" + quoted(v.SKU) +
			" vừa được dùng cho mẫu" + quoted(cur.ProductCode) + " sau khi kiểm tệp"}, true
	case v.Action == ActionCreate && has:
		return changed(v.Row, ColSKU, "SKU"+quoted(v.SKU)+" vừa được tạo"), true
	case v.Action != ActionCreate && (!has || v.ExistingID == nil || cur.ID != *v.ExistingID ||
		cur.Version != v.ExistingVersion):
		return changed(v.Row, ColSKU, "Biến thể"+quoted(v.SKU)+" vừa bị sửa hay xóa"), true
	}
	return RowError{}, false
}

func changed(row int, col, what string) RowError {
	return RowError{
		Row: row, Column: col, Code: CodeChangedSinceValidation,
		Message: what + " sau khi kiểm tệp: hãy tải tệp lên lại để kiểm",
	}
}
