package send

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// maxNoteRunes là độ dài tối đa của một ghi chú nội bộ (kế hoạch M4, Task 161; hợp đồng AddConversationNoteRequest).
const maxNoteRunes = 4000

// Quyền và chủ đề báo hiệu của ghi chú (cùng chuỗi với conversation.TopicConversationChanged; gói gốc nhập gói này nên
// không nhập ngược được).
const (
	permRead                identity.Permission = "conversation.read"
	permReply               identity.Permission = "conversation.reply"
	topicConversationChange                     = "conversation.changed"
	entityConversation                          = "conversation"
)

var (
	// ErrNotFound là lỗi khi hội thoại không có hay trên Page người gọi không có conversation.reply (404, không lộ sự
	// tồn tại).
	ErrNotFound = access.ErrNotFound
	// ErrInvalidNote là lỗi khi ghi chú rỗng (sau khi bỏ khoảng trắng hai đầu) hay dài quá 4.000 ký tự (400).
	ErrInvalidNote = errors.New("conversation: ghi chú rỗng hay quá dài")
)

// Note là một ghi chú nội bộ vừa ghi (tin kind note, direction out, origin staff). Body là dữ liệu cá nhân.
type Note struct {
	ID, ConversationID, SenderID uuid.UUID
	Body, SenderName             string
	SentAt                       time.Time
}

// AddNote ghi ghi chú nội bộ text (bỏ khoảng trắng hai đầu, 1–4.000 ký tự) của nhân viên u vào hội thoại
// conversationID, lúc now (CH07): một tin kind note, không trạng thái gửi, không mã ngoài, không lệnh nào (CHECK
// messages_note_internal của database giữ quy luật này); báo hiệu conversation.changed theo Page (bus nil: không báo).
// Người gọi cần conversation.reply trên Page của hội thoại (không có, hay hội thoại không có → ErrNotFound). Không
// đổi hội thoại (giờ tin cuối, đoạn trích, chưa đọc chỉ theo tin với khách).
func AddNote(ctx context.Context, tx pgx.Tx, bus realtime.Bus, u identity.AuthUser, conversationID uuid.UUID, text string,
	now time.Time,
) (Note, error) {
	body := strings.TrimSpace(text)
	if body == "" || utf8.RuneCountInString(body) > maxNoteRunes {
		return Note{}, ErrInvalidNote
	}
	q := store.New(tx)
	conv, err := q.ReminderConversation(ctx, conversationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Note{}, ErrNotFound
	}
	if err != nil {
		return Note{}, fmt.Errorf("đọc hội thoại của ghi chú: %w", err)
	}
	if err := access.Require(u, permReply, conv.ChannelAccountID); err != nil {
		return Note{}, err //nolint:wrapcheck // sentinel của access
	}
	name, err := q.TemplateStaffName(ctx, u.UserID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Note{}, fmt.Errorf("đọc tên người ghi chú: %w", err)
	}
	row, err := q.InsertNote(ctx, store.InsertNoteParams{
		ConversationID: conversationID, ChannelAccountID: conv.ChannelAccountID, Body: pgtype.Text{String: body, Valid: true},
		SenderStaffID: pgtype.UUID{Bytes: u.UserID, Valid: true}, SenderName: pgtype.Text{String: name, Valid: name != ""},
		SentAt: now.UTC(),
	})
	if err != nil {
		return Note{}, fmt.Errorf("ghi ghi chú: %w", err)
	}
	if bus != nil {
		if err := bus.Publish(ctx, tx, realtime.Signal{
			TenantID: u.TenantID, Topic: topicConversationChange, Requires: permRead,
			Scope:  access.PageScope(conv.ChannelAccountID),
			Entity: realtime.Entity{Type: entityConversation, ID: conversationID.String()},
		}); err != nil {
			return Note{}, fmt.Errorf("báo hiệu ghi chú: %w", err)
		}
	}
	return Note{
		ID: row.ID, ConversationID: conversationID, SenderID: u.UserID, Body: body, SenderName: name, SentAt: row.SentAt,
	}, nil
}
