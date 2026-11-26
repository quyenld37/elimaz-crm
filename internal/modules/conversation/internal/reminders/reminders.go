// Package reminders là phần của module conversation: nhắc việc trên hội thoại "nhắc tôi lúc…" (Task 169; 01 mục 6).
// Nhắc tới hạn được lịch conversation.reminders (mỗi 60 s, theo tenant) đổi sang fired, mở lại hội thoại đang chờ hay
// đã xong, báo hiệu reminder.fired. Ghi chú của nhắc là dữ liệu cá nhân: không vào log, audit, báo hiệu.
package reminders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// Trạng thái và nguồn của nhắc (CHECK của conversation.reminders, migration 0408).
const (
	StatusPending   = "pending"
	StatusFired     = "fired"
	StatusDone      = "done"
	StatusCancelled = "cancelled"

	SourceManual       = "manual"
	SourceCallCallback = "call_callback"
)

// MaxAhead là hạn xa nhất của một nhắc tính từ lúc tạo (kế hoạch M4, Task 169).
const MaxAhead = 90 * 24 * time.Hour

// maxNoteRunes khớp CHECK (length(note) <= 500).
const maxNoteRunes = 500

// Schedule là tên lịch bắn nhắc tới hạn.
const Schedule = "conversation.reminders"

const (
	fireEvery = 60 * time.Second
	fireBatch = 200
	listLimit = 200
)

// Quyền dùng ở phần này (phạm vi page:<channel_accounts.id>).
const (
	permRead   identity.Permission = "conversation.read"
	permReply  identity.Permission = "conversation.reply"
	permAssign identity.Permission = "conversation.assign"
)

// Chủ đề báo hiệu (cùng chuỗi với conversation.TopicReminderFired, TopicConversationChanged; gói gốc nhập gói này
// nên không nhập ngược được).
const (
	topicReminderFired      = "reminder.fired"
	topicConversationChange = "conversation.changed"
)

var (
	// ErrInvalidInput là lỗi khi giờ nhắc không ở tương lai hay xa quá MaxAhead, ghi chú quá dài, trạng thái lọc lạ.
	ErrInvalidInput = errors.New("conversation: nhắc việc không hợp lệ")
	// ErrNotFound là lỗi khi nhắc không có, của người khác mà người gọi không có quyền giao việc, hay hội thoại trên Page người
	// gọi không có quyền (404, không lộ sự tồn tại).
	ErrNotFound = access.ErrNotFound
	// ErrClosed là lỗi khi nhắc đã xong hay đã hủy (cập nhật có điều kiện không khớp, 409).
	ErrClosed = errors.New("conversation: nhắc việc đã đóng")
	// ErrAssignForbidden là lỗi khi giao nhắc cho người khác mà không có conversation.assign trên Page của hội thoại (403).
	ErrAssignForbidden = errors.New("conversation: không có quyền giao nhắc việc cho người khác")
	// ErrAssigneeNotAllowed là lỗi khi người được giao không phải nhân viên đang làm có quyền trả lời hội thoại (422).
	ErrAssigneeNotAllowed = errors.New("conversation: người được giao không xem được hội thoại")
)

// Reminder là một nhắc việc. FiredAt nil khi chưa bắn.
type Reminder struct {
	ID, ConversationID, UserID uuid.UUID
	RemindAt                   time.Time
	Note                       string
	Status, Source             string
	FiredAt                    *time.Time
	Version                    int32
	CreatedAt                  time.Time
}

// Input là yêu cầu tạo nhắc: UserID nil = chính người gọi.
type Input struct {
	RemindAt time.Time
	Note     string
	UserID   *uuid.UUID
}

// Create tạo nhắc nguồn source cho hội thoại conversationID. Người gọi u cần conversation.reply trên Page của hội
// thoại (không có → ErrNotFound); giao cho người khác cần conversation.assign (ErrAssignForbidden) và người đó phải là
// nhân viên đang làm có conversation.reply — quyền để tự đóng nhắc (ErrAssigneeNotAllowed). RemindAt phải sau now và không quá now + MaxAhead.
func Create(ctx context.Context, tx pgx.Tx, u identity.AuthUser, conversationID uuid.UUID, in Input, source string,
	now time.Time,
) (Reminder, error) {
	if !in.RemindAt.After(now) || in.RemindAt.After(now.Add(MaxAhead)) || len([]rune(in.Note)) > maxNoteRunes {
		return Reminder{}, ErrInvalidInput
	}
	q := store.New(tx)
	conv, err := q.ReminderConversation(ctx, conversationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reminder{}, ErrNotFound
	}
	if err != nil {
		return Reminder{}, fmt.Errorf("đọc hội thoại của nhắc việc: %w", err)
	}
	if err := access.Require(u, permReply, conv.ChannelAccountID); err != nil {
		return Reminder{}, err //nolint:wrapcheck // sentinel của access
	}
	target := u.UserID
	if in.UserID != nil && *in.UserID != u.UserID {
		if !identity.Can(u, permAssign, access.PageScope(conv.ChannelAccountID)) {
			return Reminder{}, ErrAssignForbidden
		}
		ok, err := identity.StaffHas(ctx, tx, *in.UserID, permReply)
		if err != nil {
			return Reminder{}, fmt.Errorf("kiểm người được giao nhắc việc: %w", err)
		}
		if !ok {
			return Reminder{}, ErrAssigneeNotAllowed
		}
		target = *in.UserID
	}
	row, err := q.InsertReminder(ctx, store.InsertReminderParams{
		ConversationID: conversationID, UserID: target, RemindAt: in.RemindAt, Note: optText(in.Note), Source: source,
		CreatedBy: u.UserID,
	})
	if err != nil {
		return Reminder{}, fmt.Errorf("ghi nhắc việc: %w", err)
	}
	return fromRow(store.ListUserRemindersRow(row)), nil
}

// Close đưa nhắc id sang status (StatusDone hay StatusCancelled) bằng cập nhật có điều kiện: chỉ nhắc còn pending hay
// fired. Người gọi phải là người được nhắc, hoặc có conversation.assign trên Page của hội thoại; cả hai cần
// conversation.reply trên Page đó. Không có hay không được xem → ErrNotFound; đã đóng (kể cả lần gọi song song thắng
// trước) → ErrClosed.
func Close(ctx context.Context, tx pgx.Tx, u identity.AuthUser, id uuid.UUID, status string) (Reminder, error) {
	if status != StatusDone && status != StatusCancelled {
		return Reminder{}, ErrInvalidInput
	}
	q := store.New(tx)
	cur, err := q.GetReminder(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reminder{}, ErrNotFound
	}
	if err != nil {
		return Reminder{}, fmt.Errorf("đọc nhắc việc: %w", err)
	}
	if err := access.Require(u, permReply, cur.ChannelAccountID); err != nil {
		return Reminder{}, err //nolint:wrapcheck // sentinel của access
	}
	if cur.UserID != u.UserID && !identity.Can(u, permAssign, access.PageScope(cur.ChannelAccountID)) {
		return Reminder{}, ErrNotFound
	}
	row, err := q.CloseReminder(ctx, store.CloseReminderParams{ID: id, Status: status})
	if errors.Is(err, pgx.ErrNoRows) {
		return Reminder{}, ErrClosed
	}
	if err != nil {
		return Reminder{}, fmt.Errorf("đóng nhắc việc: %w", err)
	}
	return fromRow(store.ListUserRemindersRow(row)), nil
}

// ListMine trả nhắc của userID ở trạng thái status (StatusPending hay StatusFired), sớm nhất trước, tối đa 200.
func ListMine(ctx context.Context, tx pgx.Tx, userID uuid.UUID, status string) ([]Reminder, error) {
	if status != StatusPending && status != StatusFired {
		return nil, ErrInvalidInput
	}
	rows, err := store.New(tx).ListUserReminders(ctx, store.ListUserRemindersParams{UserID: userID, Status: status, Lim: listLimit})
	if err != nil {
		return nil, fmt.Errorf("đọc nhắc việc của tôi: %w", err)
	}
	out := make([]Reminder, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromRow(r))
	}
	return out, nil
}

// Fired là một nhắc vừa bắn.
type Fired struct {
	ID, ConversationID, PageID uuid.UUID
}

// FireDue bắn tối đa khoảng 200 nhắc tới hạn (remind_at ≤ now) của tenant trong tx: khóa hội thoại của chúng theo id
// tăng dần (ADR-46 nhóm 6: conversations trước reminders), đổi nhắc pending sang fired, mở lại hội thoại pending hay
// done, báo hiệu reminder.fired (thực thể là mã nhắc) và conversation.changed cho hội thoại mở lại. Chạy lại được
// (nhắc đã fired không bắn lại). bus nil: không báo hiệu.
func FireDue(ctx context.Context, tx pgx.Tx, bus realtime.Bus, tenantID uuid.UUID, now time.Time) ([]Fired, error) {
	q := store.New(tx)
	convs, err := q.DueReminderConversations(ctx, store.DueReminderConversationsParams{Now: now, Lim: fireBatch})
	if err != nil {
		return nil, fmt.Errorf("tìm nhắc việc tới hạn: %w", err)
	}
	if len(convs) == 0 {
		return nil, nil
	}
	if convs, err = q.LockConversationsForReminders(ctx, convs); err != nil {
		return nil, fmt.Errorf("khóa hội thoại của nhắc việc: %w", err)
	}
	rows, err := q.FireDueReminders(ctx, store.FireDueRemindersParams{Now: now, ConversationIds: convs})
	if err != nil {
		return nil, fmt.Errorf("bắn nhắc việc: %w", err)
	}
	fired := make([]Fired, 0, len(rows))
	reopen := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		fired = append(fired, Fired{ID: r.ID, ConversationID: r.ConversationID, PageID: r.ChannelAccountID})
		reopen = append(reopen, r.ConversationID)
	}
	reopened, err := q.ReopenConversations(ctx, store.ReopenConversationsParams{Now: now, Ids: reopen})
	if err != nil {
		return nil, fmt.Errorf("mở lại hội thoại của nhắc việc: %w", err)
	}
	for _, f := range fired {
		if err := Signal(ctx, tx, bus, tenantID, topicReminderFired, f.PageID, "reminder", f.ID); err != nil {
			return nil, err
		}
	}
	for _, c := range reopened {
		if err := Signal(ctx, tx, bus, tenantID, topicConversationChange, c.ChannelAccountID, "conversation", c.ID); err != nil {
			return nil, err
		}
	}
	return fired, nil
}

// Signal phát báo hiệu topic cho thực thể (kind, id) trên Page pageID: người có conversation.read trên Page đó nhận,
// không mang nội dung. bus nil: không làm gì.
func Signal(ctx context.Context, tx pgx.Tx, bus realtime.Bus, tenantID uuid.UUID, topic string, pageID uuid.UUID,
	kind string, id uuid.UUID,
) error {
	if bus == nil {
		return nil
	}
	if err := bus.Publish(ctx, tx, realtime.Signal{
		TenantID: tenantID, Topic: topic, Requires: permRead, Scope: access.PageScope(pageID),
		Entity: realtime.Entity{Type: kind, ID: id.String()},
	}); err != nil {
		return fmt.Errorf("báo hiệu %s: %w", topic, err)
	}
	return nil
}

// fromRow đổi một dòng nhắc (các câu trả cùng cột) sang Reminder.
func fromRow(r store.ListUserRemindersRow) Reminder {
	out := Reminder{
		ID: r.ID, ConversationID: r.ConversationID, UserID: r.UserID, RemindAt: r.RemindAt, Note: r.Note.String,
		Status: r.Status, Source: r.Source, Version: r.Version, CreatedAt: r.CreatedAt,
	}
	if r.FiredAt.Valid {
		at := r.FiredAt.Time
		out.FiredAt = &at
	}
	return out
}

func optText(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

// fireKind: việc của lịch; lỗi thì không chạy lại (kỳ sau có việc mới), quá một kỳ chưa chạy thì expired.
func fireKind() queue.Kind {
	return queue.Kind{Name: "task." + Schedule, Policy: queue.Policy{MaxAttempts: 1, ExpiresAfter: fireEvery}}
}

// fireHandler chạy FireDue trong tx của việc (worker đặt WithTenant của tenant của việc).
type fireHandler struct{ d deps.Deps }

func (fireHandler) Kind() queue.Kind { return fireKind() }

// Handle bắn nhắc tới hạn; kết quả {"fired": n}.
func (h fireHandler) Handle(ctx context.Context, job queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	now := time.Now
	if h.d.Now != nil {
		now = h.d.Now
	}
	if job.TenantID == nil {
		return nil, fmt.Errorf("bắn nhắc việc: việc của lịch thiếu tenant: %w", queue.ErrPermanent)
	}
	fired, err := FireDue(ctx, tx, h.d.Bus, *job.TenantID, now())
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(map[string]int{"fired": len(fired)})
	if err != nil {
		return nil, fmt.Errorf("mã hóa kết quả bắn nhắc việc: %w", err)
	}
	return out, nil
}

// Parts là phần chạy nền của phần reminders: lịch conversation.reminders (theo tenant, 60 s) và handler của nó.
func Parts(d deps.Deps) deps.Parts {
	return deps.Parts{
		Jobs:      []queue.Handler{fireHandler{d: d}},
		Schedules: []schedules.Schedule{{Name: Schedule, Every: fireEvery, PerTenant: true, Kind: fireKind()}},
	}
}
