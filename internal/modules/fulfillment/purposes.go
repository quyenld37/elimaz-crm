package fulfillment

import "elimaz.com.vn/crm/internal/platform/files"

// Mục đích tệp nhãn hãng (kế hoạch M3, Task M3-04; dòng của platform.file_purposes ở migration 0306).
const (
	LabelPurposeKey = "carrier_label"
	// MaxLabelBytes: tệp nhãn lớn nhất (20 MiB).
	MaxLabelBytes = 20 << 20
)

// LabelPurpose là mục đích tệp nhãn hãng: PDF máy chủ ghi (files.Register), nhân viên không tải lên, ≤ 20 MiB, bucket
// private. Hạn giữ 30 ngày đặt bằng NewFile.ExpiresAt khi ghi (Task M3-16).
func LabelPurpose() files.Purpose {
	return files.Purpose{
		Key: LabelPurposeKey, Bucket: files.BucketPrivate, AllowedTypes: []string{"application/pdf"},
		MaxBytes: MaxLabelBytes,
	}
}

// RegisterFilePurposes đăng ký mục đích tệp của fulfillment (carrier_label) vào sổ của tiến trình; cmd/crm gọi lúc
// khởi động, trước files.CheckPurposes. Gọi lại nhiều lần không sao.
func RegisterFilePurposes() { files.RegisterPurpose(LabelPurpose()) }
