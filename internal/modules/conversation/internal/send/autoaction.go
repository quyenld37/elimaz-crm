package send

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Tự trả lời comment, tự nhắn riêng (Task 166): tin origin automation, cùng thân lệnh với trả lời comment và nhắn riêng
// của nhân viên (Task 158) nhưng loại lệnh riêng: không Looker (không rõ đã gửi → in_doubt, không tự gửi lại; 04 mục
// 5.3), kết quả cuối ghi vào lượt quy tắc qua hàm của bên gọi (gói automation).

// Lệnh của quy tắc auto_reply_comment (kế hoạch M4-26).
const (
	KindAutoReplyComment = "command.fb.auto_reply_comment"
	KindAutoPrivateReply = "command.fb.auto_private_reply"
)

// AutoReplyCommentKind là Kind của lệnh tự trả lời comment (hạn 5 phút, quá hạn thì bỏ).
func AutoReplyCommentKind() queue.Kind {
	return commands.CommandKind(KindAutoReplyComment, commentExpiresAfter, sendTimeout)
}

// AutoPrivateReplyKind là Kind của lệnh tự nhắn riêng người comment (hạn 5 phút, quá hạn thì bỏ).
func AutoPrivateReplyKind() queue.Kind {
	return commands.CommandKind(KindAutoPrivateReply, commentExpiresAfter, sendTimeout)
}

// actionCommand là phần của lệnh Task 158 mà lệnh tự động dùng lại.
type actionCommand interface {
	Execute(ctx context.Context, j queue.Job) (commands.Outcome, error)
	OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error
	OnTerminal(ctx context.Context, j queue.Job, status string, tx pgx.Tx) error
}

// autoActionCommand là lệnh tự động: thân của inner, Kind riêng, không Lookup; mỗi kết quả cuối gọi finish sau khi đổi tin.
type autoActionCommand struct {
	inner  actionCommand
	kind   queue.Kind
	finish Settled
}

func (c autoActionCommand) Kind() queue.Kind { return c.kind }

// AutoRetryNotSent: chắc chắn chưa tới Meta thì tự thử lại trong hạn 5 phút.
func (autoActionCommand) AutoRetryNotSent() bool { return true }

func (c autoActionCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	return c.inner.Execute(ctx, j) //nolint:wrapcheck // thân lệnh của Task 158
}

// OnDone đổi tin như lệnh của nhân viên rồi ghi lượt done.
func (c autoActionCommand) OnDone(ctx context.Context, j queue.Job, raw json.RawMessage, tx pgx.Tx) error {
	if err := c.inner.OnDone(ctx, j, raw, tx); err != nil {
		return err //nolint:wrapcheck // thân lệnh của Task 158 đã bọc
	}
	return c.finish(ctx, tx, j.ID, commands.StatusDone)
}

// OnTerminal đổi tin như lệnh của nhân viên (failed, expired, checking) rồi ghi kết quả cuối của lượt.
func (c autoActionCommand) OnTerminal(ctx context.Context, j queue.Job, status string, tx pgx.Tx) error {
	if err := c.inner.OnTerminal(ctx, j, status, tx); err != nil {
		return err //nolint:wrapcheck // thân lệnh của Task 158 đã bọc
	}
	return c.finish(ctx, tx, j.ID, status)
}

// AutoCommands là hai lệnh tự động (tự trả lời comment, tự nhắn riêng); finish ghi kết quả cuối vào lượt quy tắc.
func AutoCommands(d deps.Deps, finish Settled) []commands.Handler {
	base := newSendCommand(d)
	return []commands.Handler{
		autoActionCommand{inner: replyCommand{base}, kind: AutoReplyCommentKind(), finish: finish},
		autoActionCommand{inner: privateCommand{base}, kind: AutoPrivateReplyKind(), finish: finish},
	}
}

// Lý do một tin tự động không được ghi (AutoRefusal).
var (
	// ErrAutoNotAllowed: kênh không cho (comment đã xóa, quá 7 ngày, đã có tin riêng, Page không có adapter).
	ErrAutoNotAllowed = errors.New("conversation: kênh không cho tin tự động")
)

// QueueAutoCommentReply ghi câu trả lời công khai tự động text cho comment commentID lúc now, trong tx của bên gọi
// (tx comment vào, hội thoại comment đã khóa: khóa lại cùng dòng không đổi thứ tự): khóa hội thoại → Page chỉ xem →
// ErrReadOnly → SendPolicy(comment_reply) không cho → ErrAutoNotAllowed → chèn tin origin automation queued, lệnh
// command.fb.auto_reply_comment → báo hiệu. Trả mã lệnh.
func QueueAutoCommentReply(ctx context.Context, tx pgx.Tx, env Env, commentID uuid.UUID, text string, now time.Time,
) (uuid.UUID, error) {
	return withAutoComment(ctx, tx, commentID, now, func(cm store.GetCommentForActionRow, now time.Time) (uuid.UUID, error) {
		return autoCommentReply(ctx, tx, env, cm, text, now)
	})
}

// autoCommentReply là thân của QueueAutoCommentReply cho comment cm đã đọc.
func autoCommentReply(ctx context.Context, tx pgx.Tx, env Env, cm store.GetCommentForActionRow, text string, now time.Time,
) (uuid.UUID, error) {
	acc, err := lockForAuto(ctx, tx, cm)
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := policyFor(env.Registry, acc, channel.ConversationState{
		Purpose: channel.PurposeCommentReply, Kind: kindComment, CommentRemoved: cm.RemovedAt.Valid, Automated: true,
	}, now); err != nil {
		return uuid.Nil, autoRefusal(err)
	}
	return queueAuto(ctx, tx, env, actionRow{
		origin: originAuto, conv: cm.ConversationID, acc: acc, kind: kindComment, parent: cm, text: text,
		jobKind: AutoReplyCommentKind(), jobQueue: env.Queue,
	}, now)
}

// QueueAutoPrivateReply ghi tin riêng tự động text cho comment commentID lúc now, trong tx riêng của bên gọi (việc chuẩn
// bị, không phải tx comment vào): khóa hội thoại hộp thư và hội thoại comment theo id tăng dần như đường nhân viên
// (ADR-46 nhóm 6) → Page chỉ xem → ErrReadOnly → đã có tin riêng, quá 7 ngày, comment đã xóa → ErrAutoNotAllowed → chèn
// tin private_reply origin automation trong hộp thư (tạo nếu chưa có), lệnh command.fb.auto_private_reply → báo hiệu
// hộp thư và hội thoại comment. Trả mã lệnh.
func QueueAutoPrivateReply(ctx context.Context, tx pgx.Tx, env Env, commentID uuid.UUID, text string, now time.Time,
) (uuid.UUID, error) {
	return withAutoComment(ctx, tx, commentID, now, func(cm store.GetCommentForActionRow, now time.Time) (uuid.UUID, error) {
		return autoPrivateReply(ctx, tx, env, cm, text, now)
	})
}

// autoPrivateReply là thân của QueueAutoPrivateReply cho comment cm đã đọc.
func autoPrivateReply(ctx context.Context, tx pgx.Tx, env Env, cm store.GetCommentForActionRow, text string, now time.Time,
) (uuid.UUID, error) {
	th, inbox, err := inboxOf(ctx, tx, cm.ConversationID)
	if err != nil {
		return uuid.Nil, err
	}
	acc, err := lockForAuto(ctx, tx, cm)
	if err != nil {
		return uuid.Nil, err
	}
	used, err := latestPrivate(ctx, tx, acc.ID, cm.ExternalID, nil)
	if err != nil {
		return uuid.Nil, err
	}
	st := privateState(acc, cm.SentAt, used != nil)
	st.CommentRemoved, st.Automated = cm.RemovedAt.Valid, true
	if _, err := policyFor(env.Registry, acc, st, now); err != nil || cm.RemovedAt.Valid {
		return uuid.Nil, autoRefusal(err)
	}
	if inbox, err = ensureInbox(ctx, tx, th, inbox, now); err != nil {
		return uuid.Nil, err
	}
	jobID, err := queueAuto(ctx, tx, env, actionRow{
		origin: originAuto, conv: inbox, acc: acc, kind: kindPrivate, parent: cm, text: text,
		jobKind: AutoPrivateReplyKind(), jobQueue: env.Queue,
	}, now)
	if pe, ok := errors.AsType[*pgconn.PgError](err); ok && pe.Code == uniqueViolation {
		return uuid.Nil, ErrAutoNotAllowed // phòng hờ: khóa hội thoại comment đã nối tiếp với đường nhân viên
	}
	if err != nil {
		return uuid.Nil, err
	}
	return jobID, publish(ctx, tx, env.Bus, acc.TenantID, acc.ID, cm.ConversationID)
}

// withAutoComment đọc comment của khách commentID (autoComment) rồi chạy fn với mốc now theo UTC.
func withAutoComment(ctx context.Context, tx pgx.Tx, commentID uuid.UUID, now time.Time,
	fn func(cm store.GetCommentForActionRow, now time.Time) (uuid.UUID, error),
) (uuid.UUID, error) {
	cm, err := autoComment(ctx, tx, commentID)
	if err != nil {
		return uuid.Nil, err
	}
	return fn(cm, now.UTC())
}

// lockForAuto khóa hội thoại comment của cm (ADR-46 nhóm 6) và đọc Page của nó.
func lockForAuto(ctx context.Context, tx pgx.Tx, cm store.GetCommentForActionRow) (channel.ChannelAccount, error) {
	if _, err := store.New(tx).LockConversationForSend(ctx, cm.ConversationID); err != nil {
		return channel.ChannelAccount{}, fmt.Errorf("khóa hội thoại comment: %w", err)
	}
	acc, err := channel.Account(ctx, tx, cm.ChannelAccountID)
	if err != nil {
		return acc, err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	return acc, nil
}

// queueAuto chèn tin tự động r (hội thoại đã khóa) kèm lệnh, báo hiệu hội thoại của tin; trả mã lệnh.
func queueAuto(ctx context.Context, tx pgx.Tx, env Env, r actionRow, now time.Time) (uuid.UUID, error) {
	m, err := insertAction(ctx, tx, identity.AuthUser{}, r, now)
	if err != nil {
		return uuid.Nil, err
	}
	if err := publish(ctx, tx, env.Bus, r.acc.TenantID, r.acc.ID, r.conv); err != nil {
		return uuid.Nil, err
	}
	id, err := store.New(tx).GetMessageCommandJob(ctx, m.ID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("đọc lệnh của tin tự động: %w", err)
	}
	return id, nil
}

// AutoPrivateAllowed cho biết comment commentID (của khách, có mã kênh) còn nhắn riêng tự động được lúc now (chưa có tin
// riêng, chưa quá 7 ngày, chưa xóa, Page có adapter); đọc không khóa, kiểm lại dưới khóa ở QueueAutoPrivateReply.
func AutoPrivateAllowed(ctx context.Context, tx pgx.Tx, reg *channel.Registry, commentID uuid.UUID, now time.Time) (bool, error) {
	cm, err := autoComment(ctx, tx, commentID)
	if errors.Is(err, ErrNotCustomerComment) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	acc, err := channel.Account(ctx, tx, cm.ChannelAccountID)
	if err != nil {
		return false, err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	used, err := latestPrivate(ctx, tx, acc.ID, cm.ExternalID, nil)
	if err != nil {
		return false, err
	}
	st := privateState(acc, cm.SentAt, used != nil)
	st.ReadOnly, st.Automated = false, true // chỉ xem đã do lõi quy tắc kiểm
	ad := adapterOf(reg, acc)
	return ad != nil && !cm.RemovedAt.Valid && ad.SendPolicy(st, now.UTC()).Allowed, nil
}

// autoComment đọc comment của khách commentID (không khóa) cho tin tự động; không có, không phải của khách, chưa có mã
// kênh → ErrNotCustomerComment.
func autoComment(ctx context.Context, tx pgx.Tx, commentID uuid.UUID) (store.GetCommentForActionRow, error) {
	cm, err := store.New(tx).GetCommentForAction(ctx, commentID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return cm, ErrNotCustomerComment
	case err != nil:
		return cm, fmt.Errorf("đọc comment cho tin tự động: %w", err)
	case cm.Origin != originCustomer || !cm.ExternalID.Valid:
		return cm, ErrNotCustomerComment
	}
	return cm, nil
}

// autoRefusal đổi lỗi chính sách sang lỗi của tin tự động (ErrReadOnly giữ nguyên, còn lại ErrAutoNotAllowed).
func autoRefusal(err error) error {
	if errors.Is(err, ErrReadOnly) {
		return ErrReadOnly
	}
	return ErrAutoNotAllowed
}
