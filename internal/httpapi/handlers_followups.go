package httpapi

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// defaultFollowUpHours là afterHours mặc định của danh sách nhắn lại (openapi.yaml listFollowUps).
const defaultFollowUpHours = 3

// ListFollowUps (GET /api/v1/follow-ups, Task 168): hội thoại cần nhắn lại trên các Page người gọi đọc được
// (conversation.read ở guard; module lọc theo Page). Nhắn một chạm trên màn hình dùng renderReplyTemplate + sendMessage.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListFollowUps(ctx context.Context, params gen.ListFollowUpsParams) (gen.ListFollowUpsRes, error) {
	var page *uuid.UUID
	if v, ok := params.PageId.Get(); ok {
		page = &v
	}
	hours := int(params.AfterHours.Or(defaultFollowUpHours))
	var items []conversation.FollowUp
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		items, err = conversation.ListFollowUps(ctx, tx, u, page, hours, int(params.Limit.Or(0)), h.now())
		return err //nolint:wrapcheck // conversation đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc danh sách nhắn lại: %w", err)
	}
	out := &gen.FollowUpList{Items: make([]gen.FollowUpItem, 0, len(items))}
	for _, f := range items {
		it := gen.FollowUpItem{
			ConversationId: f.ConversationID, PageId: f.PageID,
			Customer:           gen.ChatCustomerRef{ID: f.CustomerID, Name: f.CustomerName},
			LastStaffMessageAt: f.LastStaffMessageAt, WindowEndsAt: f.WindowEndsAt,
		}
		if f.LastStaffSnippet != "" {
			it.LastStaffSnippet = gen.NewOptString(f.LastStaffSnippet)
		}
		out.Items = append(out.Items, it)
	}
	return out, nil
}
