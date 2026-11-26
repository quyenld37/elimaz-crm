package inbound

import "elimaz.com.vn/crm/internal/platform/files"

// Mục đích tệp của tin vào (migration 0404; gói gốc conversation công bố lại).
const (
	PurposeChatImage      = "chat_image"
	PurposeChatAttachment = "chat_attachment"
	PurposeChatAvatar     = "chat_avatar"
)

const (
	// maxAttachmentBytes là trần một tệp khách gửi (bằng trần tải của adapter Facebook).
	maxAttachmentBytes int64 = 25 << 20
	// maxAvatarBytes là trần ảnh đại diện.
	maxAvatarBytes int64 = 2 << 20
)

// imageTypes là loại ảnh được mã hóa lại bằng imager (ảnh khác như GIF, SVG chỉ cho tải về).
func imageTypes() []string { return []string{"image/jpeg", "image/png", "image/webp"} }

// FilePurposes là ba mục đích tệp của tin vào: chat_image (private, R10), chat_attachment (mọi loại, chỉ tải về),
// chat_avatar.
func FilePurposes() []files.Purpose {
	return []files.Purpose{
		{
			Key: PurposeChatImage, Bucket: files.BucketPrivate, AllowedTypes: imageTypes(), MaxBytes: maxAttachmentBytes,
			Image: true, Private: true,
		},
		{Key: PurposeChatAttachment, Bucket: files.BucketPrivate, AnyType: true, MaxBytes: maxAttachmentBytes},
		{Key: PurposeChatAvatar, Bucket: files.BucketPrivate, AllowedTypes: imageTypes(), MaxBytes: maxAvatarBytes},
	}
}
