package send

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Trạng thái nhắn riêng của một comment (PrivateReplyState.State).
const (
	PrivateReplyAvailable = "available" // còn nhắn riêng được, At là hạn (7 ngày sau lúc comment)
	PrivateReplyUsed      = "used"      // đã nhắn (tin riêng đang sống, hay Meta báo đã nhắn), At là lúc bấm gửi
	PrivateReplyExpired   = "expired"   // quá 7 ngày, hay kênh không cho
)

// PrivateReplyState là trạng thái nhắn riêng của một comment: State, mốc At, MessageID của tin riêng (used).
type PrivateReplyState struct {
	State     string
	At        *time.Time
	MessageID *uuid.UUID
}

// uniqueViolation là mã lỗi PostgreSQL khi chèn trùng unique (messages_one_live_private_reply).
const uniqueViolation = "23505"

// noteIdentityTaken là tin hệ thống khi PSID người nhận tin riêng đã thuộc hồ sơ khác (không mang dữ liệu cá nhân).
const noteIdentityTaken = "Facebook báo người nhận tin riêng đã thuộc một hồ sơ khách khác: kiểm tra và gộp hai hồ sơ nếu cùng một người"

// PrivateReply nhắn riêng người viết comment in.CommentMessageID của hội thoại comment conversationID bằng nhân viên u
// lúc now, trong tx tenant của bên gọi (WriteTx). Tin kind private_reply nằm trong hội thoại hộp thư của cùng khách và
// Page (đúng khóa luồng = mã người comment, không có thì hộp thư mới nhất của khách, không có nữa thì tạo); hai hội
// thoại khóa theo id tăng dần (ADR-46 nhóm 6). Lỗi: không có hay không quyền → access.ErrNotFound; không phải hội thoại
// comment, chữ rỗng hay quá 2 000 ký tự → ErrInvalidInput; không phải comment của khách → ErrNotCustomerComment; Page
// chỉ xem → ErrReadOnly; đã nhắn → ErrPrivateReplyUsed; quá 7 ngày → *NotAllowedError private_reply_expired.
func PrivateReply(ctx context.Context, tx pgx.Tx, env Env, u identity.AuthUser, conversationID uuid.UUID,
	in CommentInput, now time.Time,
) (Message, error) {
	now = now.UTC()
	th, inbox, err := inboxOf(ctx, tx, conversationID)
	if err != nil {
		return Message{}, err
	}
	c, err := lockConversation(ctx, tx, u, th.ID)
	if err != nil {
		return Message{}, err
	}
	if c.Kind != kindComment || validText(in.Text, maxText) != nil {
		return Message{}, ErrInvalidInput
	}
	cm, err := commentOf(ctx, tx, in.CommentMessageID, c.ID, true)
	if err != nil {
		return Message{}, err
	}
	acc, err := channel.Account(ctx, tx, c.ChannelAccountID)
	if err != nil {
		return Message{}, err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	used, err := latestPrivate(ctx, tx, acc.ID, cm.ExternalID, nil)
	if err != nil {
		return Message{}, err
	}
	if _, err := policyFor(env.Registry, acc, privateState(acc, cm.SentAt, used != nil), now); err != nil {
		if na, ok := errors.AsType[*NotAllowedError](err); ok && na.Reason == channel.DenyPrivateReplyUsed {
			return Message{}, ErrPrivateReplyUsed
		}
		return Message{}, err
	}
	if inbox, err = ensureInbox(ctx, tx, th, inbox, now); err != nil {
		return Message{}, err
	}
	m, err := insertAction(ctx, tx, u, actionRow{
		conv: inbox, acc: acc, kind: kindPrivate, parent: cm, text: in.Text, jobKind: PrivateReplyKind(), jobQueue: env.Queue,
	}, now)
	if pe, ok := errors.AsType[*pgconn.PgError](err); ok && pe.Code == uniqueViolation {
		return Message{}, ErrPrivateReplyUsed
	}
	if err != nil {
		return Message{}, err
	}
	if err := publish(ctx, tx, env.Bus, acc.TenantID, acc.ID, inbox); err != nil {
		return Message{}, err
	}
	return m, publish(ctx, tx, env.Bus, acc.TenantID, acc.ID, c.ID)
}

// inboxOf đọc hội thoại comment id (không khóa) và hội thoại hộp thư sẽ nhận tin riêng (uuid.Nil: chưa có); hộp thư có
// id nhỏ hơn thì khóa luôn (hai hội thoại khóa theo id tăng dần, ADR-46 nhóm 6). Không có hội thoại → access.ErrNotFound.
func inboxOf(ctx context.Context, tx pgx.Tx, id uuid.UUID) (store.GetConversationThreadRow, uuid.UUID, error) {
	q := store.New(tx)
	th, err := q.GetConversationThread(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return th, uuid.Nil, access.ErrNotFound
	case err != nil:
		return th, uuid.Nil, fmt.Errorf("đọc hội thoại comment: %w", err)
	}
	inbox, err := findInbox(ctx, q, th)
	if err != nil || inbox == uuid.Nil {
		return th, inbox, err
	}
	if bytes.Compare(inbox[:], th.ID[:]) < 0 {
		if _, err := q.LockConversationForSend(ctx, inbox); err != nil {
			return th, uuid.Nil, fmt.Errorf("khóa hội thoại hộp thư: %w", err)
		}
	}
	return th, inbox, nil
}

// findInbox trả hội thoại hộp thư sẽ nhận tin riêng của hội thoại comment th (đúng khóa luồng = mã người comment,
// không có thì hộp thư mới nhất của khách trên Page); uuid.Nil khi chưa có. Không khóa.
func findInbox(ctx context.Context, q *store.Queries, th store.GetConversationThreadRow) (uuid.UUID, error) {
	inbox, err := q.FindInboxForCommenter(ctx, store.FindInboxForCommenterParams{
		ChannelAccountID: th.ChannelAccountID, ThreadRef: commenterOf(th.ExternalThreadRef), CustomerID: th.CustomerID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return uuid.Nil, nil
	case err != nil:
		return uuid.Nil, fmt.Errorf("tìm hội thoại hộp thư của khách: %w", err)
	}
	return inbox, nil
}

// InboxFor trả hội thoại hộp thư sẽ nhận tin riêng của hội thoại comment id (uuid.Nil: chưa có), không khóa: bên gọi
// khóa cùng hội thoại comment theo id tăng dần trước khi gọi AskAddress (chốt comment live, Task 172). Không có hội
// thoại → access.ErrNotFound.
func InboxFor(ctx context.Context, tx pgx.Tx, id uuid.UUID) (uuid.UUID, error) {
	q := store.New(tx)
	th, err := q.GetConversationThread(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return uuid.Nil, access.ErrNotFound
	case err != nil:
		return uuid.Nil, fmt.Errorf("đọc hội thoại comment: %w", err)
	}
	return findInbox(ctx, q, th)
}

// ensureInbox khóa hội thoại hộp thư inbox có id lớn hơn hội thoại comment th (sau khi đã khóa th), hay tạo hộp thư
// của khách với khóa luồng = mã người comment khi inbox là uuid.Nil (id mới lớn hơn); trả id hộp thư.
func ensureInbox(ctx context.Context, tx pgx.Tx, th store.GetConversationThreadRow, inbox uuid.UUID, now time.Time,
) (uuid.UUID, error) {
	q := store.New(tx)
	if inbox == uuid.Nil {
		row, err := q.UpsertInboxConversation(ctx, store.UpsertInboxConversationParams{
			ChannelAccountID: th.ChannelAccountID, CustomerID: th.CustomerID,
			ExternalThreadRef: commenterOf(th.ExternalThreadRef), At: now,
		})
		if err != nil {
			return uuid.Nil, fmt.Errorf("tạo hội thoại hộp thư cho tin riêng: %w", err)
		}
		return row.ID, nil
	}
	if bytes.Compare(inbox[:], th.ID[:]) > 0 {
		if _, err := q.LockConversationForSend(ctx, inbox); err != nil {
			return uuid.Nil, fmt.Errorf("khóa hội thoại hộp thư: %w", err)
		}
	}
	return inbox, nil
}

// PrivateReplyStatus trả trạng thái nhắn riêng của comment commentID lúc now (chi tiết hội thoại comment), trong tx
// tenant của bên gọi; bên gọi đã kiểm quyền đọc. Không phải comment → ErrNotCustomerComment.
func PrivateReplyStatus(ctx context.Context, tx pgx.Tx, reg *channel.Registry, commentID uuid.UUID, now time.Time,
) (PrivateReplyState, error) {
	cm, err := store.New(tx).GetCommentForAction(ctx, commentID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return PrivateReplyState{}, ErrNotCustomerComment
	case err != nil:
		return PrivateReplyState{}, fmt.Errorf("đọc comment: %w", err)
	}
	used, err := latestPrivate(ctx, tx, cm.ChannelAccountID, cm.ExternalID, nil)
	if err != nil {
		return PrivateReplyState{}, err
	}
	if used != nil {
		at, id := used.SentAt.UTC(), used.ID
		return PrivateReplyState{State: PrivateReplyUsed, At: &at, MessageID: &id}, nil
	}
	acc, err := channel.Account(ctx, tx, cm.ChannelAccountID)
	if err != nil {
		return PrivateReplyState{}, err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	st := privateState(acc, cm.SentAt, false)
	st.ReadOnly = false // trạng thái của comment, không phải của Page lúc này
	ad := adapterOf(reg, acc)
	if ad == nil || cm.Origin != originCustomer || !cm.ExternalID.Valid {
		return PrivateReplyState{State: PrivateReplyExpired}, nil
	}
	if pol := ad.SendPolicy(st, now.UTC()); pol.Allowed {
		return PrivateReplyState{State: PrivateReplyAvailable, At: pol.Until}, nil
	}
	return PrivateReplyState{State: PrivateReplyExpired}, nil
}

// privateState là trạng thái SendPolicy(private_reply) của comment tạo lúc createdAt trên Page acc.
func privateState(acc channel.ChannelAccount, createdAt time.Time, used bool) channel.ConversationState {
	at := createdAt.UTC()
	return channel.ConversationState{
		Purpose: channel.PurposePrivateReply, Kind: kindComment, CommentCreatedAt: &at, PrivateReplyUsed: used,
		ReadOnly: acc.SendMode != channel.SendModeEnabled,
	}
}

// latestPrivate trả tin riêng mới nhất đã dùng lượt nhắn riêng comment ext của Page acc (bỏ tin exclude); nil khi chưa.
func latestPrivate(ctx context.Context, tx pgx.Tx, acc uuid.UUID, ext pgtype.Text, exclude *uuid.UUID,
) (*store.LatestPrivateReplyRow, error) {
	arg := store.LatestPrivateReplyParams{ChannelAccountID: acc, ParentExternalID: ext}
	if exclude != nil {
		arg.ExcludeID = pgtype.UUID{Bytes: *exclude, Valid: true}
	}
	row, err := store.New(tx).LatestPrivateReply(ctx, arg)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil //nolint:nilnil // chưa nhắn riêng
	case err != nil:
		return nil, fmt.Errorf("đọc tin riêng của comment: %w", err)
	}
	return &row, nil
}

// commenterOf là mã người comment trong khóa luồng của hội thoại comment (<bài>:<người comment>).
func commenterOf(threadRef string) string {
	if i := strings.LastIndexByte(threadRef, ':'); i >= 0 {
		return threadRef[i+1:]
	}
	return threadRef
}

// privateCommand là lệnh command.fb.private_reply: khuôn của lệnh gửi tin, khác phần đọc và OnDone (gắn PSID người
// nhận). Không tra Meta (Meta không cho tra tin riêng): Lookup nhúng từ sendCommand (Task 157) chỉ đọc database, tin
// đã có mã ngoài hay đã sent (echo đã khớp) → done, còn lại giữ in_doubt chờ echo hay người quyết.
type privateCommand struct{ sendCommand }

func (privateCommand) Kind() queue.Kind { return PrivateReplyKind() }

// Execute gửi tin riêng của lệnh j: tin còn queued, comment cha còn, hỏi lại SendPolicy(private_reply) (đã có tin riêng
// khác, quá 7 ngày → ghi lý do, rejected), lượt gửi của Page, gọi kênh (recipient.comment_id).
func (c privateCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	return c.executeAction(ctx, j, channel.PurposePrivateReply, func(ctx context.Context, tx pgx.Tx, m store.GetOutgoingActionRow,
		parent store.GetCommentByExternalIDRow, found bool,
	) (channel.ConversationState, string, error) {
		if !found || parent.RemovedAt.Valid {
			return channel.ConversationState{}, string(channel.DenyCommentRemoved), nil
		}
		used, err := latestPrivate(ctx, tx, m.ChannelAccountID, m.ParentExternalID, &m.ID)
		if err != nil {
			return channel.ConversationState{}, "", err
		}
		return privateState(channel.ChannelAccount{}, parent.SentAt, used != nil), "", nil
	})
}

// OnDone (trong transaction đổi lệnh sang done): người nhận Meta báo (PSID) khác mã người comment → trước hết gắn danh
// tính fb_psid (Page, PSID) cho khách của hội thoại hộp thư (ADR-46 nhóm 5, trước mọi khóa nhóm 6; đã thuộc hồ sơ khác →
// không gộp, tin hệ thống gợi ý); rồi như lệnh gửi tin (OnDone của sendCommand: giãn, khóa, gộp echo, sent); hội thoại
// hộp thư vừa dựng theo mã người comment đổi khóa luồng sang PSID; báo hiệu cả hội thoại comment (nút nhắn riêng khóa).
func (c privateCommand) OnDone(ctx context.Context, j queue.Job, raw json.RawMessage, tx pgx.Tx) error {
	res, m, ok, err := c.begin(ctx, j, raw, tx)
	if err != nil || !ok {
		return err
	}
	q := store.New(tx)
	act, err := q.GetOutgoingAction(ctx, m.ID)
	if err != nil {
		return fmt.Errorf("đọc tin riêng: %w", err)
	}
	parent, hasParent, err := parentOf(ctx, tx, m.ChannelAccountID, act.ParentExternalID)
	if err != nil {
		return err
	}
	var rekey *store.RekeyInboxConversationParams
	taken := false
	if res.RecipientID != "" && hasParent {
		if rekey, taken, err = c.attach(ctx, tx, m.ChannelAccountID, m.ConversationID, parent.ConversationID, res.RecipientID); err != nil {
			return err
		}
	}
	if err := c.settleSent(ctx, tx, j, m, res.ExternalID); err != nil {
		return err
	}
	if rekey != nil {
		if _, err := q.RekeyInboxConversation(ctx, *rekey); err != nil {
			return fmt.Errorf("đổi khóa luồng hộp thư sang PSID: %w", err)
		}
	}
	if taken {
		if err := q.InsertSystemNote(ctx, store.InsertSystemNoteParams{
			ConversationID: m.ConversationID, ChannelAccountID: m.ChannelAccountID,
			Body: pgtype.Text{String: noteIdentityTaken, Valid: true}, SentAt: c.now().UTC(),
		}); err != nil {
			return fmt.Errorf("ghi gợi ý danh tính đã có chủ: %w", err)
		}
	}
	if !hasParent {
		return nil
	}
	return publish(ctx, tx, c.d.Bus, *j.TenantID, m.ChannelAccountID, parent.ConversationID)
}

// attach gắn PSID psid (người nhận Meta báo) cho khách của hội thoại hộp thư inbox khi khác mã người comment của hội
// thoại comment commentConv. Trả việc đổi khóa luồng cần làm (sau khi khóa hội thoại), taken = PSID đã thuộc hồ sơ khác.
func (c privateCommand) attach(ctx context.Context, tx pgx.Tx, accID, inbox, commentConv uuid.UUID, psid string,
) (*store.RekeyInboxConversationParams, bool, error) {
	q := store.New(tx)
	th, err := q.GetConversationThread(ctx, commentConv)
	if err != nil {
		return nil, false, fmt.Errorf("đọc hội thoại comment: %w", err)
	}
	commenter := commenterOf(th.ExternalThreadRef)
	if psid == commenter {
		return nil, false, nil
	}
	box, err := q.GetConversationThread(ctx, inbox)
	if err != nil {
		return nil, false, fmt.Errorf("đọc hội thoại hộp thư: %w", err)
	}
	acc, err := channel.Account(ctx, tx, accID)
	if err != nil {
		return nil, false, err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	w := customer.Writer{Events: c.d.Events, Queue: c.d.Queue, Realtime: c.d.Bus}
	_, err = w.AttachChannelIdentity(ctx, tx, box.CustomerID, customer.ChannelIdentity{
		Kind: string(customer.IdentityFBPSID), Scope: acc.ExternalID, Value: psid,
	}, customer.Actor{Type: customer.ActorSystem})
	if _, ok := errors.AsType[*customer.IdentityTakenError](err); ok {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err //nolint:wrapcheck // customer đã bọc việc đang làm
	}
	return &store.RekeyInboxConversationParams{Psid: psid, ID: inbox, OldRef: commenter}, false, nil
}
