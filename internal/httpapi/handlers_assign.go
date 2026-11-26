package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Vùng hội thoại, người phụ trách (kế hoạch M4, Task 160; CH06): tự nhận (conversation.reply), giao hoặc bỏ giao
// (conversation.assign), lịch sử, danh sách người được giao. Quyền theo Page kiểm lại trong module (không có → 404).

// conversationWriter dựng conversation.Writer của yêu cầu (sự kiện, báo hiệu, RequestID của audit).
func (h *handler) conversationWriter(ctx context.Context) conversation.Writer {
	return conversation.Writer{Events: h.d.Events, Queue: queue.NewQueue(), Realtime: h.d.Realtime, RequestID: RequestID(ctx)}
}

// conversationChange chạy op trong refusableWrite (lỗi nghiệp vụ của conversation, channel, order là câu trả lời
// không đổi gì) và trả body của op.
func (h *handler) conversationChange(ctx context.Context, status int, what string,
	op func(tx pgx.Tx, w conversation.Writer, u identity.AuthUser) (any, error),
) (any, error) {
	w := h.conversationWriter(ctx)
	return h.refusableWrite(ctx, status, isConversationRefusal, what, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		return op(tx, w, u)
	})
}

// assignChange: đổi người phụ trách rồi trả chi tiết hội thoại sau khi đổi.
func (h *handler) assignChange(ctx context.Context, what string, id uuid.UUID,
	op func(tx pgx.Tx, w conversation.Writer, u identity.AuthUser) error,
) (*gen.ConversationDetail, error) {
	out, err := h.conversationChange(ctx, http.StatusOK, what, func(tx pgx.Tx, w conversation.Writer, u identity.AuthUser) (any, error) {
		if err := op(tx, w, u); err != nil {
			return nil, err
		}
		return h.conversationDetail(ctx, tx, u, id)
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.ConversationDetail), nil //nolint:forcetypeassert // fn trên trả *gen.ConversationDetail
}

// ClaimConversation (POST /api/v1/conversations/{id}/claim).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ClaimConversation(ctx context.Context, params gen.ClaimConversationParams) (gen.ClaimConversationRes, error) {
	return h.assignChange(ctx, "nhận hội thoại", params.ID, func(tx pgx.Tx, w conversation.Writer, u identity.AuthUser) error {
		return w.Claim(ctx, tx, u, params.ID)
	})
}

// AssignConversation (POST /api/v1/conversations/{id}/assign).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) AssignConversation(ctx context.Context, req *gen.AssignConversationRequest,
	params gen.AssignConversationParams,
) (gen.AssignConversationRes, error) {
	var to *uuid.UUID
	if !req.UserId.Null {
		id := req.UserId.Value
		to = &id
	}
	return h.assignChange(ctx, "giao hội thoại", params.ID, func(tx pgx.Tx, w conversation.Writer, u identity.AuthUser) error {
		return w.Assign(ctx, tx, u, params.ID, to, req.ExpectedVersion)
	})
}

// ListConversationAssignments (GET /api/v1/conversations/{id}/assignments).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListConversationAssignments(ctx context.Context, params gen.ListConversationAssignmentsParams) (gen.ListConversationAssignmentsRes, error) {
	var items []conversation.Assignment
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		items, err = conversation.ListAssignments(ctx, tx, u, params.ID)
		return err //nolint:wrapcheck // conversation đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc lịch sử phân công: %w", err)
	}
	out := &gen.ConversationAssignmentList{Items: make([]gen.ConversationAssignment, 0, len(items))}
	for _, a := range items {
		out.Items = append(out.Items, gen.ConversationAssignment{
			ID: a.ID, From: optActor(a.From), To: optActor(a.To), Actor: optActor(a.Actor),
			Reason: gen.ConversationAssignmentReason(a.Reason), CreatedAt: a.CreatedAt,
		})
	}
	return out, nil
}

func optActor(p *conversation.Person) gen.OptActor {
	if p == nil {
		return gen.OptActor{}
	}
	return gen.NewOptActor(gen.Actor{ID: p.ID, Name: p.Name})
}

// ListAssignableUsers (GET /api/v1/channels/{id}/assignable-users).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListAssignableUsers(ctx context.Context, params gen.ListAssignableUsersParams) (gen.ListAssignableUsersRes, error) {
	var users []identity.User
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		users, err = conversation.AssignableUsers(ctx, tx, u, params.ID)
		return err //nolint:wrapcheck // conversation đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc người được giao: %w", err)
	}
	out := &gen.AssignableUserList{Items: make([]gen.AssignableUser, 0, len(users))}
	for _, u := range users {
		out.Items = append(out.Items, gen.AssignableUser{ID: u.ID, Name: u.DisplayName})
	}
	return out, nil
}
