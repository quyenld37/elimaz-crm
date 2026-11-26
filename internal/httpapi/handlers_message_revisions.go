package httpapi

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/conversation"
)

// ListMessageRevisions (GET /api/v1/messages/{id}/revisions, conversation.read trên Page của tin): lịch sử sửa, xóa,
// ẩn của một tin theo thứ tự ghi (Task 154, CH13). Tin không có, là ghi chú, hay trên Page người gọi không được đọc →
// 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListMessageRevisions(ctx context.Context, params gen.ListMessageRevisionsParams) (gen.ListMessageRevisionsRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var items []conversation.MessageRevision
	err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		var err error
		items, err = conversation.ListMessageRevisions(ctx, tx, u, params.ID)
		return err //nolint:wrapcheck // bọc ở dưới
	})
	if err != nil {
		return nil, fmt.Errorf("đọc lịch sử tin: %w", err)
	}
	out := &gen.MessageRevisionList{Items: make([]gen.MessageRevision, 0, len(items))}
	for _, r := range items {
		it := gen.MessageRevision{
			IsHidden: r.IsHidden, Removed: r.Removed, Source: gen.MessageRevisionSource(r.Source), RecordedAt: r.RecordedAt,
		}
		if r.Body != "" {
			it.Body = gen.NewOptString(r.Body)
		}
		out.Items = append(out.Items, it)
	}
	return out, nil
}
