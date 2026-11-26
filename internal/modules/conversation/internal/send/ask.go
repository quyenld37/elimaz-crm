package send

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Kết quả xin địa chỉ sau khi chốt comment live (AskResult.Status; cột ask_status của live_comment_claims, Task 172).
const (
	AskQueued        = "queued"         // tin riêng hay tin thường đã vào hàng gửi
	AskSkippedPolicy = "skipped_policy" // chính sách của kênh hay chế độ chỉ xem không cho gửi; Reason là câu tiếng Việt
)

// noInboxText là lý do khi không nhắn riêng được và khách chưa có hội thoại hộp thư trên Page.
const noInboxText = "Khách chưa nhắn vào hộp thư của Page"

// AskResult là kết quả AskAddress: Status (AskQueued, AskSkippedPolicy), Reason (câu tiếng Việt khi bỏ), Message (tin
// vừa vào hàng gửi khi queued).
type AskResult struct {
	Status  string
	Reason  string
	Message *Message
}

// AskAddress gửi text (tin xin địa chỉ đã điền mẫu) cho người viết comment commentID bằng nhân viên u lúc now, trong
// tx tenant của bên gọi (Q29, Task 172). Bên gọi đã kiểm quyền (live.operate trên Page) và đã khóa hội thoại comment
// cùng hộp thư inbox của khách (InboxFor; uuid.Nil: chưa có) theo id tăng dần; khóa lại ở đây là việc không đổi gì,
// không tìm lại hộp thư (không khóa hội thoại nào khác sau khi bên gọi đã khóa đơn, tồn). Thứ tự:
//
//  1. Page chỉ xem → AskSkippedPolicy, lý do chỉ xem.
//  2. SendPolicy(private_reply) cho → tin riêng như PrivateReply (hộp thư của khách, tạo nếu chưa có), lệnh
//     command.fb.private_reply.
//  3. Không cho mà khách có hộp thư còn gửi được tin thường (SendPolicy(message), không phải chỉ thẻ nhân viên) → tin
//     thường như Send, lệnh command.fb.send_message.
//  4. Không nữa → AskSkippedPolicy, lý do của cả hai.
//
// Lỗi: không phải comment của khách → ErrNotCustomerComment; chữ rỗng hay quá 2 000 ký tự → ErrInvalidInput.
func AskAddress(ctx context.Context, tx pgx.Tx, env Env, u identity.AuthUser, commentID, inbox uuid.UUID, text string,
	now time.Time,
) (AskResult, error) {
	now = now.UTC()
	if validText(text, maxText) != nil {
		return AskResult{}, ErrInvalidInput
	}
	cm, err := store.New(tx).GetCommentForAction(ctx, commentID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return AskResult{}, ErrNotCustomerComment
	case err != nil:
		return AskResult{}, fmt.Errorf("đọc comment: %w", err)
	case cm.Origin != originCustomer || !cm.ExternalID.Valid:
		return AskResult{}, ErrNotCustomerComment
	}
	acc, err := channel.Account(ctx, tx, cm.ChannelAccountID)
	if err != nil {
		return AskResult{}, err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	if acc.SendMode != channel.SendModeEnabled {
		return AskResult{Status: AskSkippedPolicy, Reason: FailReasonText(string(channel.DenyReadOnly))}, nil
	}
	th, err := store.New(tx).GetConversationThread(ctx, cm.ConversationID)
	if err != nil {
		return AskResult{}, fmt.Errorf("đọc hội thoại comment: %w", err)
	}
	used, err := latestPrivate(ctx, tx, acc.ID, cm.ExternalID, nil)
	if err != nil {
		return AskResult{}, err
	}
	_, perr := policyFor(env.Registry, acc, privateState(acc, cm.SentAt, used != nil), now)
	if perr == nil {
		return askPrivate(ctx, tx, env, u, th, inbox, acc, cm, text, now)
	}
	na, ok := errors.AsType[*NotAllowedError](perr)
	if !ok {
		return AskResult{}, perr
	}
	privateWhy := FailReasonText(string(na.Reason))
	if inbox == uuid.Nil {
		return AskResult{Status: AskSkippedPolicy, Reason: privateWhy + "; " + noInboxText}, nil
	}
	c, err := store.New(tx).LockConversationForSend(ctx, inbox)
	if err != nil {
		return AskResult{}, fmt.Errorf("khóa hội thoại hộp thư: %w", err)
	}
	if err := allowed(env.Registry, conversationRow(c), acc, false, now); err != nil {
		mna, ok := errors.AsType[*NotAllowedError](err)
		if !ok {
			return AskResult{}, err
		}
		return AskResult{Status: AskSkippedPolicy, Reason: privateWhy + "; " + FailReasonText(string(mna.Reason))}, nil
	}
	m, err := insert(ctx, tx, env, u, inbox, acc, text, []Attachment{}, now)
	if err != nil {
		return AskResult{}, err
	}
	return AskResult{Status: AskQueued, Message: &m}, nil
}

// askPrivate là nhánh nhắn riêng của AskAddress: hộp thư của khách (tạo nếu chưa có), tin riêng queued, lệnh, báo hiệu
// hộp thư và hội thoại comment.
func askPrivate(ctx context.Context, tx pgx.Tx, env Env, u identity.AuthUser, th store.GetConversationThreadRow,
	inbox uuid.UUID, acc channel.ChannelAccount, cm store.GetCommentForActionRow, text string, now time.Time,
) (AskResult, error) {
	inbox, err := ensureInbox(ctx, tx, th, inbox, now)
	if err != nil {
		return AskResult{}, err
	}
	m, err := insertAction(ctx, tx, u, actionRow{
		conv: inbox, acc: acc, kind: kindPrivate, parent: cm, text: text, jobKind: PrivateReplyKind(), jobQueue: env.Queue,
	}, now)
	if err != nil {
		return AskResult{}, err
	}
	if err := publish(ctx, tx, env.Bus, acc.TenantID, acc.ID, inbox); err != nil {
		return AskResult{}, err
	}
	if err := publish(ctx, tx, env.Bus, acc.TenantID, acc.ID, th.ID); err != nil {
		return AskResult{}, err
	}
	return AskResult{Status: AskQueued, Message: &m}, nil
}
