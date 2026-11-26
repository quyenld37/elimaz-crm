package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Vùng nhãn hội thoại (kế hoạch M4, Task 160; CH07). Nhãn là tài nguyên chung của cả shop (Q7): tạo, sửa, xóa cần
// conversation.settings phạm vi all (chủ shop 26/11) — grant chỉ một Page → 403. Gắn, gỡ nhãn cần conversation.reply
// trên Page của hội thoại (module kiểm).

const permConversationSettings = identity.Permission("conversation.settings")

// requireShopSettings: u có conversation.settings phạm vi all, không thì errForbiddenTarget (403).
func requireShopSettings(u identity.AuthUser) error {
	if identity.GrantedAt(u, permConversationSettings, identity.Scope{Type: identity.ScopeAll}) {
		return nil
	}
	return fmt.Errorf("nhãn hội thoại dùng chung cần conversation.settings toàn shop: %w", errForbiddenTarget)
}

// ListConversationTags (GET /api/v1/conversation-tags).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListConversationTags(ctx context.Context) (gen.ListConversationTagsRes, error) {
	var tags []conversation.Tag
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, _ identity.AuthUser) (err error) {
		tags, err = conversation.ListTags(ctx, tx)
		return err //nolint:wrapcheck // conversation đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc nhãn hội thoại: %w", err)
	}
	out := &gen.ConversationTagList{Items: make([]gen.ConversationTag, 0, len(tags))}
	for _, t := range tags {
		out.Items = append(out.Items, conversationTagOut(t))
	}
	return out, nil
}

// tagChange chạy op (sau khi kiểm quyền toàn shop) và trả nhãn.
func (h *handler) tagChange(ctx context.Context, status int, what string,
	op func(tx pgx.Tx, w conversation.Writer, u identity.AuthUser) (conversation.Tag, error),
) (*gen.ConversationTag, error) {
	out, err := h.conversationChange(ctx, status, what, func(tx pgx.Tx, w conversation.Writer, u identity.AuthUser) (any, error) {
		if err := requireShopSettings(u); err != nil {
			return nil, err
		}
		t, err := op(tx, w, u)
		if err != nil {
			return nil, err
		}
		o := conversationTagOut(t)
		return &o, nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.ConversationTag), nil //nolint:forcetypeassert // fn trên trả *gen.ConversationTag
}

// CreateConversationTag (POST /api/v1/conversation-tags).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateConversationTag(ctx context.Context, req *gen.CreateConversationTagRequest,
	_ gen.CreateConversationTagParams,
) (gen.CreateConversationTagRes, error) {
	return h.tagChange(ctx, http.StatusCreated, "tạo nhãn hội thoại", func(tx pgx.Tx, w conversation.Writer, u identity.AuthUser) (conversation.Tag, error) {
		return w.CreateTag(ctx, tx, u, conversation.TagInput{Name: req.Name, Color: req.Color, Sort: req.Sort.Or(0)})
	})
}

// UpdateConversationTag (PATCH /api/v1/conversation-tags/{id}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UpdateConversationTag(ctx context.Context, req *gen.UpdateConversationTagRequest,
	params gen.UpdateConversationTagParams,
) (gen.UpdateConversationTagRes, error) {
	var p conversation.TagPatch
	if v, ok := req.Name.Get(); ok {
		p.Name = &v
	}
	if v, ok := req.Color.Get(); ok {
		p.Color = &v
	}
	if v, ok := req.Sort.Get(); ok {
		p.Sort = &v
	}
	return h.tagChange(ctx, http.StatusOK, "sửa nhãn hội thoại", func(tx pgx.Tx, w conversation.Writer, u identity.AuthUser) (conversation.Tag, error) {
		return w.UpdateTag(ctx, tx, u, params.ID, p, req.ExpectedVersion)
	})
}

// DeleteConversationTag (DELETE /api/v1/conversation-tags/{id}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) DeleteConversationTag(ctx context.Context, params gen.DeleteConversationTagParams) (gen.DeleteConversationTagRes, error) {
	if _, err := h.conversationChange(ctx, http.StatusNoContent, "xóa nhãn hội thoại", func(tx pgx.Tx, w conversation.Writer, u identity.AuthUser) (any, error) {
		if err := requireShopSettings(u); err != nil {
			return nil, err
		}
		return nil, w.DeleteTag(ctx, tx, u, params.ID)
	}); err != nil {
		return nil, err
	}
	return &gen.DeleteConversationTagNoContent{}, nil
}

// AddConversationTag (PUT /api/v1/conversations/{id}/tags/{tagId}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) AddConversationTag(ctx context.Context, params gen.AddConversationTagParams) (gen.AddConversationTagRes, error) {
	if _, err := h.conversationChange(ctx, http.StatusNoContent, "gắn nhãn hội thoại", func(tx pgx.Tx, w conversation.Writer, u identity.AuthUser) (any, error) {
		return nil, w.AddTag(ctx, tx, u, params.ID, params.TagId)
	}); err != nil {
		return nil, err
	}
	return &gen.AddConversationTagNoContent{}, nil
}

// RemoveConversationTag (DELETE /api/v1/conversations/{id}/tags/{tagId}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) RemoveConversationTag(ctx context.Context, params gen.RemoveConversationTagParams) (gen.RemoveConversationTagRes, error) {
	if _, err := h.conversationChange(ctx, http.StatusNoContent, "gỡ nhãn hội thoại", func(tx pgx.Tx, w conversation.Writer, u identity.AuthUser) (any, error) {
		return nil, w.RemoveTag(ctx, tx, u, params.ID, params.TagId)
	}); err != nil {
		return nil, err
	}
	return &gen.RemoveConversationTagNoContent{}, nil
}
