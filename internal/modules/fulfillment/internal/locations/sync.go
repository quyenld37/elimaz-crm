// Package locations khớp địa giới của CRM (bộ 2025, customer.admin_units_v) với địa giới của từng hãng vận chuyển
// (kế hoạch M3, Task M3-18, Q-19): so tên đã chuẩn hóa trong cùng cha đã khớp; tên trùng thì không ghép (không gửi
// đoán). Gói không gọi hãng, không đọc database: lệnh đồng bộ của fulfillment làm việc đó.
package locations

import (
	"slices"
	"strings"
	"unicode"

	"elimaz.com.vn/crm/internal/platform/textnorm"
)

// prefixes là tiền tố cấp hành chính bỏ đi khi so tên (sau textnorm.Fold, không dấu câu); dài trước ngắn. "tp", "p"
// là cách hãng viết tắt "thành phố", "phường" ("TP. Hồ Chí Minh", "P. Tân Định").
func prefixes() []string {
	return []string{"thanh pho", "dac khu", "phuong", "tinh", "tp", "xa", "p"}
}

// Normalize trả tên để so: bỏ dấu, chữ thường (textnorm.Fold), dấu câu thành khoảng trắng (giữ dấu nháy đơn của tên
// như "H'Leo"), gộp khoảng trắng, bỏ một tiền tố cấp hành chính đứng đầu là một từ trọn. Chỉ có tiền tố → rỗng.
func Normalize(name string) string {
	s := strings.Map(func(r rune) rune {
		if r == '\'' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return ' '
	}, textnorm.Fold(name))
	s = strings.Join(strings.Fields(s), " ")
	for _, p := range prefixes() {
		if s == p {
			return ""
		}
		if rest, ok := strings.CutPrefix(s, p+" "); ok {
			return rest
		}
	}
	return s
}

// Unit là một đơn vị của CRM (mã, tên gốc) trong một nhóm cùng cha.
type Unit struct {
	Code, Name string
}

// Remote là một đơn vị của hãng (mã, tên gốc của hãng) trong nhóm tương ứng.
type Remote struct {
	Code, Name string
}

// Pair là một mã CRM đã khớp với mã, tên của hãng.
type Pair struct {
	OurCode, CarrierCode, CarrierName string
}

// Match ghép ours với theirs (cùng một nhóm: mọi tỉnh, hay xã của một tỉnh đã khớp) theo tên đã Normalize: chỉ ghép khi
// tên khác rỗng và đúng một đơn vị mỗi bên mang tên đó. Trả cặp theo mã CRM tăng dần và số mã CRM chưa ghép được.
func Match(ours []Unit, theirs []Remote) ([]Pair, int) {
	byName := make(map[string][]Remote, len(theirs))
	for _, r := range theirs {
		if n := Normalize(r.Name); n != "" {
			byName[n] = append(byName[n], r)
		}
	}
	ourCount := make(map[string]int, len(ours))
	for _, u := range ours {
		ourCount[Normalize(u.Name)]++
	}
	pairs := make([]Pair, 0, len(ours))
	for _, u := range ours {
		n := Normalize(u.Name)
		if rs := byName[n]; n != "" && ourCount[n] == 1 && len(rs) == 1 {
			pairs = append(pairs, Pair{OurCode: u.Code, CarrierCode: rs[0].Code, CarrierName: rs[0].Name})
		}
	}
	slices.SortFunc(pairs, func(a, b Pair) int { return strings.Compare(a.OurCode, b.OurCode) })
	return pairs, len(ours) - len(pairs)
}
