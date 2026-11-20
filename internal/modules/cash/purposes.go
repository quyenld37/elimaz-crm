package cash

import "elimaz.com.vn/crm/internal/platform/files"

// Mục đích tệp bảng kê COD (kế hoạch M3, Task M3-04; dòng của platform.file_purposes ở migration 0306).
const (
	StatementPurposeKey = "cod_statement"
	// MaxStatementBytes: bảng kê lớn nhất nhận tải lên (10 MiB).
	MaxStatementBytes = 10 << 20
)

// StatementPurpose là mục đích tệp bảng kê COD: kế toán tải lên .xlsx hay .csv, ≤ 10 MiB, bucket private. Hạn giữ 400
// ngày đặt bằng ExpiresAt của tệp khi nhận bảng kê (Task M3-31).
func StatementPurpose() files.Purpose {
	return files.Purpose{
		Key: StatementPurposeKey, Bucket: files.BucketPrivate,
		AllowedTypes: []string{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "text/csv"},
		MaxBytes:     MaxStatementBytes, Upload: true,
	}
}

// RegisterFilePurposes đăng ký mục đích tệp của cash (cod_statement) vào sổ của tiến trình; cmd/crm gọi lúc khởi động,
// trước files.CheckPurposes. Gọi lại nhiều lần không sao.
func RegisterFilePurposes() { files.RegisterPurpose(StatementPurpose()) }
