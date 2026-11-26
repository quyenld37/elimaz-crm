package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Mẫu trả lời có biến (Task 161; 01 mục 6). Quyết định chủ shop 26/11: mẫu chung chỉ người có conversation.settings
// phạm vi all tạo, sửa, xóa (grant một Page → 403); mẫu của một Page cần quyền đó trên Page. Kiểm trong module
// (conversation.TemplateWriter), lỗi đổi ở errors_conversation.go.

// ListReplyTemplates (GET /api/v1/reply-templates?q=&pageId=, conversation.read): mẫu khớp tiền tố phím tắt hay tiêu
// đề, gồm mẫu chung và mẫu của Page (không pageId: mọi Page người gọi đọc được); Page không đọc được → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListReplyTemplates(ctx context.Context, params gen.ListReplyTemplatesParams) (gen.ListReplyTemplatesRes, error) {
	var items []conversation.ReplyTemplate
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		items, err = conversation.ListTemplates(ctx, tx, u, params.Q.Or(""), optUUID(params.PageId))
		return err //nolint:wrapcheck // bọc ở dưới
	}); err != nil {
		return nil, fmt.Errorf("liệt kê mẫu trả lời: %w", err)
	}
	out := &gen.ReplyTemplateList{Items: make([]gen.ReplyTemplate, 0, len(items))}
	for _, t := range items {
		out.Items = append(out.Items, *replyTemplateOut(t))
	}
	return out, nil
}

// CreateReplyTemplate (POST /api/v1/reply-templates, conversation.settings): 201; phím tắt trùng → 409
// already_exists; biến lạ, dữ liệu sai → 422 invalid_input; không đủ phạm vi → 403.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateReplyTemplate(ctx context.Context, req *gen.CreateReplyTemplateRequest,
	_ gen.CreateReplyTemplateParams,
) (gen.CreateReplyTemplateRes, error) {
	in := conversation.TemplateInput{
		PageID: optUUID(req.PageId), Shortcut: req.Shortcut, Title: req.Title, Body: req.Body,
		AttachmentFileID: optUUID(req.AttachmentFileId), QuickReplies: quickRepliesIn(req.QuickReplies),
	}
	return h.templateWrite(ctx, http.StatusCreated, "tạo mẫu trả lời",
		func(tx pgx.Tx, w conversation.TemplateWriter, u identity.AuthUser) (conversation.ReplyTemplate, error) {
			return w.Create(ctx, tx, u, in)
		})
}

// UpdateReplyTemplate (PATCH /api/v1/reply-templates/{id}): chỉ đổi trường được gửi; version cũ → 409.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UpdateReplyTemplate(ctx context.Context, req *gen.UpdateReplyTemplateRequest,
	params gen.UpdateReplyTemplateParams,
) (gen.UpdateReplyTemplateRes, error) {
	p := conversation.TemplatePatch{
		SetPage: req.PageId.Set, PageID: optNilUUID(req.PageId), Shortcut: optString(req.Shortcut.Set, req.Shortcut.Value),
		Title: optString(req.Title.Set, req.Title.Value), Body: optString(req.Body.Set, req.Body.Value), SetAttachment: req.AttachmentFileId.Set,
		AttachmentFileID: optNilUUID(req.AttachmentFileId), ExpectedVersion: req.ExpectedVersion,
	}
	if req.QuickReplies != nil {
		qr := quickRepliesIn(req.QuickReplies)
		p.QuickReplies = &qr
	}
	return h.templateWrite(ctx, http.StatusOK, "sửa mẫu trả lời",
		func(tx pgx.Tx, w conversation.TemplateWriter, u identity.AuthUser) (conversation.ReplyTemplate, error) {
			return w.Update(ctx, tx, u, params.ID, p)
		})
}

// DeleteReplyTemplate (DELETE /api/v1/reply-templates/{id}): xóa mềm; không có (kể cả đã xóa) → 404. 204.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) DeleteReplyTemplate(ctx context.Context, params gen.DeleteReplyTemplateParams) (gen.DeleteReplyTemplateRes, error) {
	w := conversation.TemplateWriter{RequestID: RequestID(ctx)}
	if _, err := h.refusableWrite(ctx, http.StatusNoContent, isConversationRefusal, "xóa mẫu trả lời",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			return nil, w.Delete(ctx, tx, u, params.ID)
		}); err != nil {
		return nil, err
	}
	return &gen.DeleteReplyTemplateNoContent{}, nil
}

// RenderReplyTemplate (POST /api/v1/reply-templates/{id}/render, conversation.read trên Page của hội thoại): điền biến
// cho hội thoại; hội thoại, mẫu không có hay không đọc được → 404; biến thiếu giá trị → 422 template_variable_missing
// kèm name; đơn không phải của khách → 422 invalid_input. Không đổi dữ liệu.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) RenderReplyTemplate(ctx context.Context, req *gen.RenderReplyTemplateRequest,
	params gen.RenderReplyTemplateParams,
) (gen.RenderReplyTemplateRes, error) {
	out, err := h.refusableWrite(ctx, http.StatusOK, isConversationRefusal, "điền mẫu trả lời",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			r, err := conversation.RenderTemplate(ctx, tx, params.ID, conversation.RenderInput{
				ConversationID: req.ConversationId, OrderID: optUUID(req.OrderId), Actor: &u,
			})
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của conversation
			}
			o := &gen.RenderedReplyTemplate{Text: r.Text, QuickReplies: quickRepliesOut(r.QuickReplies)}
			if r.AttachmentFileID != nil {
				o.AttachmentFileId = gen.NewOptUUID(*r.AttachmentFileID)
			}
			return o, nil
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.RenderedReplyTemplate), nil //nolint:forcetypeassert // fn trên trả *gen.RenderedReplyTemplate
}

// templateWrite chạy op trong refusableWrite với TemplateWriter của yêu cầu (RequestID của audit) và trả thân mẫu.
func (h *handler) templateWrite(ctx context.Context, status int, what string,
	op func(tx pgx.Tx, w conversation.TemplateWriter, u identity.AuthUser) (conversation.ReplyTemplate, error),
) (*gen.ReplyTemplate, error) {
	w := conversation.TemplateWriter{RequestID: RequestID(ctx)}
	out, err := h.refusableWrite(ctx, status, isConversationRefusal, what, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		t, err := op(tx, w, u)
		if err != nil {
			return nil, err
		}
		return replyTemplateOut(t), nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.ReplyTemplate), nil //nolint:forcetypeassert // fn trên trả *gen.ReplyTemplate
}

func replyTemplateOut(t conversation.ReplyTemplate) *gen.ReplyTemplate {
	o := &gen.ReplyTemplate{
		ID: t.ID, Shortcut: t.Shortcut, Title: t.Title, Body: t.Body, QuickReplies: quickRepliesOut(t.QuickReplies),
		Version: t.Version, CreatedAt: t.CreatedAt,
	}
	if t.PageID != nil {
		o.PageId = gen.NewOptUUID(*t.PageID)
	}
	if t.AttachmentFileID != nil {
		o.AttachmentFileId = gen.NewOptUUID(*t.AttachmentFileID)
	}
	return o
}

func quickRepliesIn(in []gen.QuickReply) []channel.QuickReply {
	return mapAll(in, func(q gen.QuickReply) channel.QuickReply {
		return channel.QuickReply{Title: q.Title, Payload: q.Payload}
	})
}

func quickRepliesOut(in []channel.QuickReply) []gen.QuickReply {
	return mapAll(in, func(q channel.QuickReply) gen.QuickReply { return gen.QuickReply{Title: q.Title, Payload: q.Payload} })
}

// mapAll trả f của từng phần tử của in, cùng thứ tự (rỗng → slice rỗng, không nil: mảng JSON bắt buộc).
func mapAll[T, U any](in []T, f func(T) U) []U {
	out := make([]U, 0, len(in))
	for _, v := range in {
		out = append(out, f(v))
	}
	return out
}

// optNilUUID: không gửi hay null → nil; có giá trị → con trỏ tới giá trị.
func optNilUUID(o gen.OptNilUUID) *uuid.UUID {
	if !o.Set || o.Null {
		return nil
	}
	v := o.Value
	return &v
}
