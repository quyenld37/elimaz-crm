package send

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/files"
)

// payload là payload của lệnh ra kênh: chỉ mã tin (không nội dung, không PSID, không token); Hidden: trạng thái muốn
// đặt của lệnh ẩn/hiện comment (Task 158).
type payload struct {
	MessageID uuid.UUID `json:"messageId"`
	Hidden    *bool     `json:"hidden,omitempty"`
}

// Send gửi tin của nhân viên u vào hội thoại hộp thư conversationID lúc now, trong tx tenant của bên gọi (WriteTx).
// Thứ tự (ADR-46 nhóm 6, 13): khóa hội thoại → kiểm quyền conversation.reply trên Page (không có → access.ErrNotFound,
// như không có hội thoại) → Page chỉ xem → ErrReadOnly → SendPolicy của adapter (không cho → *NotAllowedError; chỉ còn
// thẻ nhân viên mà không có HumanAgent → *NotAllowedError window_closed kèm hạn 7 ngày) → ảnh (mục đích chat_outgoing,
// ready) → chèn tin queued, cập nhật hội thoại → ghi lệnh command.fb.send_message → báo hiệu.
func Send(ctx context.Context, tx pgx.Tx, env Env, u identity.AuthUser, conversationID uuid.UUID, in Input, now time.Time,
) (Message, error) {
	if err := in.validate(); err != nil {
		return Message{}, err
	}
	now = now.UTC()
	q := store.New(tx)
	c, err := q.LockConversationForSend(ctx, conversationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Message{}, access.ErrNotFound
	case err != nil:
		return Message{}, fmt.Errorf("khóa hội thoại để gửi: %w", err)
	}
	if err := access.Require(u, permReply, c.ChannelAccountID); err != nil {
		return Message{}, err //nolint:wrapcheck // sentinel của access
	}
	if c.Kind != kindInbox {
		return Message{}, ErrInvalidInput
	}
	acc, err := channel.Account(ctx, tx, c.ChannelAccountID)
	if err != nil {
		return Message{}, err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	if err := allowed(env.Registry, conversationRow(c), acc, in.HumanAgent, now); err != nil {
		return Message{}, err
	}
	atts, err := outgoingAttachments(ctx, tx, in.AttachmentFileID)
	if err != nil {
		return Message{}, err
	}
	return insert(ctx, tx, env, u, c.ID, acc, in.Text, atts, now)
}

// allowed hỏi SendPolicy của adapter của Page cho tin thường của nhân viên.
func allowed(reg *channel.Registry, c conversationRow, acc channel.ChannelAccount, humanAgent bool, now time.Time) error {
	pol, err := policyFor(reg, acc, state(c, acc, false), now)
	if err != nil {
		return err
	}
	if pol.Mode == channel.SendHumanAgent && !humanAgent {
		return &NotAllowedError{Reason: channel.DenyWindowClosed, Until: pol.Until}
	}
	return nil
}

// outgoingAttachments kiểm ảnh fileID (nil: tin chữ) và trả cột attachments của tin. Tệp không có, của tenant khác,
// đã xóa → access.ErrNotFound; sai mục đích hay bị từ chối → ErrAttachmentRejected; chưa ready → ErrAttachmentNotReady.
func outgoingAttachments(ctx context.Context, tx pgx.Tx, fileID *uuid.UUID) ([]Attachment, error) {
	if fileID == nil {
		return []Attachment{}, nil
	}
	st, err := files.Status(ctx, tx, *fileID)
	switch {
	case errors.Is(err, files.ErrNotFound):
		return nil, access.ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("đọc ảnh gửi kèm: %w", err)
	case st.Deleted:
		return nil, access.ErrNotFound
	case st.Purpose != OutgoingImagePurpose, st.Status == files.StatusRejected:
		return nil, ErrAttachmentRejected
	case st.Status != files.StatusReady:
		return nil, ErrAttachmentNotReady
	}
	id := *fileID
	return []Attachment{{Index: 0, Type: attachImage, Status: attachReady, FileID: &id}}, nil
}

// insert chèn tin queued, cập nhật hội thoại đã khóa, ghi lệnh gửi (DedupeKey = mã tin), gắn mã lệnh vào tin, báo hiệu.
func insert(ctx context.Context, tx pgx.Tx, env Env, u identity.AuthUser, convID uuid.UUID, acc channel.ChannelAccount,
	text string, atts []Attachment, now time.Time,
) (Message, error) {
	q := store.New(tx)
	raw, err := json.Marshal(atts)
	if err != nil {
		return Message{}, fmt.Errorf("mã hóa ảnh gửi kèm: %w", err)
	}
	ref, err := uuid.NewV7()
	if err != nil {
		return Message{}, fmt.Errorf("tạo client_ref: %w", err)
	}
	id, err := q.InsertOutgoingMessage(ctx, store.InsertOutgoingMessageParams{
		ConversationID: convID, ChannelAccountID: acc.ID, ClientRef: pgtype.UUID{Bytes: ref, Valid: true},
		Body: pgtype.Text{String: text, Valid: text != ""}, Attachments: raw,
		SenderStaffID: pgtype.UUID{Bytes: u.UserID, Valid: u.UserID != uuid.Nil}, SentAt: now,
	})
	if err != nil {
		return Message{}, fmt.Errorf("ghi tin gửi: %w", err)
	}
	tenant := acc.TenantID
	if err := queueOutgoing(ctx, tx, env.Queue, CommandKind(), tenant, convID, id, deps.Snippet(text, len(atts) > 0), now); err != nil {
		return Message{}, err
	}
	if err := publish(ctx, tx, env.Bus, tenant, acc.ID, convID); err != nil {
		return Message{}, err
	}
	return Message{
		ID: id, ConversationID: convID, ClientRef: ref, SenderStaffID: u.UserID, Origin: originStaff, Kind: kindMessage,
		Body: text, Attachments: atts, SendStatus: statusQueued, SentAt: now,
	}, nil
}
