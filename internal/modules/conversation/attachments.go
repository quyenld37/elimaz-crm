package conversation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/assign"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/files"
)

// AttachmentURLTTL là hạn của đường dẫn tới tệp đính kèm (Task 155; TP02).
const AttachmentURLTTL = 5 * time.Minute

// AttachmentLink là đường dẫn ký có hạn tới một tệp đính kèm.
type AttachmentLink struct {
	URL       string
	ExpiresAt time.Time
}

// AttachmentURL ký đường dẫn hạn 5 phút (trên tên miền của kho tệp, FILES_PUBLIC_BASE_URL) tới tệp fileID của một tin
// trong hội thoại conversationID: cần conversation.read trên Page của hội thoại; hội thoại không có, không có quyền,
// tệp không thuộc tin nào của hội thoại, tệp đã xóa, hết hạn → ErrNotFound. Ảnh (mục đích chế độ private, chat_image)
// → biến thể JPEG lớn nhất trên bucket private, mở trong trình duyệt; tệp khác (chat_attachment) → tải về
// (Content-Disposition: attachment, tên tep-<8 ký tự>.<đuôi>; đối tượng ghi application/octet-stream, TP04). Chưa xử
// lý xong → ErrAttachmentNotReady; bị từ chối → ErrAttachmentRejected.
func AttachmentURL(ctx context.Context, tx pgx.Tx, fs files.FileStore, actor identity.AuthUser, conversationID,
	fileID uuid.UUID, now time.Time,
) (AttachmentLink, error) {
	row, err := store.New(tx).ConversationHasFile(ctx, store.ConversationHasFileParams{FileID: fileID, ID: conversationID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return AttachmentLink{}, ErrNotFound
	case err != nil:
		return AttachmentLink{}, fmt.Errorf("đọc tệp đính kèm của hội thoại: %w", err)
	}
	if err := access.Require(actor, assign.PermRead, row.ChannelAccountID); err != nil {
		return AttachmentLink{}, err //nolint:wrapcheck // sentinel của module
	}
	if !row.HasFile {
		return AttachmentLink{}, ErrNotFound
	}
	info, err := files.Describe(ctx, tx, fileID, "")
	if err != nil {
		return AttachmentLink{}, linkError(err)
	}
	name := "" // ảnh: mở trong trình duyệt (biến thể đã mã hóa lại)
	if p, ok := files.PurposeOf(info.Purpose); !ok || !p.Image {
		name = info.OriginalName
	}
	u, err := files.DownloadURL(ctx, tx, fs, fileID, AttachmentURLTTL, name)
	if err != nil {
		return AttachmentLink{}, linkError(err)
	}
	return AttachmentLink{URL: u, ExpiresAt: now.UTC().Add(AttachmentURLTTL)}, nil
}

// linkError đổi lỗi của kho tệp sang lỗi của module.
func linkError(err error) error {
	switch {
	case errors.Is(err, files.ErrNotFound), errors.Is(err, files.ErrExpired):
		return ErrNotFound
	case errors.Is(err, files.ErrNotReady):
		return ErrAttachmentNotReady
	case errors.Is(err, files.ErrRejected):
		return ErrAttachmentRejected
	}
	return fmt.Errorf("ký đường dẫn tệp đính kèm: %w", err)
}
