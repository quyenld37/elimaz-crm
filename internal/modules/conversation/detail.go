package conversation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/assign"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Conversation là một hội thoại (dòng như danh sách hội thoại của khách: khách, nhãn còn sống, bài viết, đơn nháp đang
// mở) kèm tên người phụ trách, tin khách cuối, cờ khách chặn Page — dữ liệu của chi tiết hội thoại (dùng chung cho
// nhận/giao ở Task 160 và chi tiết hội thoại của Task 159).
type Conversation struct {
	CustomerConversation
	AssigneeName          string
	LastCustomerMessageAt *time.Time
	BlockedByUser         bool
}

// Get trả hội thoại id (cần conversation.read trên Page; không có hay không có quyền → ErrNotFound).
func Get(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, id uuid.UUID) (Conversation, error) {
	q := store.New(tx)
	r, err := q.GetConversationRow(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, ErrNotFound
	}
	if err != nil {
		return Conversation{}, fmt.Errorf("đọc hội thoại: %w", err)
	}
	if err := access.Require(actor, assign.PermRead, r.ChannelAccountID); err != nil {
		return Conversation{}, err //nolint:wrapcheck // sentinel của module
	}
	tags, err := q.ListConversationTagsOf(ctx, id)
	if err != nil {
		return Conversation{}, fmt.Errorf("đọc nhãn của hội thoại: %w", err)
	}
	c := Conversation{
		CustomerConversation: CustomerConversation{
			ID: r.ID, PageID: r.ChannelAccountID, CustomerID: r.CustomerID, CustomerName: r.CustomerName, Kind: r.Kind,
			Status: r.Status, AssigneeID: assign.PtrUUID(r.AssigneeID), Tags: make([]Tag, 0, len(tags)),
			UnreadCount: r.UnreadCount, LastMessageAt: r.LastMessageAt, LastSnippet: r.LastSnippet.String,
			PostID: assign.PtrUUID(r.PostID), PostSnippet: r.PostSnippet.String, HasOpenDraft: r.HasOpenDraft,
			NewLead: r.NewLead, Version: r.Version,
		},
		AssigneeName: r.AssigneeName, LastCustomerMessageAt: assign.OptTime(r.LastCustomerMessageAt), BlockedByUser: r.BlockedByUser,
	}
	c.SetPostCard(r.PostCreatedTime, r.PostPermalink, r.PostImageFileID)
	for _, t := range tags {
		c.Tags = append(c.Tags, Tag(t))
	}
	return c, nil
}
