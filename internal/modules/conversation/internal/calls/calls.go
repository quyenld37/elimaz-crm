// Package calls là phần của module conversation: ghi kết quả cuộc gọi khách (Task 169; Q14, CH17 "lưu vào hồ sơ
// khách và hội thoại"), đọc cuộc gọi và hội thoại của cả nhóm khách (mọi hồ sơ cùng root_id) trên các Page người xem
// được đọc. Gọi điện là ứng dụng điện thoại của nhân viên (tel:), CRM không làm tổng đài. Ghi chú cuộc gọi là dữ liệu
// cá nhân: không vào log, audit (audit chỉ ghi tên trường), tin hệ thống, báo hiệu.
package calls

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/reminders"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Kết quả cuộc gọi (CHECK của conversation.call_logs, migration 0408).
const (
	OutcomeClosed      = "closed"
	OutcomeNoAnswer    = "no_answer"
	OutcomeCallback    = "callback"
	OutcomeWrongNumber = "wrong_number"
	OutcomeDeclined    = "declined"
	OutcomeOther       = "other"
)

// maxNoteRunes khớp CHECK (length(note) <= 1000).
const maxNoteRunes = 1000

const (
	defaultLimit = 50
	maxLimit     = 100
)

const permReply identity.Permission = "conversation.reply"

// vietnamOffset: giờ Việt Nam UTC+7 (không đổi giờ mùa hè), để ghi giờ hẹn gọi lại trong tin hệ thống.
const vietnamOffset = 7 * 60 * 60

// outcomeLabel: nhãn tiếng Việt của kết quả trong tin hệ thống "Đã gọi: …" (không có ghi chú).
func outcomeLabel(outcome string) (string, bool) {
	switch outcome {
	case OutcomeClosed:
		return "đã chốt", true
	case OutcomeNoAnswer:
		return "không nghe máy", true
	case OutcomeCallback:
		return "hẹn gọi lại", true
	case OutcomeWrongNumber:
		return "sai số", true
	case OutcomeDeclined:
		return "khách từ chối", true
	case OutcomeOther:
		return "khác", true
	}
	return "", false
}

var (
	// ErrInvalidInput là lỗi dữ liệu cuộc gọi sai: kết quả lạ; callbackAt thiếu khi hẹn gọi lại hay có khi kết quả khác; hẹn gọi lại không gắn hội
	// thoại; hội thoại hay đơn không thuộc nhóm khách; ghi chú quá dài.
	ErrInvalidInput = errors.New("conversation: cuộc gọi không hợp lệ")
	// ErrNotFound là lỗi khi hội thoại không có hay trên Page người gọi không có quyền (404, không lộ sự tồn tại).
	ErrNotFound = access.ErrNotFound
)

// CallLog là một cuộc gọi đã ghi. Reminder có khi kết quả là hẹn gọi lại (nhắc việc tự tạo cho người gọi).
type CallLog struct {
	ID, CustomerID          uuid.UUID
	ConversationID, OrderID *uuid.UUID
	Outcome, Note           string
	CallbackAt              *time.Time
	CreatedBy               uuid.UUID
	CreatedAt               time.Time
	Reminder                *reminders.Reminder
}

// Input là yêu cầu ghi cuộc gọi. RequestID đi vào audit.
type Input struct {
	ConversationID, OrderID *uuid.UUID
	Outcome, Note           string
	CallbackAt              *time.Time
	RequestID               string
}

// Record ghi cuộc gọi của nhân viên u với khách customerID trong tx (Q14): một dòng call_logs; có hội thoại → thêm tin
// hệ thống "Đã gọi: <kết quả>" vào hội thoại và báo hiệu conversation.changed; hẹn gọi lại → nhắc việc call_callback
// cho u lúc CallbackAt (cùng kiểm của reminders.Create: tương lai, tối đa 90 ngày); audit chỉ ghi tên trường. Khách
// không có → customer.ErrNotFound; hội thoại không được trả lời → ErrNotFound; dữ liệu sai → ErrInvalidInput.
func Record(ctx context.Context, tx pgx.Tx, bus realtime.Bus, u identity.AuthUser, customerID uuid.UUID, in Input,
	now time.Time,
) (CallLog, error) {
	label, ok := outcomeLabel(in.Outcome)
	if !ok || (in.Outcome == OutcomeCallback) != (in.CallbackAt != nil) || len([]rune(in.Note)) > maxNoteRunes ||
		(in.Outcome == OutcomeCallback && in.ConversationID == nil) {
		return CallLog{}, ErrInvalidInput
	}
	conv, err := links(ctx, tx, u, customerID, in)
	if err != nil {
		return CallLog{}, err
	}
	q := store.New(tx)
	// Thứ tự ghi theo ADR-46 nhóm 6: reminders → messages → call_logs.
	var reminder *reminders.Reminder
	if in.Outcome == OutcomeCallback {
		r, err := reminders.Create(ctx, tx, u, conv.ID, reminders.Input{RemindAt: *in.CallbackAt},
			reminders.SourceCallCallback, now)
		if errors.Is(err, reminders.ErrInvalidInput) {
			return CallLog{}, ErrInvalidInput
		}
		if err != nil {
			return CallLog{}, fmt.Errorf("tạo nhắc gọi lại: %w", err)
		}
		reminder = &r
		label += " lúc " + in.CallbackAt.In(time.FixedZone("ICT", vietnamOffset)).Format("15:04 02/01/2006")
	}
	if conv != nil {
		if _, err := q.InsertSystemMessage(ctx, store.InsertSystemMessageParams{
			ConversationID: conv.ID, ChannelAccountID: conv.ChannelAccountID,
			Body: pgtype.Text{String: "Đã gọi: " + label, Valid: true}, SenderStaffID: db.NullUUID(&u.UserID), SentAt: now,
		}); err != nil {
			return CallLog{}, fmt.Errorf("ghi tin cuộc gọi vào hội thoại: %w", err)
		}
	}
	row, err := q.InsertCallLog(ctx, store.InsertCallLogParams{
		CustomerID: customerID, ConversationID: db.NullUUID(in.ConversationID), OrderID: db.NullUUID(in.OrderID),
		Outcome: in.Outcome, Note: pgtype.Text{String: in.Note, Valid: in.Note != ""}, CallbackAt: optTime(in.CallbackAt),
		CreatedBy: u.UserID, CreatedAt: now,
	})
	if err != nil {
		return CallLog{}, fmt.Errorf("ghi cuộc gọi: %w", err)
	}
	out := fromRow(store.ListGroupCallsRow(row))
	out.Reminder = reminder
	if err := audit(ctx, tx, u, out, in.RequestID, now); err != nil {
		return CallLog{}, err
	}
	if conv != nil {
		if err := reminders.Signal(ctx, tx, bus, u.TenantID, "conversation.changed", conv.ChannelAccountID,
			"conversation", conv.ID); err != nil {
			return CallLog{}, err //nolint:wrapcheck // đã bọc ở reminders.Signal
		}
	}
	return out, nil
}

// links kiểm khách customerID có, hội thoại (nếu có) trên Page u được trả lời và cùng nhóm khách, đơn (nếu có) cùng
// nhóm khách; trả hội thoại (nil khi không gắn).
func links(ctx context.Context, tx pgx.Tx, u identity.AuthUser, customerID uuid.UUID, in Input,
) (*store.ReminderConversationRow, error) {
	root, err := customer.RootOf(ctx, tx, customerID)
	if err != nil {
		return nil, fmt.Errorf("đọc khách của cuộc gọi: %w", err)
	}
	q := store.New(tx)
	var conv *store.ReminderConversationRow
	if in.ConversationID != nil {
		c, err := q.ReminderConversation(ctx, *in.ConversationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("đọc hội thoại của cuộc gọi: %w", err)
		}
		if err := access.Require(u, permReply, c.ChannelAccountID); err != nil {
			return nil, err //nolint:wrapcheck // sentinel của access
		}
		if err := sameGroup(ctx, tx, root, c.CustomerID); err != nil {
			return nil, err
		}
		conv = &c
	}
	if in.OrderID != nil {
		owner, err := q.OrderCustomer(ctx, *in.OrderID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidInput
		}
		if err != nil {
			return nil, fmt.Errorf("đọc đơn của cuộc gọi: %w", err)
		}
		if err := sameGroup(ctx, tx, root, owner); err != nil {
			return nil, err
		}
	}
	return conv, nil
}

// sameGroup trả ErrInvalidInput khi hồ sơ id không thuộc nhóm có gốc root.
func sameGroup(ctx context.Context, tx pgx.Tx, root, id uuid.UUID) error {
	r, err := customer.RootOf(ctx, tx, id)
	if err != nil && !errors.Is(err, customer.ErrNotFound) {
		return fmt.Errorf("đọc gốc khách: %w", err)
	}
	if err != nil || r != root {
		return ErrInvalidInput
	}
	return nil
}

// audit ghi call_log.create: chỉ kết quả, mã liên kết, giờ hẹn và tên trường note (true khi có), không có nội dung.
func audit(ctx context.Context, tx pgx.Tx, u identity.AuthUser, c CallLog, requestID string, now time.Time) error {
	after := map[string]any{"outcome": c.Outcome, "customer_id": c.CustomerID.String()}
	if c.ConversationID != nil {
		after["conversation_id"] = c.ConversationID.String()
	}
	if c.OrderID != nil {
		after["order_id"] = c.OrderID.String()
	}
	if c.CallbackAt != nil {
		after["callback_at"] = c.CallbackAt.UTC().Format(time.RFC3339)
	}
	if c.Note != "" {
		after["note"] = true
	}
	if err := identity.RecordAudit(ctx, tx, identity.AuditEntry{
		TenantID: u.TenantID, ActorType: "staff", ActorID: u.UserID.String(), Action: "call_log.create",
		EntityType: "call_log", EntityID: c.ID.String(), After: after, RequestID: requestID, At: now,
	}); err != nil {
		return fmt.Errorf("ghi audit cuộc gọi: %w", err)
	}
	return nil
}

// scopeOf dựng tham số chung của hai danh sách theo nhóm khách customerID (không có → customer.ErrNotFound): hồ sơ của
// nhóm, Page u có conversation.read, mốc trang (after nil: trang đầu), Lim = giới hạn + 1; trả kèm giới hạn đã chặn
// (≤ 0: 50; tối đa 100).
func scopeOf(ctx context.Context, tx pgx.Tx, u identity.AuthUser, customerID uuid.UUID, after *customer.ListCursor,
	limit int,
) (store.ListGroupCallsParams, int, error) {
	root, err := customer.RootOf(ctx, tx, customerID)
	if err != nil {
		return store.ListGroupCallsParams{}, 0, fmt.Errorf("đọc khách: %w", err)
	}
	members, err := customer.GroupMembers(ctx, tx, root)
	if err != nil {
		return store.ListGroupCallsParams{}, 0, fmt.Errorf("đọc nhóm khách: %w", err)
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	limit = min(limit, maxLimit)
	p := store.ListGroupCallsParams{
		Members: members, BeforeAt: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), BeforeID: uuid.Max,
		Lim: int32(limit + 1),
	}
	if after != nil {
		p.BeforeAt, p.BeforeID = after.CreatedAt, after.ID
	}
	p.AllPages, p.Pages = access.VisiblePages(u)
	return p, limit, nil
}

// List trả một trang cuộc gọi của cả nhóm khách customerID (mới nhất trước) mà u được xem: cuộc gọi không gắn hội
// thoại, hoặc gắn hội thoại trên Page u có conversation.read; con trỏ trang sau nil khi hết.
func List(ctx context.Context, tx pgx.Tx, u identity.AuthUser, customerID uuid.UUID, after *customer.ListCursor, limit int,
) ([]CallLog, *customer.ListCursor, error) {
	p, limit, err := scopeOf(ctx, tx, u, customerID, after, limit)
	if err != nil {
		return nil, nil, err
	}
	rows, err := store.New(tx).ListGroupCalls(ctx, p)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc cuộc gọi của khách: %w", err)
	}
	var next *customer.ListCursor
	if len(rows) > limit {
		rows = rows[:limit]
		next = &customer.ListCursor{CreatedAt: rows[limit-1].CreatedAt, ID: rows[limit-1].ID}
	}
	out := make([]CallLog, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromRow(r))
	}
	return out, next, nil
}

// Tag là nhãn còn sống của một hội thoại.
type Tag struct {
	ID            uuid.UUID
	Name, Color   string
	Sort, Version int32
}

// CustomerConversation là một hội thoại trong danh sách hội thoại của khách. HasOpenDraft: có đơn nháp gắn hội thoại.
// Thẻ bài (hội thoại comment, Task 501): PostPostedAt giờ đăng, PostPermalink đường dẫn, PostImageFileID ảnh bài đã chép
// về CRM. NewLead: khách tiềm năng mới (CH08: hồ sơ tạo từ chính hội thoại này, không gộp, chưa có đơn nào).
type CustomerConversation struct {
	ID, PageID, CustomerID uuid.UUID
	CustomerName           string
	Kind, Status           string
	AssigneeID             *uuid.UUID
	Tags                   []Tag
	UnreadCount            int32
	LastMessageAt          time.Time
	LastSnippet            string
	PostID                 *uuid.UUID
	PostSnippet            string
	PostPostedAt           *time.Time
	PostPermalink          string
	PostImageFileID        *uuid.UUID
	HasOpenDraft, NewLead  bool
	Version                int32
}

// Conversations trả một trang hội thoại của cả nhóm khách customerID (mọi hồ sơ cùng root_id) trên các Page u có
// conversation.read, theo tin cuối mới nhất trước (con trỏ: CreatedAt là last_message_at); con trỏ trang sau nil khi hết.
func Conversations(ctx context.Context, tx pgx.Tx, u identity.AuthUser, customerID uuid.UUID, after *customer.ListCursor,
	limit int,
) ([]CustomerConversation, *customer.ListCursor, error) {
	p, limit, err := scopeOf(ctx, tx, u, customerID, after, limit)
	if err != nil {
		return nil, nil, err
	}
	q := store.New(tx)
	rows, err := q.ListGroupConversations(ctx, store.ListGroupConversationsParams(p))
	if err != nil {
		return nil, nil, fmt.Errorf("đọc hội thoại của khách: %w", err)
	}
	var next *customer.ListCursor
	if len(rows) > limit {
		rows = rows[:limit]
		next = &customer.ListCursor{CreatedAt: rows[limit-1].LastMessageAt, ID: rows[limit-1].ID}
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	tags, err := q.ConversationTagsOf(ctx, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc nhãn hội thoại của khách: %w", err)
	}
	byConv := map[uuid.UUID][]Tag{}
	for _, t := range tags {
		byConv[t.ConversationID] = append(byConv[t.ConversationID], Tag{
			ID: t.ID, Name: t.Name, Color: t.Color, Sort: t.Sort, Version: t.Version,
		})
	}
	out := make([]CustomerConversation, 0, len(rows))
	for _, r := range rows {
		out = append(out, CustomerConversation{
			ID: r.ID, PageID: r.ChannelAccountID, CustomerID: r.CustomerID, CustomerName: r.CustomerName, Kind: r.Kind,
			Status: r.Status, AssigneeID: ptrUUID(r.AssigneeID), Tags: byConv[r.ID], UnreadCount: r.UnreadCount,
			LastMessageAt: r.LastMessageAt, LastSnippet: r.LastSnippet.String, PostID: ptrUUID(r.PostID),
			PostSnippet: r.PostSnippet.String, HasOpenDraft: r.HasOpenDraft, NewLead: r.NewLead, Version: r.Version,
		})
		out[len(out)-1].SetPostCard(r.PostCreatedTime, r.PostPermalink, r.PostImageFileID)
	}
	return out, next, nil
}

func fromRow(r store.ListGroupCallsRow) CallLog {
	out := CallLog{
		ID: r.ID, CustomerID: r.CustomerID, ConversationID: ptrUUID(r.ConversationID), OrderID: ptrUUID(r.OrderID),
		Outcome: r.Outcome, Note: r.Note.String, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt,
	}
	out.CallbackAt = ptrIf(r.CallbackAt.Time, r.CallbackAt.Valid)
	return out
}

// ptrUUID đổi cột uuid có thể rỗng sang con trỏ (nil khi NULL).
func ptrUUID(v pgtype.UUID) *uuid.UUID { return ptrIf(uuid.UUID(v.Bytes), v.Valid) }

// SetPostCard điền thẻ bài của c từ các cột posts (created_time, permalink, image_file_id; NULL → trống).
func (c *CustomerConversation) SetPostCard(created pgtype.Timestamptz, permalink pgtype.Text, image pgtype.UUID) {
	c.PostPostedAt = ptrIf(created.Time.UTC(), created.Valid)
	c.PostPermalink, c.PostImageFileID = permalink.String, ptrUUID(image)
}

// ptrIf trả &v khi ok, nil khi không.
func ptrIf[T any](v T, ok bool) *T {
	if ok {
		return &v
	}
	return nil
}

// optTime đổi con trỏ thời điểm sang tham số timestamptz có thể rỗng (nil → NULL).
func optTime(v *time.Time) (out pgtype.Timestamptz) {
	if v != nil {
		out = pgtype.Timestamptz{Time: *v, Valid: true}
	}
	return out
}

// Parts là phần chạy nền của phần calls: không có (ghi cuộc gọi là đường yêu cầu).
func Parts(_ deps.Deps) deps.Parts { return deps.Parts{} }
