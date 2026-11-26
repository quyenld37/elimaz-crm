package conversation

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/assign"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/query"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Đọc hộp chat (Task 159; hiện thực ở internal/query): danh sách hội thoại có bộ lọc, số đếm, tin theo trang, thẻ
// khách; đổi trạng thái và đánh dấu đã đọc (internal/assign, cùng chỗ khóa dòng hội thoại).

// Giá trị đặc biệt của bộ lọc người phụ trách.
const (
	AssigneeMe   = "me"
	AssigneeNone = "none"
)

// Trạng thái hội thoại.
const (
	StatusOpen    = assign.StatusOpen
	StatusPending = assign.StatusPending
	StatusDone    = assign.StatusDone
	StatusSpam    = assign.StatusSpam
)

// MaxListLimit là cỡ trang tối đa của danh sách hội thoại và tin; ListSinceSlack là khoảng lùi của updatedSince.
const (
	MaxListLimit   = query.MaxLimit
	ListSinceSlack = query.SinceSlack
)

// ListFilter là bộ lọc danh sách hội thoại. Assignee: "", AssigneeMe, AssigneeNone hay mã nhân viên (sai →
// ErrInvalidInput). Q: mã khách, SĐT, đuôi SĐT từ 4 số, tên từ 2 ký tự (customer.Search; ngắn → customer.ErrQueryTooShort).
type ListFilter struct {
	PageID       *uuid.UUID
	Status       string
	TagID        *uuid.UUID
	Assignee     string
	Kind         string
	Unread       bool
	Q            string
	UpdatedSince *time.Time
	After        *PageCursor
	Limit        int
}

// ListRow là một dòng danh sách hội thoại (kèm tên người phụ trách).
type ListRow = query.Row

type (
	// Counts là số đếm của hộp thư.
	Counts = query.Counts
	// PageCount là số đếm của một Page.
	PageCount = query.PageCount
)

type (
	// ChatMessage là một tin của hội thoại trong lịch sử (đọc; Message của send là tin vừa gửi).
	ChatMessage = query.Message
	// MessageAttachment là một tệp đính kèm của tin.
	MessageAttachment = query.Attachment
	// PrivateReply là tin riêng của một comment.
	PrivateReply = query.PrivateReply
	// MessagePage là một trang tin theo thứ tự thời gian.
	MessagePage = query.MessagePage
)

// PrivateReplyNone là trạng thái tin riêng của comment chưa có tin riêng đang sống.
const PrivateReplyNone = query.PrivateReplyNone

type (
	// Card là thẻ khách cạnh khung chat.
	Card = query.Card
	// CardOrder là một đơn trên thẻ khách.
	CardOrder = query.CardOrder
)

// ErrInvalidStatus là lỗi trạng thái hội thoại lạ.
var ErrInvalidStatus = assign.ErrInvalidStatus

// List trả một trang hội thoại trên các Page actor được đọc khớp f, mới nhất trước; con trỏ trang sau nil khi hết.
func List(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, f ListFilter) ([]ListRow, *PageCursor, error) {
	qf := query.Filter{
		PageID: f.PageID, Status: f.Status, TagID: f.TagID, Kind: f.Kind, Unread: f.Unread, Q: f.Q,
		UpdatedSince: f.UpdatedSince, After: f.After, Limit: f.Limit,
	}
	switch f.Assignee {
	case "":
	case AssigneeMe:
		me := actor.UserID
		qf.Assignee.UserID = &me
	case AssigneeNone:
		qf.Assignee.None = true
	default:
		id, err := uuid.Parse(f.Assignee)
		if err != nil {
			return nil, nil, fmt.Errorf("bộ lọc người phụ trách: %w", ErrInvalidInput)
		}
		qf.Assignee.UserID = &id
	}
	return query.List(ctx, tx, actor, qf) //nolint:wrapcheck // query đã bọc
}

// CountConversations trả số đếm của hộp thư cho actor (Page được đọc).
func CountConversations(ctx context.Context, tx pgx.Tx, actor identity.AuthUser) (Counts, error) {
	return query.CountConversations(ctx, tx, actor) //nolint:wrapcheck // query đã bọc
}

// ListMessages trả một trang tin của hội thoại id (cần conversation.read trên Page): after → tin mới sau mốc; ngược
// lại tin trước before (nil: mới nhất). Gửi cả hai → ErrInvalidInput.
func ListMessages(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, id uuid.UUID, before, after *PageCursor,
	limit int,
) (MessagePage, error) {
	if before != nil && after != nil {
		return MessagePage{}, fmt.Errorf("trang tin: before và after cùng lúc: %w", ErrInvalidInput)
	}
	return query.Messages(ctx, tx, actor, id, before, after, limit) //nolint:wrapcheck // query đã bọc
}

// CustomerCard trả thẻ khách của hội thoại id (cần conversation.read trên Page; SĐT, gợi ý gộp chỉ khi có
// customer.read).
func CustomerCard(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, id uuid.UUID) (Card, error) {
	return query.CustomerCard(ctx, tx, actor, id) //nolint:wrapcheck // query đã bọc
}

// SetStatus đổi trạng thái hội thoại id khi expectedVersion khớp (cần conversation.reply trên Page).
func (w Writer) SetStatus(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, id uuid.UUID, status string,
	expectedVersion int32,
) error {
	return w.assign().SetStatus(ctx, tx, actor, id, status, expectedVersion) //nolint:wrapcheck // assign đã bọc
}

// MarkRead đánh dấu hội thoại id đã đọc tới tin upTo (cần conversation.read trên Page; không tăng version).
func (w Writer) MarkRead(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, id, upTo uuid.UUID) error {
	return w.assign().MarkRead(ctx, tx, actor, id, upTo) //nolint:wrapcheck // assign đã bọc
}
