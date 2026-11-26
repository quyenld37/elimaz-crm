package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/templates"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Tự trả lời comment, tự nhắn riêng (Task 166; kế hoạch M4-26, 01 mục 6, Q11): comment khách mới → hai lượt Run độc
// lập "reply:<comment_id>" (trả lời công khai, câu xoay vòng) và "pm:<comment_id>" (nhắn riêng, chữ hay mẫu). Trả lời
// công khai ghi ngay trong tx comment vào; tin riêng ghi qua việc chuẩn bị (tx riêng, khóa hộp thư đúng thứ tự ADR-46,
// tệp task-166 Quyết định 2).

// KindPrepareAutoPrivate là việc chuẩn bị tin riêng tự động của một lượt "pm:".
const KindPrepareAutoPrivate = "task.conversation.auto_private_reply"

// Tiền tố external_ref của hai lượt.
const (
	refReply   = "reply:"
	refPrivate = "pm:"
)

// Giới hạn của cấu hình auto_reply_comment (openapi.yaml AutoReplyCommentConfig).
const (
	maxReplies      = 5
	maxReplyRunes   = 500
	maxPrivateRunes = 2000
	maxPosts        = 200
	minPerUserHours = 1
	maxPerUserHours = 72
	prepareAttempts = 5
	prepareExpires  = 5 * time.Minute
	prepareTimeout  = 30 * time.Second
)

// PrivateMessageConfig là tin riêng của auto_reply_comment: mẫu (TemplateID) hay chữ (Text).
type PrivateMessageConfig struct {
	TemplateID *uuid.UUID `json:"templateId,omitempty"`
	Text       string     `json:"text,omitempty"`
}

// ReplyConfig là cấu hình auto_reply_comment. PostIDs rỗng = mọi bài; PrivateMessage nil = không nhắn riêng.
type ReplyConfig struct {
	Kind                string                `json:"kind"`
	Replies             []string              `json:"replies"`
	PrivateMessage      *PrivateMessageConfig `json:"privateMessage,omitempty"`
	PostIDs             []string              `json:"postIds"`
	ExceptPostIDs       []string              `json:"exceptPostIds"`
	IncludeLive         bool                  `json:"includeLive"`
	PerUserPerPostHours int                   `json:"perUserPerPostHours"`
}

// covers cho biết quy tắc áp cho bài post.
func (c ReplyConfig) covers(post string) bool {
	return !slices.Contains(c.ExceptPostIDs, post) && (len(c.PostIDs) == 0 || slices.Contains(c.PostIDs, post))
}

// ReplyIndex là câu thứ mấy trong n câu dùng cho comment commentID: FNV-1a 32 bit của mã comment mod n (cùng comment
// luôn cùng câu, comment khác nhau trải đều).
func ReplyIndex(commentID string, n int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(commentID))         // hash.Hash không bao giờ trả lỗi
	return int(h.Sum32() % uint32(max(n, 1))) //nolint:gosec // n từ 1 tới 5
}

// validReplyConfig kiểm cấu hình auto_reply_comment của Page pageID (ErrInvalidRule): 1–5 câu không rỗng ≤ 500 ký tự,
// tối đa 200 bài mỗi danh sách, 1–72 giờ; tin riêng có mẫu thì mẫu còn và dùng được trên Page, không thì chữ 1–2 000
// ký tự.
func validReplyConfig(ctx context.Context, tx pgx.Tx, pageID uuid.UUID, raw json.RawMessage) error {
	var c ReplyConfig
	if err := json.Unmarshal(raw, &c); err != nil || len(c.Replies) == 0 || len(c.Replies) > maxReplies ||
		len(c.PostIDs) > maxPosts || len(c.ExceptPostIDs) > maxPosts ||
		c.PerUserPerPostHours < minPerUserHours || c.PerUserPerPostHours > maxPerUserHours {
		return ErrInvalidRule
	}
	for _, r := range c.Replies {
		if !validText(r, maxReplyRunes) {
			return ErrInvalidRule
		}
	}
	pm := c.PrivateMessage
	switch {
	case pm == nil:
		return nil
	case pm.TemplateID == nil:
		if !validText(pm.Text, maxPrivateRunes) {
			return ErrInvalidRule
		}
		return nil
	}
	ok, err := templates.UsableOnPage(ctx, tx, *pm.TemplateID, pageID)
	if err != nil {
		return err //nolint:wrapcheck // templates đã bọc việc đang làm
	}
	if !ok {
		return ErrInvalidRule
	}
	return nil
}

func validText(s string, limit int) bool {
	return strings.TrimSpace(s) != "" && utf8.RuneCountInString(s) <= limit
}

// autoReply áp quy tắc auto_reply_comment của Page cho comment msgID: hai lượt độc lập (trả lời công khai, nhắn riêng).
func autoReply(ctx context.Context, tx pgx.Tx, d deps.Deps, msgID uuid.UUID, t Trigger) error {
	rule, err := LoadRule(ctx, tx, t.Account.ID, KindAutoReplyComment)
	if err != nil || !rule.Enabled {
		return err
	}
	var c ReplyConfig
	if json.Unmarshal(rule.Config, &c) != nil || len(c.Replies) == 0 || !c.covers(t.PostExternalID) {
		return nil // cấu hình đã kiểm khi lưu; đọc lỗi coi như tắt
	}
	// SubjectRef là hội thoại comment: một hội thoại mỗi (bài, người comment), không chứa PSID (Quyết định 1).
	t.SubjectRef = t.ConversationID.String()
	env := send.Env{Registry: d.Registry, Queue: d.Queue, Bus: d.Bus}
	now := d.Clock()()
	pub := t
	pub.ExternalRef = refReply + t.CommentExternalID
	if _, err := Run(ctx, tx, d, rule, pub, func(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
		if err := c.limit(ctx, tx, rule.ID, pub, refReply); err != nil {
			return uuid.Nil, err
		}
		text := c.Replies[ReplyIndex(t.CommentExternalID, len(c.Replies))]
		return skipRefusal(send.QueueAutoCommentReply(ctx, tx, env, msgID, text, now))
	}); err != nil {
		return err
	}
	if c.PrivateMessage == nil {
		return nil
	}
	pm := t
	pm.ExternalRef = refPrivate + t.CommentExternalID
	_, err = Run(ctx, tx, d, rule, pm, func(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
		if err := c.limit(ctx, tx, rule.ID, pm, refPrivate); err != nil {
			return uuid.Nil, err
		}
		ok, err := send.AutoPrivateAllowed(ctx, tx, d.Registry, msgID, now)
		switch {
		case err != nil:
			return uuid.Nil, err //nolint:wrapcheck // send đã bọc việc đang làm
		case !ok:
			return uuid.Nil, Skip(OutcomeSkippedPolicy)
		}
		id, _, err := d.Queue.Enqueue(ctx, tx, prepareKind(), queue.NewJob{
			TenantID: &t.Account.TenantID, Payload: preparePayload{MessageID: msgID}, DedupeKey: "auto_pm:" + msgID.String(),
		})
		if err != nil {
			return uuid.Nil, err //nolint:wrapcheck // queue đã bọc
		}
		return id, nil
	})
	return err
}

// limit là cổng riêng của loại: comment live khi không bật includeLive → skipped_live; đã có lượt enqueued|done cùng
// loại (prefix), cùng người cùng bài trong perUserPerPostHours giờ → skipped_limit.
func (c ReplyConfig) limit(ctx context.Context, tx pgx.Tx, ruleID uuid.UUID, t Trigger, prefix string) error {
	if t.IsLive && !c.IncludeLive {
		return Skip(OutcomeSkippedLive)
	}
	n, err := store.New(tx).CountRecentAutomationRuns(ctx, store.CountRecentAutomationRunsParams{
		RuleID: ruleID, SubjectRef: t.SubjectRef, Prefix: prefix, ExternalRef: t.ExternalRef,
		Hours: int32(c.PerUserPerPostHours), //nolint:gosec // 1–72, đã kiểm khi lưu
	})
	if err != nil {
		return fmt.Errorf("đếm lượt tự trả lời gần đây: %w", err)
	}
	if n > 0 {
		return Skip(OutcomeSkippedLimit)
	}
	return nil
}

// skipRefusal đổi lỗi từ chối của send sang kết quả bỏ qua: Page chỉ xem → skipped_read_only; kênh không cho, không
// phải comment của khách → skipped_policy.
func skipRefusal(id uuid.UUID, err error) (uuid.UUID, error) {
	switch {
	case errors.Is(err, send.ErrReadOnly):
		return uuid.Nil, Skip(OutcomeSkippedReadOnly)
	case errors.Is(err, send.ErrAutoNotAllowed), errors.Is(err, send.ErrNotCustomerComment):
		return uuid.Nil, Skip(OutcomeSkippedPolicy)
	case err != nil:
		return uuid.Nil, err
	}
	return id, nil
}

// preparePayload là payload của việc chuẩn bị: chỉ mã comment (không nội dung).
type preparePayload struct {
	MessageID uuid.UUID `json:"messageId"`
}

func prepareKind() queue.Kind {
	p := queue.DefaultPolicy
	p.MaxAttempts, p.ExpiresAfter, p.Timeout = prepareAttempts, prepareExpires, prepareTimeout
	return queue.Kind{Name: KindPrepareAutoPrivate, Policy: p}
}

// prepareHandler là việc chuẩn bị tin riêng tự động: trong tx của việc, lượt "pm:" đang gắn mã việc này → điền nội
// dung (chữ, hay mẫu qua templates.Render, Actor nil: {nhan_vien} là tên Page) → send.QueueAutoPrivateReply (khóa hai
// hội thoại theo id tăng dần, kiểm lại đã có tin riêng) → gắn mã lệnh gửi vào lượt; bị từ chối → lượt skipped_*;
// quy tắc vừa tắt hay bỏ tin riêng → dropped; mẫu không điền được → failed.
type prepareHandler struct{ d deps.Deps }

func (prepareHandler) Kind() queue.Kind { return prepareKind() }

// Handle chạy việc chuẩn bị j (xem prepareHandler).
func (h prepareHandler) Handle(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	var p preparePayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.MessageID == uuid.Nil {
		return nil, fmt.Errorf("%w: payload việc chuẩn bị tin riêng tự động", queue.ErrPermanent)
	}
	q := store.New(tx)
	run, err := q.GetAutomationRunByJob(ctx, pgtype.UUID{Bytes: j.ID, Valid: true})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil // lượt đã đổi lệnh (chạy lại sau khi đã ghi)
	case err != nil:
		return nil, fmt.Errorf("đọc lượt tin riêng tự động: %w", err)
	case run.Outcome != string(OutcomeEnqueued):
		return nil, nil
	}
	out, jobID, err := h.queue(ctx, tx, p.MessageID)
	if err != nil {
		return nil, err
	}
	if err := q.SetAutomationRunJob(ctx, store.SetAutomationRunJobParams{
		CommandJobID: pgtype.UUID{Bytes: jobID, Valid: jobID != uuid.Nil}, Outcome: string(out), ID: run.ID,
	}); err != nil {
		return nil, fmt.Errorf("gắn lệnh tin riêng vào lượt: %w", err)
	}
	return nil, nil
}

// queue điền nội dung và ghi tin riêng của comment msgID; trả kết quả lượt và mã lệnh (enqueued) hay kết quả bỏ.
func (h prepareHandler) queue(ctx context.Context, tx pgx.Tx, msgID uuid.UUID) (Outcome, uuid.UUID, error) {
	cm, err := store.New(tx).GetCommentForAction(ctx, msgID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return OutcomeSkippedPolicy, uuid.Nil, nil
	case err != nil:
		return OutcomeNone, uuid.Nil, fmt.Errorf("đọc comment của tin riêng tự động: %w", err)
	}
	rule, err := LoadRule(ctx, tx, cm.ChannelAccountID, KindAutoReplyComment)
	if err != nil {
		return OutcomeNone, uuid.Nil, err
	}
	var c ReplyConfig
	if !rule.Enabled || json.Unmarshal(rule.Config, &c) != nil || c.PrivateMessage == nil {
		return OutcomeDropped, uuid.Nil, nil
	}
	text := c.PrivateMessage.Text
	if id := c.PrivateMessage.TemplateID; id != nil {
		r, err := templates.Render(ctx, tx, *id, templates.RenderInput{ConversationID: cm.ConversationID})
		var missing *templates.VariableMissingError
		switch {
		case errors.Is(err, templates.ErrNotFound), errors.As(err, &missing):
			return OutcomeFailed, uuid.Nil, nil
		case err != nil:
			return OutcomeNone, uuid.Nil, err //nolint:wrapcheck // templates đã bọc việc đang làm
		}
		text = r.Text
	}
	if !validText(text, maxPrivateRunes) {
		return OutcomeFailed, uuid.Nil, nil
	}
	env := send.Env{Registry: h.d.Registry, Queue: h.d.Queue, Bus: h.d.Bus}
	jobID, err := skipRefusal(send.QueueAutoPrivateReply(ctx, tx, env, msgID, text, h.d.Clock()()))
	if se, ok := errors.AsType[*skipError](err); ok {
		return se.outcome, uuid.Nil, nil
	}
	if err != nil {
		return OutcomeNone, uuid.Nil, err
	}
	return OutcomeEnqueued, jobID, nil
}

// OnTerminal: việc chuẩn bị chết (hết lượt) → lượt failed; quá hạn → dropped.
func (prepareHandler) OnTerminal(ctx context.Context, j queue.Job, status string, tx pgx.Tx) error {
	return finish(ctx, tx, j.ID, terminalOutcome(status))
}

// terminalOutcome đổi trạng thái cuối của lệnh/việc tự động sang kết quả lượt: done → done; dead → failed; in_doubt,
// expired → dropped (04 mục 5.3: tin tự động không rõ thì bỏ, không gửi lại).
func terminalOutcome(status string) Outcome {
	switch status {
	case commands.StatusDone:
		return OutcomeDone
	case queue.StatusDead:
		return OutcomeFailed
	}
	return OutcomeDropped
}

// finishAuto là send.AutoTerminalFunc của hai lệnh tự động.
func finishAuto(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, status string) error {
	return finish(ctx, tx, jobID, terminalOutcome(status))
}
