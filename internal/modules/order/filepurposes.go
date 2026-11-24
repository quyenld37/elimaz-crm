package order

import "elimaz.com.vn/crm/internal/platform/files"

// Mục đích tệp ảnh khách gửi kèm yêu cầu về đơn (kế hoạch M5, Task 195; dòng của platform.file_purposes ở migration
// 0504; R10).
const (
	RequestPhotoPurposeKey = "order_request_photo"
	// MaxRequestPhotoBytes: ảnh lớn nhất (6 MiB, bằng x-body-limit của webUploadRequestFile).
	MaxRequestPhotoBytes = 6 << 20
)

// RequestPhotoPurpose là mục đích ảnh khách gửi kèm yêu cầu: JPEG, PNG, WebP ≤ 6 MiB, chỉ máy chủ ghi (website gửi qua
// CRM, không tải thẳng lên kho), chế độ private (R10): ảnh thô chỉ ở crm-tmp, lệnh file.process_private của M1 kiểm nội
// dung và ghi biến thể vào bucket private; nhân viên xem bằng link ký có hạn.
func RequestPhotoPurpose() files.Purpose {
	return files.Purpose{
		Key: RequestPhotoPurposeKey, Bucket: files.BucketPrivate,
		AllowedTypes: []string{"image/jpeg", "image/png", "image/webp"}, MaxBytes: MaxRequestPhotoBytes,
		Image: true, Private: true,
	}
}

// RegisterFilePurposes đăng ký mục đích tệp của order (order_request_photo) vào sổ của tiến trình; cmd/crm gọi lúc khởi
// động, trước files.CheckPurposes. Gọi lại nhiều lần không sao.
func RegisterFilePurposes() { files.RegisterPurpose(RequestPhotoPurpose()) }
