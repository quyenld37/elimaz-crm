package query

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
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// permCustomerRead: người có quyền này mới thấy SĐT và gợi ý gộp trên thẻ khách.
const permCustomerRead identity.Permission = "customer.read"

const (
	recentOrders = 5
	maxDrafts    = 20
)

// CardOrder là một đơn trên thẻ khách (số tiền đọc nguyên từ ord.orders_v, int64 đồng).
type CardOrder struct {
	ID             uuid.UUID
	Code, Status   string
	GrandTotal     int64
	CreatedAt      time.Time
	DraftExpiresAt *time.Time
	Version        int64
}

// Card là thẻ khách cạnh khung chat: hồ sơ gốc và chỉ số nguyên của customer.Get (không tính lại, C3), nguồn tên, cờ
// không liên hệ, SĐT của nhóm và gợi ý gộp (chỉ khi người xem có customer.read), đơn gần đây và đơn nháp của mọi hồ sơ
// cùng gốc.
type Card struct {
	Customer        customer.Customer
	NameSource      string
	DoNotContact    bool
	Phones          []logging.Phone
	RecentOrders    []CardOrder
	OpenDrafts      []CardOrder
	MergeSuggestion *uuid.UUID
}

// ConversationRef trả Page, khách, loại, bài viết của hội thoại id khi u có conversation.read trên Page; không có → ErrNotFound.
func ConversationRef(ctx context.Context, tx pgx.Tx, u identity.AuthUser, id uuid.UUID,
) (store.GetOrderContextRowRow, error) {
	r, err := store.New(tx).GetOrderContextRow(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, access.ErrNotFound
	}
	if err != nil {
		return r, fmt.Errorf("đọc hội thoại: %w", err)
	}
	if err := access.Require(u, assign.PermRead, r.ChannelAccountID); err != nil {
		return r, err //nolint:wrapcheck // sentinel của module (404)
	}
	return r, nil
}

// CustomerCard trả thẻ khách của hội thoại id (cần conversation.read trên Page của hội thoại).
func CustomerCard(ctx context.Context, tx pgx.Tx, u identity.AuthUser, id uuid.UUID) (Card, error) {
	conv, err := ConversationRef(ctx, tx, u, id)
	if err != nil {
		return Card{}, err
	}
	prof, err := customer.Get(ctx, tx, conv.CustomerID)
	if err != nil {
		return Card{}, fmt.Errorf("đọc hồ sơ khách: %w", err)
	}
	dnc, err := customer.IsDoNotContact(ctx, tx, conv.CustomerID)
	if err != nil {
		return Card{}, fmt.Errorf("đọc cờ không liên hệ: %w", err)
	}
	q := store.New(tx)
	source, err := q.CustomerNameSource(ctx, prof.ID)
	if err != nil {
		return Card{}, fmt.Errorf("đọc nguồn tên khách: %w", err)
	}
	c := Card{Customer: prof.Customer, NameSource: source, DoNotContact: dnc}
	if c.RecentOrders, err = cardOrders(ctx, q, prof.MemberIDs, false); err != nil {
		return Card{}, err
	}
	if c.OpenDrafts, err = cardOrders(ctx, q, prof.MemberIDs, true); err != nil {
		return Card{}, err
	}
	if !identity.Can(u, permCustomerRead, nil) {
		return c, nil
	}
	c.Phones = []logging.Phone{}
	for _, idn := range prof.Identities {
		if idn.Kind == customer.IdentityPhone {
			c.Phones = append(c.Phones, logging.Phone(idn.Value))
		}
	}
	cands, err := customer.MergeCandidates(ctx, tx, prof.ID)
	if err != nil {
		return Card{}, fmt.Errorf("đọc gợi ý gộp khách: %w", err)
	}
	if len(cands) > 0 {
		c.MergeSuggestion = &cands[0].RootID
	}
	return c, nil
}

func cardOrders(ctx context.Context, q *store.Queries, members []uuid.UUID, drafts bool) ([]CardOrder, error) {
	var rows []store.RecentGroupOrdersRow
	if drafts {
		d, err := q.GroupDraftOrders(ctx, store.GroupDraftOrdersParams{Members: members, Lim: maxDrafts})
		if err != nil {
			return nil, fmt.Errorf("đọc đơn nháp của khách: %w", err)
		}
		for _, r := range d {
			rows = append(rows, store.RecentGroupOrdersRow(r))
		}
	} else {
		var err error
		if rows, err = q.RecentGroupOrders(ctx, store.RecentGroupOrdersParams{Members: members, Lim: recentOrders}); err != nil {
			return nil, fmt.Errorf("đọc đơn gần đây của khách: %w", err)
		}
	}
	out := make([]CardOrder, 0, len(rows))
	for _, r := range rows {
		out = append(out, CardOrder{
			ID: r.ID, Code: r.Code, Status: r.Status, GrandTotal: r.GrandTotal, CreatedAt: r.CreatedAt,
			DraftExpiresAt: assign.OptTime(r.DraftExpiresAt), Version: r.Version,
		})
	}
	return out, nil
}
