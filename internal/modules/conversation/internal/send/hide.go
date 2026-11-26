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

// sourceStaff là nguồn của dòng lịch sử khi nhân viên ẩn/hiện comment qua CRM.
const sourceStaff = "staff"

// Comment là comment của khách sau khi nhân viên bấm ẩn/hiện: trạng thái trong CRM lúc này (lệnh đổi trên Page chạy sau
// commit; xong thì IsHidden đổi theo, comment vẫn hiện trong CRM).
type Comment struct {
	ID, ConversationID uuid.UUID
	Direction, Origin  string
	Body               string
	IsHidden, Removed  bool
	EditedCount        int
	SentAt             time.Time
}

// SetCommentHidden ghi lệnh command.fb.hide_comment ẩn (hidden) hay hiện comment messageID của khách bằng nhân viên u lúc
// now, trong tx tenant của bên gọi (WriteTx). Không có tin comment hay không có conversation.reply trên Page →
// access.ErrNotFound; không phải comment của khách → ErrNotCustomerComment; Page chỉ xem → ErrReadOnly; comment đã xóa →
// *NotAllowedError comment_removed. Mỗi lần bấm là một lệnh (DedupeKey mới); khóa chống trùng của API chặn gửi lại.
func SetCommentHidden(ctx context.Context, tx pgx.Tx, env Env, u identity.AuthUser, messageID uuid.UUID, hidden bool,
	now time.Time,
) (Comment, error) {
	q := store.New(tx)
	cm, err := q.GetCommentForAction(ctx, messageID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Comment{}, access.ErrNotFound
	case err != nil:
		return Comment{}, fmt.Errorf("đọc comment cần ẩn: %w", err)
	}
	if err := access.Require(u, permReply, cm.ChannelAccountID); err != nil {
		return Comment{}, err //nolint:wrapcheck // sentinel của access
	}
	if cm.Origin != originCustomer || !cm.ExternalID.Valid {
		return Comment{}, ErrNotCustomerComment
	}
	acc, err := channel.Account(ctx, tx, cm.ChannelAccountID)
	if err != nil {
		return Comment{}, err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	if _, err := policyFor(env.Registry, acc, channel.ConversationState{Purpose: channel.PurposeHide, Kind: kindComment}, now.UTC()); err != nil {
		return Comment{}, err
	}
	if cm.RemovedAt.Valid {
		return Comment{}, &NotAllowedError{Reason: channel.DenyCommentRemoved}
	}
	key, err := uuid.NewV7()
	if err != nil {
		return Comment{}, fmt.Errorf("tạo khóa lệnh ẩn: %w", err)
	}
	tenant := acc.TenantID
	if _, _, err := env.Queue.Enqueue(ctx, tx, HideCommentKind(), queue.NewJob{
		TenantID: &tenant, Payload: payload{MessageID: cm.ID, Hidden: &hidden}, DedupeKey: key.String(),
	}); err != nil {
		return Comment{}, fmt.Errorf("ghi lệnh ẩn comment: %w", err)
	}
	edits, err := q.CountCommentEdits(ctx, cm.ID)
	if err != nil {
		return Comment{}, fmt.Errorf("đếm lần sửa comment: %w", err)
	}
	return Comment{
		ID: cm.ID, ConversationID: cm.ConversationID, Direction: cm.Direction, Origin: cm.Origin, Body: cm.Body.String,
		IsHidden: cm.IsHidden, Removed: cm.RemovedAt.Valid, EditedCount: int(edits), SentAt: cm.SentAt.UTC(),
	}, nil
}

// hideCommand là lệnh command.fb.hide_comment: khuôn của lệnh gửi tin (run, outcome), tự thử lại khi chắc chắn chưa tới
// Meta trong 1 giờ; Lookup đọc is_hidden; OnDone ghi trạng thái, lịch sử. Lệnh không có tin queued: lý do bị từ chối
// chỉ ở last_error của việc (note không đổi dòng comment).
type hideCommand struct{ sendCommand }

func (hideCommand) Kind() queue.Kind { return HideCommentKind() }

// Execute ẩn/hiện comment của lệnh j: comment còn, có mã của kênh, hỏi lại SendPolicy(hide) (Page chỉ xem → read_only),
// lượt gửi của Page, gọi kênh.
func (c hideCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	return c.run(ctx, j, func(ctx context.Context, tx pgx.Tx, p payload, now time.Time) (prepared, string, error) {
		cm, err := store.New(tx).GetCommentForAction(ctx, p.MessageID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return prepared{}, outcomeNotQueued, nil
		case err != nil:
			return prepared{}, "", fmt.Errorf("đọc comment cần ẩn: %w", err)
		case p.Hidden == nil || !cm.ExternalID.Valid:
			return prepared{}, outcomeNotQueued, nil
		}
		pr, pol, refusal, err := c.channelFor(ctx, tx, cm.ChannelAccountID, func(channel.ChannelAccount) channel.ConversationState {
			return channel.ConversationState{Purpose: channel.PurposeHide, Kind: kindComment}
		}, now)
		if err != nil || refusal != "" {
			return prepared{}, refusal, err
		}
		pr.cmd = channel.OutboundCommand{
			Purpose: channel.PurposeHide, CommentID: cm.ExternalID.String, Hidden: p.Hidden, Mode: pol.Mode,
		}
		pr.clickedAt = now
		return pr, "", nil
	})
}

// Lookup đọc is_hidden của comment trên Meta: đúng như muốn → done; khác → not_sent (đặt lại an toàn, tự thử lại);
// lỗi hay không tra được → unknown.
func (c hideCommand) Lookup(ctx context.Context, j queue.Job) (string, error) {
	var p payload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.Hidden == nil || j.TenantID == nil {
		return commands.StatusUnknown, nil
	}
	var cm store.GetCommentForActionRow
	var pr prepared
	ok := false
	if err := tenancy.WithTenant(ctx, c.d.DB, *j.TenantID, func(tx pgx.Tx) error {
		var err error
		ok = false
		if cm, err = store.New(tx).GetCommentForAction(ctx, p.MessageID); err != nil {
			return err //nolint:wrapcheck // bọc ở dưới
		}
		pr, ok, err = c.lookupFor(ctx, tx, cm.ChannelAccountID)
		return err
	}); err != nil {
		return commands.StatusUnknown, fmt.Errorf("đọc comment cần tra: %w", err)
	}
	if !ok || !cm.ExternalID.Valid {
		return commands.StatusUnknown, nil
	}
	res, err := pr.adapter.Lookup(ctx, pr.acc, pr.cred, channel.LookupQuery{
		Purpose: channel.PurposeHide, CommentID: cm.ExternalID.String, WantHidden: p.Hidden,
	})
	switch {
	case err != nil:
		return commands.StatusUnknown, err //nolint:wrapcheck // lỗi của adapter đã có việc đang làm
	case res.Status == channel.OutcomeSent:
		return commands.StatusDone, nil
	case res.Status == channel.OutcomeNotSent:
		return commands.StatusNotSent, nil
	}
	return commands.StatusUnknown, nil
}

// OnDone (trong transaction đổi lệnh sang done, cả khi done do tra lại): kênh xin giãn → NoteBlockedUntil (nhóm 3);
// khóa comment (như đường vào: chỉ dòng tin), đã đúng trạng thái muốn (webhook ẩn tới trước) → không ghi; khác → đặt
// is_hidden, ghi lịch sử nguồn staff, báo hiệu hội thoại.
func (c hideCommand) OnDone(ctx context.Context, j queue.Job, raw json.RawMessage, tx pgx.Tx) error {
	var p payload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.Hidden == nil {
		return nil //nolint:nilerr // payload hỏng: không có gì để đổi
	}
	var res result
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &res); err != nil {
			return fmt.Errorf("đọc kết quả lệnh ẩn comment: %w", err)
		}
	}
	q := store.New(tx)
	cm, err := q.GetCommentForAction(ctx, p.MessageID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("đọc comment đã ẩn: %w", err)
	}
	if res.SlowDownMs > 0 {
		until := c.now().UTC().Add(time.Duration(res.SlowDownMs) * time.Millisecond)
		if err := channel.NoteBlockedUntil(ctx, tx, cm.ChannelAccountID, until); err != nil {
			return err //nolint:wrapcheck // channel đã bọc việc đang làm
		}
	}
	row, err := q.LockCommentByID(ctx, p.MessageID)
	if err != nil {
		return fmt.Errorf("khóa comment đã ẩn: %w", err)
	}
	if row.IsHidden == *p.Hidden {
		return nil
	}
	if err := q.InsertRevisionState(ctx, store.InsertRevisionStateParams{
		MessageID: row.ID, Body: row.Body, IsHidden: pgtype.Bool{Bool: *p.Hidden, Valid: true},
		Removed: pgtype.Bool{Bool: row.RemovedAt.Valid, Valid: true}, Source: sourceStaff,
	}); err != nil {
		return fmt.Errorf("ghi lịch sử ẩn comment: %w", err)
	}
	if err := q.UpdateCommentState(ctx, store.UpdateCommentStateParams{
		Body: row.Body, EditedAt: row.EditedAt, RemovedAt: row.RemovedAt, IsHidden: *p.Hidden, ID: row.ID,
	}); err != nil {
		return fmt.Errorf("đặt trạng thái ẩn comment: %w", err)
	}
	return publish(ctx, tx, c.d.Bus, *j.TenantID, row.ChannelAccountID, row.ConversationID)
}

// OnTerminal: lệnh ẩn không có tin queued để đổi (dead, expired, in_doubt chỉ ở trạng thái của việc).
func (hideCommand) OnTerminal(context.Context, queue.Job, string, pgx.Tx) error { return nil }
