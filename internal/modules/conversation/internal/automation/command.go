package automation

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
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// KindHideCommand là lệnh ẩn comment do quy tắc auto_hide ghi (tệp task-165, Quyết định 1: tách khỏi lệnh ẩn tay
// command.fb.hide_comment của Task 158 vì kết quả ghi vào automation_runs).
const KindHideCommand = "command.fb.auto_hide_comment"

// Hạn của lệnh ẩn: còn chờ quá 1 giờ thì expired (dropped); mỗi lần Execute tối đa 30 giây.
const (
	hideExpiresAfter = time.Hour
	hideTimeout      = 30 * time.Second
	minBucketWait    = 50 * time.Millisecond
	sourceAutomation = "automation"
)

// Mã lý do của lệnh ẩn (last_error của việc, không có nội dung).
const (
	reasonBadPayload  = "bad_payload"
	reasonGone        = "comment_gone"
	reasonNoAdapter   = "no_adapter"
	reasonPrepare     = "prepare_failed"
	reasonBucket      = "send_bucket"
	reasonBucketEmpty = "send_bucket_empty"
	reasonRejected    = "rejected"
	reasonInDoubt     = "in_doubt"
	reasonNotSent     = "not_sent"
)

// HideCommandKind là Kind của lệnh ẩn tự động.
func HideCommandKind() queue.Kind {
	return commands.CommandKind(KindHideCommand, hideExpiresAfter, hideTimeout)
}

// hideCommand là lệnh command.fb.auto_hide_comment: Execute (ngoài transaction), OnDone, OnTerminal. Không có Looker:
// không rõ đã ẩn chưa thì in_doubt → lượt dropped (04 mục 5.3: tin tự động không rõ thì bỏ, không gửi lại).
type hideCommand struct {
	d   deps.Deps
	now func() time.Time
}

func (hideCommand) Kind() queue.Kind { return HideCommandKind() }

// AutoRetryNotSent: chắc chắn chưa tới kênh thì tự thử lại trong hạn (ẩn hai lần không hại).
func (hideCommand) AutoRetryNotSent() bool { return true }

// hidePrep là thứ cần để gọi kênh, đọc trong transaction chỉ đọc.
type hidePrep struct {
	acc     channel.ChannelAccount
	adapter channel.MessagingAdapter
	cred    channel.Credentials
	comment string
	already bool
}

// Execute ẩn comment của lệnh j: đọc comment (đã xóa → rejected; đã ẩn → done không gọi kênh), hỏi SendPolicy (Page
// chỉ xem lúc này → rejected), giải mã token, lấy lượt gửi của Page (hết → not_sent chờ nạp), rồi gọi kênh. Lỗi của
// CRM trước khi gọi kênh → not_sent.
func (c hideCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	prep, stop := c.ready(ctx, j)
	if stop != nil {
		return *stop, nil
	}
	hidden := true
	// Lỗi của kênh không lên last_error (có thể mang nội dung); kết quả đã đủ để phân loại.
	out, _ := prep.adapter.Send(ctx, prep.acc, prep.cred, channel.OutboundCommand{
		Purpose: channel.PurposeHide, CommentID: prep.comment, Hidden: &hidden, Mode: channel.SendCommentReply,
	})
	return hideOutcome(out), nil
}

// ready chuẩn bị lần gọi kênh của lệnh j; stop khác nil: kết quả trả ngay, không gọi kênh.
func (c hideCommand) ready(ctx context.Context, j queue.Job) (hidePrep, *commands.Outcome) {
	stopWith := func(status, reason string) (hidePrep, *commands.Outcome) {
		return hidePrep{}, &commands.Outcome{Status: status, Reason: reason}
	}
	var p hidePayload
	if json.Unmarshal(j.Payload, &p) != nil || p.MessageID == uuid.Nil || j.TenantID == nil {
		return stopWith(commands.StatusRejected, reasonBadPayload)
	}
	tenant, now := *j.TenantID, c.now().UTC()
	prep, refusal, err := c.read(ctx, tenant, p.MessageID, now)
	if err != nil {
		return stopWith(commands.StatusNotSent, reasonPrepare)
	}
	if refusal != "" {
		return stopWith(commands.StatusRejected, refusal)
	}
	if prep.already {
		return stopWith(commands.StatusDone, "")
	}
	return prep, c.takeToken(ctx, tenant, prep.acc.ID, now)
}

// read chạy prepare trong transaction tenant chỉ đọc (kết quả gán lại mỗi lần chạy).
func (c hideCommand) read(ctx context.Context, tenant, msgID uuid.UUID, now time.Time) (prep hidePrep, refusal string, err error) {
	err = tenancy.WithTenant(ctx, c.d.DB, tenant, func(tx pgx.Tx) error {
		var e error
		prep, refusal, e = c.prepare(ctx, tx, msgID, now)
		return e
	})
	return prep, refusal, err //nolint:wrapcheck // prepare đã bọc
}

// takeToken lấy một lượt gửi của Page accountID; nil khi lấy được, ngược lại not_sent (thùng hết: chờ nạp, không
// tiêu lượt của lệnh).
func (c hideCommand) takeToken(ctx context.Context, tenant, accountID uuid.UUID, now time.Time) *commands.Outcome {
	ok, wait, err := channel.AcquireSendToken(ctx, c.d.DB, tenant, accountID, now)
	if err != nil {
		return &commands.Outcome{Status: commands.StatusNotSent, Reason: reasonBucket}
	}
	if ok {
		return nil
	}
	return &commands.Outcome{Status: commands.StatusNotSent, Reason: reasonBucketEmpty, RetryAfter: max(wait, minBucketWait), KeepAttempt: true}
}

// hideOutcome đổi kết quả của kênh sang kết quả lệnh.
func hideOutcome(out channel.SendOutcome) commands.Outcome {
	switch out.Status {
	case channel.OutcomeSent:
		return commands.Outcome{Status: commands.StatusDone}
	case channel.OutcomeRejected:
		reason := string(out.Reason)
		if reason == "" {
			reason = reasonRejected
		}
		return commands.Outcome{Status: commands.StatusRejected, Reason: reason}
	case channel.OutcomeNotSent:
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonNotSent, RetryAfter: out.RetryAfter}
	case channel.OutcomeUnknown:
	}
	return commands.Outcome{Status: commands.StatusUnknown, Reason: reasonInDoubt}
}

// prepare đọc comment msgID và mọi thứ cần gọi kênh. refusal khác rỗng: không ẩn (mã lý do). err: lỗi tạm database.
func (c hideCommand) prepare(ctx context.Context, tx pgx.Tx, msgID uuid.UUID, now time.Time) (hidePrep, string, error) {
	m, err := store.New(tx).GetCommentForHide(ctx, msgID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return hidePrep{}, reasonGone, nil
	case err != nil:
		return hidePrep{}, "", fmt.Errorf("đọc comment cần ẩn: %w", err)
	case m.IsHidden:
		return hidePrep{already: true}, "", nil
	}
	acc, err := channel.Account(ctx, tx, m.ChannelAccountID)
	if err != nil {
		return hidePrep{}, "", err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	var ad channel.MessagingAdapter
	if c.d.Registry != nil {
		ad, _ = c.d.Registry.Get(acc.Channel)
	}
	if ad == nil {
		return hidePrep{}, reasonNoAdapter, nil
	}
	pol := ad.SendPolicy(channel.ConversationState{
		Purpose: channel.PurposeHide, Kind: "comment", CommentRemoved: m.RemovedAt.Valid,
		ReadOnly: acc.SendMode != channel.SendModeEnabled, Automated: true,
	}, now)
	switch {
	case m.RemovedAt.Valid:
		return hidePrep{}, string(channel.DenyCommentRemoved), nil
	case acc.SendMode != channel.SendModeEnabled:
		return hidePrep{}, string(channel.DenyReadOnly), nil
	case !pol.Allowed:
		return hidePrep{}, string(pol.Reason), nil
	}
	cred, err := channel.CredentialsFor(ctx, tx, c.d.Keys, acc.ID)
	if err != nil {
		return hidePrep{}, string(channel.DenyNoPermission), nil //nolint:nilerr // không có hay không giải được token: không ẩn được
	}
	return hidePrep{acc: acc, adapter: ad, cred: cred, comment: m.ExternalID.String}, "", nil
}

// OnDone ghi comment đã ẩn (trong tx của Runner, khóa hội thoại rồi tin, ADR-46 nhóm 6): is_hidden, một dòng lịch sử
// nguồn automation (khi chưa ẩn), báo hiệu hội thoại đổi; lượt chạy → done.
func (c hideCommand) OnDone(ctx context.Context, j queue.Job, _ json.RawMessage, tx pgx.Tx) error {
	var p hidePayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || j.TenantID == nil {
		return fmt.Errorf("giải payload lệnh ẩn tự động: %w", queue.ErrPermanent)
	}
	q := store.New(tx)
	m, err := q.GetCommentForHide(ctx, p.MessageID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return finish(ctx, tx, j.ID, OutcomeDone)
	case err != nil:
		return fmt.Errorf("đọc comment vừa ẩn: %w", err)
	}
	if err := q.LockConversation(ctx, m.ConversationID); err != nil {
		return fmt.Errorf("khóa hội thoại của comment vừa ẩn: %w", err)
	}
	row, err := q.LockCommentByID(ctx, p.MessageID)
	if err != nil {
		return fmt.Errorf("khóa comment vừa ẩn: %w", err)
	}
	if !row.IsHidden {
		if err := q.InsertRevisionState(ctx, store.InsertRevisionStateParams{
			MessageID: row.ID, Body: row.Body, IsHidden: pgtype.Bool{Bool: true, Valid: true},
			Removed: pgtype.Bool{Bool: row.RemovedAt.Valid, Valid: true}, Source: sourceAutomation,
		}); err != nil {
			return fmt.Errorf("ghi lịch sử comment vừa ẩn: %w", err)
		}
		if err := q.UpdateCommentState(ctx, store.UpdateCommentStateParams{
			Body: row.Body, EditedAt: row.EditedAt, RemovedAt: row.RemovedAt, IsHidden: true, ID: row.ID,
		}); err != nil {
			return fmt.Errorf("đánh dấu comment đã ẩn: %w", err)
		}
		if err := access.Signal(ctx, tx, c.d.Bus, *j.TenantID, deps.TopicConversationChanged, m.ChannelAccountID,
			deps.EntityConversation, m.ConversationID); err != nil {
			return err //nolint:wrapcheck // access đã bọc
		}
	}
	return finish(ctx, tx, j.ID, OutcomeDone)
}

// OnTerminal ghi kết quả cuối của lượt: dead (từ chối, hết lượt) → failed; in_doubt, expired → dropped (không gửi lại).
func (hideCommand) OnTerminal(ctx context.Context, j queue.Job, status string, tx pgx.Tx) error {
	out := OutcomeDropped
	if status == queue.StatusDead {
		out = OutcomeFailed
	}
	return finish(ctx, tx, j.ID, out)
}

// finish ghi kết quả out vào lượt chạy của lệnh jobID.
func finish(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, out Outcome) error {
	if _, err := store.New(tx).FinishAutomationRun(ctx, store.FinishAutomationRunParams{
		Outcome: string(out), CommandJobID: pgtype.UUID{Bytes: jobID, Valid: true},
	}); err != nil {
		return fmt.Errorf("ghi kết quả lượt quy tắc tự động: %w", err)
	}
	return nil
}
