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
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// mergeOn, mergeOff là giá trị cờ app.merge_reply của transaction (trigger messages_delete_guard, migration 0415).
const (
	mergeOn  = "on"
	mergeOff = "off"
)

// ReplyToComment trả lời công khai comment in.CommentMessageID của hội thoại comment conversationID bằng nhân viên u lúc
// now, trong tx tenant của bên gọi (WriteTx). Thứ tự (ADR-46 nhóm 6): khóa hội thoại → quyền conversation.reply trên
// Page (không có → access.ErrNotFound) → hội thoại không phải comment, chữ rỗng hay quá 8 000 ký tự → ErrInvalidInput
// → comment không thuộc hội thoại → ErrNotCustomerComment → Page chỉ xem → ErrReadOnly → SendPolicy(comment_reply)
// (comment đã xóa → *NotAllowedError comment_removed) → chèn tin out kind comment (comment cha, bài) queued, cập nhật
// hội thoại → lệnh command.fb.reply_comment → báo hiệu.
func ReplyToComment(ctx context.Context, tx pgx.Tx, env Env, u identity.AuthUser, conversationID uuid.UUID,
	in CommentInput, now time.Time,
) (Message, error) {
	now = now.UTC()
	c, err := lockConversation(ctx, tx, u, conversationID)
	if err != nil {
		return Message{}, err
	}
	if c.Kind != kindComment || validText(in.Text, maxCommentText) != nil {
		return Message{}, ErrInvalidInput
	}
	cm, err := commentOf(ctx, tx, in.CommentMessageID, c.ID, false)
	if err != nil {
		return Message{}, err
	}
	acc, err := channel.Account(ctx, tx, c.ChannelAccountID)
	if err != nil {
		return Message{}, err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	if _, err := policyFor(env.Registry, acc, channel.ConversationState{
		Purpose: channel.PurposeCommentReply, Kind: c.Kind, CommentRemoved: cm.RemovedAt.Valid,
	}, now); err != nil {
		return Message{}, err
	}
	m, err := insertAction(ctx, tx, u, actionRow{
		conv: c.ID, acc: acc, kind: kindComment, parent: cm, text: in.Text, jobKind: ReplyCommentKind(), jobQueue: env.Queue,
	}, now)
	if err != nil {
		return Message{}, err
	}
	return m, publish(ctx, tx, env.Bus, acc.TenantID, acc.ID, c.ID)
}

// lockConversation khóa hội thoại id rồi kiểm conversation.reply của u trên Page của nó (không có hội thoại hay không
// có quyền → access.ErrNotFound).
func lockConversation(ctx context.Context, tx pgx.Tx, u identity.AuthUser, id uuid.UUID) (store.LockConversationForSendRow, error) {
	c, err := store.New(tx).LockConversationForSend(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return c, access.ErrNotFound
	case err != nil:
		return c, fmt.Errorf("khóa hội thoại để gửi: %w", err)
	}
	if err := access.Require(u, permReply, c.ChannelAccountID); err != nil {
		return c, err //nolint:wrapcheck // sentinel của access
	}
	return c, nil
}

// replyCommand là lệnh command.fb.reply_comment: dùng chung khuôn của lệnh gửi tin (run, outcome, OnTerminal), khác
// phần đọc, OnDone (gộp dòng webhook feed của chính câu trả lời) và có Lookup.
type replyCommand struct{ sendCommand }

func (replyCommand) Kind() queue.Kind { return ReplyCommentKind() }

// Execute gửi câu trả lời của lệnh j: tin còn queued, comment cha còn (đã xóa → comment_removed), hỏi lại
// SendPolicy(comment_reply), lượt gửi của Page, gọi kênh.
func (c replyCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	return c.executeAction(ctx, j, channel.PurposeCommentReply, func(_ context.Context, _ pgx.Tx, _ store.GetOutgoingActionRow,
		parent store.GetCommentByExternalIDRow, found bool,
	) (channel.ConversationState, string, error) {
		return channel.ConversationState{
			Purpose: channel.PurposeCommentReply, Kind: kindComment, CommentRemoved: !found || parent.RemovedAt.Valid,
		}, "", nil
	})
}

// Lookup tra Meta câu trả lời "không rõ đã gửi" của lệnh j: comment con của comment cha sau lúc bấm gửi − 60 s, của
// Page, cùng nội dung. Thấy → ghi mã vào tin của CRM (gộp dòng webhook feed cùng mã nếu đã có) rồi done (OnDone đặt
// sent); không thấy, lỗi, hay mã thấy đã thuộc dòng khác (câu trả lời cũ cùng chữ) → unknown (giữ in_doubt); không
// tra được (không adapter, token) → unknown.
func (c replyCommand) Lookup(ctx context.Context, j queue.Job) (string, error) {
	var p payload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.MessageID == uuid.Nil || j.TenantID == nil {
		return commands.StatusUnknown, nil
	}
	var m store.GetOutgoingActionRow
	var pr prepared
	ok := false
	if err := tenancy.WithTenant(ctx, c.d.DB, *j.TenantID, func(tx pgx.Tx) error {
		var err error
		ok = false
		if m, err = store.New(tx).GetOutgoingAction(ctx, p.MessageID); err != nil {
			return err //nolint:wrapcheck // bọc ở dưới
		}
		pr, ok, err = c.lookupFor(ctx, tx, m.ChannelAccountID)
		return err
	}); err != nil {
		return commands.StatusUnknown, fmt.Errorf("đọc câu trả lời cần tra: %w", err)
	}
	if !ok || !m.ParentExternalID.Valid {
		return commands.StatusUnknown, nil
	}
	res, err := pr.adapter.Lookup(ctx, pr.acc, pr.cred, channel.LookupQuery{
		Purpose: channel.PurposeCommentReply, CommentID: m.ParentExternalID.String, Text: m.Body.String, After: m.SentAt,
	})
	if err != nil || res.Status != channel.OutcomeSent || res.ExternalID == "" {
		return commands.StatusUnknown, err //nolint:wrapcheck // lỗi của adapter đã có việc đang làm
	}
	ext := pgtype.Text{String: res.ExternalID, Valid: true}
	owned := false
	if err := tenancy.WithTenant(ctx, c.d.DB, *j.TenantID, func(tx pgx.Tx) error {
		if err := c.merge(ctx, tx, m.ID, m.ConversationID, m.ChannelAccountID, ext); err != nil {
			return err
		}
		n, err := store.New(tx).SetOwnExternalID(ctx, store.SetOwnExternalIDParams{ExternalID: ext, ID: m.ID})
		owned = n == 1
		return err //nolint:wrapcheck // bọc ở dưới
	}); err != nil {
		return commands.StatusUnknown, fmt.Errorf("ghi mã câu trả lời đã tra: %w", err)
	}
	if !owned { // mã đã thuộc dòng khác: câu thấy là câu trả lời cũ cùng chữ, chưa biết câu này
		return commands.StatusUnknown, nil
	}
	return commands.StatusDone, nil
}

// merge khóa hội thoại conv rồi tin msgID (ADR-46 nhóm 6) và xóa dòng page_external mang mã ext của Page acc (webhook
// feed của chính câu trả lời tới trước), với cờ app.merge_reply chỉ bật quanh câu xóa.
func (c replyCommand) merge(ctx context.Context, tx pgx.Tx, msgID, conv, acc uuid.UUID, ext pgtype.Text) error {
	q := store.New(tx)
	if _, err := q.LockConversationForSend(ctx, conv); err != nil {
		return fmt.Errorf("khóa hội thoại của câu trả lời: %w", err)
	}
	if _, err := q.LockOutgoingMessage(ctx, msgID); err != nil {
		return fmt.Errorf("khóa câu trả lời: %w", err)
	}
	if err := q.SetMergeReplyFlag(ctx, mergeOn); err != nil {
		return fmt.Errorf("bật cờ gộp câu trả lời: %w", err)
	}
	if _, err := q.DeletePageReplyEcho(ctx, store.DeletePageReplyEchoParams{ChannelAccountID: acc, ExternalID: ext}); err != nil {
		return fmt.Errorf("gộp dòng feed của câu trả lời: %w", err)
	}
	if err := q.SetMergeReplyFlag(ctx, mergeOff); err != nil {
		return fmt.Errorf("tắt cờ gộp câu trả lời: %w", err)
	}
	return nil
}

// OnDone (trong transaction đổi lệnh sang done): kênh xin giãn → NoteBlockedUntil (nhóm 3); khóa hội thoại, tin; gộp
// dòng page_external cùng mã (giữ dòng của CRM, có sender_staff_id); đặt tin sent kèm mã comment; báo hiệu. result nil
// (done do tra lại: Lookup đã ghi mã) → sent, giữ mã đã có.
func (c replyCommand) OnDone(ctx context.Context, j queue.Job, raw json.RawMessage, tx pgx.Tx) error {
	return c.done(ctx, j, raw, tx, c.settleReply)
}

// settleReply: có mã ext → khóa, gộp dòng feed cùng mã; không → khóa; rồi đặt câu trả lời sent.
func (c replyCommand) settleReply(ctx context.Context, tx pgx.Tx, j queue.Job, m store.GetOutgoingMessageRow, ext string) error {
	mark := store.MarkOwnMessageSentParams{SentAt: c.now().UTC(), ID: m.ID}
	if ext != "" {
		mark.ExternalID = pgtype.Text{String: ext, Valid: true}
		if err := c.merge(ctx, tx, m.ID, m.ConversationID, m.ChannelAccountID, mark.ExternalID); err != nil {
			return err
		}
	} else if _, err := lock(ctx, tx, m); err != nil {
		return err
	}
	return c.markSent(ctx, tx, j, m, mark)
}
