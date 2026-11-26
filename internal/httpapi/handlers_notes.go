package httpapi

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// AddConversationNote (POST /api/v1/conversations/{id}/notes, conversation.reply trên Page của hội thoại): ghi chú nội
// bộ (Task 161; CH07) — tin kind note, không bao giờ gửi ra ngoài (không lệnh, không job). Hội thoại không có hay Page
// không được trả lời → 404; rỗng hay quá 4.000 ký tự → 400. 201 kèm tin.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) AddConversationNote(ctx context.Context, req *gen.AddConversationNoteRequest,
	params gen.AddConversationNoteParams,
) (gen.AddConversationNoteRes, error) {
	out, err := h.refusableWrite(ctx, http.StatusCreated, isConversationRefusal, "ghi chú hội thoại",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			n, err := conversation.AddNote(ctx, tx, h.d.Realtime, u, params.ID, req.Text, h.now())
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của conversation
			}
			return noteOut(n), nil
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.ChatMessage), nil //nolint:forcetypeassert // fn trên trả *gen.ChatMessage
}

func noteOut(n conversation.Note) *gen.ChatMessage {
	return &gen.ChatMessage{
		ID: n.ID, ConversationId: n.ConversationID, Direction: gen.MessageDirectionOut, Kind: gen.MessageKindNote,
		Origin: gen.MessageOriginStaff, Body: gen.NewOptString(n.Body), Attachments: []gen.ChatAttachment{},
		QuickReplies: []gen.QuickReply{}, Sender: gen.NewOptActor(gen.Actor{ID: n.SenderID, Name: n.SenderName}),
		SenderName: gen.NewOptString(n.SenderName), SentAt: n.SentAt,
	}
}
