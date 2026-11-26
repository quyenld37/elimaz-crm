package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Đối chiếu cuối phiên (02 mục 2, LV02): phiên chuyển ended (webhook, hỏi định kỳ, bấm tay) ghi lệnh
// command.fb.live_reconcile (chỉ đọc, một lệnh mỗi phiên). Execute đọc Page, token trong transaction riêng, hỏi kênh
// mọi comment của phiên (hết các trang) và số Meta đếm ngoài transaction, rồi trong một transaction ngắn ghi comment
// còn thiếu thành việc vào (trùng thì bỏ) và đếm comment CRM có (tin is_live của phiên ∪ comment vừa đọc, vì việc vừa
// ghi chưa thành tin). OnDone ghi meta_comment_count, reconciled_at; thiếu → cảnh báo live-missing kèm hai con số.
const (
	ReconcileCommandName = "command.fb.live_reconcile"
	reconcileTimeout     = 2 * time.Minute
)

type reconcilePayload struct {
	SessionID uuid.UUID `json:"sessionId"`
}

// reconcileResult là kết quả đối chiếu: chỉ hai con số (không nội dung comment).
type reconcileResult struct {
	Got  int `json:"got"`
	Meta int `json:"meta"`
}

func reconcileKind() queue.Kind {
	return commands.CommandKind(ReconcileCommandName, 0, reconcileTimeout)
}

// enqueueReconcile ghi lệnh đối chiếu của phiên s vừa kết thúc trong tx (một lệnh mỗi phiên).
func enqueueReconcile(ctx context.Context, tx pgx.Tx, s Session) error {
	tenant := s.TenantID
	if _, _, err := queue.NewQueue().Enqueue(ctx, tx, reconcileKind(), queue.NewJob{
		TenantID: &tenant, Payload: reconcilePayload{SessionID: s.ID}, DedupeKey: "live-reconcile:" + s.ID.String(),
	}); err != nil {
		return fmt.Errorf("ghi lệnh đối chiếu phiên live: %w", err)
	}
	return nil
}

// ReconcileCommand là lệnh command.fb.live_reconcile.
type ReconcileCommand struct{ d deps.Deps }

// NewReconcileCommand dựng lệnh đối chiếu cuối phiên.
func NewReconcileCommand(d deps.Deps) *ReconcileCommand { return &ReconcileCommand{d: d} }

// Kind là loại lệnh (không hết hạn; hạn một lần chạy 2 phút, worker chặn trong nửa thời gian giữ chỗ).
func (*ReconcileCommand) Kind() queue.Kind { return reconcileKind() }

// AutoRetryNotSent trả true: lệnh chỉ đọc kênh, ghi việc vào chống trùng, làm lại không hại.
func (*ReconcileCommand) AutoRetryNotSent() bool { return true }

// Lookup luôn trả not_sent (lệnh chỉ đọc).
func (*ReconcileCommand) Lookup(context.Context, queue.Job) (string, error) {
	return commands.StatusNotSent, nil
}

// Execute đối chiếu phiên của lệnh j. Phiên không có hay kênh không đọc được live → rejected; database hay kênh lỗi →
// not_sent (tự chạy lại).
func (c *ReconcileCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	var p reconcilePayload
	if j.TenantID == nil || json.Unmarshal(j.Payload, &p) != nil || p.SessionID == uuid.Nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil
	}
	tenant := *j.TenantID
	var s Session
	var t sourceTarget
	err := tenancy.WithTenant(ctx, c.d.DB, tenant, func(tx pgx.Tx) error {
		r, err := store.New(tx).GetLiveSession(ctx, p.SessionID)
		if err != nil {
			return fmt.Errorf("đọc phiên live: %w", err)
		}
		s = sessionFrom(r)
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return commands.Outcome{Status: commands.StatusRejected, Reason: "session_missing"}, nil
	}
	if err == nil {
		t, err = readTarget(ctx, c.d, s)
	}
	switch {
	case errors.Is(err, errNoLiveSource), errors.Is(err, channel.ErrAccountNotFound):
		return commands.Outcome{Status: commands.StatusRejected, Reason: "no_live_source"}, nil
	case err != nil:
		return commands.Outcome{Status: commands.StatusNotSent, Reason: "read_session"}, nil
	}
	// Đọc hết comment của video (không chặn bằng started_at: phiên theo dõi tay bắt đầu lúc bấm, Meta vẫn đếm comment
	// trước đó).
	evs, err := t.src.PollComments(ctx, t.acc, t.cred, s.LiveVideoID, time.Time{})
	if err != nil {
		return commands.Outcome{Status: commands.StatusNotSent, Reason: "poll_comments"}, nil
	}
	meta, err := t.src.CountComments(ctx, t.acc, t.cred, s.LiveVideoID)
	if err != nil {
		return commands.Outcome{Status: commands.StatusNotSent, Reason: "count_comments"}, nil
	}
	got, err := c.store(ctx, s, t.acc.Channel, evs)
	if err != nil {
		return commands.Outcome{Status: commands.StatusNotSent, Reason: "write_comments"}, nil
	}
	body, err := json.Marshal(reconcileResult{Got: got, Meta: meta})
	if err != nil {
		return commands.Outcome{}, fmt.Errorf("mã hóa kết quả đối chiếu live: %w", err)
	}
	return commands.Outcome{Status: commands.StatusDone, Result: body}, nil
}

// store ghi evs thành việc vào (trùng thì bỏ) và trả số comment khác nhau CRM có của phiên s: mã comment của tin
// is_live trên bài của phiên ∪ mã comment trong evs.
func (c *ReconcileCommand) store(ctx context.Context, s Session, source string, evs []webhooks.InboundEvent) (int, error) {
	var got int
	err := tenancy.WithTenant(ctx, c.d.DB, s.TenantID, func(tx pgx.Tx) error {
		got = 0
		received := clock(c.d)
		ids := map[string]struct{}{}
		for _, e := range evs {
			if _, err := webhooks.EnqueueInbound(ctx, tx, c.d.Queue, s.TenantID, source, e, received, webhooks.ViaLivePoll); err != nil {
				return err //nolint:wrapcheck // webhooks đã bọc việc đang làm
			}
			var cp channel.CommentPayload
			if json.Unmarshal(e.Payload, &cp) == nil && cp.CommentID != "" {
				ids[cp.CommentID] = struct{}{}
			}
		}
		posts := []string{s.LiveVideoID}
		for _, v := range []string{s.VideoID, s.PostExternalID} {
			if v != "" {
				posts = append(posts, v)
			}
		}
		have, err := store.New(tx).LiveCommentIDsOfSession(ctx, store.LiveCommentIDsOfSessionParams{
			ChannelAccountID: s.AccountID, Posts: posts,
		})
		if err != nil {
			return fmt.Errorf("đọc comment live đã có của phiên: %w", err)
		}
		for _, id := range have {
			ids[id] = struct{}{}
		}
		got = len(ids)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("ghi comment đối chiếu live: %w", err)
	}
	return got, nil
}

// OnDone ghi kết quả trong transaction của Runner: meta_comment_count, reconciled_at; CRM có ít hơn số Meta đếm →
// cảnh báo live-missing (một lần mỗi lệnh).
func (c *ReconcileCommand) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error {
	var p reconcilePayload
	var res reconcileResult
	if json.Unmarshal(j.Payload, &p) != nil || json.Unmarshal(result, &res) != nil || j.TenantID == nil {
		return fmt.Errorf("giải kết quả đối chiếu live: %w", queue.ErrPermanent)
	}
	if err := store.New(tx).SetLiveReconciled(ctx, store.SetLiveReconciledParams{
		MetaCommentCount: int32(min(res.Meta, 1<<31-1)), ReconciledAt: tsOf(clock(c.d)), ID: p.SessionID, //nolint:gosec // đã chặn
	}); err != nil {
		return fmt.Errorf("ghi kết quả đối chiếu live: %w", err)
	}
	if res.Got >= res.Meta {
		return nil
	}
	return enqueueAlert(ctx, tx, c.d.Queue, *j.TenantID,
		alertPayload{Kind: alertMissing, SessionID: p.SessionID, Got: res.Got, Meta: res.Meta},
		"live-missing:"+p.SessionID.String()+":"+j.ID.String())
}
