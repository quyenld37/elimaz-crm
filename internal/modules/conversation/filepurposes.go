package conversation

import (
	"elimaz.com.vn/crm/internal/modules/conversation/internal/inbound"
	"elimaz.com.vn/crm/internal/platform/files"
)

// Mục đích tệp của tin khách gửi vào (Task 155; dòng của platform.file_purposes ở migration 0404).
const (
	// PurposeChatImage: ảnh khách gửi (JPEG, PNG, WebP ≤ 25 MiB), chế độ private (R10): ảnh thô chỉ ở crm-tmp, chỉ
	// file.process_private của M1 xử lý (mã hóa lại, bỏ siêu dữ liệu), biến thể ở bucket private.
	PurposeChatImage = inbound.PurposeChatImage
	// PurposeChatAttachment: tệp không phải ảnh (mọi loại máy chủ nhận dạng, ≤ 25 MiB), chỉ cho tải về.
	PurposeChatAttachment = inbound.PurposeChatAttachment
	// PurposeChatAvatar: ảnh đại diện của khách đọc từ hồ sơ kênh (JPEG, PNG, WebP ≤ 2 MiB).
	PurposeChatAvatar = inbound.PurposeChatAvatar
	// PurposeChatPostImage: ảnh bài viết của Page đọc khi đọc bài (JPEG, PNG, WebP ≤ 5 MiB; Task 501, 0417).
	PurposeChatPostImage = inbound.PurposeChatPostImage
)

// InboundFilePurposes là ba mục đích tệp của tin vào; không ai tải lên qua API (Upload = false).
func InboundFilePurposes() []files.Purpose { return inbound.FilePurposes() }

// RegisterFilePurposes đăng ký bốn mục đích tệp của conversation vào sổ của tiến trình: chat_outgoing (Task 156,
// 0413) và ba mục đích của tin vào (Task 155, 0404); cmd/crm gọi lúc khởi động, trước files.CheckPurposes. Gọi lại
// nhiều lần không sao.
func RegisterFilePurposes() {
	files.RegisterPurpose(OutgoingImageFilePurpose())
	for _, p := range InboundFilePurposes() {
		files.RegisterPurpose(p)
	}
}
