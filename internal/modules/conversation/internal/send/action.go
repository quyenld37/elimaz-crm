package send

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Phần dùng chung của trả lời comment, nhắn riêng, ẩn/hiện (Task 158): chính sách của kênh ở API, đọc kênh trong lệnh,
// chèn tin queued kèm lệnh.

// policyFor hỏi SendPolicy của adapter của Page acc cho trạng thái st (ReadOnly đặt theo Page): Page chỉ xem →
// ErrReadOnly; tiến trình không có adapter → *NotAllowedError no_permission; không cho → *NotAllowedError.
func policyFor(reg *channel.Registry, acc channel.ChannelAccount, st channel.ConversationState, now time.Time,
) (channel.SendPolicy, error) {
	if acc.SendMode != channel.SendModeEnabled {
		return channel.SendPolicy{}, ErrReadOnly
	}
	ad := adapterOf(reg, acc)
	if ad == nil {
		return channel.SendPolicy{}, &NotAllowedError{Reason: channel.DenyNoPermission}
	}
	pol := ad.SendPolicy(st, now)
	if !pol.Allowed {
		return pol, &NotAllowedError{Reason: pol.Reason, Until: pol.Until}
	}
	return pol, nil
}

// adapterOf trả adapter của kênh của acc; nil khi tiến trình không bật kênh đó.
func adapterOf(reg *channel.Registry, acc channel.ChannelAccount) channel.MessagingAdapter { //nolint:ireturn // adapter là interface của kênh
	if reg == nil {
		return nil
	}
	ad, _ := reg.Get(acc.Channel)
	return ad
}

// channelFor đọc Page accID, adapter, chính sách cho trạng thái st(acc) (ReadOnly đặt theo chế độ gửi của Page) lúc now
// và token, trong tx chỉ đọc của lệnh.
// refusal khác rỗng: không gọi kênh, mã lý do ghi vào tin (không adapter, chính sách không cho, không token).
func (c sendCommand) channelFor(ctx context.Context, tx pgx.Tx, accID uuid.UUID,
	st func(channel.ChannelAccount) channel.ConversationState, now time.Time,
) (prepared, channel.SendPolicy, string, error) {
	acc, err := channel.Account(ctx, tx, accID)
	if err != nil {
		return prepared{}, channel.SendPolicy{}, "", err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	ad := adapterOf(c.d.Registry, acc)
	if ad == nil {
		return prepared{}, channel.SendPolicy{}, reasonNoAdapter, nil
	}
	cs := st(acc)
	cs.ReadOnly = acc.SendMode != channel.SendModeEnabled
	pol := ad.SendPolicy(cs, now)
	if !pol.Allowed {
		return prepared{}, pol, string(pol.Reason), nil
	}
	cred, err := channel.CredentialsFor(ctx, tx, c.d.Keys, acc.ID)
	switch {
	case errors.Is(err, channel.ErrNoCredentials), errors.Is(err, channel.ErrNoKeyring):
		return prepared{}, pol, string(channel.DenyNoPermission), nil
	case err != nil:
		return prepared{}, pol, reasonAuth, nil // bản mã không giải được: token phải nhập lại
	}
	return prepared{acc: acc, adapter: ad, cred: cred}, pol, "", nil
}

// actionState trả trạng thái SendPolicy cho tin m của lệnh comment và comment cha parent (found = còn trong CRM);
// refusal khác rỗng: không gọi kênh, mã lý do ghi vào tin.
type actionState func(ctx context.Context, tx pgx.Tx, m store.GetOutgoingActionRow, parent store.GetCommentByExternalIDRow,
	found bool) (channel.ConversationState, string, error)

// executeAction là Execute của lệnh trả lời comment, nhắn riêng: tin còn queued, comment cha, trạng thái st, hỏi lại
// SendPolicy, lượt gửi của Page, gọi kênh với mục đích purpose (comment cha, nội dung, client_ref).
func (c sendCommand) executeAction(ctx context.Context, j queue.Job, purpose channel.Purpose, st actionState,
) (commands.Outcome, error) {
	return c.run(ctx, j, func(ctx context.Context, tx pgx.Tx, p payload, now time.Time) (prepared, string, error) {
		m, refusal, err := actionOf(ctx, tx, p.MessageID)
		if err != nil || refusal != "" {
			return prepared{}, refusal, err
		}
		parent, found, err := parentOf(ctx, tx, m.ChannelAccountID, m.ParentExternalID)
		if err != nil {
			return prepared{}, "", err
		}
		cs, refusal, err := st(ctx, tx, m, parent, found)
		if err != nil || refusal != "" {
			return prepared{}, refusal, err
		}
		pr, pol, refusal, err := c.channelFor(ctx, tx, m.ChannelAccountID, func(channel.ChannelAccount) channel.ConversationState {
			return cs
		}, now)
		if err != nil || refusal != "" {
			return prepared{}, refusal, err
		}
		pr.cmd = channel.OutboundCommand{
			Purpose: purpose, CommentID: m.ParentExternalID.String, Text: m.Body.String, Mode: pol.Mode,
			ClientRef: uuid.UUID(m.ClientRef.Bytes).String(),
		}
		pr.clickedAt = m.SentAt
		return pr, "", nil
	})
}

// lookupFor đọc Page accID, adapter và token cho Lookup của lệnh (transaction ngắn riêng); ok = false khi không tra
// được (không adapter, không token: giữ in_doubt).
func (c sendCommand) lookupFor(ctx context.Context, tx pgx.Tx, accID uuid.UUID) (prepared, bool, error) {
	acc, err := channel.Account(ctx, tx, accID)
	if err != nil {
		return prepared{}, false, err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	ad := adapterOf(c.d.Registry, acc)
	if ad == nil {
		return prepared{}, false, nil
	}
	cred, err := channel.CredentialsFor(ctx, tx, c.d.Keys, acc.ID)
	if err != nil {
		return prepared{}, false, nil //nolint:nilerr // không có token thì chưa tra được, giữ in_doubt
	}
	return prepared{acc: acc, adapter: ad, cred: cred}, true, nil
}

// actionRow là tin nhân viên cần chèn cho một lần trả lời comment (trong hội thoại comment) hay nhắn riêng (trong hội
// thoại hộp thư).
type actionRow struct {
	conv     uuid.UUID
	acc      channel.ChannelAccount
	kind     string
	parent   store.GetCommentForActionRow
	text     string
	jobKind  queue.Kind
	jobQueue queue.JobQueue
}

// insertAction chèn tin queued r của nhân viên u lúc now, cập nhật hội thoại (đã khóa), ghi lệnh (DedupeKey = mã tin),
// gắn mã lệnh vào tin.
func insertAction(ctx context.Context, tx pgx.Tx, u identity.AuthUser, r actionRow, now time.Time) (Message, error) {
	q := store.New(tx)
	ref, err := uuid.NewV7()
	if err != nil {
		return Message{}, fmt.Errorf("tạo client_ref: %w", err)
	}
	ins := store.InsertOutgoingActionParams{
		ConversationID: r.conv, ChannelAccountID: r.acc.ID, ClientRef: pgtype.UUID{Bytes: ref, Valid: true}, Kind: r.kind,
		ParentExternalID: r.parent.ExternalID, Body: pgtype.Text{String: r.text, Valid: true},
		SenderStaffID: pgtype.UUID{Bytes: u.UserID, Valid: u.UserID != uuid.Nil}, SentAt: now,
	}
	if r.kind == kindComment {
		ins.PostID = r.parent.PostID
	}
	id, err := q.InsertOutgoingAction(ctx, ins)
	if err != nil {
		return Message{}, fmt.Errorf("ghi tin %s: %w", r.kind, err)
	}
	if err := queueOutgoing(ctx, tx, r.jobQueue, r.jobKind, r.acc.TenantID, r.conv, id, deps.Snippet(r.text, false), now); err != nil {
		return Message{}, err
	}
	parent := r.parent.ID
	return Message{
		ID: id, ConversationID: r.conv, ClientRef: ref, SenderStaffID: u.UserID, Kind: r.kind, ParentMessageID: &parent,
		Body: r.text, Attachments: []Attachment{}, SendStatus: statusQueued, SentAt: now,
	}, nil
}

// queueOutgoing là phần sau khi chèn tin queued id của hội thoại conv (đã khóa): cập nhật mốc, đoạn trích snip của hội
// thoại, ghi lệnh kind (DedupeKey = mã tin, payload chỉ mã tin), gắn mã lệnh vào tin.
func queueOutgoing(ctx context.Context, tx pgx.Tx, jq queue.JobQueue, kind queue.Kind, tenant, conv, id uuid.UUID,
	snip string, now time.Time,
) error {
	q := store.New(tx)
	if err := q.BumpConversationOutbound(ctx, store.BumpConversationOutboundParams{
		At: now, Snippet: pgtype.Text{String: snip, Valid: snip != ""}, ID: conv,
	}); err != nil {
		return fmt.Errorf("cập nhật hội thoại sau khi gửi: %w", err)
	}
	jobID, _, err := jq.Enqueue(ctx, tx, kind, queue.NewJob{
		TenantID: &tenant, Payload: payload{MessageID: id}, DedupeKey: id.String(),
	})
	if err != nil {
		return fmt.Errorf("ghi lệnh %s: %w", kind.Name, err)
	}
	if err := q.SetMessageCommandJob(ctx, store.SetMessageCommandJobParams{
		CommandJobID: pgtype.UUID{Bytes: jobID, Valid: true}, ID: id,
	}); err != nil {
		return fmt.Errorf("gắn lệnh vào tin: %w", err)
	}
	return nil
}

// commentOf đọc comment id cho thao tác trong hội thoại conv (đã khóa): không có, không thuộc conv, chưa có mã của
// kênh, hay (customerOnly) không phải của khách → ErrNotCustomerComment.
func commentOf(ctx context.Context, tx pgx.Tx, id, conv uuid.UUID, customerOnly bool) (store.GetCommentForActionRow, error) {
	cm, err := store.New(tx).GetCommentForAction(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return cm, ErrNotCustomerComment
	case err != nil:
		return cm, fmt.Errorf("đọc comment: %w", err)
	case cm.ConversationID != conv, !cm.ExternalID.Valid, customerOnly && cm.Origin != originCustomer:
		return cm, ErrNotCustomerComment
	}
	return cm, nil
}

// parentOf đọc comment cha mã ngoài ext của Page acc cho lệnh (không khóa); không còn → ok = false.
func parentOf(ctx context.Context, tx pgx.Tx, acc uuid.UUID, ext pgtype.Text) (store.GetCommentByExternalIDRow, bool, error) {
	cm, err := store.New(tx).GetCommentByExternalID(ctx, store.GetCommentByExternalIDParams{
		ChannelAccountID: acc, ExternalID: ext,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return cm, false, nil
	case err != nil:
		return cm, false, fmt.Errorf("đọc comment cha: %w", err)
	}
	return cm, true, nil
}

// actionOf đọc tin của lệnh comment (không khóa) cho Execute: không còn hay đã đổi trạng thái → refusal
// outcomeNotQueued.
func actionOf(ctx context.Context, tx pgx.Tx, id uuid.UUID) (store.GetOutgoingActionRow, string, error) {
	m, err := store.New(tx).GetOutgoingAction(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return m, outcomeNotQueued, nil
	case err != nil:
		return m, "", fmt.Errorf("đọc tin của lệnh: %w", err)
	case m.SendStatus.String != statusQueued || !m.ParentExternalID.Valid:
		return m, outcomeNotQueued, nil
	}
	return m, "", nil
}
