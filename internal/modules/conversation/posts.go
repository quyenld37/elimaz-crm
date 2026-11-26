package conversation

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/assign"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Cỡ trang của danh sách bài viết của Page.
const (
	defaultPostLimit = 50
	maxPostLimit     = 100
)

// PagePost là một bài viết CRM đã gặp trên Page (có comment): ExternalID là giá trị của exceptPostIds, postIds trong quy
// tắc tự động; PostedAt nil, Snippet, Permalink rỗng khi chưa đọc được bài.
type PagePost struct {
	ID                 uuid.UUID
	ExternalID, Kind   string
	Snippet, Permalink string
	PostedAt           *time.Time
}

// ListPagePosts trả một trang bài của Page pageID (cần conversation.read trên Page; không có quyền, Page không có →
// ErrNotFound), mới gặp trước; after là id của dòng cuối trang trước (nil: đầu); con trỏ trang sau nil khi hết. limit ≤
// 0 → 50, tối đa 100 (Task 501).
func ListPagePosts(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, pageID uuid.UUID, after *uuid.UUID, limit int,
) ([]PagePost, *uuid.UUID, error) {
	if err := requirePage(ctx, tx, actor, assign.PermRead, pageID); err != nil {
		return nil, nil, err
	}
	if limit <= 0 {
		limit = defaultPostLimit
	}
	limit = min(limit, maxPostLimit)
	before := uuid.Max
	if after != nil {
		before = *after
	}
	rows, err := store.New(tx).ListPagePosts(ctx, store.ListPagePostsParams{
		ChannelAccountID: pageID, BeforeID: before, Lim: int32(limit + 1),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("đọc bài viết của Page: %w", err)
	}
	var next *uuid.UUID
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1].ID
		next = &last
	}
	out := make([]PagePost, 0, len(rows))
	for _, r := range rows {
		p := PagePost{ID: r.ID, ExternalID: r.ExternalID, Kind: r.Kind, Snippet: r.Snippet.String, Permalink: r.Permalink.String}
		if r.CreatedTime.Valid {
			t := r.CreatedTime.Time.UTC()
			p.PostedAt = &t
		}
		out = append(out, p)
	}
	return out, next, nil
}
