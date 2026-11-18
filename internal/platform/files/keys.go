package files

import (
	"strconv"

	"github.com/google/uuid"
)

// Khóa trên kho do hệ thống sinh, chỉ gồm tiền tố cố định, mã uuid, mã băm hex và số: không bao giờ có tên tệp của
// người dùng (kế hoạch M1, Task M1-41; R10).

// TmpKey là khóa của tệp vừa tải lên, chưa kiểm, trong BucketTmp: tmp/<tenantId>/<fileId>.
func TmpKey(tenantID, fileID uuid.UUID) string {
	return "tmp/" + tenantID.String() + "/" + fileID.String()
}

// ServerKey là khóa của tệp riêng tư do máy chủ ghi (nhãn vận đơn, tệp chat, tệp xuất; A8): priv/<tenantId>/<fileId>.
func ServerKey(tenantID, fileID uuid.UUID) string { return serverPrefix(tenantID) + fileID.String() }

// serverPrefix là tiền tố chung của mọi khóa riêng tư của một tenant: priv/<tenantId>/.
func serverPrefix(tenantID uuid.UUID) string { return "priv/" + tenantID.String() + "/" }

// OriginalKey là khóa ảnh gốc theo mã băm nội dung: orig/<sha256 hex>.
func OriginalKey(sha256Hex string) string { return "orig/" + sha256Hex }

// VariantKey là khóa biến thể ảnh công khai: img/<sha256 hex>/v<preset>/<rộng>.<định dạng>.
func VariantKey(sha256Hex string, preset, width int, format string) string {
	return "img/" + sha256Hex + "/v" + strconv.Itoa(preset) + "/" + strconv.Itoa(width) + "." + format
}

// PrivateVariantKey là khóa biến thể ảnh riêng tư (R10, Task M1-43): priv/<tenantId>/<fileId>/v<preset>/<rộng>.<định dạng>.
func PrivateVariantKey(tenantID, fileID uuid.UUID, preset, width int, format string) string {
	return ServerKey(tenantID, fileID) + "/v" + strconv.Itoa(preset) + "/" + strconv.Itoa(width) + "." + format
}
