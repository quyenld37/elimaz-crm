package live

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/automation"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// DraftItem là một dòng của đơn nháp: biến thể, tên chụp lúc đặt, số lượng.
type DraftItem struct {
	VariantID uuid.UUID
	Name      string
	Qty       int32
}

// Draft là đơn nháp của khách trong phiên live: mã, dòng, tổng, version (để thêm vào nháp), hạn nháp.
type Draft struct {
	OrderID    uuid.UUID
	Code       string
	Items      []DraftItem
	GrandTotal int64
	Version    int64
	ExpiresAt  *time.Time
}

// CommentContext là bối cảnh chốt một comment live: hội thoại comment (gói gốc đọc thẻ khách), đơn nháp khách đang có
// trong phiên, SĐT tách từ comment (dạng 0xxxxxxxxx), người bán của Page.
type CommentContext struct {
	ConversationID uuid.UUID
	OpenDrafts     []Draft
	Phones         []string
	SellerID       uuid.UUID
}

// Context trả bối cảnh chốt comment live commentID cho u (cần conversation.read trên Page; không có, không phải
// comment live → ErrNotFound), trong tx tenant của bên gọi; không khóa.
func Context(ctx context.Context, tx pgx.Tx, u identity.AuthUser, commentID uuid.UUID) (CommentContext, error) {
	q := store.New(tx)
	cm, err := q.GetLiveComment(ctx, commentID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return CommentContext{}, ErrNotFound
	case err != nil:
		return CommentContext{}, fmt.Errorf("đọc comment live: %w", err)
	}
	if err := access.Require(u, permRead, cm.ChannelAccountID); err != nil {
		return CommentContext{}, err //nolint:wrapcheck // sentinel của phần (404)
	}
	sid, err := IsLiveComment(ctx, tx, cm.ChannelAccountID, cm.PostExternalID)
	if err != nil {
		return CommentContext{}, err
	}
	if !cm.IsLive || cm.Direction != "in" || sid == nil {
		return CommentContext{}, ErrNotFound
	}
	root, err := q.CustomerRootOf(ctx, cm.CustomerID)
	if err != nil {
		return CommentContext{}, fmt.Errorf("đọc gốc khách của comment: %w", err)
	}
	drafts, err := openDrafts(ctx, q, root, *sid)
	if err != nil {
		return CommentContext{}, err
	}
	acc, err := channel.Account(ctx, tx, cm.ChannelAccountID)
	if err != nil {
		return CommentContext{}, err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	return CommentContext{
		ConversationID: cm.ConversationID, OpenDrafts: drafts, Phones: automation.ExtractPhones(cm.Body.String),
		SellerID: acc.SellerID,
	}, nil
}

// openDrafts trả đơn nháp còn draft của nhóm khách root có ghi nhận trong phiên session, kèm dòng; mới nhất trước.
func openDrafts(ctx context.Context, q *store.Queries, root, session uuid.UUID) ([]Draft, error) {
	rows, err := q.OpenDraftsOfRootInSession(ctx, store.OpenDraftsOfRootInSessionParams{RootID: root, LiveSessionID: session})
	if err != nil {
		return nil, fmt.Errorf("đọc đơn nháp của khách trong phiên: %w", err)
	}
	out := make([]Draft, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, 0, len(rows))
	at := make(map[uuid.UUID]int, len(rows))
	for _, r := range rows {
		d := Draft{OrderID: r.ID, Code: r.Code, GrandTotal: r.GrandTotal, Version: r.Version, Items: []DraftItem{}}
		if r.DraftExpiresAt.Valid {
			t := r.DraftExpiresAt.Time.UTC()
			d.ExpiresAt = &t
		}
		at[r.ID] = len(out)
		out = append(out, d)
		ids = append(ids, r.ID)
	}
	items, err := q.DraftItems(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("đọc dòng đơn nháp: %w", err)
	}
	for _, it := range items {
		i := at[it.OrderID]
		out[i].Items = append(out[i].Items, DraftItem{VariantID: it.VariantID, Name: it.NameSnapshot, Qty: it.Quantity})
	}
	return out, nil
}
