package reporting

import "elimaz.com.vn/crm/internal/platform/files"

// Mục đích tệp xuất của M6 (kế hoạch M6, Task 226; dòng của platform.file_purposes ở migration 0604, R5).
const (
	// PurposeReportExport: tệp Excel của báo cáo và danh sách sản phẩm (BC04, Q25).
	PurposeReportExport = "report_export"
	// PurposeCustomerExport: tệp Excel danh sách khách (PQ06), tách riêng để giữ hạn ngắn hơn.
	PurposeCustomerExport = "customer_export"
	// XLSXContentType là loại tệp duy nhất hai mục đích nhận.
	XLSXContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	// MaxExportBytes: tệp xuất lớn nhất (100 MiB; 200.000 dòng của REPORT_MAX_ROWS vừa dưới mức này).
	MaxExportBytes = 100 << 20
)

// FilePurposes trả hai mục đích tệp xuất: bucket private, chỉ máy chủ ghi (Upload = false, trình duyệt không tải lên
// được), chỉ nhận .xlsx.
func FilePurposes() []files.Purpose {
	return []files.Purpose{
		{Key: PurposeReportExport, Bucket: files.BucketPrivate, AllowedTypes: []string{XLSXContentType}, MaxBytes: MaxExportBytes},
		{Key: PurposeCustomerExport, Bucket: files.BucketPrivate, AllowedTypes: []string{XLSXContentType}, MaxBytes: MaxExportBytes},
	}
}

// RegisterFilePurposes đăng ký hai mục đích tệp xuất vào sổ của tiến trình; cmd/crm gọi lúc khởi động, trước
// files.CheckPurposes. Gọi lại nhiều lần không sao.
func RegisterFilePurposes() {
	for _, p := range FilePurposes() {
		files.RegisterPurpose(p)
	}
}
