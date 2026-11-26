package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/assign"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// PrivateReplyNone là trạng thái tin riêng của comment chưa có tin riêng đang sống (chưa gửi, hay đã lỗi/hết hạn/bỏ:
// nhắn lại được).
const PrivateReplyNone = "none"

// Attachment là một tệp đính kèm của tin: chỉ mã tệp (khi đã chép về kho) và trạng thái; đường dẫn lấy bằng
// getAttachmentUrl (Task 155).
type Attachment struct {
	FileID       *uuid.UUID
	Type, Status string
	Name         string
}

// PrivateReply là tin riêng của một comment: State là PrivateReplyNone hay send_status của tin riêng đang sống.
type PrivateReply struct {
	State     string
	MessageID *uuid.UUID
}

// Message là một tin của hội thoại (Body, SenderName là dữ liệu cá nhân). PrivateReply chỉ có ở comment; ParentID là
// comment cha (comment trả lời trong luồng).
type Message struct {
	ID, ConversationID      uuid.UUID
	Direction, Kind, Origin string
	Body                    string
	Attachments             []Attachment
	SenderID                *uuid.UUID
	SenderStaffName         string
	SenderName              string
	SendStatus, FailReason  string
	IsHidden, Removed       bool
	EditedCount             int32
	PrivateReply            *PrivateReply
	ParentID                *uuid.UUID
	SentAt                  time.Time
	DeliveredAt, ReadAt     *time.Time
}

// MessagePage là một trang tin theo thứ tự thời gian. Older: còn tin cũ hơn (gửi làm before); Newer: mốc tin mới nhất
// của trang (gửi làm after để lấy tin mới; trang rỗng của after giữ nguyên mốc đã gửi).
type MessagePage struct {
	Items        []Message
	Older, Newer *customer.ListCursor
}

// ConversationPage trả Page của hội thoại id khi u có quyền p trên đó; không có hay không quyền → access.ErrNotFound.
func ConversationPage(ctx context.Context, tx pgx.Tx, u identity.AuthUser, p identity.Permission, id uuid.UUID,
) (uuid.UUID, error) {
	page, err := store.New(tx).GetConversationPage(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, access.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("đọc hội thoại: %w", err)
	}
	if err := access.Require(u, p, page); err != nil {
		return uuid.Nil, err //nolint:wrapcheck // sentinel của module (404)
	}
	return page, nil
}

// Messages trả một trang tin của hội thoại id (cần conversation.read trên Page): after khác nil → tin sau mốc, cũ trước;
// ngược lại tin trước before (nil: mới nhất), trả theo thứ tự thời gian. limit ≤ 0 → DefaultLimit, tối đa MaxLimit.
func Messages(ctx context.Context, tx pgx.Tx, u identity.AuthUser, id uuid.UUID, before, after *customer.ListCursor,
	limit int,
) (MessagePage, error) {
	if _, err := ConversationPage(ctx, tx, u, assign.PermRead, id); err != nil {
		return MessagePage{}, err
	}
	if limit <= 0 {
		limit = DefaultLimit
	}
	limit = min(limit, MaxLimit)
	q := store.New(tx)
	var (
		page MessagePage
		err  error
	)
	if after != nil {
		page, err = newer(ctx, q, id, *after, limit)
	} else {
		page, err = older(ctx, q, id, before, limit)
	}
	if err != nil {
		return MessagePage{}, err
	}
	if n := len(page.Items); n > 0 {
		page.Newer = &customer.ListCursor{CreatedAt: page.Items[n-1].SentAt, ID: page.Items[n-1].ID}
	}
	return page, nil
}

// newer trả tin sau mốc after, cũ trước; Newer mặc định là after (trang rỗng giữ nguyên mốc).
func newer(ctx context.Context, q *store.Queries, id uuid.UUID, after customer.ListCursor, limit int) (MessagePage, error) {
	rows, err := q.ListChatMessagesAfter(ctx, store.ListChatMessagesAfterParams{
		ConversationID: id, At: after.CreatedAt, MsgID: after.ID, Lim: int32(limit), //nolint:gosec // ≤ MaxLimit
	})
	if err != nil {
		return MessagePage{}, fmt.Errorf("đọc tin mới: %w", err)
	}
	page := MessagePage{Items: make([]Message, 0, len(rows)), Newer: &after}
	for _, r := range rows {
		m, err := messageOf(store.ListChatMessagesBeforeRow(r))
		if err != nil {
			return MessagePage{}, err
		}
		page.Items = append(page.Items, m)
	}
	return page, nil
}

// older trả tin trước mốc before (nil: mới nhất) theo thứ tự thời gian; Older khi còn tin cũ hơn.
func older(ctx context.Context, q *store.Queries, id uuid.UUID, before *customer.ListCursor, limit int) (MessagePage, error) {
	at, mid := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), uuid.Max
	if before != nil {
		at, mid = before.CreatedAt, before.ID
	}
	rows, err := q.ListChatMessagesBefore(ctx, store.ListChatMessagesBeforeParams{
		ConversationID: id, At: at, MsgID: mid, Lim: int32(limit + 1), //nolint:gosec // ≤ MaxLimit + 1
	})
	if err != nil {
		return MessagePage{}, fmt.Errorf("đọc tin: %w", err)
	}
	var page MessagePage
	if len(rows) > limit {
		rows = rows[:limit]
		page.Older = &customer.ListCursor{CreatedAt: rows[limit-1].SentAt, ID: rows[limit-1].ID}
	}
	page.Items = make([]Message, 0, len(rows))
	for _, r := range slices.Backward(rows) {
		m, err := messageOf(r)
		if err != nil {
			return MessagePage{}, err
		}
		page.Items = append(page.Items, m)
	}
	return page, nil
}

// storedAttachment là một mục của cột messages.attachments (đường vào Task 153, tải tệp Task 155).
type storedAttachment struct {
	Type   string  `json:"type"`
	Status string  `json:"status"`
	FileID *string `json:"fileId"`
	Name   string  `json:"name"`
}

func messageOf(r store.ListChatMessagesBeforeRow) (Message, error) {
	var stored []storedAttachment
	if err := json.Unmarshal(r.Attachments, &stored); err != nil {
		return Message{}, fmt.Errorf("đọc đính kèm của tin: %w", err)
	}
	m := Message{
		ID: r.ID, ConversationID: r.ConversationID, Direction: r.Direction, Kind: r.Kind, Origin: r.Origin,
		Body: r.Body.String, Attachments: make([]Attachment, 0, len(stored)), SenderID: assign.PtrUUID(r.SenderStaffID),
		SenderStaffName: r.SenderStaffName, SenderName: r.SenderName.String, SendStatus: r.SendStatus.String,
		FailReason: r.FailReason.String, IsHidden: r.IsHidden, Removed: r.Removed, EditedCount: r.EditedCount,
		ParentID: assign.PtrUUID(r.ParentID), SentAt: r.SentAt, DeliveredAt: assign.OptTime(r.DeliveredAt),
		ReadAt: assign.OptTime(r.ReadAt),
	}
	for _, a := range stored {
		att := Attachment{Type: a.Type, Status: a.Status, Name: a.Name}
		if a.FileID != nil {
			if id, err := uuid.Parse(*a.FileID); err == nil {
				att.FileID = &id
			}
		}
		m.Attachments = append(m.Attachments, att)
	}
	if r.Kind == kindComment {
		m.PrivateReply = &PrivateReply{State: PrivateReplyNone}
		if r.PrivateReplyID.Valid {
			m.PrivateReply = &PrivateReply{State: r.PrivateReplyStatus.String, MessageID: assign.PtrUUID(r.PrivateReplyID)}
		}
	}
	return m, nil
}

const kindComment = "comment"
