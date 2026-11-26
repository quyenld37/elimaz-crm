package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/templates"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// Nhắn lại khách chưa trả lời (kế hoạch M4, Task 168; CH16, 01 mục 6, Q11). Ứng viên: hội thoại hộp thư đang mở hay
// chờ, tin người thật cuối của Page (nhân viên trong CRM hay trên Meta; không tính tin tự động) mới hơn tin khách cuối
// và đã im quá afterHours, cửa sổ 24 giờ còn ít nhất 5 phút, khách không chặn Page, không do_not_contact. Danh sách cho
// nhân viên nhắn một chạm; quy tắc follow_up (tắt mặc định) tự gửi một bước: mỗi tin người thật tối đa một lần.

// FollowUpSchedule là lịch theo tenant chạy quy tắc nhắn lại.
const FollowUpSchedule = "conversation.follow_up"

// TriggerFollowUpDue là Trigger.Kind của hội thoại tới hạn nhắn lại.
const TriggerFollowUpDue = "follow_up_due"

const (
	followUpEvery    = 300 * time.Second
	windowLength     = 24 * time.Hour
	windowMargin     = 5 * time.Minute
	minAfterHours    = 1
	maxAfterHours    = 23
	defaultFollowUps = 50
	maxFollowUps     = 100
	// followUpBatch: số hội thoại tối đa mỗi Page mỗi lượt lịch (lượt sau làm tiếp).
	followUpBatch = 100
)

// ErrInvalidFollowUp là lỗi: afterHours ngoài 1–23 (gói gốc đổi thành ErrInvalidInput).
var ErrInvalidFollowUp = errors.New("conversation: tham số nhắn lại không hợp lệ")

// FollowUpConfig là cấu hình follow_up.
type FollowUpConfig struct {
	Kind       string    `json:"kind"`
	AfterHours int       `json:"afterHours"`
	TemplateID uuid.UUID `json:"templateId"`
}

// FollowUpItem là một hội thoại cần nhắn lại. CustomerName là dữ liệu cá nhân.
type FollowUpItem struct {
	ConversationID, PageID, CustomerID uuid.UUID
	CustomerName                       string
	LastStaffMessageAt, WindowEndsAt   time.Time
}

// ListFollowUps trả tối đa limit (≤ 0 → 50, tối đa 100) hội thoại cần nhắn lại lúc now trên các Page u được đọc
// (pageID khác nil: chỉ Page đó; không được đọc → rỗng), im quá afterHours (1–23, không → ErrInvalidFollowUp); cửa sổ
// đóng sớm trước.
func ListFollowUps(ctx context.Context, tx pgx.Tx, u identity.AuthUser, pageID *uuid.UUID, afterHours, limit int,
	now time.Time,
) ([]FollowUpItem, error) {
	if afterHours < minAfterHours || afterHours > maxAfterHours {
		return nil, ErrInvalidFollowUp
	}
	if limit <= 0 {
		limit = defaultFollowUps
	}
	all, pages := access.VisiblePages(u)
	if pageID != nil {
		if !all && !slices.Contains(pages, *pageID) {
			return []FollowUpItem{}, nil
		}
		all, pages = false, []uuid.UUID{*pageID}
	}
	if !all && len(pages) == 0 {
		return []FollowUpItem{}, nil
	}
	rows, err := candidates(ctx, tx, all, pages, uuid.Nil, afterHours, min(limit, maxFollowUps), now)
	if err != nil {
		return nil, err
	}
	out := make([]FollowUpItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, FollowUpItem{
			ConversationID: r.ID, PageID: r.ChannelAccountID, CustomerID: r.CustomerID, CustomerName: r.CustomerName,
			LastStaffMessageAt: r.StaffMessageAt.UTC(), WindowEndsAt: r.LastCustomerMessageAt.Time.UTC().Add(windowLength),
		})
	}
	return out, nil
}

// candidates đọc ứng viên nhắn lại lúc now (không khóa); ruleID khác uuid.Nil: bỏ hội thoại mà tin nhân viên cuối đã có
// lượt của quy tắc đó (lịch).
func candidates(ctx context.Context, tx pgx.Tx, all bool, pages []uuid.UUID, ruleID uuid.UUID, afterHours, limit int,
	now time.Time,
) ([]store.ListFollowUpCandidatesRow, error) {
	now = now.UTC()
	rows, err := store.New(tx).ListFollowUpCandidates(ctx, store.ListFollowUpCandidatesParams{
		AllPages: all, Pages: pages, RuleID: pgtype.UUID{Bytes: ruleID, Valid: ruleID != uuid.Nil},
		WindowAfter:  now.Add(-windowLength + windowMargin),
		SilentBefore: now.Add(-time.Duration(afterHours) * time.Hour), Lim: int32(limit), //nolint:gosec // ≤ 100
	})
	if err != nil {
		return nil, fmt.Errorf("đọc hội thoại cần nhắn lại: %w", err)
	}
	return rows, nil
}

// dueFollowUp là một ứng viên của lịch kèm quy tắc, cấu hình và Page của nó.
type dueFollowUp struct {
	row  store.ListFollowUpCandidatesRow
	rule Rule
	cfg  FollowUpConfig
	acc  channel.ChannelAccount
}

// RunFollowUps chạy quy tắc follow_up đang bật của mọi Page của tenant lúc now, trong tx tenant của bên gọi (việc
// lịch): đọc ứng viên chưa có lượt của mỗi Page (tối đa 100; lượt sau làm tiếp), khóa hội thoại theo mã tăng dần (ADR-46 nhóm 6), bỏ hội thoại khách
// vừa nhắn sau tin nhân viên, rồi Run với ExternalRef "fu:<hội thoại>:<tin nhân viên cuối>" (một bước: mỗi tin nhân
// viên tối đa một lượt) → mẫu điền biến, tin tự động command.fb.auto_send. Mẫu không dùng được, kênh không cho gửi →
// skipped_policy. Trả số lượt đã ghi lệnh.
func RunFollowUps(ctx context.Context, tx pgx.Tx, d deps.Deps, now time.Time) (int, error) {
	due, err := dueFollowUps(ctx, tx, now)
	if err != nil {
		return 0, err
	}
	q, sent := store.New(tx), 0
	for _, f := range due {
		locked, err := q.LockConversationForSend(ctx, f.row.ID)
		if err != nil {
			return 0, fmt.Errorf("khóa hội thoại cần nhắn lại: %w", err)
		}
		if locked.LastCustomerMessageAt.Valid && !f.row.StaffMessageAt.After(locked.LastCustomerMessageAt.Time) {
			continue // khách vừa nhắn sau khi đọc ứng viên
		}
		t := Trigger{
			Kind: TriggerFollowUpDue, Account: f.acc, ConversationID: f.row.ID, CustomerID: f.row.CustomerID,
			ExternalRef: "fu:" + f.row.ID.String() + ":" + f.row.StaffMessageID.String(), SubjectRef: f.row.ID.String(),
			OccurredAt: now,
		}
		out, err := Run(ctx, tx, d, f.rule, t, func(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
			return sendFollowUp(ctx, tx, d, f.cfg.TemplateID, f.row.ID, now)
		})
		if err != nil {
			return 0, err
		}
		if out == OutcomeEnqueued {
			sent++
		}
	}
	return sent, nil
}

// dueFollowUps trả ứng viên của mọi quy tắc follow_up đang bật, sắp theo mã hội thoại tăng dần (thứ tự khóa).
func dueFollowUps(ctx context.Context, tx pgx.Tx, now time.Time) ([]dueFollowUp, error) {
	rules, err := store.New(tx).ListEnabledRulesOfKind(ctx, KindFollowUp)
	if err != nil {
		return nil, fmt.Errorf("đọc quy tắc nhắn lại đang bật: %w", err)
	}
	var due []dueFollowUp
	for _, r := range rules {
		rule := ruleFrom(store.GetAutomationRuleRow(r))
		var cfg FollowUpConfig
		if json.Unmarshal(rule.Config, &cfg) != nil || cfg.AfterHours < minAfterHours || cfg.AfterHours > maxAfterHours {
			continue // cấu hình đã kiểm khi lưu; đọc lỗi coi như tắt
		}
		acc, err := channel.Account(ctx, tx, rule.AccountID)
		if err != nil {
			return nil, err //nolint:wrapcheck // channel đã bọc việc đang làm
		}
		rows, err := candidates(ctx, tx, false, []uuid.UUID{rule.AccountID}, rule.ID, cfg.AfterHours, followUpBatch, now)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			due = append(due, dueFollowUp{row: row, rule: rule, cfg: cfg, acc: acc})
		}
	}
	slices.SortFunc(due, func(a, b dueFollowUp) int { return slices.Compare(a.row.ID[:], b.row.ID[:]) })
	return due, nil
}

// sendFollowUp điền mẫu templateID cho hội thoại conv (tin tự động: {nhan_vien} là tên Page) và ghi tin tự động; mẫu
// không có, đã xóa, của Page khác, thiếu biến, chữ rỗng; Page chỉ xem, kênh không cho gửi → Skip(skipped_policy).
func sendFollowUp(ctx context.Context, tx pgx.Tx, d deps.Deps, templateID, conv uuid.UUID, now time.Time) (uuid.UUID, error) {
	r, err := templates.Render(ctx, tx, templateID, templates.RenderInput{ConversationID: conv})
	_, missing := errors.AsType[*templates.VariableMissingError](err)
	switch {
	case missing, errors.Is(err, templates.ErrNotFound), errors.Is(err, templates.ErrOrderNotOfCustomer):
		return uuid.Nil, Skip(OutcomeSkippedPolicy)
	case err != nil:
		return uuid.Nil, err //nolint:wrapcheck // templates đã bọc việc đang làm
	case strings.TrimSpace(r.Text) == "":
		return uuid.Nil, Skip(OutcomeSkippedPolicy)
	}
	_, jobID, err := send.Automated(ctx, tx, send.Env{Registry: d.Registry, Queue: d.Queue, Bus: d.Bus}, conv, r.Text, now)
	_, denied := errors.AsType[*send.NotAllowedError](err)
	switch {
	case denied, errors.Is(err, send.ErrReadOnly), errors.Is(err, send.ErrInvalidInput):
		return uuid.Nil, Skip(OutcomeSkippedPolicy)
	case err != nil:
		return uuid.Nil, err //nolint:wrapcheck // send đã bọc việc đang làm
	}
	return jobID, nil
}

// checkFollowUp kiểm cấu hình follow_up khi lưu: afterHours 1–23; bật thì templateId phải là mẫu còn sống dùng được
// cho Page pageID (mẫu chung hay của chính Page). Sai → ErrInvalidRule.
func checkFollowUp(ctx context.Context, tx pgx.Tx, pageID uuid.UUID, enabled bool, config json.RawMessage) error {
	var c FollowUpConfig
	if err := json.Unmarshal(config, &c); err != nil || c.AfterHours < minAfterHours || c.AfterHours > maxAfterHours {
		return ErrInvalidRule
	}
	if !enabled {
		return nil
	}
	ok, err := store.New(tx).FollowUpTemplateUsable(ctx, store.FollowUpTemplateUsableParams{
		ID: c.TemplateID, ChannelAccountID: pageID,
	})
	switch {
	case err != nil:
		return fmt.Errorf("kiểm mẫu của quy tắc nhắn lại: %w", err)
	case !ok:
		return ErrInvalidRule
	}
	return nil
}

// followUpKind: việc của lịch; lỗi thì không chạy lại (kỳ sau có việc mới), quá một kỳ chưa chạy thì expired.
func followUpKind() queue.Kind {
	return queue.Kind{Name: "task." + FollowUpSchedule, Policy: queue.Policy{MaxAttempts: 1, ExpiresAfter: followUpEvery}}
}

func followUpSchedule() schedules.Schedule {
	return schedules.Schedule{Name: FollowUpSchedule, Every: followUpEvery, PerTenant: true, Kind: followUpKind()}
}

// followUpHandler chạy RunFollowUps với đồng hồ của Deps trong tx của việc (worker đặt WithTenant của tenant của việc).
type followUpHandler struct{ d deps.Deps }

func (followUpHandler) Kind() queue.Kind { return followUpKind() }

// Handle chạy quy tắc nhắn lại của tenant của việc; kết quả {"enqueued": n}.
func (h followUpHandler) Handle(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	if j.TenantID == nil {
		return nil, fmt.Errorf("%w: việc nhắn lại thiếu tenant", queue.ErrPermanent)
	}
	n, err := RunFollowUps(ctx, tx, h.d, h.d.Clock()())
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(map[string]int{"enqueued": n})
	if err != nil {
		return nil, fmt.Errorf("mã hóa kết quả nhắn lại: %w", err)
	}
	return out, nil
}
