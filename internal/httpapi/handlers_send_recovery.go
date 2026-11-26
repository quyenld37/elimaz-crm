package httpapi

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// ResendMessage (POST /api/v1/messages/{id}/resend, conversation.reply trên Page của hội thoại): gửi lại tin "không rõ
// đã gửi chưa" (in_doubt), "chưa gửi được" (expired) hay lỗi tạm (Task 157; CH18, CH19). Tin không có hay Page không
// được trả lời → 404; trạng thái đã đổi hay lệnh cũ đang chạy → 409 version_conflict; Page chỉ xem → 409
// channel_read_only; kênh không cho gửi → 409 send_not_allowed. 201 kèm tin mới queued và mayDuplicate (tin cũ in_doubt).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ResendMessage(ctx context.Context, params gen.ResendMessageParams) (gen.ResendMessageRes, error) {
	out, err := h.refusableWrite(ctx, http.StatusCreated, isConversationRefusal, "gửi lại tin", func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		r, err := conversation.ResendMessage(ctx, tx, h.sendEnv(), u, params.ID, h.now())
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của conversation, đổi sang HTTP ở errors_conversation.go
		}
		return &gen.ResendMessageResult{Message: *chatMessageOut(r.Message), MayDuplicate: r.MayDuplicate}, nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.ResendMessageResult), nil //nolint:forcetypeassert // fn trên trả *gen.ResendMessageResult
}

// DiscardMessage (POST /api/v1/messages/{id}/discard, conversation.reply trên Page của hội thoại): bỏ tin in_doubt,
// checking, expired, failed (Task 157); lệnh gửi còn chờ bị bỏ. Tin không có hay Page không được trả lời → 404; trạng
// thái đã đổi hay lệnh đang chạy → 409 version_conflict. 200 kèm tin discarded.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) DiscardMessage(ctx context.Context, params gen.DiscardMessageParams) (gen.DiscardMessageRes, error) {
	m, err := h.writeChatMessage(ctx, http.StatusOK, "bỏ tin", func(tx pgx.Tx, u identity.AuthUser) (conversation.Message, error) {
		return conversation.DiscardMessage(ctx, tx, h.sendEnv(), u, params.ID, h.now())
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}
