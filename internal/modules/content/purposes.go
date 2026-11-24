package content

import "elimaz.com.vn/crm/internal/platform/files"

// Mục đích tệp ảnh banner (Task 453; dòng của platform.file_purposes ở migration 0521, R5).
const (
	BannerPurposeKey = "web_banner"
	// MaxBannerBytes: ảnh lớn nhất (8 MiB, như website cũ).
	MaxBannerBytes = 8 << 20
	// MinBannerWidth: ảnh hẹp hơn bị kho tệp từ chối too_small (như website cũ).
	MinBannerWidth = 600
)

// BannerPurpose là mục đích ảnh banner: nhân viên tải lên (đường tải tệp của M1), JPEG, PNG, WebP ≤ 8 MiB, rộng ≥ 600 px;
// ảnh công khai, bộ kích thước riêng files.PresetBannerV1 (WebP, JPEG rộng 1200 và 2400, chất lượng 82, không phóng to,
// bỏ siêu dữ liệu).
func BannerPurpose() files.Purpose {
	return files.Purpose{
		Key: BannerPurposeKey, Bucket: files.BucketOriginals,
		AllowedTypes: []string{"image/jpeg", "image/png", "image/webp"}, MaxBytes: MaxBannerBytes,
		Image: true, Upload: true, Preset: files.PresetBannerV1, MinWidth: MinBannerWidth,
	}
}

// RegisterFilePurposes đăng ký mục đích tệp của content (web_banner) vào sổ của tiến trình; cmd/crm gọi lúc khởi động,
// trước files.CheckPurposes. Gọi lại nhiều lần không sao.
func RegisterFilePurposes() { files.RegisterPurpose(BannerPurpose()) }
