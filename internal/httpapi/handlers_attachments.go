package httpapi

import (
	"context"
	"fmt"
	"net/url"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/conversation"
)

// GetAttachmentUrl (GET /api/v1/conversations/{id}/attachments/{fileId}/url, conversation.read trên Page của hội
// thoại): đường dẫn ký hạn 5 phút trên tên miền của kho tệp (FILES_PUBLIC_BASE_URL, khác tên miền màn CRM) tới tệp của
// một tin trong hội thoại (Task 155; TP02, TP04). Ảnh → biến thể đã mã hóa lại trên bucket private; tệp khác → chỉ
// tải về (attachment, application/octet-stream). Hội thoại không có, Page không được xem, tệp không thuộc hội thoại →
// 404; chưa xử lý xong → 422 attachment_not_ready; bị từ chối → 422 attachment_rejected.
//
//nolint:ireturn,revive // tên và chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetAttachmentUrl(ctx context.Context, params gen.GetAttachmentUrlParams) (gen.GetAttachmentUrlRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	if h.d.Files == nil {
		return nil, errNoFileStore
	}
	var link conversation.AttachmentLink
	err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		var err error
		link, err = conversation.AttachmentURL(ctx, tx, h.d.Files, u, params.ID, params.FileId, h.now())
		return err //nolint:wrapcheck // bọc ở dưới
	})
	if err != nil {
		return nil, fmt.Errorf("đường dẫn tệp đính kèm: %w", err)
	}
	parsed, err := url.Parse(link.URL)
	if err != nil {
		return nil, fmt.Errorf("đường dẫn tệp đính kèm: %w", err)
	}
	return &gen.AttachmentUrl{URL: *parsed, ExpiresAt: link.ExpiresAt}, nil
}
