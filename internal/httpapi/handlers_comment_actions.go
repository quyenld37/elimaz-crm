package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Trả lời comment, nhắn riêng, ẩn/hiện comment (Task 158; CH05, CH11 phần tay). Cả bốn cần conversation.reply trên Page
// (không có, hay không có hội thoại/comment → 404); Page chỉ xem → 409 channel_read_only; kênh không cho → 409
// send_not_allowed kèm denyReason; lỗi nghiệp vụ không lưu theo khóa chống trùng. Lệnh ra Meta chạy sau commit.

// ReplyToComment (POST /api/v1/conversations/{id}/comment-replies): 201 kèm câu trả lời queued (kind comment) dưới
// comment commentMessageId của hội thoại comment; comment không thuộc hội thoại → 422 invalid_input; đã xóa → 409
// send_not_allowed comment_removed.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ReplyToComment(ctx context.Context, req *gen.ReplyToCommentRequest, params gen.ReplyToCommentParams,
) (gen.ReplyToCommentRes, error) {
	in := conversation.CommentInput{CommentMessageID: req.CommentMessageId, Text: req.Text}
	return h.commentWrite(ctx, http.StatusCreated, "trả lời comment", func(tx pgx.Tx, u identity.AuthUser, env conversation.SendEnv) (any, error) {
		m, err := conversation.ReplyToComment(ctx, tx, env, u, params.ID, in, h.now())
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của conversation, đổi sang HTTP ở errors_conversation.go
		}
		return chatMessageOut(m), nil
	})
}

// SendPrivateReply (POST /api/v1/conversations/{id}/private-replies): 201 kèm tin riêng queued (kind private_reply) nằm
// trong hội thoại hộp thư của khách; đã nhắn → 409 private_reply_used; quá 7 ngày → 409 send_not_allowed
// private_reply_expired; không phải comment của khách → 422 invalid_input.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SendPrivateReply(ctx context.Context, req *gen.SendPrivateReplyRequest, params gen.SendPrivateReplyParams,
) (gen.SendPrivateReplyRes, error) {
	in := conversation.CommentInput{CommentMessageID: req.CommentMessageId, Text: req.Text}
	return h.commentWrite(ctx, http.StatusCreated, "nhắn riêng", func(tx pgx.Tx, u identity.AuthUser, env conversation.SendEnv) (any, error) {
		m, err := conversation.SendPrivateReply(ctx, tx, env, u, params.ID, in, h.now())
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của conversation
		}
		return chatMessageOut(m), nil
	})
}

// HideComment (POST /api/v1/messages/{id}/hide): 202 kèm comment (trạng thái trong CRM lúc này); lệnh ẩn trên Page chạy
// sau commit, xong thì isHidden = true, comment vẫn hiện trong CRM. Không phải comment của khách → 422 invalid_input.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) HideComment(ctx context.Context, params gen.HideCommentParams) (gen.HideCommentRes, error) {
	return h.setHidden(ctx, params.ID, true)
}

// UnhideComment (POST /api/v1/messages/{id}/unhide): như HideComment, hiện lại comment trên Page.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UnhideComment(ctx context.Context, params gen.UnhideCommentParams) (gen.UnhideCommentRes, error) {
	return h.setHidden(ctx, params.ID, false)
}

func (h *handler) setHidden(ctx context.Context, id uuid.UUID, hidden bool) (*gen.ChatMessage, error) {
	what := "hiện comment"
	if hidden {
		what = "ẩn comment"
	}
	return h.commentWrite(ctx, http.StatusAccepted, what, func(tx pgx.Tx, u identity.AuthUser, env conversation.SendEnv) (any, error) {
		c, err := conversation.SetCommentHidden(ctx, tx, env, u, id, hidden, h.now())
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của conversation
		}
		return commentOut(c), nil
	})
}

// commentWrite chạy fn (trả *gen.ChatMessage) trong đường ghi có khóa chống trùng, với SendEnv của tiến trình.
func (h *handler) commentWrite(ctx context.Context, status int, what string,
	fn func(tx pgx.Tx, u identity.AuthUser, env conversation.SendEnv) (any, error),
) (*gen.ChatMessage, error) {
	env := conversation.SendEnv{Registry: h.d.Channels, Queue: queue.NewQueue(), Bus: h.d.Realtime}
	out, err := h.refusableWrite(ctx, status, isConversationRefusal, what, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		return fn(tx, u, env)
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.ChatMessage), nil //nolint:forcetypeassert // fn trả *gen.ChatMessage
}

// commentOut là thân ChatMessage của comment của khách sau khi bấm ẩn/hiện.
func commentOut(c conversation.Comment) *gen.ChatMessage {
	out := &gen.ChatMessage{
		ID: c.ID, ConversationId: c.ConversationID, Direction: gen.MessageDirection(c.Direction), Kind: gen.MessageKindComment,
		Origin: gen.MessageOrigin(c.Origin), Attachments: []gen.ChatAttachment{}, IsHidden: c.IsHidden, Removed: c.Removed,
		EditedCount: int32(c.EditedCount), SentAt: c.SentAt, //nolint:gosec // số lần sửa nhỏ
	}
	if c.Body != "" {
		out.Body = gen.NewOptString(c.Body)
	}
	return out
}
