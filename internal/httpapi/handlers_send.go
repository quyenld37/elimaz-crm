package httpapi

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// SendMessage (POST /api/v1/conversations/{id}/messages, conversation.reply trên Page của hội thoại): gửi chữ hoặc một
// ảnh chat_outgoing cho khách (Task 156; CH03, CH04, CH09). Hội thoại không có hay Page không được trả lời → 404; Page
// chỉ xem → 409 channel_read_only; chính sách của kênh không cho → 409 send_not_allowed kèm denyReason, until; ảnh chưa
// xong → 422 attachment_not_ready, sai mục đích → 422 attachment_rejected; không đúng một trong chữ, ảnh → 400. 201
// kèm tin queued; lệnh command.fb.send_message gửi sau commit. Lỗi nghiệp vụ không lưu theo khóa chống trùng.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SendMessage(ctx context.Context, req *gen.SendMessageRequest, params gen.SendMessageParams,
) (gen.SendMessageRes, error) {
	in := conversation.SendInput{Text: req.Text.Or(""), AttachmentFileID: optUUID(req.AttachmentFileId), HumanAgent: req.HumanAgent.Or(false)}
	m, err := h.writeChatMessage(ctx, http.StatusCreated, "gửi tin", func(tx pgx.Tx, u identity.AuthUser) (conversation.Message, error) {
		return conversation.SendMessage(ctx, tx, h.sendEnv(), u, params.ID, in, h.now())
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

// sendEnv là phụ thuộc của đường gửi tin: bộ adapter kênh, hàng đợi, bus báo hiệu.
func (h *handler) sendEnv() conversation.SendEnv {
	return conversation.SendEnv{Registry: h.d.Channels, Queue: queue.NewQueue(), Bus: h.d.Realtime}
}

// writeChatMessage chạy fn trong đường ghi có khóa chống trùng của hộp chat (lỗi nghiệp vụ không lưu) và trả tin của
// nó dưới dạng ChatMessage với mã status; what là việc đang làm (bọc lỗi).
func (h *handler) writeChatMessage(ctx context.Context, status int, what string,
	fn func(tx pgx.Tx, u identity.AuthUser) (conversation.Message, error),
) (*gen.ChatMessage, error) {
	out, err := h.refusableWrite(ctx, status, isConversationRefusal, what, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		m, err := fn(tx, u)
		if err != nil {
			return nil, err
		}
		return chatMessageOut(m), nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.ChatMessage), nil //nolint:forcetypeassert // fn trên trả *gen.ChatMessage
}

// chatMessageOut là thân ChatMessage của tin nhân viên vừa gửi hay vừa bỏ (tin, câu trả lời comment, tin riêng: Kind,
// comment cha; Task 157: nguồn, mã lý do lỗi của tin cũ).
func chatMessageOut(m conversation.Message) *gen.ChatMessage {
	origin := gen.MessageOriginStaff
	if m.Origin != "" {
		origin = gen.MessageOrigin(m.Origin)
	}
	out := &gen.ChatMessage{
		ID: m.ID, ConversationId: m.ConversationID, Direction: gen.MessageDirectionOut, Kind: gen.MessageKind(m.Kind),
		Origin: origin, Attachments: make([]gen.ChatAttachment, 0, len(m.Attachments)),
		SendStatus: gen.NewOptMessageSendStatus(gen.MessageSendStatus(m.SendStatus)), SentAt: m.SentAt,
	}
	if m.ParentMessageID != nil {
		out.ParentMessageId = gen.NewOptUUID(*m.ParentMessageID)
	}
	if m.Body != "" {
		out.Body = gen.NewOptString(m.Body)
	}
	if m.FailReason != "" {
		out.FailReason = gen.NewOptString(m.FailReason)
	}
	for _, a := range m.Attachments {
		att := gen.ChatAttachment{Type: a.Type, Status: a.Status}
		if a.FileID != nil {
			att.FileId = gen.NewOptUUID(*a.FileID)
		}
		out.Attachments = append(out.Attachments, att)
	}
	return out
}
