package formats

import "elimaz.com.vn/crm/internal/modules/fulfillment"

// ElimazV1Key là khóa của định dạng chung của CRM (Q-16): kế toán dán số khi hãng đổi mẫu, khi chưa có mẫu của hãng.
const ElimazV1Key = "elimaz_v1"

// ElimazV1 là định dạng chung: dòng 1 là tiêu đề, cột ma_van_don, tien_cod, phi_van_chuyen, thuc_nhan bắt buộc;
// phi_khac, ngay_chuyen tùy chọn; ngày dd/mm/yyyy, d/m/yyyy hoặc yyyy-mm-dd (ô ngày của .xlsx đọc theo số ngày Excel).
// Dùng cho mọi hãng (CarrierKey rỗng).
func ElimazV1() Format {
	return Format{
		Key: ElimazV1Key, Version: 1, HeaderRow: 1,
		Columns: map[fulfillment.StatementField][]string{
			fulfillment.FieldTrackingCode: {"ma_van_don"},
			fulfillment.FieldCOD:          {"tien_cod"},
			fulfillment.FieldFee:          {"phi_van_chuyen"},
			fulfillment.FieldOtherFee:     {"phi_khac"},
			fulfillment.FieldNet:          {"thuc_nhan"},
			fulfillment.FieldPaidOn:       {"ngay_chuyen"},
		},
		Required: []fulfillment.StatementField{
			fulfillment.FieldTrackingCode, fulfillment.FieldCOD, fulfillment.FieldFee, fulfillment.FieldNet,
		},
		DateLayouts: []string{"02/01/2006", "2/1/2006", "2006-01-02"},
	}
}
