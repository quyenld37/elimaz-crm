// Package assign là phần của module conversation: người phụ trách hội thoại (tự nhận, quản lý giao, bỏ giao), lịch sử
// phân công và xoay vòng khi có hội thoại mới (Task 160; 04 mục 7, Q15). Mỗi lần đổi người phụ trách: khóa dòng hội
// thoại, tăng version, một dòng conversation_assignments, audit, sự kiện conversation.assigned (Seq = version mới),
// báo hiệu — tất cả trong tx của bên gọi.
package assign

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
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Lỗi của phần phân công (gói gốc conversation công bố lại; lớp HTTP đổi mã ở errors_conversation.go).
var (
	// ErrAlreadyAssigned: hội thoại đang do người khác phụ trách (tự nhận).
	ErrAlreadyAssigned = errors.New("conversation: hội thoại đã có người khác nhận")
	// ErrAssigneeNotAllowed: người được giao không còn hoạt động hoặc không có conversation.read trên Page.
	ErrAssigneeNotAllowed = access.ErrAssigneeNotAllowed
	// ErrVersionConflict: version gửi lên khác version hiện tại.
	ErrVersionConflict = errors.New("conversation: dữ liệu đã đổi, tải lại rồi thử lại")
)

// Lý do của một dòng lịch sử phân công (CHECK của conversation_assignments.reason).
const (
	ReasonSelf       = "self"
	ReasonManager    = "manager"
	ReasonRoundRobin = "round_robin"
	ReasonUnassign   = "unassign"
)

// Tên quyền, chủ đề, sự kiện (trùng hằng của gói gốc conversation; test của gói gốc ghim hai bên bằng nhau).
const (
	PermRead   identity.Permission = "conversation.read"
	PermReply  identity.Permission = "conversation.reply"
	PermAssign identity.Permission = "conversation.assign"

	TopicChanged       = "conversation.changed"
	EventAssigned      = "conversation.assigned"
	EntityConversation = "conversation"
)

// Parts là phần chạy nền của phần assign: không có (xoay vòng chạy trong tx của tin vào qua RoundRobin).
func Parts(_ deps.Deps) deps.Parts { return deps.Parts{} }

// Writer là đường ghi người phụ trách. Dựng mới cho mỗi yêu cầu; không giữ trạng thái giữa các lần chạy lại.
type Writer struct {
	// Events là sổ sự kiện (nil: sổ rỗng, sự kiện vẫn ghi vào domain_events).
	Events *events.Registry
	// Queue ghi việc của consumer sự kiện (nil: queue.NewQueue()).
	Queue queue.JobQueue
	// Realtime phát báo hiệu trong tx (nil: không phát; đường vào tự phát sau hook).
	Realtime  realtime.Bus
	RequestID string
}

// Assignment là một dòng lịch sử phân công; From, To, Actor nil khi không có (bỏ giao, hệ thống).
type Assignment struct {
	ID              uuid.UUID
	From, To, Actor *Person
	Reason          string
	CreatedAt       time.Time
}

// Person là nhân viên kèm tên hiển thị (rỗng khi không còn).
type Person struct {
	ID   uuid.UUID
	Name string
}

type locked = store.LockConversationForAssignRow

func lockConversation(ctx context.Context, q *store.Queries, id uuid.UUID) (locked, error) {
	c, err := q.LockConversationForAssign(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return locked{}, access.ErrNotFound
	}
	if err != nil {
		return locked{}, fmt.Errorf("khóa hội thoại: %w", err)
	}
	return c, nil
}

// Claim nhận hội thoại id về actor (cần conversation.reply trên Page; không có → access.ErrNotFound). Chưa ai nhận →
// actor (lý do self; actor phải active và có conversation.read trên Page, không thì ErrAssigneeNotAllowed); actor đang
// nhận → không đổi gì; người khác đang nhận → ErrAlreadyAssigned.
func (w Writer) Claim(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, id uuid.UUID) error {
	q := store.New(tx)
	c, err := lockConversation(ctx, q, id)
	if err != nil {
		return err
	}
	if err := access.Require(actor, PermReply, c.ChannelAccountID); err != nil {
		return err //nolint:wrapcheck // sentinel của module (404)
	}
	if c.AssigneeID.Valid {
		if uuid.UUID(c.AssigneeID.Bytes) == actor.UserID {
			return nil
		}
		return ErrAlreadyAssigned
	}
	ok, err := Eligible(ctx, tx, c.ChannelAccountID, actor.UserID)
	if err != nil {
		return err
	}
	if !ok { // có conversation.reply nhưng không còn đọc được hội thoại của Page (hay vừa bị khóa)
		return ErrAssigneeNotAllowed
	}
	me := actor.UserID
	return w.change(ctx, tx, c, &me, &me, ReasonSelf)
}

// Assign giao hội thoại id cho to (nil: bỏ giao) khi expectedVersion khớp (cần conversation.assign trên Page). Người
// nhận phải active và có conversation.read trên Page (ErrAssigneeNotAllowed). Cùng người đang phụ trách → không đổi.
// Lý do: unassign (to nil), self (to là actor), manager (còn lại).
func (w Writer) Assign(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, id uuid.UUID, to *uuid.UUID,
	expectedVersion int32,
) error {
	q := store.New(tx)
	c, err := lockConversation(ctx, q, id)
	if err != nil {
		return err
	}
	if err := access.Require(actor, PermAssign, c.ChannelAccountID); err != nil {
		return err //nolint:wrapcheck // sentinel của module (404)
	}
	if c.Version != expectedVersion {
		return ErrVersionConflict
	}
	if sameAssignee(c.AssigneeID, to) {
		return nil
	}
	reason := ReasonUnassign
	if to != nil {
		ok, err := Eligible(ctx, tx, c.ChannelAccountID, *to)
		if err != nil {
			return err
		}
		if !ok {
			return ErrAssigneeNotAllowed
		}
		reason = ReasonManager
		if *to == actor.UserID {
			reason = ReasonSelf
		}
	}
	me := actor.UserID
	return w.change(ctx, tx, c, to, &me, reason)
}

func sameAssignee(cur pgtype.UUID, to *uuid.UUID) bool {
	if to == nil {
		return !cur.Valid
	}
	return cur.Valid && uuid.UUID(cur.Bytes) == *to
}

// Eligible cho biết userID có thể phụ trách hội thoại của Page pageID (active, có conversation.read trên Page).
func Eligible(ctx context.Context, tx pgx.Tx, pageID, userID uuid.UUID) (bool, error) {
	staff, err := identity.StaffWithPermissionAt(ctx, tx, PermRead, *access.PageScope(pageID))
	if err != nil {
		return false, fmt.Errorf("đọc người được giao: %w", err)
	}
	for _, u := range staff {
		if u.ID == userID {
			return true, nil
		}
	}
	return false, nil
}

// IDOrNil trả giá trị cho audit, payload (nil thật thay vì con trỏ nil có kiểu; mã dạng chuỗi).
func IDOrNil(id *uuid.UUID) any {
	if id != nil {
		return id.String()
	}
	return nil
}

// PtrUUID đổi cột uuid có thể rỗng của sqlc sang con trỏ (NULL → nil).
func PtrUUID(v pgtype.UUID) (id *uuid.UUID) {
	if v.Valid {
		id = new(uuid.UUID(v.Bytes))
	}
	return id
}

// change đổi người phụ trách của c (đã khóa) sang to; actorID nil là hệ thống (xoay vòng).
func (w Writer) change(ctx context.Context, tx pgx.Tx, c locked, to, actorID *uuid.UUID, reason string) error {
	q := store.New(tx)
	version, err := q.SetConversationAssignee(ctx, store.SetConversationAssigneeParams{AssigneeID: db.NullUUID(to), ID: c.ID})
	if err != nil {
		return fmt.Errorf("đổi người phụ trách: %w", err)
	}
	if err := q.InsertConversationAssignment(ctx, store.InsertConversationAssignmentParams{
		ConversationID: c.ID, FromUserID: c.AssigneeID, ToUserID: db.NullUUID(to), ActorID: db.NullUUID(actorID), Reason: reason,
	}); err != nil {
		return fmt.Errorf("ghi lịch sử phân công: %w", err)
	}
	from, toID := IDOrNil(PtrUUID(c.AssigneeID)), IDOrNil(to)
	entry := identity.AuditEntry{
		TenantID: c.TenantID, ActorType: "system", Action: EventAssigned, EntityType: EntityConversation,
		EntityID: c.ID.String(), Before: map[string]any{"assignee_id": from},
		After: map[string]any{"assignee_id": toID, "reason": reason}, RequestID: w.RequestID,
	}
	if actorID != nil {
		entry.ActorType, entry.ActorID = "staff", actorID.String()
	}
	if err := identity.RecordAudit(ctx, tx, entry); err != nil {
		return fmt.Errorf("ghi audit phân công: %w", err)
	}
	ev, jq := w.Events, w.Queue
	if ev == nil {
		ev = events.NewRegistry()
	}
	if jq == nil {
		jq = queue.NewQueue()
	}
	if _, _, err := ev.Emit(ctx, tx, jq, events.Event{
		TenantID: c.TenantID, AggregateType: EntityConversation, AggregateID: c.ID.String(), Seq: int64(version),
		Topic: EventAssigned, Payload: map[string]any{
			"conversationId": c.ID, "pageId": c.ChannelAccountID, "fromUserId": from, "toUserId": toID, "reason": reason,
		},
	}); err != nil {
		return fmt.Errorf("phát sự kiện phân công: %w", err)
	}
	return Signal(ctx, tx, w.Realtime, c.TenantID, c.ChannelAccountID, EntityConversation, c.ID)
}

// Signal phát conversation.changed của thực thể (entity, id) trên Page pageID (chỉ người có conversation.read trên Page
// nhận); bus nil → không phát.
func Signal(ctx context.Context, tx pgx.Tx, bus realtime.Bus, tenantID, pageID uuid.UUID, entity string, id uuid.UUID) error {
	return access.Signal(ctx, tx, bus, tenantID, TopicChanged, pageID, entity, id) //nolint:wrapcheck // access đã bọc
}

// History trả lịch sử phân công của hội thoại id, cũ trước (cần conversation.read trên Page).
func History(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, id uuid.UUID) ([]Assignment, error) {
	q := store.New(tx)
	page, err := q.GetConversationPage(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, access.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc hội thoại: %w", err)
	}
	if err := access.Require(actor, PermRead, page); err != nil {
		return nil, err //nolint:wrapcheck // sentinel của module (404)
	}
	rows, err := q.ListConversationAssignments(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("đọc lịch sử phân công: %w", err)
	}
	out := make([]Assignment, 0, len(rows))
	for _, r := range rows {
		out = append(out, Assignment{
			ID: r.ID, From: person(r.FromUserID, r.FromName), To: person(r.ToUserID, r.ToName),
			Actor: person(r.ActorID, r.ActorName), Reason: r.Reason, CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}

func person(id pgtype.UUID, name string) *Person {
	if !id.Valid {
		return nil
	}
	return &Person{ID: uuid.UUID(id.Bytes), Name: name}
}
