// Package query là phần đọc của hộp chat (Task 159): danh sách hội thoại có bộ lọc, số đếm, tin theo trang, thẻ khách.
// Quyền theo Page kiểm ở đây (access): danh sách lọc theo VisiblePages, đọc một hội thoại cần conversation.read trên
// Page của nó (không có → access.ErrNotFound, 404).
package query

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/assign"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/calls"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
)

const (
	// DefaultLimit là cỡ trang mặc định của danh sách hội thoại và tin.
	DefaultLimit = 50
	// MaxLimit là cỡ trang tối đa của danh sách hội thoại và tin.
	MaxLimit = 100
	// SinceSlack là khoảng lùi 30 giây của updatedSince để không sót dòng của transaction commit muộn (màn hình bỏ trùng theo id).
	SinceSlack = 30 * time.Second
	// searchLimit: số nhóm khách tối đa lấy từ customer.Search cho bộ lọc q.
	searchLimit = 100
)

// Assignee chọn người phụ trách của bộ lọc.
type Assignee struct {
	// None: chỉ hội thoại chưa ai nhận; UserID khác nil: chỉ của người đó; cả hai rỗng: không lọc.
	None   bool
	UserID *uuid.UUID
}

// Filter là bộ lọc danh sách hội thoại (mọi trường tùy chọn).
type Filter struct {
	PageID       *uuid.UUID
	Status       string
	TagID        *uuid.UUID
	Assignee     Assignee
	Kind         string
	Unread       bool
	Q            string
	UpdatedSince *time.Time
	After        *customer.ListCursor
	Limit        int
}

// Row là một dòng danh sách: hội thoại như danh sách của khách (Task 169) kèm tên người phụ trách.
type Row struct {
	calls.CustomerConversation
	AssigneeName string
}

// ListParams là bộ lọc đã giải (Page được xem, nhóm khách khớp q): đầu vào của câu SQL danh sách.
type ListParams struct {
	Filter
	// AllPages: mọi Page; ngược lại chỉ Pages.
	AllPages bool
	Pages    []uuid.UUID
	// Roots khác nil: chỉ hội thoại của khách thuộc các nhóm này.
	Roots []uuid.UUID
}

// List trả một trang hội thoại (mới nhất trước, keyset (last_message_at, id)) trên các Page u được đọc khớp f; con trỏ
// trang sau nil khi hết. f.Q → customer.Search (ngắn → customer.ErrQueryTooShort). Page ngoài quyền: rỗng.
func List(ctx context.Context, tx pgx.Tx, u identity.AuthUser, f Filter) ([]Row, *customer.ListCursor, error) {
	p, ok, err := resolve(ctx, tx, u, f)
	if err != nil || !ok {
		return []Row{}, nil, err
	}
	sql, args := BuildListSQL(p)
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc danh sách hội thoại: %w", err)
	}
	out, err := pgx.CollectRows(rows, scanRow)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc dòng hội thoại: %w", err)
	}
	var next *customer.ListCursor
	if len(out) > p.Limit {
		out = out[:p.Limit]
		last := out[p.Limit-1]
		next = &customer.ListCursor{CreatedAt: last.LastMessageAt, ID: last.ID}
	}
	if err := enrich(ctx, tx, out); err != nil {
		return nil, nil, err
	}
	return out, next, nil
}

// resolve giao Page lọc với Page u được đọc, kiểm nhãn của bộ lọc còn sống (BuildListSQL chỉ lọc theo dòng gắn) và tìm
// nhóm khách theo q; ok = false khi chắc chắn rỗng.
func resolve(ctx context.Context, tx pgx.Tx, u identity.AuthUser, f Filter) (ListParams, bool, error) {
	if f.Limit <= 0 {
		f.Limit = DefaultLimit
	}
	f.Limit = min(f.Limit, MaxLimit)
	p := ListParams{Filter: f}
	p.AllPages, p.Pages = access.VisiblePages(u)
	if f.PageID != nil {
		if !p.AllPages && !slices.Contains(p.Pages, *f.PageID) {
			return p, false, nil
		}
		p.AllPages, p.Pages = false, []uuid.UUID{*f.PageID}
	}
	if !p.AllPages && len(p.Pages) == 0 {
		return p, false, nil
	}
	if f.TagID != nil {
		alive, err := store.New(tx).TagAlive(ctx, *f.TagID)
		if err != nil {
			return p, false, fmt.Errorf("đọc nhãn của bộ lọc: %w", err)
		}
		if !alive {
			return p, false, nil
		}
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		hits, err := customer.Search(ctx, tx, q, searchLimit)
		if err != nil {
			return p, false, err //nolint:wrapcheck // sentinel của customer (query_too_short)
		}
		p.Roots = make([]uuid.UUID, 0, len(hits))
		for _, h := range hits {
			p.Roots = append(p.Roots, h.RootID)
		}
		if len(p.Roots) == 0 {
			return p, false, nil
		}
	}
	return p, true, nil
}

// listSelect: cột của hội thoại (tên khách, người phụ trách, bài viết đọc sau bằng ConversationRowExtras, để câu có
// bộ lọc chỉ chạm conversations và index của nó).
const listSelect = `SELECT c.id, c.channel_account_id, c.customer_id, c.kind, c.status, c.assignee_id, c.unread_count,
       c.last_message_at, c.last_snippet, c.post_id, c.version
FROM conversation.conversations AS c
WHERE (c.last_message_at, c.id) < ($1::timestamptz, $2::uuid)`

// BuildListSQL dựng câu danh sách và tham số của p (chỉ thêm điều kiện của bộ lọc có giá trị để planner chọn index
// theo Page + trạng thái, người phụ trách hay nhãn); lấy p.Limit + 1 dòng để biết còn trang sau.
func BuildListSQL(p ListParams) (string, []any) {
	at, id := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), uuid.Max
	if p.After != nil {
		at, id = p.After.CreatedAt, p.After.ID
	}
	args := []any{at, id}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	conds := []string{listSelect}
	if !p.AllPages {
		conds = append(conds, "c.channel_account_id = ANY("+arg(p.Pages)+"::uuid[])")
	}
	if p.Status != "" {
		conds = append(conds, "c.status = "+arg(p.Status))
	}
	if p.Kind != "" {
		conds = append(conds, "c.kind = "+arg(p.Kind))
	}
	switch {
	case p.Assignee.None:
		conds = append(conds, "c.assignee_id IS NULL")
	case p.Assignee.UserID != nil:
		conds = append(conds, "c.assignee_id = "+arg(*p.Assignee.UserID)+"::uuid")
	}
	if p.Unread {
		conds = append(conds, "c.unread_count > 0")
	}
	if p.UpdatedSince != nil {
		conds = append(conds, "c.last_message_at >= "+arg(p.UpdatedSince.Add(-SinceSlack))+"::timestamptz")
	}
	if p.TagID != nil {
		conds = append(conds, `EXISTS (SELECT 1 FROM conversation.conversation_tag_links AS l
              WHERE l.tenant_id = c.tenant_id AND l.conversation_id = c.id AND l.tag_id = `+arg(*p.TagID)+`::uuid)`)
	}
	if p.Roots != nil {
		conds = append(conds, "c.customer_id IN (SELECT m.id FROM customer.customers_v AS m WHERE m.root_id = ANY("+
			arg(p.Roots)+"::uuid[]))")
	}
	lim := arg(int32(p.Limit + 1)) //nolint:gosec // ≤ MaxLimit + 1
	return strings.Join(conds, "\n  AND ") + "\nORDER BY c.last_message_at DESC, c.id DESC\nLIMIT " + lim, args
}

func scanRow(r pgx.CollectableRow) (Row, error) {
	var (
		out            Row
		assignee, post pgtype.UUID
		snippet        pgtype.Text
	)
	c := &out.CustomerConversation
	if err := r.Scan(&c.ID, &c.PageID, &c.CustomerID, &c.Kind, &c.Status, &assignee, &c.UnreadCount, &c.LastMessageAt,
		&snippet, &post, &c.Version); err != nil {
		return Row{}, err //nolint:wrapcheck // List bọc
	}
	c.AssigneeID, c.PostID = assign.PtrUUID(assignee), assign.PtrUUID(post)
	c.LastSnippet = snippet.String
	return out, nil
}

// enrich điền tên khách, người phụ trách, đoạn bài viết, nhãn còn sống và cờ có đơn nháp cho các dòng.
func enrich(ctx context.Context, tx pgx.Tx, rows []Row) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	q := store.New(tx)
	tags, err := q.ConversationTagsOf(ctx, ids)
	if err != nil {
		return fmt.Errorf("đọc nhãn của hội thoại: %w", err)
	}
	byConv := map[uuid.UUID][]calls.Tag{}
	for _, t := range tags {
		byConv[t.ConversationID] = append(byConv[t.ConversationID], calls.Tag{
			ID: t.ID, Name: t.Name, Color: t.Color, Sort: t.Sort, Version: t.Version,
		})
	}
	extras, err := q.ConversationRowExtras(ctx, ids)
	if err != nil {
		return fmt.Errorf("đọc tên, bài viết, đơn nháp của hội thoại: %w", err)
	}
	byID := make(map[uuid.UUID]store.ConversationRowExtrasRow, len(extras))
	for _, x := range extras {
		byID[x.ID] = x
	}
	for i := range rows {
		r := &rows[i]
		x := byID[r.ID]
		r.CustomerName, r.AssigneeName, r.PostSnippet, r.HasOpenDraft = x.CustomerName, x.AssigneeName,
			x.PostSnippet.String, x.HasOpenDraft
		r.Tags = byConv[r.ID]
		if r.Tags == nil {
			r.Tags = []calls.Tag{}
		}
	}
	return nil
}
