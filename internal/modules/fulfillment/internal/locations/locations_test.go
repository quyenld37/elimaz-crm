package locations_test

import (
	"fmt"
	"slices"
	"testing"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/locations"
)

// Tên của hai bên khớp sau khi bỏ dấu, chữ thường, bỏ dấu câu và tiền tố cấp hành chính (kế hoạch M3, Task M3-18).
func TestNormalize(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{"TP. Hồ Chí Minh", "Thành phố Hồ Chí Minh", "ho chi minh"},
		{"TP Hồ Chí Minh", "Thành  phố  Hồ Chí Minh", "ho chi minh"},
		{"Hà Nội", "Thành phố Hà Nội", "ha noi"},
		{"Tỉnh Gia Lai", "Gia Lai", "gia lai"},
		{"Phuong Ba Dinh", "Phường Ba Đình", "ba dinh"},
		{"P. Ba Đình", "phường ba đình", "ba dinh"},
		{"Xã Ea H'Leo", "xa ea h'leo", "ea h'leo"},
		{"Đặc khu Phú Quốc", "Phú Quốc", "phu quoc"},
		{"Phường 1", "phuong 1", "1"},
		{"Xã Tân Phú, ", "Tân Phú", "tan phu"},
	}
	for _, c := range cases {
		if got := locations.Normalize(c.a); got != c.want {
			t.Errorf("Normalize(%q) = %q, muốn %q", c.a, got, c.want)
		}
		if got := locations.Normalize(c.b); got != c.want {
			t.Errorf("Normalize(%q) = %q, muốn %q", c.b, got, c.want)
		}
	}
	// Chỉ bỏ tiền tố là một từ trọn: "Tpx" không phải "TP", "Xã" một mình thành rỗng (không khớp với gì).
	for in, want := range map[string]string{"Tphcm": "tphcm", "Xã": "", "Phường Xã Đàn": "xa dan", "Thành phố": ""} {
		if got := locations.Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, muốn %q", in, got, want)
		}
	}
}

// Ghép theo tên đã chuẩn hóa: tên trùng ở bên nào cũng không ghép (không đoán); tên rỗng không ghép; kết quả theo mã CRM.
func TestMatch(t *testing.T) {
	ours := []locations.Unit{
		{Code: "79", Name: "Thành phố Hồ Chí Minh"},
		{Code: "01", Name: "Thành phố Hà Nội"},
		{Code: "64", Name: "Tỉnh Gia Lai"},
		{Code: "50", Name: "Tỉnh Trùng Tên"},
		{Code: "51", Name: "Trùng Tên"},
		{Code: "60", Name: "Tỉnh Hai Bên"},
		{Code: "70", Name: "Xã"},
	}
	theirs := []locations.Remote{
		{Code: "HCM", Name: "TP. Hồ Chí Minh"},
		{Code: "HN", Name: "Hà Nội"},
		{Code: "GL", Name: "Tỉnh Gia Lai"},
		{Code: "TT", Name: "Trùng Tên"},
		{Code: "HB1", Name: "Hai Bên"},
		{Code: "HB2", Name: "Tỉnh Hai Ben"},
		{Code: "X", Name: "Phường"},
	}
	pairs, unmatched := locations.Match(ours, theirs)
	want := []locations.Pair{
		{OurCode: "01", CarrierCode: "HN", CarrierName: "Hà Nội"},
		{OurCode: "64", CarrierCode: "GL", CarrierName: "Tỉnh Gia Lai"},
		{OurCode: "79", CarrierCode: "HCM", CarrierName: "TP. Hồ Chí Minh"},
	}
	if !slices.Equal(pairs, want) || unmatched != 4 {
		t.Fatalf("Match = %+v, chưa khớp %d; muốn %+v, 4", pairs, unmatched, want)
	}
}

// Quy luật của Match: mỗi mã CRM, mỗi mã hãng ghép tối đa một lần; hai tên của một cặp chuẩn hóa bằng nhau và khác
// rỗng; số cặp + số chưa khớp = số mã CRM.
func TestMatch_Properties(t *testing.T) {
	names := []string{"Ba Đình", "Ba Dinh", "Phường Ba Đình", "Hoàn Kiếm", "TP. Hoàn Kiếm", "Sài Gòn", "Xã", "", "Gia Lai"}
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(0, 8).Draw(rt, "ours")
		ours := make([]locations.Unit, n)
		for i := range ours {
			ours[i] = locations.Unit{Code: fmt.Sprintf("O%02d", i), Name: rapid.SampledFrom(names).Draw(rt, "ourName")}
		}
		m := rapid.IntRange(0, 8).Draw(rt, "theirs")
		theirs := make([]locations.Remote, m)
		for i := range theirs {
			theirs[i] = locations.Remote{Code: fmt.Sprintf("C%02d", i), Name: rapid.SampledFrom(names).Draw(rt, "theirName")}
		}
		pairs, unmatched := locations.Match(ours, theirs)
		if len(pairs)+unmatched != n {
			rt.Fatalf("cặp %d + chưa khớp %d != %d", len(pairs), unmatched, n)
		}
		seenOur, seenTheir := map[string]bool{}, map[string]bool{}
		ourName := map[string]string{}
		for _, u := range ours {
			ourName[u.Code] = u.Name
		}
		for _, p := range pairs {
			if seenOur[p.OurCode] || seenTheir[p.CarrierCode] {
				rt.Fatalf("mã ghép hai lần: %+v", p)
			}
			seenOur[p.OurCode], seenTheir[p.CarrierCode] = true, true
			a, b := locations.Normalize(ourName[p.OurCode]), locations.Normalize(p.CarrierName)
			if a == "" || a != b {
				rt.Fatalf("cặp %+v: %q != %q", p, a, b)
			}
		}
	})
}

// Map tra theo (cấp, mã CRM); cấp khác hay mã không có → false.
func TestMap(t *testing.T) {
	m := locations.NewMap([]locations.Row{
		{Level: "province", OurCode: "79", CarrierCode: "HCM"}, {Level: "ward", OurCode: "26734", CarrierCode: "W9"},
	})
	if c, ok := m.Carrier("province", "79"); !ok || c != "HCM" {
		t.Errorf("tỉnh 79 = %q %v", c, ok)
	}
	if c, ok := m.Carrier("ward", "26734"); !ok || c != "W9" {
		t.Errorf("phường 26734 = %q %v", c, ok)
	}
	for _, k := range [][2]string{{"ward", "79"}, {"province", "26734"}, {"ward", "00000"}, {"", ""}} {
		if c, ok := m.Carrier(k[0], k[1]); ok {
			t.Errorf("Carrier(%q, %q) = %q, muốn không có", k[0], k[1], c)
		}
	}
	if _, ok := (locations.Map{}).Carrier("province", "79"); ok {
		t.Error("Map rỗng phải trả false")
	}
}
