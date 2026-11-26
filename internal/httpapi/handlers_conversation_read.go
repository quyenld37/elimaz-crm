package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Đọc hộp chat (kế hoạch M4, Task 159; CH01, CH06, CH07, CH09, PQ04): danh sách hội thoại có bộ lọc, số đếm, chi tiết
// (dùng conversationDetail của Task 160), tin theo trang, thẻ khách, bối cảnh lên đơn; đánh dấu đã đọc, đổi trạng thái.
// Quyền theo Page kiểm lại trong module (không có → 404).

// ListConversations (GET /api/v1/conversations).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListConversations(ctx context.Context, params gen.ListConversationsParams) (gen.ListConversationsRes, error) {
	_, after, err := pageStart(ctx, params.Cursor)
	if err != nil {
		return nil, fmt.Errorf("danh sách hội thoại: %w", err)
	}
	f := conversation.ListFilter{
		Assignee: params.Assignee.Or(""), Unread: params.Unread.Or(false), Q: params.Q.Or(""), After: after,
		Limit: int(params.Limit.Or(0)),
	}
	if v, ok := params.PageId.Get(); ok {
		f.PageID = &v
	}
	if v, ok := params.TagId.Get(); ok {
		f.TagID = &v
	}
	if v, ok := params.Status.Get(); ok {
		f.Status = string(v)
	}
	if v, ok := params.Kind.Get(); ok {
		f.Kind = string(v)
	}
	if v, ok := params.UpdatedSince.Get(); ok {
		f.UpdatedSince = &v
	}
	var (
		rows []conversation.ListRow
		next *conversation.PageCursor
	)
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		rows, next, err = conversation.List(ctx, tx, u, f)
		return err //nolint:wrapcheck // conversation đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc danh sách hội thoại: %w", err)
	}
	out := &gen.ConversationPage{Items: make([]gen.ConversationSummary, 0, len(rows))}
	for _, r := range rows {
		names := map[uuid.UUID]string{}
		if r.AssigneeID != nil {
			names[*r.AssigneeID] = r.AssigneeName
		}
		out.Items = append(out.Items, customerConversationOut(r.CustomerConversation, names))
	}
	if next != nil {
		out.NextCursor = gen.NewOptString(next.String())
	}
	return out, nil
}

// CountConversations (GET /api/v1/conversations/counts).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CountConversations(ctx context.Context) (gen.CountConversationsRes, error) {
	var c conversation.Counts
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		c, err = conversation.CountConversations(ctx, tx, u)
		return err //nolint:wrapcheck // conversation đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đếm hội thoại: %w", err)
	}
	out := &gen.ConversationCounts{
		ByPage: make([]gen.ConversationPageCount, 0, len(c.ByPage)), Mine: c.Mine,
		Unassigned: c.Unassigned,
	}
	for _, p := range c.ByPage {
		out.ByPage = append(out.ByPage, gen.ConversationPageCount{PageId: p.PageID, Unread: p.Unread, Open: p.Open})
	}
	return out, nil
}

// GetConversation (GET /api/v1/conversations/{id}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetConversation(ctx context.Context, params gen.GetConversationParams) (gen.GetConversationRes, error) {
	var out *gen.ConversationDetail
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		out, err = h.conversationDetail(ctx, tx, u, params.ID)
		return err
	}); err != nil {
		return nil, fmt.Errorf("đọc chi tiết hội thoại: %w", err)
	}
	return out, nil
}

// ListMessages (GET /api/v1/conversations/{id}/messages).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListMessages(ctx context.Context, params gen.ListMessagesParams) (gen.ListMessagesRes, error) {
	_, before, err := pageStart(ctx, params.Before)
	if err != nil {
		return nil, fmt.Errorf("trang tin: %w", err)
	}
	_, after, err := pageStart(ctx, params.After)
	if err != nil {
		return nil, fmt.Errorf("trang tin: %w", err)
	}
	var p conversation.MessagePage
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		p, err = conversation.ListMessages(ctx, tx, u, params.ID, before, after, int(params.Limit.Or(0)))
		return err //nolint:wrapcheck // conversation đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc tin của hội thoại: %w", err)
	}
	out := &gen.MessagePage{Items: make([]gen.ChatMessage, 0, len(p.Items))}
	for _, m := range p.Items {
		out.Items = append(out.Items, chatHistoryOut(m))
	}
	if p.Older != nil {
		out.OlderCursor = gen.NewOptString(p.Older.String())
	}
	if p.Newer != nil {
		out.NewerCursor = gen.NewOptString(p.Newer.String())
	}
	return out, nil
}

// chatHistoryOut đổi một tin của lịch sử sang hợp đồng (nội dung, tên người gửi là dữ liệu cá nhân: chỉ vào thân trả về).
func chatHistoryOut(m conversation.ChatMessage) gen.ChatMessage {
	out := gen.ChatMessage{
		ID: m.ID, ConversationId: m.ConversationID, Direction: gen.MessageDirection(m.Direction),
		Kind: gen.MessageKind(m.Kind), Origin: gen.MessageOrigin(m.Origin),
		Attachments: make([]gen.ChatAttachment, 0, len(m.Attachments)), IsHidden: m.IsHidden, Removed: m.Removed,
		EditedCount: m.EditedCount, SentAt: m.SentAt,
	}
	if m.Body != "" {
		out.Body = gen.NewOptString(m.Body)
	}
	if m.IsHidden && m.HiddenReason != "" {
		out.HiddenReason = gen.NewOptChatMessageHiddenReason(gen.ChatMessageHiddenReason(m.HiddenReason))
	}
	for _, a := range m.Attachments {
		out.Attachments = append(out.Attachments, historyAttachmentOut(a))
	}
	if len(m.QuickReplies) > 0 {
		out.QuickReplies = quickRepliesOut(m.QuickReplies)
	}
	if m.SenderID != nil {
		out.Sender = gen.NewOptActor(gen.Actor{ID: *m.SenderID, Name: m.SenderStaffName})
	}
	if m.SenderName != "" {
		out.SenderName = gen.NewOptString(m.SenderName)
	}
	if m.SendStatus != "" {
		out.SendStatus = gen.NewOptMessageSendStatus(gen.MessageSendStatus(m.SendStatus))
	}
	if m.FailReason != "" {
		out.FailReason = gen.NewOptString(m.FailReason)
	}
	if m.PrivateReply != nil {
		pr := gen.ChatPrivateReplyRef{State: m.PrivateReply.State}
		if m.PrivateReply.MessageID != nil {
			pr.MessageId = gen.NewOptUUID(*m.PrivateReply.MessageID)
		}
		out.PrivateReply = gen.NewOptChatPrivateReplyRef(pr)
	}
	if m.ParentID != nil {
		out.ParentMessageId = gen.NewOptUUID(*m.ParentID)
	}
	if m.DeliveredAt != nil {
		out.DeliveredAt = gen.NewOptDateTime(*m.DeliveredAt)
	}
	if m.ReadAt != nil {
		out.ReadAt = gen.NewOptDateTime(*m.ReadAt)
	}
	return out
}

// historyAttachmentOut: tệp đính kèm chỉ có mã tệp, loại, trạng thái, tên (đường dẫn lấy bằng getAttachmentUrl).
func historyAttachmentOut(a conversation.MessageAttachment) gen.ChatAttachment {
	return gen.ChatAttachment{Type: a.Type, Status: a.Status, FileId: optUUIDOut(a.FileID), Name: nonEmpty(a.Name)}
}

// GetConversationCustomerCard (GET /api/v1/conversations/{id}/customer-card).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetConversationCustomerCard(ctx context.Context, params gen.GetConversationCustomerCardParams,
) (gen.GetConversationCustomerCardRes, error) {
	var c conversation.Card
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		c, err = conversation.CustomerCard(ctx, tx, u, params.ID)
		return err //nolint:wrapcheck // conversation đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc thẻ khách: %w", err)
	}
	info := cardInfoOut(c)
	out := &gen.ConversationCustomerCard{
		Customer: info, RecentOrders: chatOrdersOut(c.RecentOrders),
		OpenDrafts: chatOrdersOut(c.OpenDrafts),
	}
	if c.MergeSuggestion != nil {
		out.MergeSuggestionCustomerId = gen.NewOptUUID(*c.MergeSuggestion)
	}
	return out, nil
}

// cardInfoOut là thẻ khách rút gọn của hội thoại (khung chat, bối cảnh chốt comment live).
func cardInfoOut(c conversation.Card) gen.ChatCustomerCardInfo {
	st := c.Customer.Stats
	info := gen.ChatCustomerCardInfo{
		ID: c.Customer.ID, RootId: c.Customer.RootID, Name: string(c.Customer.FullName), NameSource: c.NameSource,
		DoNotContact: c.DoNotContact, OrderCount: int32(st.OrderCount), DeliveredCount: int32(st.DeliveredCount), //nolint:gosec // số đơn
		ReturnedCount: int32(st.ReturnedCount), ReturnRateBp: int32(st.ReturnRateBP), //nolint:gosec // ≤ 10000
		TotalSpent: gen.Money(st.TotalSpent),
	}
	if st.LastOrderAt != nil {
		info.LastOrderAt = gen.NewOptDateTime(*st.LastOrderAt)
	}
	if c.Phones != nil {
		info.Phones = make([]string, 0, len(c.Phones))
		for _, p := range c.Phones {
			info.Phones = append(info.Phones, string(p))
		}
	}
	return info
}

func chatOrdersOut(orders []conversation.CardOrder) []gen.ChatOrderRef {
	out := make([]gen.ChatOrderRef, 0, len(orders))
	for _, o := range orders {
		r := gen.ChatOrderRef{
			ID: o.ID, Code: o.Code, Status: o.Status, GrandTotal: gen.Money(o.GrandTotal),
			CreatedAt: o.CreatedAt, Version: o.Version,
		}
		if o.DraftExpiresAt != nil {
			r.DraftExpiresAt = gen.NewOptDateTime(*o.DraftExpiresAt)
		}
		out = append(out, r)
	}
	return out
}

// GetConversationOrderContext (GET /api/v1/conversations/{id}/order-context).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetConversationOrderContext(ctx context.Context, params gen.GetConversationOrderContextParams,
) (gen.GetConversationOrderContextRes, error) {
	var c conversation.OrderContextResult
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		c, err = conversation.OrderContext(ctx, tx, u, params.ID)
		return err //nolint:wrapcheck // conversation đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc bối cảnh lên đơn: %w", err)
	}
	out := &gen.ConversationOrderContext{
		ChannelAccountId: c.ChannelAccountID, ConversationId: c.ConversationID,
		CustomerId: gen.NewOptUUID(c.CustomerID), SellerId: c.SellerID,
	}
	if c.TrafficSourceID != nil {
		out.TrafficSourceId = gen.NewOptNilUUID(*c.TrafficSourceID)
	} else {
		out.TrafficSourceId.SetToNull()
	}
	return out, nil
}

// MarkConversationRead (POST /api/v1/conversations/{id}/read).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) MarkConversationRead(ctx context.Context, req *gen.MarkConversationReadRequest,
	params gen.MarkConversationReadParams,
) (gen.MarkConversationReadRes, error) {
	if _, err := h.conversationChange(ctx, http.StatusNoContent, "đánh dấu đã đọc",
		func(tx pgx.Tx, w conversation.Writer, u identity.AuthUser) (any, error) {
			return nil, w.MarkRead(ctx, tx, u, params.ID, req.UpToMessageId)
		}); err != nil {
		return nil, err
	}
	return &gen.MarkConversationReadNoContent{}, nil
}

// SetConversationStatus (POST /api/v1/conversations/{id}/status).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SetConversationStatus(ctx context.Context, req *gen.SetConversationStatusRequest,
	params gen.SetConversationStatusParams,
) (gen.SetConversationStatusRes, error) {
	return h.assignChange(ctx, "đổi trạng thái hội thoại", params.ID, func(tx pgx.Tx, w conversation.Writer, u identity.AuthUser) error {
		return w.SetStatus(ctx, tx, u, params.ID, string(req.Status), req.ExpectedVersion)
	})
}
