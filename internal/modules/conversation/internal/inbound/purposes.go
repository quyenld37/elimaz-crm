package inbound

import "elimaz.com.vn/crm/internal/platform/files"

// Mục đích tệp của tin vào (migration 0404; gói gốc conversation công bố lại).
const (
	PurposeChatImage      = "chat_image"
	PurposeChatAttachment = "chat_attachment"
	PurposeChatAvatar     = "chat_avatar"
	// PurposeChatPostImage: ảnh bài viết của Page (full_picture) đọc khi đọc bài (Task 501, 0417).
	PurposeChatPostImage = "chat_post_image"
)

const (
	// maxAttachmentBytes là trần một tệp khách gửi (bằng trần tải của adapter Facebook).
	maxAttachmentBytes int64 = 25 << 20
	// maxAvatarBytes là trần ảnh đại diện.
	maxAvatarBytes int64 = 2 << 20
	// maxPostImageBytes là trần ảnh bài viết.
	maxPostImageBytes int64 = 5 << 20
)

// imageTypes là loại ảnh được mã hóa lại bằng imager (ảnh khác như GIF, SVG chỉ cho tải về).
func imageTypes() []string { return []string{"image/jpeg", "image/png", "image/webp"} }

// FilePurposes là bốn mục đích tệp của tin vào: chat_image (private, R10), chat_attachment (mọi loại, chỉ tải về),
// chat_avatar, chat_post_image (Task 501).
func FilePurposes() []files.Purpose {
	return []files.Purpose{
		{
			Key: PurposeChatImage, Bucket: files.BucketPrivate, AllowedTypes: imageTypes(), MaxBytes: maxAttachmentBytes,
			Image: true, Private: true,
		},
		{Key: PurposeChatAttachment, Bucket: files.BucketPrivate, AnyType: true, MaxBytes: maxAttachmentBytes},
		{Key: PurposeChatAvatar, Bucket: files.BucketPrivate, AllowedTypes: imageTypes(), MaxBytes: maxAvatarBytes},
		{Key: PurposeChatPostImage, Bucket: files.BucketPrivate, AllowedTypes: imageTypes(), MaxBytes: maxPostImageBytes},
	}
}
