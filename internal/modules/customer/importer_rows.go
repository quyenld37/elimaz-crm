package customer

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/xlsx"
)

// Mã lỗi dòng của tệp nhập (ImportError.Code), cộng hai mã ô của xlsx (formula_not_allowed, cell_too_long).
const (
	rowRequired          = "required"
	rowInvalidName       = "invalid_name"
	rowInvalidPhone      = "invalid_phone"
	rowInvalidDate       = "invalid_date"
	rowUnknownProvince   = "unknown_province"
	rowUnknownWard       = "unknown_ward"
	rowWardNotInProvince = "ward_not_in_province"
	rowInvalidText       = "invalid_text"
	rowTagTooLong        = "tag_too_long"
	rowTooManyTags       = "too_many_tags"
)

const (
	maxImportTags = 20
	// maxExcelSerial là số ngày kiểu Excel của 31/12/9999 (ngày 0 tính là 30/12/1899, đúng cho mọi ngày từ
	// 01/03/1900 do lỗi năm nhuận 1900 của Excel).
	maxExcelSerial = 2_958_465
)

// importRowInput là một dòng tệp đã kiểm, sẵn sàng ghi.
type importRowInput struct {
	row      int
	name     string
	phone    logging.Phone
	birthday *time.Time
	addr     *AddressInput
	tags     []string
	note     string
}

// parseImportRow kiểm một dòng tệp: trả dữ liệu đã chuẩn hóa, hoặc lỗi đầu tiên của dòng (theo thứ tự cột). Ô xlsx
// không đọc được (công thức, quá dài) là lỗi của dòng.
func parseImportRow(r xlsx.Row, cellErrs []xlsx.CellError, units *adminIndex, today time.Time) (importRowInput, *ImportError) {
	fail := func(col, code string) (importRowInput, *ImportError) {
		return importRowInput{}, &ImportError{Row: r.Number, Column: col, Code: code}
	}
	if len(cellErrs) > 0 {
		return fail(cellErrs[0].Column, cellErrs[0].Code)
	}
	cell := func(col string) string { return strings.TrimSpace(r.Cells[col]) }
	in := importRowInput{row: r.Number}
	var err error
	if in.name, err = validName(cell(colName)); err != nil {
		return fail(colName, requiredOr(cell(colName), rowInvalidName))
	}
	if in.phone, err = NormalizePhone(cell(colPhone)); err != nil {
		return fail(colPhone, requiredOr(cell(colPhone), rowInvalidPhone))
	}
	if raw := cell(colBirthday); raw != "" {
		bd, ok := parseBirthday(raw, today)
		if !ok {
			return fail(colBirthday, rowInvalidDate)
		}
		in.birthday = &bd
	}
	addr, col, code := units.address(cell(colProvince), cell(colWard), cell(colStreet))
	if code != "" {
		return fail(col, code)
	}
	if addr != nil {
		addr.Name, addr.Phone = in.name, string(in.phone)
		in.addr = addr
	}
	if in.tags, code = parseTags(cell(colTags)); code != "" {
		return fail(colTags, code)
	}
	if raw := cell(colNote); raw != "" {
		note, err := validNote(raw)
		if err != nil {
			return fail(colNote, rowInvalidText)
		}
		in.note = note
	}
	return in, nil
}

// requiredOr: ô trống → required, ngược lại code.
func requiredOr(raw, code string) string {
	if raw == "" {
		return rowRequired
	}
	return code
}

// parseBirthday đọc ngày sinh dd/mm/yyyy (d/m/yyyy cũng nhận) hoặc số ngày kiểu Excel (ô định dạng ngày, đọc giá trị
// thô, bỏ phần giờ sau dấu chấm); ngày phải từ năm 1900 và không sau today.
func parseBirthday(raw string, today time.Time) (time.Time, bool) {
	var t time.Time
	serial, frac, _ := strings.Cut(raw, ".") // ô ngày giờ: phần lẻ là giờ trong ngày
	if _, err := strconv.ParseUint("0"+frac, 10, 64); err != nil {
		serial = raw
	}
	if n, err := strconv.Atoi(serial); err == nil {
		if n < 1 || n > maxExcelSerial {
			return time.Time{}, false
		}
		t = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n)
	} else {
		var perr error
		if t, perr = time.Parse("2/1/2006", raw); perr != nil {
			return time.Time{}, false
		}
	}
	if t.Year() < 1900 || t.After(today) {
		return time.Time{}, false
	}
	return t, true
}

// parseTags tách nhãn theo ";", bỏ khoảng trắng hai đầu, bỏ ô rỗng và nhãn trùng (giữ thứ tự); quá 20 nhãn, nhãn quá
// 50 ký tự hay có ký tự điều khiển → mã lỗi.
func parseTags(raw string) ([]string, string) {
	if raw == "" {
		return nil, ""
	}
	var out []string
	seen := map[string]bool{}
	for part := range strings.SplitSeq(raw, ";") {
		tag := strings.TrimSpace(part)
		if tag == "" || seen[tag] {
			continue
		}
		if utf8.RuneCountInString(tag) > maxTagNameRunes {
			return nil, rowTagTooLong
		}
		if _, err := validText(tag, maxTagNameRunes); err != nil {
			return nil, rowInvalidText
		}
		seen[tag] = true
		out = append(out, tag)
	}
	if len(out) > maxImportTags {
		return nil, rowTooManyTags
	}
	return out, ""
}

// adminIndex đổi tên (có dấu hay không, có hay không tiền tố cấp) hoặc mã tỉnh, xã trong tệp sang mã của danh mục.
// Khóa trùng giữa hai đơn vị khác nhau bị đánh dấu mơ hồ (chuỗi rỗng) và không khớp.
type adminIndex struct {
	provinces map[string]string            // khóa → mã tỉnh
	wards     map[string]map[string]string // mã tỉnh → khóa → mã xã
	wardKeys  map[string]bool              // khóa có ở một tỉnh nào đó
}

// Tiền tố cấp hành chính (dạng bỏ dấu) được phép bỏ khi ghi tên trong tệp.
func provincePrefixes() []string { return []string{"thanh pho ", "tinh "} }
func wardPrefixes() []string     { return []string{"phuong ", "xa ", "dac khu ", "thi tran "} }

func loadAdminIndex(ctx context.Context, tx pgx.Tx) (*adminIndex, error) {
	rows, err := store.New(tx).AllAdminUnits(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc danh mục hành chính: %w", err)
	}
	idx := &adminIndex{provinces: map[string]string{}, wards: map[string]map[string]string{}, wardKeys: map[string]bool{}}
	for _, u := range rows {
		switch AdminLevel(u.Level) {
		case AdminProvince:
			addKeys(idx.provinces, u.Code, u.NameSearch, provincePrefixes())
		case AdminWard:
			parent := u.ParentCode.String
			if idx.wards[parent] == nil {
				idx.wards[parent] = map[string]string{}
			}
			for _, k := range addKeys(idx.wards[parent], u.Code, u.NameSearch, wardPrefixes()) {
				idx.wardKeys[k] = true
			}
		}
	}
	return idx, nil
}

// addKeys thêm các khóa của đơn vị code vào m: mã (chữ thường), tên bỏ dấu, tên bỏ tiền tố. Trả các khóa đã thêm.
func addKeys(m map[string]string, code, nameSearch string, prefixes []string) []string {
	keys := []string{strings.ToLower(code), nameSearch}
	for _, p := range prefixes {
		if rest, ok := strings.CutPrefix(nameSearch, p); ok && rest != "" {
			keys = append(keys, rest)
		}
	}
	for _, k := range keys {
		if old, ok := m[k]; ok && old != code {
			m[k] = "" // mơ hồ
			continue
		}
		m[k] = code
	}
	return keys
}

// lookup tìm khóa của s trong m: tên đầy đủ đã bỏ dấu trước, rồi tên bỏ tiền tố.
func lookup(m map[string]string, s string, prefixes []string) (string, bool) {
	key := NormalizeName(s)
	if code, ok := m[key]; ok {
		return code, code != ""
	}
	for _, p := range prefixes {
		if rest, ok := strings.CutPrefix(key, p); ok {
			if code, ok := m[rest]; ok {
				return code, code != ""
			}
		}
	}
	return "", false
}

// address kiểm ba cột địa chỉ: trống cả ba → nil; có một mà thiếu cột khác → required ở cột thiếu; tỉnh không có →
// unknown_province; xã không thuộc tỉnh (có ở tỉnh khác) → ward_not_in_province, không có ở đâu → unknown_ward.
// Trả (địa chỉ chưa có tên, SĐT người nhận; cột lỗi; mã lỗi).
func (idx *adminIndex) address(province, ward, street string) (*AddressInput, string, string) {
	if province == "" && ward == "" && street == "" {
		return nil, "", ""
	}
	for _, c := range []struct{ col, v string }{{colProvince, province}, {colWard, ward}, {colStreet, street}} {
		if c.v == "" {
			return nil, c.col, rowRequired
		}
	}
	pc, ok := lookup(idx.provinces, province, provincePrefixes())
	if !ok {
		return nil, colProvince, rowUnknownProvince
	}
	wc, ok := lookup(idx.wards[pc], ward, wardPrefixes())
	if !ok {
		if idx.anyWard(ward) {
			return nil, colWard, rowWardNotInProvince
		}
		return nil, colWard, rowUnknownWard
	}
	line, err := validText(street, maxLineRunes)
	if err != nil {
		return nil, colStreet, rowInvalidText
	}
	return &AddressInput{Line: line, ProvinceCode: pc, WardCode: wc}, "", ""
}

// anyWard: tên xã có ở một tỉnh nào đó không (để phân biệt "không thuộc tỉnh" với "không có").
func (idx *adminIndex) anyWard(ward string) bool {
	key := NormalizeName(ward)
	if idx.wardKeys[key] {
		return true
	}
	for _, p := range wardPrefixes() {
		if rest, ok := strings.CutPrefix(key, p); ok && idx.wardKeys[rest] {
			return true
		}
	}
	return false
}
