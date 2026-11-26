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
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Trạng thái hội thoại (CHECK của conversations.status) và hành động audit đổi trạng thái (Task 159).
const (
	StatusOpen    = "open"
	StatusPending = "pending"
	StatusDone    = "done"
	StatusSpam    = "spam"

	actionStatus = "conversation.status"
)

// ErrInvalidStatus là lỗi trạng thái lạ (lớp HTTP đã kiểm theo enum của hợp đồng; đây là chốt của module).
var ErrInvalidStatus = errors.New("conversation: trạng thái hội thoại không hợp lệ")

// SetStatus đổi trạng thái hội thoại id khi expectedVersion khớp (cần conversation.reply trên Page; không có →
// access.ErrNotFound): khóa dòng, so version (khác → ErrVersionConflict), đổi và tăng version, audit chỉ tên trường,
// báo hiệu. Cùng trạng thái → không đổi gì.
func (w Writer) SetStatus(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, id uuid.UUID, status string,
	expectedVersion int32,
) error {
	switch status {
	case StatusOpen, StatusPending, StatusDone, StatusSpam:
	default:
		return ErrInvalidStatus
	}
	q := store.New(tx)
	c, err := lockPermitted(ctx, q, actor, PermReply, id)
	if err != nil {
		return err
	}
	if c.Version != expectedVersion {
		return ErrVersionConflict
	}
	r, err := q.SetConversationStatus(ctx, store.SetConversationStatusParams{Status: status, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("đổi trạng thái hội thoại: %w", err)
	}
	if err := identity.RecordAudit(ctx, tx, identity.AuditEntry{
		TenantID: c.TenantID, ActorType: "staff", ActorID: actor.UserID.String(), Action: actionStatus,
		EntityType: EntityConversation, EntityID: id.String(), After: map[string]any{"status": r.Status, "version": r.Version},
		RequestID: w.RequestID,
	}); err != nil {
		return fmt.Errorf("ghi audit trạng thái hội thoại: %w", err)
	}
	return Signal(ctx, tx, w.Realtime, c.TenantID, c.ChannelAccountID, EntityConversation, id)
}

// MarkRead đánh dấu hội thoại id đã đọc tới tin upTo (cần conversation.read trên Page; hộp thư dùng chung, không gửi
// "đã xem" cho khách, Q10): khóa dòng, số chưa đọc = số tin khách sau upTo (tin đến sau lúc màn hình đọc vẫn tính) nhưng
// không bao giờ tăng (mốc cũ hơn chỗ người khác đã đọc tới), không tăng version, báo hiệu khi số đổi. Tin không thuộc
// hội thoại → access.ErrNotFound.
func (w Writer) MarkRead(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, id, upTo uuid.UUID) error {
	q := store.New(tx)
	c, err := lockPermitted(ctx, q, actor, PermRead, id)
	if err != nil {
		return err
	}
	mark, err := q.GetMessageMark(ctx, store.GetMessageMarkParams{ID: upTo, ConversationID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return access.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("đọc tin đã xem tới: %w", err)
	}
	before, err := q.GetUnreadCount(ctx, id)
	if err != nil {
		return fmt.Errorf("đọc số chưa đọc: %w", err)
	}
	after, err := q.SetUnreadAfter(ctx, store.SetUnreadAfterParams{At: mark.SentAt, MsgID: mark.ID, ID: id})
	if err != nil {
		return fmt.Errorf("đánh dấu đã đọc: %w", err)
	}
	if after == before {
		return nil
	}
	return Signal(ctx, tx, w.Realtime, c.TenantID, c.ChannelAccountID, EntityConversation, id)
}

// lockPermitted khóa dòng hội thoại id rồi kiểm actor có quyền p trên Page của nó (không có hội thoại hay không quyền →
// access.ErrNotFound).
func lockPermitted(ctx context.Context, q *store.Queries, actor identity.AuthUser, p identity.Permission, id uuid.UUID,
) (locked, error) {
	c, err := lockConversation(ctx, q, id)
	if err == nil {
		err = access.Require(actor, p, c.ChannelAccountID)
	}
	return c, err //nolint:wrapcheck // sentinel của module (404); lockConversation đã bọc
}

// OptTime đổi cột timestamptz có thể rỗng của sqlc sang con trỏ (NULL → nil).
func OptTime(v pgtype.Timestamptz) *time.Time {
	if v.Valid {
		return new(v.Time)
	}
	return nil
}
