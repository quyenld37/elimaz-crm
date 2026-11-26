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
	if validText(text, maxText) != nil {
		return AskResult{}, ErrInvalidInput
	}
	p, err := planAsk(ctx, tx, env.Registry, commentID, inbox, now, false, true)
	if err != nil {
		return AskResult{}, err
	}
	if p.route == routeSkip && p.reason == "" {
		p.reason = p.privateWhy + "; " + p.inboxWhy
	}
	return p.deliver(ctx, tx, env, u, text, now)
}

// RemindText là lý do bỏ nhắc khi hộp thư đã quá 24 giờ và comment đã được nhắn riêng (LV05).
const RemindText = "Đã quá 24 giờ và đã nhắn riêng cho comment này"

// RemindPlan là đường gửi đã chọn cho một tin nhắc gửi địa chỉ (Task 173): Allowed, Reason, rồi Deliver trong cùng tx.
type RemindPlan struct{ p askPlan }

// PlanRemind chọn đường gửi tin nhắc cho khách của comment commentID lúc now, trong tx tenant của bên gọi. Bên gọi đã
// kiểm quyền và đã khóa hội thoại comment cùng hộp thư inbox theo id tăng dần (như AskAddress; khóa lại là việc không
// đổi gì). Khác AskAddress ở thứ tự: hộp thư còn gửi được tin thường của nhân viên → tin thường; không thì nhắn riêng
// nếu comment còn lượt; không nữa → không gửi (hộp thư quá 24 giờ và comment đã nhắn riêng → RemindText; khác → lý do
// của cả hai). Lỗi: không phải comment của khách → ErrNotCustomerComment.
func PlanRemind(ctx context.Context, tx pgx.Tx, reg *channel.Registry, commentID, inbox uuid.UUID, now time.Time,
) (RemindPlan, error) {
	p, err := planAsk(ctx, tx, reg, commentID, inbox, now, true, true)
	if err != nil {
		return RemindPlan{}, err
	}
	p.remindReason()
	return RemindPlan{p: p}, nil
}

// Allowed cho biết tin nhắc gửi được (tin thường hay nhắn riêng).
func (r RemindPlan) Allowed() bool { return r.p.route != routeSkip }

// Reason là lý do không gửi được (câu tiếng Việt); rỗng khi Allowed.
func (r RemindPlan) Reason() string { return r.p.reason }

// Deliver gửi text (nhân viên u bấm) theo đường đã chọn: tin vào hàng gửi và lệnh command.fb.send_message /
// command.fb.private_reply, hay AskSkippedPolicy kèm Reason. Chữ rỗng hay quá 2 000 ký tự → ErrInvalidInput.
func (r RemindPlan) Deliver(ctx context.Context, tx pgx.Tx, env Env, u identity.AuthUser, text string, now time.Time,
) (AskResult, error) {
	if validText(text, maxText) != nil {
		return AskResult{}, ErrInvalidInput
	}
	return r.p.deliver(ctx, tx, env, u, text, now)
}

// RemindCheck trả đường PlanRemind sẽ chọn lúc now mà không khóa, không ghi (cột canRemind của danh sách thiếu
// địa chỉ): được gửi, hay lý do không (câu tiếng Việt). Lỗi như AskAddress.
func RemindCheck(ctx context.Context, tx pgx.Tx, reg *channel.Registry, commentID, inbox uuid.UUID, now time.Time,
) (bool, string, error) {
	p, err := planAsk(ctx, tx, reg, commentID, inbox, now, true, false)
	if err != nil {
		return false, "", err
	}
	p.remindReason()
	return p.route != routeSkip, p.reason, nil
}

// Đường gửi của planAsk.
const (
	routeSkip = iota
	routePrivate
	routeInbox
)

// askPlan là đường gửi đã chọn cho tin xin địa chỉ hay tin nhắc: Page, comment, hội thoại comment, hộp thư; lý do
// không nhắn riêng, không gửi tin thường (câu tiếng Việt và mã); reason khi bỏ vì chỉ xem.
type askPlan struct {
	route                  int
	reason                 string
	privateWhy, inboxWhy   string
	privateCode, inboxCode channel.DenyReason
	acc                    channel.ChannelAccount
	cm                     store.GetCommentForActionRow
	inbox                  uuid.UUID
}

// planAsk chọn đường gửi cho người viết comment commentID lúc now: Page chỉ xem → bỏ (reason); xét nhắn riêng
// (SendPolicy(private_reply)) và tin thường trong hộp thư inbox (uuid.Nil: chưa có; lock: khóa dòng, bên gọi đã khóa
// theo id tăng dần nên là việc không đổi gì); inboxFirst: tin thường trước nhắn riêng. Không đường nào → routeSkip,
// bên gọi ghép lý do.
func planAsk(ctx context.Context, tx pgx.Tx, reg *channel.Registry, commentID, inbox uuid.UUID, now time.Time,
	inboxFirst, lock bool,
) (askPlan, error) {
	now = now.UTC()
	q := store.New(tx)
	cm, err := q.GetCommentForAction(ctx, commentID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return askPlan{}, ErrNotCustomerComment
	case err != nil:
		return askPlan{}, fmt.Errorf("đọc comment: %w", err)
	case cm.Origin != originCustomer || !cm.ExternalID.Valid:
		return askPlan{}, ErrNotCustomerComment
	}
	acc, err := channel.Account(ctx, tx, cm.ChannelAccountID)
	if err != nil {
		return askPlan{}, err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	p := askPlan{acc: acc, cm: cm, inbox: inbox}
	if acc.SendMode != channel.SendModeEnabled {
		p.reason = FailReasonText(string(channel.DenyReadOnly))
		return p, nil
	}
	used, err := latestPrivate(ctx, tx, acc.ID, cm.ExternalID, nil)
	if err != nil {
		return askPlan{}, err
	}
	_, perr := policyFor(reg, acc, privateState(acc, cm.SentAt, used != nil), now)
	privateOK := perr == nil
	if !privateOK {
		na, ok := errors.AsType[*NotAllowedError](perr)
		if !ok {
			return askPlan{}, perr
		}
		p.privateCode, p.privateWhy = na.Reason, FailReasonText(string(na.Reason))
	}
	if privateOK && !inboxFirst {
		p.route = routePrivate
		return p, nil
	}
	inboxOK, err := p.inboxAllowed(ctx, q, reg, lock, now)
	if err != nil {
		return askPlan{}, err
	}
	switch {
	case inboxOK:
		p.route = routeInbox
	case privateOK:
		p.route = routePrivate
	}
	return p, nil
}

// inboxAllowed: hộp thư của plan còn gửi được tin thường của nhân viên (không phải chỉ thẻ nhân viên); không thì ghi
// lý do vào plan.
func (p *askPlan) inboxAllowed(ctx context.Context, q *store.Queries, reg *channel.Registry, lock bool, now time.Time,
) (bool, error) {
	if p.inbox == uuid.Nil {
		p.inboxWhy = noInboxText
		return false, nil
	}
	var c conversationRow
	if lock {
		row, err := q.LockConversationForSend(ctx, p.inbox)
		if err != nil {
			return false, fmt.Errorf("khóa hội thoại hộp thư: %w", err)
		}
		c = conversationRow(row)
	} else {
		row, err := q.GetConversationForSend(ctx, p.inbox)
		if err != nil {
			return false, fmt.Errorf("đọc hội thoại hộp thư: %w", err)
		}
		c = row
	}
	err := allowed(reg, c, p.acc, false, false, now)
	if err == nil {
		return true, nil
	}
	na, ok := errors.AsType[*NotAllowedError](err)
	if !ok {
		return false, err
	}
	p.inboxCode, p.inboxWhy = na.Reason, FailReasonText(string(na.Reason))
	return false, nil
}

// remindReason ghép lý do bỏ nhắc (PlanRemind, RemindCheck).
func (p *askPlan) remindReason() {
	switch {
	case p.route != routeSkip || p.reason != "":
	case p.inboxCode == channel.DenyWindowClosed && p.privateCode == channel.DenyPrivateReplyUsed:
		p.reason = RemindText
	default:
		p.reason = p.inboxWhy + "; " + p.privateWhy
	}
}

// deliver gửi text theo đường đã chọn: tin riêng (askPrivate), tin thường trong hộp thư, hay AskSkippedPolicy.
func (p askPlan) deliver(ctx context.Context, tx pgx.Tx, env Env, u identity.AuthUser, text string, now time.Time,
) (AskResult, error) {
	now = now.UTC()
	switch p.route {
	case routePrivate:
		th, err := store.New(tx).GetConversationThread(ctx, p.cm.ConversationID)
		if err != nil {
			return AskResult{}, fmt.Errorf("đọc hội thoại comment: %w", err)
		}
		return askPrivate(ctx, tx, env, u, th, p.inbox, p.acc, p.cm, text, now)
	case routeInbox:
		m, err := insert(ctx, tx, env, u, p.inbox, p.acc, outgoing{text: text, atts: []Attachment{}}, now)
		if err != nil {
			return AskResult{}, err
		}
		return AskResult{Status: AskQueued, Message: &m}, nil
	}
	return AskResult{Status: AskSkippedPolicy, Reason: p.reason}, nil
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
