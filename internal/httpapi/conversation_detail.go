package httpapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// errNoChannelAdapter: Page của hội thoại thuộc kênh không có adapter trong Deps.Channels (lỗi nối, 500).
var errNoChannelAdapter = errors.New("httpapi: không có adapter của kênh")

// conversationDetail đọc chi tiết hội thoại id cho u trong tx (Task 160; Task 159 dùng lại cho getConversation):
// hội thoại (khách, người phụ trách, nhãn, bài viết, đơn nháp đang mở), Page, chính sách gửi mục đích message lúc
// h.now() (adapter của kênh, không gọi mạng), thẻ nhân viên, khách chặn Page, cờ không liên hệ.
func (h *handler) conversationDetail(ctx context.Context, tx pgx.Tx, u identity.AuthUser, id uuid.UUID) (*gen.ConversationDetail, error) {
	c, err := conversation.Get(ctx, tx, u, id)
	if err != nil {
		return nil, err //nolint:wrapcheck // sentinel của module
	}
	page, err := channel.Account(ctx, tx, c.PageID)
	if err != nil {
		return nil, err //nolint:wrapcheck // channel đã bọc
	}
	var adapter channel.MessagingAdapter
	if h.d.Channels != nil {
		adapter, _ = h.d.Channels.Get(page.Channel)
	}
	if adapter == nil {
		return nil, fmt.Errorf("chi tiết hội thoại, kênh %s: %w", page.Channel, errNoChannelAdapter)
	}
	pol := adapter.SendPolicy(channel.ConversationState{
		Purpose: channel.PurposeMessage, Kind: c.Kind, LastCustomerMessageAt: c.LastCustomerMessageAt,
		BlockedByUser: c.BlockedByUser, ReadOnly: page.SendMode == channel.SendModeReadOnly,
		HumanAgentEnabled: page.HumanAgentEnabled,
	}, h.now())
	dnc, err := customer.IsDoNotContact(ctx, tx, c.CustomerID)
	if err != nil {
		return nil, fmt.Errorf("đọc cờ không liên hệ: %w", err)
	}
	return &gen.ConversationDetail{
		Conversation: conversationSummaryOut(c), Page: *channelAccountOut(page), SendPolicy: sendPolicyOut(pol),
		HumanAgentAvailable: page.HumanAgentEnabled, BlockedByUser: c.BlockedByUser, DoNotContact: dnc,
	}, nil
}

func conversationSummaryOut(c conversation.Conversation) gen.ConversationSummary {
	names := map[uuid.UUID]string{}
	if c.AssigneeID != nil {
		names[*c.AssigneeID] = c.AssigneeName
	}
	return customerConversationOut(c.CustomerConversation, names)
}

func conversationTagOut(t conversation.Tag) gen.ConversationTag {
	return gen.ConversationTag{ID: t.ID, Name: t.Name, Color: t.Color, Sort: t.Sort, Version: t.Version}
}

func sendPolicyOut(p channel.SendPolicy) gen.SendPolicy {
	o := gen.SendPolicy{Allowed: p.Allowed}
	if p.Mode != "" {
		o.Mode = gen.NewOptSendMode(gen.SendMode(p.Mode))
	}
	if p.Until != nil {
		o.Until = gen.NewOptDateTime(*p.Until)
	}
	if p.Remaining != nil {
		o.Remaining = gen.NewOptInt32(int32(*p.Remaining)) //nolint:gosec // số lần còn lại nhỏ
	}
	if p.Reason != "" {
		o.Reason = gen.NewOptSendDenyReason(gen.SendDenyReason(p.Reason))
	}
	return o
}
