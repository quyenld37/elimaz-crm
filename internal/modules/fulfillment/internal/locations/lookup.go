package locations

// Row là một dòng của bảng đổi mã của một hãng (fulfillment.carrier_locations).
type Row struct {
	Level, OurCode, CarrierCode string
}

type key struct{ level, our string }

// Map là bảng đổi mã địa giới của CRM sang mã của một hãng, đọc một lần trong transaction của bên gọi; hiện thực
// fulfillment.LocationMap. Giá trị rỗng không có mã nào.
type Map struct {
	m map[key]string
}

// NewMap dựng Map từ rows.
func NewMap(rows []Row) Map {
	m := make(map[key]string, len(rows))
	for _, r := range rows {
		m[key{r.Level, r.OurCode}] = r.CarrierCode
	}
	return Map{m: m}
}

// Carrier trả mã của hãng cho mã ourCode của CRM ở cấp level (province, ward); không có → ("", false): bên gọi trả
// address_unmapped, không gửi đoán.
func (m Map) Carrier(level, ourCode string) (string, bool) {
	c, ok := m.m[key{level, ourCode}]
	return c, ok
}
