package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// CreateCallLog (POST /api/v1/customers/{id}/calls, conversation.reply): ghi kết quả cuộc gọi khách (Q14, CH17);
// có conversationId → tin hệ thống trong hội thoại; callback → nhắc gọi lại cho người gọi lúc callbackAt. Khách không
// có → 404; hội thoại trên Page không được trả lời → 404; callbackAt thiếu/thừa, hẹn gọi lại không gắn hội thoại, hội
// thoại hay đơn không thuộc khách → 400. 201 kèm cuộc gọi.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateCallLog(ctx context.Context, req *gen.CreateCallLogRequest, params gen.CreateCallLogParams,
) (gen.CreateCallLogRes, error) {
	in := conversation.CallInput{
		ConversationID: optUUID(req.ConversationId), OrderID: optUUID(req.OrderId), Outcome: string(req.Outcome),
		Note: req.Note.Or(""), RequestID: RequestID(ctx),
	}
	if at, ok := req.CallbackAt.Get(); ok {
		in.CallbackAt = &at
	}
	calls := conversation.Calls{Bus: h.d.Realtime}
	out, err := h.refusableWrite(ctx, http.StatusCreated, isConversationRefusal, "ghi cuộc gọi",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			c, err := calls.Record(ctx, tx, u, params.ID, in, h.now())
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel
			}
			names, err := staffNames(ctx, tx, []uuid.UUID{c.CreatedBy})
			if err != nil {
				return nil, err
			}
			return callLogOut(c, names), nil
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.CallLog), nil //nolint:forcetypeassert // fn trên trả *gen.CallLog
}

// ListCustomerCalls (GET /api/v1/customers/{id}/calls, conversation.read): cuộc gọi của cả nhóm khách (mọi hồ sơ đã
// gộp), chỉ cuộc gọi không gắn hội thoại hoặc gắn hội thoại trên Page được xem; mới nhất trước.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListCustomerCalls(ctx context.Context, params gen.ListCustomerCallsParams) (gen.ListCustomerCallsRes, error) {
	u, after, err := pageStart(ctx, params.Cursor)
	if err != nil {
		return nil, fmt.Errorf("cuộc gọi của khách: %w", err)
	}
	var (
		items []conversation.CallLog
		next  *conversation.PageCursor
		names map[uuid.UUID]string
	)
	err = h.catalogRead(ctx, func(tx pgx.Tx) error {
		var err error
		items, next, err = conversation.ListCustomerCalls(ctx, tx, u, params.ID, after, int(params.Limit.Or(0)))
		if err != nil {
			return err //nolint:wrapcheck // bọc ở dưới
		}
		names, err = actorNamesOf(ctx, tx, items, func(c conversation.CallLog) *uuid.UUID { return &c.CreatedBy })
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("cuộc gọi của khách: %w", err)
	}
	out := &gen.CallLogPage{Items: make([]gen.CallLog, 0, len(items))}
	for _, c := range items {
		out.Items = append(out.Items, *callLogOut(c, names))
	}
	if next != nil {
		out.NextCursor = gen.NewOptString(next.String())
	}
	return out, nil
}

// ListCustomerConversations (GET /api/v1/customers/{id}/conversations, conversation.read): hội thoại của cả nhóm
// khách trên các Page được xem, tin cuối mới nhất trước.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListCustomerConversations(ctx context.Context, params gen.ListCustomerConversationsParams,
) (gen.ListCustomerConversationsRes, error) {
	u, after, err := pageStart(ctx, params.Cursor)
	if err != nil {
		return nil, fmt.Errorf("hội thoại của khách: %w", err)
	}
	var (
		items []conversation.CustomerConversation
		next  *conversation.PageCursor
		names map[uuid.UUID]string
	)
	err = h.catalogRead(ctx, func(tx pgx.Tx) error {
		var err error
		items, next, err = conversation.ListCustomerConversations(ctx, tx, u, params.ID, after, int(params.Limit.Or(0)))
		if err != nil {
			return err //nolint:wrapcheck // bọc ở dưới
		}
		names, err = actorNamesOf(ctx, tx, items, func(c conversation.CustomerConversation) *uuid.UUID { return c.AssigneeID })
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("hội thoại của khách: %w", err)
	}
	out := &gen.ConversationPage{Items: make([]gen.ConversationSummary, 0, len(items))}
	for _, c := range items {
		out.Items = append(out.Items, customerConversationOut(c, names))
	}
	if next != nil {
		out.NextCursor = gen.NewOptString(next.String())
	}
	return out, nil
}

// pageStart trả người gọi và con trỏ trang đã đọc (nil: trang đầu); con trỏ hỏng → customer.ErrInvalidInput (400).
func pageStart(ctx context.Context, cursor gen.OptString) (identity.AuthUser, *conversation.PageCursor, error) {
	u, ok := authUser(ctx)
	if !ok {
		return identity.AuthUser{}, nil, errNoAuthUser
	}
	s, ok := cursor.Get()
	if !ok {
		return u, nil, nil
	}
	c, err := customer.ParseListCursor(s)
	if err != nil {
		return identity.AuthUser{}, nil, err //nolint:wrapcheck // sentinel, bên gọi bọc
	}
	return u, &c, nil
}

func callLogOut(c conversation.CallLog, names map[uuid.UUID]string) *gen.CallLog {
	out := &gen.CallLog{
		ID: c.ID, CustomerId: c.CustomerID, Outcome: gen.CallOutcome(c.Outcome),
		CreatedBy: gen.Actor{ID: c.CreatedBy, Name: names[c.CreatedBy]}, CreatedAt: c.CreatedAt,
	}
	if c.ConversationID != nil {
		out.ConversationId = gen.NewOptUUID(*c.ConversationID)
	}
	if c.OrderID != nil {
		out.OrderId = gen.NewOptUUID(*c.OrderID)
	}
	if c.Note != "" {
		out.Note = gen.NewOptString(c.Note)
	}
	if c.CallbackAt != nil {
		out.CallbackAt = gen.NewOptDateTime(*c.CallbackAt)
	}
	return out
}

func customerConversationOut(c conversation.CustomerConversation, names map[uuid.UUID]string) gen.ConversationSummary {
	out := gen.ConversationSummary{
		ID: c.ID, PageId: c.PageID, Kind: gen.ConversationKind(c.Kind), Status: gen.ConversationStatus(c.Status),
		Customer: gen.ChatCustomerRef{ID: c.CustomerID, Name: c.CustomerName}, Tags: make([]gen.ConversationTag, 0, len(c.Tags)),
		UnreadCount: c.UnreadCount, LastMessageAt: c.LastMessageAt, HasOpenDraft: c.HasOpenDraft, Version: c.Version,
	}
	if c.AssigneeID != nil {
		out.Assignee = gen.NewOptActor(gen.Actor{ID: *c.AssigneeID, Name: names[*c.AssigneeID]})
	}
	for _, t := range c.Tags {
		out.Tags = append(out.Tags, gen.ConversationTag{ID: t.ID, Name: t.Name, Color: t.Color, Sort: t.Sort, Version: t.Version})
	}
	if c.LastSnippet != "" {
		out.LastSnippet = gen.NewOptString(c.LastSnippet)
	}
	if c.PostID != nil {
		post := gen.ChatPostRef{ID: *c.PostID}
		if c.PostSnippet != "" {
			post.Snippet = gen.NewOptString(c.PostSnippet)
		}
		out.Post = gen.NewOptChatPostRef(post)
	}
	return out
}
