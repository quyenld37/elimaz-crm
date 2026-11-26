package send

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/commands"
)

// Gửi lại, bỏ tin "không rõ đã gửi" hay "chưa gửi được" (Task 157; CH18, CH19): chỉ nhân viên quyết, mỗi lần gửi lại
// là một tin mới với client_ref mới (tin cũ không bao giờ quay lại hàng chờ).

// ErrNotRecoverable là lỗi: tin không ở trạng thái gửi lại hay bỏ được (đã gửi, đang chờ, đã bỏ, lỗi vĩnh viễn) hoặc lệnh của
// nó đang chạy (409 version_conflict: màn hình đang cũ).
var ErrNotRecoverable = errors.New("conversation: tin không gửi lại hay bỏ được ở trạng thái này")

// Trạng thái, lý do.
const (
	statusDiscarded = "discarded"
	// Mã lý do bỏ lệnh cũ (last_error của việc, không nội dung).
	abandonResend  = "resend"
	abandonDiscard = "discard"
)

// ResendResult là kết quả gửi lại: tin mới (queued) và MayDuplicate = true khi tin cũ "không rõ đã gửi" (khách có thể
// nhận hai lần).
type ResendResult struct {
	Message      Message
	MayDuplicate bool
}

// resendable: in_doubt, expired, hay failed với lý do tạm (kênh bận, chưa tới được kênh; tệp task-157, Quyết định 1).
func resendable(m store.LockRecoverableMessageRow) bool {
	switch m.SendStatus.String {
	case statusInDoubt, statusExpired:
		return true
	case statusFailed:
		return m.FailReason.String == reasonRateLimited || m.FailReason.String == reasonNotSent
	}
	return false
}

// discardable: không rõ, đang chờ echo, quá hạn, lỗi.
func discardable(m store.LockRecoverableMessageRow) bool {
	switch m.SendStatus.String {
	case statusInDoubt, statusChecking, statusExpired, statusFailed:
		return true
	}
	return false
}

// recoverable là tin cần gửi lại hay bỏ đã khóa cùng hội thoại và Page của nó.
type recoverable struct {
	msg  store.LockRecoverableMessageRow
	conv conversationRow
	acc  channel.ChannelAccount
}

// lockRecoverable khóa hội thoại của tin id rồi tin (ADR-46 nhóm 6), kiểm conversation.reply trên Page. Tin không có,
// không phải tin gửi ra (tin thường, câu trả lời comment, tin riêng) của nhân viên/tự động, hay Page không được trả lời
// → access.ErrNotFound.
func lockRecoverable(ctx context.Context, tx pgx.Tx, u identity.AuthUser, id uuid.UUID) (recoverable, error) {
	q := store.New(tx)
	convID, err := q.GetRecoverableMessageConversation(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return recoverable{}, access.ErrNotFound
	case err != nil:
		return recoverable{}, fmt.Errorf("đọc hội thoại của tin: %w", err)
	}
	c, err := q.LockConversationForSend(ctx, convID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return recoverable{}, access.ErrNotFound
	case err != nil:
		return recoverable{}, fmt.Errorf("khóa hội thoại của tin: %w", err)
	}
	if err := access.Require(u, permReply, c.ChannelAccountID); err != nil {
		return recoverable{}, err //nolint:wrapcheck // sentinel của access
	}
	m, err := q.LockRecoverableMessage(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return recoverable{}, access.ErrNotFound
	case err != nil:
		return recoverable{}, fmt.Errorf("khóa tin: %w", err)
	}
	acc, err := channel.Account(ctx, tx, c.ChannelAccountID)
	if err != nil {
		return recoverable{}, err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	return recoverable{msg: m, conv: conversationRow(c), acc: acc}, nil
}

// markDiscarded đổi tin đã khóa sang discarded (có điều kiện trạng thái đã đọc).
func markDiscarded(ctx context.Context, tx pgx.Tx, m store.LockRecoverableMessageRow) error {
	n, err := store.New(tx).MarkMessageDiscarded(ctx, store.MarkMessageDiscardedParams{ID: m.ID, FromStatus: m.SendStatus})
	switch {
	case err != nil:
		return fmt.Errorf("bỏ tin: %w", err)
	case n == 0:
		return ErrNotRecoverable
	}
	return nil
}

// abandon bỏ lệnh gửi của tin (nếu còn chờ hay in_doubt; ADR-46 nhóm 13, sau mọi khóa dòng nghiệp vụ). Lệnh đã kết
// thúc hay đã dọn → không làm gì; đang chạy → ErrNotRecoverable.
func abandon(ctx context.Context, tx pgx.Tx, m store.LockRecoverableMessageRow, reason string) error {
	if !m.CommandJobID.Valid {
		return nil
	}
	err := commands.Abandon(ctx, tx, uuid.UUID(m.CommandJobID.Bytes), reason)
	switch {
	case err == nil, errors.Is(err, commands.ErrCommandNotFound):
		return nil
	case errors.Is(err, commands.ErrCommandRunning):
		return ErrNotRecoverable
	}
	return err //nolint:wrapcheck // commands đã bọc việc đang làm
}

// Resend gửi lại tin id (in_doubt, expired, failed lý do tạm) bằng nhân viên u lúc now, trong tx tenant của bên gọi
// (WriteTx): khóa hội thoại → tin; trạng thái không hợp lệ → ErrNotRecoverable; tin thường (resendMessage), câu trả lời
// comment hay tin riêng (resendAction, Task 497) hỏi lại chính sách như gửi mới; tin cũ → discarded (0 dòng →
// ErrNotRecoverable); chèn tin mới cùng loại, nguồn, nội dung, client_ref mới, lệnh mới của nhân viên (không rõ lần
// này thì nhân viên quyết, như tin thường); bỏ lệnh cũ. Lỗi thì bên gọi lùi transaction, tin cũ giữ nguyên.
func Resend(ctx context.Context, tx pgx.Tx, env Env, u identity.AuthUser, id uuid.UUID, now time.Time) (ResendResult, error) {
	now = now.UTC()
	r, err := lockRecoverable(ctx, tx, u, id)
	if err != nil {
		return ResendResult{}, err
	}
	if !resendable(r.msg) {
		return ResendResult{}, ErrNotRecoverable
	}
	var m Message
	switch r.msg.Kind {
	case kindMessage:
		m, err = resendMessage(ctx, tx, env, u, r, now)
	case kindComment, kindPrivate:
		m, err = resendAction(ctx, tx, env, u, r, now)
	default:
		err = ErrNotRecoverable
	}
	if err != nil {
		return ResendResult{}, err
	}
	if err := abandon(ctx, tx, r.msg, abandonResend); err != nil {
		return ResendResult{}, err
	}
	return ResendResult{Message: m, MayDuplicate: r.msg.SendStatus.String == statusInDoubt}, nil
}

// resendMessage gửi lại tin thường r (hội thoại hộp thư): hỏi lại chế độ chỉ xem và SendPolicy như gửi mới (không có
// thẻ nhân viên: chỉ còn gửi được bằng "Trả lời (nhân viên)" → *NotAllowedError window_closed; tin tự động như tin
// tự động); kiểm lại ảnh; tin cũ → discarded; tin mới giữ nguồn (tin chào gửi lại vẫn là tin tự động, không đổi mốc tin
// nhân viên) và nút trả lời nhanh (Task 497).
func resendMessage(ctx context.Context, tx pgx.Tx, env Env, u identity.AuthUser, r recoverable, now time.Time) (Message, error) {
	if r.conv.Kind != kindInbox {
		return Message{}, ErrNotRecoverable
	}
	if err := allowed(env.Registry, r.conv, r.acc, false, r.msg.Origin == originAuto, now); err != nil {
		return Message{}, err
	}
	var atts []Attachment
	if err := json.Unmarshal(r.msg.Attachments, &atts); err != nil {
		return Message{}, fmt.Errorf("đọc ảnh của tin cũ: %w", err)
	}
	var fileID *uuid.UUID
	if len(atts) > 0 {
		fileID = atts[0].FileID
	}
	atts, err := outgoingAttachments(ctx, tx, fileID)
	if err != nil {
		return Message{}, err
	}
	if r.msg.Body.String == "" && len(atts) == 0 {
		return Message{}, ErrNotRecoverable
	}
	if err := markDiscarded(ctx, tx, r.msg); err != nil {
		return Message{}, err
	}
	return insert(ctx, tx, env, u, r.conv.ID, r.acc, outgoing{
		origin: r.msg.Origin, text: r.msg.Body.String, atts: atts, quickReplies: r.msg.QuickReplies,
	}, now)
}

// resendAction gửi lại câu trả lời comment (trong hội thoại comment) hay tin riêng (trong hội thoại hộp thư) r: comment
// cha không còn hay đã xóa → *NotAllowedError comment_removed; Page chỉ xem → ErrReadOnly; SendPolicy như gửi mới (tin
// riêng: đã có tin riêng khác đang sống → ErrPrivateReplyUsed, quá 7 ngày → *NotAllowedError); tin cũ → discarded; chèn
// tin cùng loại, cùng comment cha, cùng nguồn với lệnh của nhân viên (reply_comment, private_reply); báo hiệu hội thoại
// của tin và (tin riêng) hội thoại comment.
func resendAction(ctx context.Context, tx pgx.Tx, env Env, u identity.AuthUser, r recoverable, now time.Time) (Message, error) {
	parent, found, err := parentOf(ctx, tx, r.acc.ID, r.msg.ParentExternalID)
	if err != nil {
		return Message{}, err
	}
	if !found || parent.RemovedAt.Valid {
		return Message{}, &NotAllowedError{Reason: channel.DenyCommentRemoved}
	}
	row := actionRow{
		origin: r.msg.Origin, conv: r.conv.ID, acc: r.acc, kind: r.msg.Kind, text: r.msg.Body.String, jobQueue: env.Queue,
		parent: store.GetCommentForActionRow{ID: parent.ID, ExternalID: r.msg.ParentExternalID, PostID: r.msg.PostID},
	}
	var st channel.ConversationState
	switch {
	case r.msg.Kind == kindComment && r.conv.Kind == kindComment:
		row.jobKind = ReplyCommentKind()
		st = channel.ConversationState{Purpose: channel.PurposeCommentReply, Kind: kindComment}
	case r.msg.Kind == kindPrivate && r.conv.Kind == kindInbox:
		row.jobKind = PrivateReplyKind()
		used, err := latestPrivate(ctx, tx, r.acc.ID, r.msg.ParentExternalID, &r.msg.ID)
		if err != nil {
			return Message{}, err
		}
		st = privateState(r.acc, parent.SentAt, used != nil)
	default:
		return Message{}, ErrNotRecoverable
	}
	st.Automated = r.msg.Origin == originAuto
	if _, err := policyFor(env.Registry, r.acc, st, now); err != nil {
		return Message{}, privateUsed(err)
	}
	if err := markDiscarded(ctx, tx, r.msg); err != nil {
		return Message{}, err
	}
	m, err := insertAction(ctx, tx, u, row, now)
	if err != nil {
		return Message{}, privateUsed(err)
	}
	if err := publishAction(ctx, tx, env, r, parent); err != nil {
		return Message{}, err
	}
	return m, nil
}

// publishAction báo hiệu hội thoại của tin r và, khi r là tin riêng, hội thoại comment của comment cha (nút nhắn riêng).
func publishAction(ctx context.Context, tx pgx.Tx, env Env, r recoverable, parent store.GetCommentByExternalIDRow) error {
	if err := publish(ctx, tx, env.Bus, r.acc.TenantID, r.acc.ID, r.conv.ID); err != nil {
		return err
	}
	if r.msg.Kind != kindPrivate || parent.ConversationID == r.conv.ID {
		return nil
	}
	return publish(ctx, tx, env.Bus, r.acc.TenantID, r.acc.ID, parent.ConversationID)
}

// Discard bỏ tin id (in_doubt, checking, expired, failed) bằng nhân viên u, trong tx tenant của bên gọi: khóa hội thoại
// → tin; trạng thái khác → ErrNotRecoverable; tin → discarded; bỏ lệnh gửi (còn chờ, in_doubt → expired); báo hiệu hội
// thoại của tin và (tin riêng) hội thoại comment: tin riêng đã bỏ không còn tính là "đã nhắn riêng", nhắn riêng lại được
// (Task 497). Echo tới sau đó vẫn đặt tin sent (sự thật thắng). Trả tin đã bỏ.
func Discard(ctx context.Context, tx pgx.Tx, env Env, u identity.AuthUser, id uuid.UUID, _ time.Time) (Message, error) {
	r, err := lockRecoverable(ctx, tx, u, id)
	if err != nil {
		return Message{}, err
	}
	if !discardable(r.msg) {
		return Message{}, ErrNotRecoverable
	}
	if err := markDiscarded(ctx, tx, r.msg); err != nil {
		return Message{}, err
	}
	if err := abandon(ctx, tx, r.msg, abandonDiscard); err != nil {
		return Message{}, err
	}
	var parent store.GetCommentByExternalIDRow
	var parentID *uuid.UUID
	if r.msg.Kind != kindMessage {
		p, found, err := parentOf(ctx, tx, r.acc.ID, r.msg.ParentExternalID)
		if err != nil {
			return Message{}, err
		}
		if found {
			parent, parentID = p, &p.ID
		}
	}
	if err := publishAction(ctx, tx, env, r, parent); err != nil {
		return Message{}, err
	}
	var atts []Attachment
	if err := json.Unmarshal(r.msg.Attachments, &atts); err != nil || atts == nil {
		atts = []Attachment{}
	}
	return Message{
		ID: r.msg.ID, ConversationID: r.conv.ID, Origin: r.msg.Origin, Kind: r.msg.Kind, ParentMessageID: parentID,
		Body: r.msg.Body.String, Attachments: atts, SendStatus: statusDiscarded, FailReason: r.msg.FailReason.String, SentAt: r.msg.SentAt,
	}, nil
}
