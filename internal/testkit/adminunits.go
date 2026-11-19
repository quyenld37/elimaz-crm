package testkit

import "testing"

// Mã đơn vị hành chính mẫu do SeedAdminUnits chèn: tỉnh SampleProvinceCode có xã SampleWardCode (mã có chữ T để không
// trùng danh mục thật). Dùng cho test của module cần địa chỉ đủ hai cấp (đơn hàng).
const (
	SampleProvinceCode = "T01"
	SampleWardCode     = "T01001"
)

// SeedAdminUnits chèn một tỉnh và một xã mẫu vào customer.admin_units bằng chủ bảng (crm_app không ghi được bảng
// này); chạy lại không lỗi. Test của chính customer tự chèn danh mục đầy đủ hơn của mình.
func (s *Staff) SeedAdminUnits(t testing.TB) {
	t.Helper()
	if _, err := s.Owner.Pool.Exec(t.Context(), `INSERT INTO customer.admin_units
		(code, level, parent_code, name, name_search, effective_from, source) VALUES
		($1, 'province', NULL, 'Thành phố Thử Một', 'thanh pho thu mot', DATE '2025-07-01', 'test'),
		($2, 'ward', $1, 'Phường Bến Thử', 'phuong ben thu', DATE '2025-07-01', 'test')
		ON CONFLICT DO NOTHING`, SampleProvinceCode, SampleWardCode); err != nil {
		t.Fatalf("testkit: chèn danh mục hành chính mẫu: %v", err)
	}
}
