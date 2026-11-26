package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/queries"
)

// AddCustomerNote (POST /api/v1/customers/{id}/notes, customer.write): thêm ghi chú (chỉ ghi thêm) cho gốc của khách
// (Task M2-12); thân rỗng → 400 invalid_input. Trả ghi chú kèm tên người viết, 201.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) AddCustomerNote(ctx context.Context, req *gen.AddCustomerNoteRequest, params gen.AddCustomerNoteParams) (gen.AddCustomerNoteRes, error) {
	w := customer.Writer{Events: h.d.Events, Queue: queue.NewQueue(), Realtime: h.d.Realtime}
	return h.writeNote(ctx, isCustomerRefusal, "ghi chú khách", func(tx pgx.Tx, u identity.AuthUser) (writtenNote, error) {
		a := customer.Actor{Type: customer.ActorStaff, ID: u.UserID, RequestID: RequestID(ctx)}
		n, err := w.AddNote(ctx, tx, params.ID, req.Body, a, time.Now().UTC())
		if err != nil {
			return writtenNote{}, err //nolint:wrapcheck // sentinel của customer
		}
		return writtenNote{ID: n.ID, Body: string(n.Body), AuthorID: n.AuthorID, CreatedAt: n.CreatedAt}, nil
	})
}

// GetCustomerTimeline (GET /api/v1/customers/{id}/timeline, customer.read): dòng thời gian của cả nhóm khách, mới
// nhất trước, trang theo nextCursor; con trỏ hỏng → 400 invalid_input; khách không có hay của tenant khác → 404. Gộp
// thêm hội thoại và cuộc gọi (C7, Task 169) trên các Page người xem có conversation.read: lấy tối đa limit mục từ
// customer.Timeline và từ queries.ChatTimeline sau cùng con trỏ, gộp theo (at, kind, ref_id) giảm dần, cắt limit.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetCustomerTimeline(ctx context.Context, params gen.GetCustomerTimelineParams) (gen.GetCustomerTimelineRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var after *customer.TimelineCursor
	if s, ok := params.Cursor.Get(); ok {
		c, err := customer.ParseTimelineCursor(s)
		if err != nil {
			return nil, fmt.Errorf("dòng thời gian của khách: %w", err)
		}
		after = &c
	}
	limit := int(params.Limit.Or(0))
	if limit <= 0 {
		limit = customerTimelineLimit
	}
	limit = min(limit, customer.MaxListLimit)
	var (
		entries []timelineEntry
		more    bool
		names   map[uuid.UUID]string
	)
	err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		items, next, err := customer.Timeline(ctx, tx, params.ID, after, limit)
		if err != nil {
			return err //nolint:wrapcheck // lỗi của customer đã bọc
		}
		chat, err := chatTimeline(ctx, tx, u, params.ID, after, limit+1)
		if err != nil {
			return err
		}
		entries, more = mergeTimeline(items, chat, limit)
		more = more || next != nil
		names, err = actorNamesOf(ctx, tx, entries, func(e timelineEntry) *uuid.UUID { return e.ActorID })
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("dòng thời gian của khách: %w", err)
	}
	out := &gen.TimelinePage{Items: make([]gen.TimelineItem, 0, len(entries))}
	for _, e := range entries {
		out.Items = append(out.Items, timelineItemOut(e, names))
	}
	if more && len(entries) > 0 {
		last := entries[len(entries)-1]
		out.NextCursor = gen.NewOptString(customer.TimelineCursor{At: last.At, Kind: last.Kind, RefID: last.RefID}.String())
	}
	return out, nil
}

// customerTimelineLimit khớp mặc định của tham số limit trong openapi.yaml (cùng số với customer.Timeline).
const customerTimelineLimit = 50

// chatTimeline đọc tối đa limit mục hội thoại, cuộc gọi của cả nhóm khách id sau con trỏ after, chỉ trên các Page u có
// conversation.read (ScopesFor); không có quyền đó ở Page nào thì không có mục nào.
func chatTimeline(ctx context.Context, tx pgx.Tx, u identity.AuthUser, id uuid.UUID, after *customer.TimelineCursor,
	limit int,
) ([]queries.ChatTimelineRow, error) {
	root, err := customer.RootOf(ctx, tx, id)
	if err != nil {
		return nil, fmt.Errorf("đọc gốc khách: %w", err)
	}
	members, err := customer.GroupMembers(ctx, tx, root)
	if err != nil {
		return nil, fmt.Errorf("đọc nhóm khách: %w", err)
	}
	var pages []uuid.UUID
	if all, ids := identity.ScopesFor(u, "conversation.read"); !all {
		if len(ids) == 0 {
			return nil, nil // không đọc hội thoại ở Page nào: không mục hội thoại, cuộc gọi (như GET …/calls)
		}
		pages = make([]uuid.UUID, 0, len(ids))
		for _, s := range ids {
			if p, err := uuid.Parse(s); err == nil {
				pages = append(pages, p)
			}
		}
	}
	var key *queries.TimelineKey
	if after != nil {
		key = &queries.TimelineKey{At: after.At, Kind: after.Kind, RefID: after.RefID}
	}
	rows, err := queries.ChatTimeline(ctx, tx, members, pages, key, limit)
	if err != nil {
		return nil, err //nolint:wrapcheck // đã bọc ở queries
	}
	return rows, nil
}

// mergeTimeline gộp hai danh sách đã sắp (at, kind, ref_id) giảm dần, trả limit mục đầu và có còn mục sau không (một
// trong hai danh sách có hơn số mục được lấy).
func mergeTimeline(items []customer.TimelineItem, chat []queries.ChatTimelineRow, limit int) ([]timelineEntry, bool) {
	out := make([]timelineEntry, 0, len(items)+len(chat))
	for _, it := range items {
		out = append(out, timelineEntry{
			At: it.At, Kind: it.Kind, RefID: it.RefID, ActorID: it.ActorID, Summary: it.Summary,
			Note: string(it.Note), IsNote: it.Kind == customer.TimelineNote,
		})
	}
	for _, c := range chat {
		out = append(out, timelineEntry{At: c.At, Kind: c.Kind, RefID: c.RefID, ActorID: c.ActorID, Summary: c.Summary})
	}
	slices.SortFunc(out, func(a, b timelineEntry) int {
		if c := b.At.Compare(a.At); c != 0 {
			return c
		}
		if c := strings.Compare(b.Kind, a.Kind); c != 0 {
			return c
		}
		return bytes.Compare(b.RefID[:], a.RefID[:])
	})
	if len(out) > limit {
		return out[:limit], true
	}
	return out, false
}

// staffNames đọc tên hiển thị của từng nhân viên trong ids (mỗi mã một lần); nhân viên không còn → tên rỗng.
func staffNames(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(ids))
	for _, id := range ids {
		if _, done := out[id]; done {
			continue
		}
		staff, err := identity.GetStaff(ctx, tx, id)
		switch {
		case err == nil:
			out[id] = staff.DisplayName
		case errors.Is(err, identity.ErrStaffNotFound):
			out[id] = ""
		default:
			return nil, fmt.Errorf("đọc tên người làm: %w", err)
		}
	}
	return out, nil
}

// actorNamesOf đọc tên hiển thị (staffNames) của người làm actor(it) khác nil của mọi phần tử trong items.
func actorNamesOf[T any](ctx context.Context, tx pgx.Tx, items []T, actor func(T) *uuid.UUID) (map[uuid.UUID]string, error) {
	ids := make([]uuid.UUID, 0, len(items))
	for _, it := range items {
		if a := actor(it); a != nil {
			ids = append(ids, *a)
		}
	}
	return staffNames(ctx, tx, ids)
}

// writtenNote là ghi chú vừa ghi (khách, đơn) trước khi dựng thân API.
type writtenNote struct {
	ID, AuthorID uuid.UUID
	Body         string
	CreatedAt    time.Time
}

// writeNote chạy add (ghi ghi chú, audit nếu có) trong refusableWrite (lỗi nghiệp vụ theo refusal không lưu theo khóa),
// đọc tên người viết cùng tx và trả thân Note, 201.
func (h *handler) writeNote(ctx context.Context, refusal func(error) bool, what string,
	add func(tx pgx.Tx, u identity.AuthUser) (writtenNote, error),
) (*gen.Note, error) {
	out, err := h.refusableWrite(ctx, http.StatusCreated, refusal, what, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		n, err := add(tx, u)
		if err != nil {
			return nil, err
		}
		names, err := staffNames(ctx, tx, []uuid.UUID{n.AuthorID})
		if err != nil {
			return nil, err
		}
		return &gen.Note{ID: n.ID, Body: n.Body, Author: gen.Actor{ID: n.AuthorID, Name: names[n.AuthorID]}, CreatedAt: n.CreatedAt}, nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Note), nil //nolint:forcetypeassert // fn trên trả *gen.Note
}

// timelineEntry là một mục dòng thời gian (khách, đơn) trước khi dựng thân API; Note chỉ gửi khi IsNote; ActorType ""
// không gửi actorType (dòng thời gian khách).
type timelineEntry struct {
	At        time.Time
	Kind      string
	RefID     uuid.UUID
	ActorID   *uuid.UUID
	ActorType string
	Summary   map[string]string
	Note      string
	IsNote    bool
}

// timelineItemOut dựng TimelineItem của e, tên người làm lấy từ names.
func timelineItemOut(e timelineEntry, names map[uuid.UUID]string) gen.TimelineItem {
	item := gen.TimelineItem{At: e.At, Kind: e.Kind, RefId: e.RefID}
	if e.ActorID != nil {
		item.Actor = gen.NewOptActor(gen.Actor{ID: *e.ActorID, Name: names[*e.ActorID]})
	}
	if e.ActorType != "" {
		item.ActorType = gen.NewOptTimelineItemActorType(gen.TimelineItemActorType(e.ActorType))
	}
	if len(e.Summary) > 0 {
		item.Summary = gen.NewOptTimelineItemSummary(gen.TimelineItemSummary(e.Summary))
	}
	if e.IsNote {
		item.Note = gen.NewOptString(e.Note)
	}
	return item
}
