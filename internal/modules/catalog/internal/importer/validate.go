package importer

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/platform/xlsx"
)

// pricePattern: số nguyên đồng viết liền (350000) hoặc nhóm ba chữ số bằng dấu chấm (350.000).
var pricePattern = regexp.MustCompile(`^(?:[0-9]+|[0-9]{1,3}(?:\.[0-9]{3})+)$`)

// measurePattern: số đo là số nguyên viết liền (88), không số lẻ.
var measurePattern = regexp.MustCompile(`^[0-9]{1,4}$`)

// line là kết quả đọc một dòng: phần mẫu (product), phần biến thể (variant) và ô nào đã có lỗi.
type line struct {
	n        int
	code     string            // đã chuẩn hóa; rỗng khi thiếu hay sai
	keys     map[string]string // cột của mẫu → giá trị chuẩn hóa để so giữa các dòng cùng mã
	bad      map[string]bool   // ô đã có lỗi
	product  PlannedProduct
	variant  PlannedVariant
	explicit bool // ô Barcode có giá trị
}

// validator gom lỗi của một lần Validate.
type validator struct {
	refs     Refs
	attrCols []string // mọi cột "Thuộc tính: …" có ở tệp, theo tên
	errs     []RowError
}

// attributeColumns: các cột thuộc tính có giá trị ở ít nhất một dòng (Row.Cells chỉ giữ ô có giá trị), xếp theo tên.
func attributeColumns(rows []xlsx.Row) []string {
	seen := map[string]bool{}
	for _, r := range rows {
		for col := range r.Cells {
			if strings.HasPrefix(col, AttributePrefix) {
				seen[col] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

func (v *validator) fail(l *line, col, code, msg string) {
	l.bad[col] = true
	v.errs = append(v.errs, RowError{Row: l.n, Column: col, Code: code, Message: msg})
}

// Validate kiểm từng dòng của tệp (đã đọc bằng xlsx.Read với RequiredColumns) theo refs và trả kế hoạch ghi. Có lỗi
// thì Plan rỗng (nhập là tất cả hoặc không, Task M1-47) và lỗi xếp theo dòng, cột. Mỗi ô báo tối đa một lỗi.
//
// Validate không thấy lỗi ô của xlsx.Read (ô công thức, ô quá dài đọc thành rỗng; ô Mã màu, Giá nhập rỗng của biến
// thể có sẵn là "xóa"): bên gọi chỉ áp Plan khi MergeCellErrors(cellErrs, lỗi của Validate) rỗng.
func Validate(rows []xlsx.Row, refs Refs) (Plan, []RowError) {
	v := &validator{refs: refs, attrCols: attributeColumns(rows)}
	lines := make([]*line, len(rows))
	for i, r := range rows {
		lines[i] = v.parse(r)
	}
	groups := v.group(lines)
	v.checkSKUs(lines)
	v.checkExistingOptions(lines)
	if len(v.errs) > 0 {
		sortErrors(v.errs)
		return Plan{}, v.errs
	}
	return v.plan(groups), nil
}

func (v *validator) parse(r xlsx.Row) *line {
	l := &line{n: r.Number, keys: map[string]string{}, bad: map[string]bool{}}
	cell := func(col string) string { return strings.TrimSpace(r.Cells[col]) }
	required := func(col string) (string, bool) {
		s := cell(col)
		if s == "" {
			v.fail(l, col, CodeMissingRequired, "Thiếu "+col)
		}
		return s, s != ""
	}
	if s, ok := required(ColCode); ok {
		if c := strings.ToUpper(s); CodePattern.MatchString(c) {
			l.code = c
		} else {
			v.fail(l, ColCode, CodeInvalidCode, "Mã mẫu"+quoted(s)+" chỉ được có chữ không dấu, số, dấu . _ - (tối đa 64 ký tự)")
		}
	}
	if s, ok := required(ColName); ok {
		l.product.Name = v.text(l, ColName, s, MaxName, false)
	}
	desc := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(r.Cells[ColDescription], "\r\n", "\n"), "\r", "\n"))
	l.product.Description = v.text(l, ColDescription, desc, MaxDescription, true)
	l.product.Status = v.status(l, cell(ColStatus))
	l.product.CategoryID = v.category(l, cell(ColCategory))
	l.product.TagIDs = v.tags(l, cell(ColTags))
	l.product.AttributeValueIDs = v.attributes(l, r)
	l.product.LengthCm = v.measure(l, ColLengthCm, cell(ColLengthCm))
	l.product.WidthCm = v.measure(l, ColWidthCm, cell(ColWidthCm))
	l.product.HeightCm = v.measure(l, ColHeightCm, cell(ColHeightCm))
	l.keys[ColName], l.keys[ColDescription], l.keys[ColStatus] = l.product.Name, l.product.Description, l.product.Status
	v.parseVariant(l, cell, required)
	return l
}

func (v *validator) parseVariant(l *line, cell func(string) string, required func(string) (string, bool)) {
	pv := &l.variant
	pv.Row = l.n
	if s, ok := required(ColColor); ok {
		pv.Color = v.text(l, ColColor, s, MaxOption, false)
	}
	if s, ok := required(ColSize); ok {
		pv.Size = v.text(l, ColSize, s, MaxOption, false)
	}
	if s := strings.ToUpper(cell(ColColorHex)); s != "" {
		if ColorHexPattern.MatchString(s) {
			pv.ColorHex = s
		} else {
			v.fail(l, ColColorHex, CodeInvalidColorHex, "Mã màu"+quoted(cell(ColColorHex))+" phải có dạng #RRGGBB, ví dụ #FF0000")
		}
	}
	if s, ok := required(ColSKU); ok {
		if c := strings.ToUpper(s); CodePattern.MatchString(c) {
			pv.SKU = c
		} else {
			v.fail(l, ColSKU, CodeInvalidSKU, "SKU"+quoted(s)+" chỉ được có chữ không dấu, số, dấu . _ - (tối đa 64 ký tự)")
		}
	}
	if s := cell(ColBarcode); s != "" {
		if BarcodePattern.MatchString(s) {
			pv.Barcode, l.explicit = s, true
		} else {
			v.fail(l, ColBarcode, CodeInvalidBarcode, "Barcode"+quoted(s)+" chỉ được có 4–32 chữ không dấu, số, dấu -")
		}
	}
	if s, ok := required(ColPrice); ok {
		if p, ok := v.money(l, ColPrice, s); ok {
			pv.Price = p
		}
	}
	if s := cell(ColCostPrice); s != "" {
		if p, ok := v.money(l, ColCostPrice, s); ok {
			pv.CostPrice = &p
		}
	}
}

// text kiểm một ô chữ (một dòng, hoặc nhiều dòng khi multiline) dài tối đa limit ký tự, không ký tự điều khiển.
func (v *validator) text(l *line, col, s string, limit int, multiline bool) string {
	bad := utf8.RuneCountInString(s) > limit || strings.ContainsFunc(s, func(r rune) bool {
		return (r != '\n' || !multiline) && (r < 0x20 || (r >= 0x7f && r <= 0x9f))
	})
	if bad {
		v.fail(l, col, CodeInvalidText, fmt.Sprintf("%s dài quá %d ký tự hoặc có ký tự điều khiển", col, limit))
		return ""
	}
	return s
}

func (v *validator) money(l *line, col, s string) (int64, bool) {
	if pricePattern.MatchString(s) {
		if p, err := strconv.ParseInt(strings.ReplaceAll(s, ".", ""), 10, 64); err == nil && p <= MaxMoney {
			return p, true
		}
	}
	v.fail(l, col, CodeInvalidPrice, col+quoted(s)+" không hợp lệ: số nguyên đồng không âm, ví dụ 350000 hoặc 350.000")
	return 0, false
}

// measure đọc một ô số đo (rỗng → nil) và ghi giá trị vào khóa so giữa các dòng cùng mã.
func (v *validator) measure(l *line, col, s string) *int {
	l.keys[col] = s
	if s == "" {
		return nil
	}
	if measurePattern.MatchString(s) {
		if n, err := strconv.Atoi(s); err == nil && n >= MinMeasureCm && n <= MaxMeasureCm {
			l.keys[col] = strconv.Itoa(n)
			return &n
		}
	}
	v.fail(l, col, CodeInvalidMeasure, fmt.Sprintf("%s%s không hợp lệ: số nguyên cm từ %d đến %d", col, quoted(s),
		MinMeasureCm, MaxMeasureCm))
	return nil
}

func (v *validator) status(l *line, s string) string {
	if s == "" {
		return ""
	}
	for _, x := range statusLabels() {
		if strings.EqualFold(s, x.label) {
			return x.status
		}
	}
	v.fail(l, ColStatus, CodeInvalidStatus, "Trạng thái"+quoted(s)+" không có: chọn Đang nhập, Đang bán hoặc Ngừng bán")
	return ""
}

func (v *validator) category(l *line, s string) *uuid.UUID {
	if s == "" {
		l.keys[ColCategory] = ""
		return nil
	}
	path := CategoryPath(strings.Split(s, ">")...)
	l.keys[ColCategory] = path
	id, ok := v.refs.Categories[path]
	if !ok {
		v.fail(l, ColCategory, CodeUnknownCategory, "Danh mục"+quoted(path)+" không có")
		return nil
	}
	return &id
}

// splitList tách ô nhiều giá trị (dấu ;), bỏ khoảng trắng, ô trống, giá trị trùng; giữ thứ tự.
func splitList(s string, fold func(string) string) []string {
	var out []string
	for p := range strings.SplitSeq(s, ";") {
		if p = fold(strings.TrimSpace(p)); p != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

func (v *validator) tags(l *line, s string) []uuid.UUID {
	codes := splitList(s, strings.ToLower)
	l.keys[ColTags] = strings.Join(slices.Sorted(slices.Values(codes)), ";")
	ids := make([]uuid.UUID, 0, len(codes))
	for _, c := range codes {
		id, ok := v.refs.Tags[c]
		if !ok {
			v.fail(l, ColTags, CodeUnknownTag, "Nhãn"+quoted(c)+" không có")
			return nil
		}
		ids = append(ids, id)
	}
	return sortedIDs(ids)
}

// attributes đọc mọi cột "Thuộc tính: <tên>" của dòng (theo tên cột) và trả mã giá trị của cả mẫu.
func (v *validator) attributes(l *line, r xlsx.Row) []uuid.UUID {
	var ids []uuid.UUID
	for _, col := range v.attrCols {
		name, _ := strings.CutPrefix(col, AttributePrefix)
		values := splitList(r.Cells[col], func(s string) string { return s })
		l.keys[col] = strings.Join(slices.Sorted(slices.Values(values)), ";")
		known, attrOK := v.refs.Attributes[strings.TrimSpace(name)]
		for _, val := range values {
			id, ok := known[val]
			if !attrOK || !ok {
				v.fail(l, col, CodeUnknownAttributeValue, "Thuộc tính "+strings.TrimSpace(name)+" không có giá trị"+quoted(val))
				break
			}
			ids = append(ids, id)
		}
	}
	return sortedIDs(ids)
}

func sortedIDs(ids []uuid.UUID) []uuid.UUID {
	out := slices.SortedFunc(slices.Values(ids), func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return slices.Compact(out)
}

// group gom dòng theo mã mẫu (theo thứ tự xuất hiện) và kiểm luật trong một mẫu: cột mẫu giống nhau, không hai dòng
// cùng màu và size, tối đa MaxVariants biến thể.
func (v *validator) group(lines []*line) [][]*line {
	index := map[string]int{}
	var groups [][]*line
	for _, l := range lines {
		if l.code == "" {
			continue
		}
		i, ok := index[l.code]
		if !ok {
			i = len(groups)
			index[l.code] = i
			groups = append(groups, nil)
		}
		groups[i] = append(groups[i], l)
	}
	for _, g := range groups {
		v.checkConsistent(g)
		options := map[[2]string]bool{}
		for i, l := range g {
			if i == MaxVariants {
				v.fail(l, ColSKU, CodeTooManyVariants, fmt.Sprintf("Một mẫu có tối đa %d biến thể", MaxVariants))
			}
			if l.bad[ColColor] || l.bad[ColSize] {
				continue
			}
			key := [2]string{l.variant.Color, l.variant.Size}
			if options[key] {
				v.fail(l, ColSize, CodeDuplicateOptionInFile, "Mẫu đã có dòng cùng màu"+quoted(key[0])+" và size"+quoted(key[1]))
			}
			options[key] = true
		}
	}
	return groups
}

// checkConsistent: mỗi cột của mẫu so với dòng đầu tiên của mẫu mà ô đó không lỗi.
func (v *validator) checkConsistent(g []*line) {
	cols := slices.Sorted(maps.Keys(g[0].keys))
	for _, col := range cols {
		var base *line
		for _, l := range g {
			if l.bad[col] {
				continue
			}
			if base == nil {
				base = l
				continue
			}
			if l.keys[col] != base.keys[col] {
				v.fail(l, col, CodeInconsistentProductFields,
					fmt.Sprintf("%s khác dòng %d cùng mã mẫu %s", col, base.n, l.code))
			}
		}
	}
}

// checkSKUs: SKU không trùng trong tệp và không thuộc mẫu khác; barcode gõ tay không trùng trong tệp và không thuộc
// biến thể khác.
func (v *validator) checkSKUs(lines []*line) {
	skuRow := map[string]int{}
	barcodeRow := map[string]int{}
	barcodeSKU := make(map[string]string, len(v.refs.ExistingSKUs)+len(v.refs.ExistingBarcodes))
	maps.Copy(barcodeSKU, v.refs.ExistingBarcodes)
	for sku, ex := range v.refs.ExistingSKUs {
		if ex.Barcode != "" {
			barcodeSKU[ex.Barcode] = sku
		}
	}
	for _, l := range lines {
		if l.variant.SKU != "" {
			v.checkSKU(l, skuRow)
		}
		if l.explicit {
			v.checkBarcode(l, barcodeRow, barcodeSKU)
		}
	}
}

func (v *validator) checkSKU(l *line, skuRow map[string]int) {
	sku := l.variant.SKU
	if first, dup := skuRow[sku]; dup {
		v.fail(l, ColSKU, CodeDuplicateSKUInFile, fmt.Sprintf("SKU %s trùng dòng %d", sku, first))
		return
	}
	skuRow[sku] = l.n
	if ex, ok := v.refs.ExistingSKUs[sku]; ok && l.code != "" && ex.ProductCode != l.code {
		v.fail(l, ColSKU, CodeSKUBelongsToOtherProduct, fmt.Sprintf("SKU %s đã thuộc mẫu %s", sku, ex.ProductCode))
	}
}

func (v *validator) checkBarcode(l *line, barcodeRow map[string]int, barcodeSKU map[string]string) {
	b := l.variant.Barcode
	if first, dup := barcodeRow[b]; dup {
		v.fail(l, ColBarcode, CodeInvalidBarcode, fmt.Sprintf("Barcode trùng dòng %d", first))
		return
	}
	barcodeRow[b] = l.n
	if owner, ok := barcodeSKU[b]; ok && owner != l.variant.SKU {
		v.fail(l, ColBarcode, CodeInvalidBarcode, "Barcode đã thuộc SKU "+owner)
	}
}

// checkExistingOptions: biến thể của mẫu có sẵn không được lấy màu, size đang thuộc một biến thể có sẵn khác của mẫu
// đó, trừ khi biến thể kia cũng có trong tệp (nó được sửa sang màu, size khác hay giữ nguyên chỗ của mình).
func (v *validator) checkExistingOptions(lines []*line) {
	inFile := make(map[string]bool, len(lines))
	for _, l := range lines {
		if l.variant.SKU != "" {
			inFile[l.variant.SKU] = true
		}
	}
	for _, l := range lines {
		ex, ok := v.refs.ExistingProducts[l.code]
		if !ok || l.code == "" || l.variant.SKU == "" || l.bad[ColColor] || l.bad[ColSize] || l.bad[ColSKU] {
			continue
		}
		owner, taken := ex.Options[Option{Color: l.variant.Color, Size: l.variant.Size}]
		if taken && owner != l.variant.SKU && !inFile[owner] {
			v.fail(l, ColSize, CodeOptionTaken, fmt.Sprintf("Mẫu %s đã có SKU %s cùng màu%s và size%s", l.code, owner,
				quoted(l.variant.Color), quoted(l.variant.Size)))
		}
	}
}

// plan dựng kế hoạch của tệp không lỗi.
func (v *validator) plan(groups [][]*line) Plan {
	out := Plan{Products: make([]PlannedProduct, 0, len(groups))}
	for _, g := range groups {
		p := g[0].product
		p.Code = g[0].code
		p.Variants = make([]PlannedVariant, 0, len(g))
		if ex, ok := v.refs.ExistingProducts[p.Code]; ok {
			pid := ex.ID
			p.ExistingProductID = &pid
		}
		for _, l := range g {
			pv := l.variant
			pv.Action = ActionCreate
			if ex, ok := v.refs.ExistingSKUs[pv.SKU]; ok {
				pid, vid := ex.ProductID, ex.ID
				p.ExistingProductID, pv.ExistingID, pv.ExistingVersion = &pid, &vid, ex.Version
				if !l.explicit {
					pv.Barcode = ex.Barcode
				}
				pv.Action = ActionUpdate
				if sameVariant(pv, ex) {
					pv.Action = ActionUnchanged
				}
			}
			p.Variants = append(p.Variants, pv)
		}
		out.Products = append(out.Products, p)
	}
	return out
}

func sameVariant(pv PlannedVariant, ex ExistingVariant) bool {
	sameCost := (pv.CostPrice == nil) == (ex.CostPrice == nil) && (pv.CostPrice == nil || *pv.CostPrice == *ex.CostPrice)
	return sameCost && pv.Barcode == ex.Barcode && pv.Color == ex.Color && pv.ColorHex == ex.ColorHex &&
		pv.Size == ex.Size && pv.Price == ex.Price
}
