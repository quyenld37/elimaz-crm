package query

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// PageCount là số đếm của một Page: Unread = số hội thoại mở/chờ còn tin chưa đọc, Open = số hội thoại đang mở.
type PageCount struct {
	PageID       uuid.UUID
	Unread, Open int32
}

// Counts là số đếm của hộp thư trên các Page u được đọc: theo Page (chỉ Page có hội thoại mở/chờ), hội thoại đang mở
// của u, đang mở chưa ai nhận.
type Counts struct {
	ByPage           []PageCount
	Mine, Unassigned int32
}

// CountConversations trả số đếm của hộp thư cho u.
func CountConversations(ctx context.Context, tx pgx.Tx, u identity.AuthUser) (Counts, error) {
	all, pages := access.VisiblePages(u)
	out := Counts{ByPage: []PageCount{}}
	if !all && len(pages) == 0 {
		return out, nil
	}
	if pages == nil {
		pages = []uuid.UUID{}
	}
	q := store.New(tx)
	rows, err := q.CountConversationsByPage(ctx, store.CountConversationsByPageParams{AllPages: all, Pages: pages})
	if err != nil {
		return Counts{}, fmt.Errorf("đếm hội thoại theo Page: %w", err)
	}
	for _, r := range rows {
		out.ByPage = append(out.ByPage, PageCount{PageID: r.ChannelAccountID, Unread: r.Unread, Open: r.Open})
	}
	m, err := q.CountMineAndUnassigned(ctx, store.CountMineAndUnassignedParams{UserID: u.UserID, AllPages: all, Pages: pages})
	if err != nil {
		return Counts{}, fmt.Errorf("đếm hội thoại của tôi: %w", err)
	}
	out.Mine, out.Unassigned = m.Mine, m.Unassigned
	return out, nil
}
