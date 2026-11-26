package conversation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// permRead là quyền đọc hội thoại (theo Page, Q36).
const permRead identity.Permission = "conversation.read"

// MessageRevision là một dòng lịch sử sửa, xóa, ẩn của tin (Task 154): Body là nội dung trước lần đổi (rỗng khi không
// biết: sự kiện tới trước khi có tin), IsHidden, Removed là trạng thái sau lần đổi; Source: customer, staff,
// page_external, automation. Body là dữ liệu cá nhân.
type MessageRevision struct {
	Body              string
	IsHidden, Removed bool
	Source            string
	RecordedAt        time.Time
}

// ListMessageRevisions trả lịch sử của tin messageID theo thứ tự ghi; cần conversation.read trên Page của tin. Tin
// không có, là ghi chú nội bộ, hay trên Page u không được đọc → ErrNotFound (404, không lộ sự tồn tại).
func ListMessageRevisions(ctx context.Context, tx pgx.Tx, u identity.AuthUser, messageID uuid.UUID) ([]MessageRevision, error) {
	q := store.New(tx)
	page, err := q.MessagePage(ctx, messageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc Page của tin: %w", err)
	}
	if err := access.Require(u, permRead, page); err != nil {
		return nil, err //nolint:wrapcheck // sentinel của phần, lớp HTTP đổi 404
	}
	rows, err := q.ListMessageRevisions(ctx, messageID)
	if err != nil {
		return nil, fmt.Errorf("đọc lịch sử tin: %w", err)
	}
	out := make([]MessageRevision, 0, len(rows))
	for _, r := range rows {
		out = append(out, MessageRevision{
			Body: r.Body.String, IsHidden: r.IsHidden, Removed: r.Removed, Source: r.Source, RecordedAt: r.RecordedAt.UTC(),
		})
	}
	return out, nil
}
