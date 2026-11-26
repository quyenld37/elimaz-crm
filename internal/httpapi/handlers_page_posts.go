package httpapi

import (
	"context"
	"fmt"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// ListPagePosts (GET /api/v1/channels/{id}/posts, conversation.read trên Page): bài CRM đã gặp trên Page, mới gặp
// trước, để chọn bài ngoại lệ tự ẩn, bài của tự trả lời comment (Task 501). Con trỏ là id dòng cuối trang trước (sai →
// 400).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListPagePosts(ctx context.Context, params gen.ListPagePostsParams) (gen.ListPagePostsRes, error) {
	after, err := idCursor(params.Cursor, conversation.ErrCursorInvalid)
	if err != nil {
		return nil, err
	}
	var (
		posts []conversation.PagePost
		next  *uuid.UUID
	)
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		posts, next, err = conversation.ListPagePosts(ctx, tx, u, params.ID, after, int(params.Limit.Or(0)))
		return err //nolint:wrapcheck // conversation đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc bài viết của Page: %w", err)
	}
	out := &gen.PagePostPage{Items: make([]gen.PagePost, 0, len(posts))}
	for _, p := range posts {
		it := gen.PagePost{ID: p.ID, ExternalId: p.ExternalID, Kind: gen.PagePostKind(p.Kind)}
		if p.Snippet != "" {
			it.Snippet = gen.NewOptString(p.Snippet)
		}
		if p.PostedAt != nil {
			it.PostedAt = gen.NewOptDateTime(*p.PostedAt)
		}
		if u, err := url.Parse(p.Permalink); p.Permalink != "" && err == nil {
			it.Permalink = gen.NewOptURI(*u)
		}
		out.Items = append(out.Items, it)
	}
	if next != nil {
		out.NextCursor = gen.NewOptString(next.String())
	}
	return out, nil
}
