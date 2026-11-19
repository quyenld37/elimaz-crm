package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/queue"
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
// nhất trước, trang theo nextCursor; con trỏ hỏng → 400 invalid_input; khách không có hay của tenant khác → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetCustomerTimeline(ctx context.Context, params gen.GetCustomerTimelineParams) (gen.GetCustomerTimelineRes, error) {
	var after *customer.TimelineCursor
	if s, ok := params.Cursor.Get(); ok {
		c, err := customer.ParseTimelineCursor(s)
		if err != nil {
			return nil, fmt.Errorf("dòng thời gian của khách: %w", err)
		}
		after = &c
	}
	var (
		items []customer.TimelineItem
		next  *customer.TimelineCursor
		names map[uuid.UUID]string
	)
	err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		var err error
		items, next, err = customer.Timeline(ctx, tx, params.ID, after, int(params.Limit.Or(0)))
		if err != nil {
			return err //nolint:wrapcheck // lỗi của customer đã bọc
		}
		names, err = actorNamesOf(ctx, tx, items, func(it customer.TimelineItem) *uuid.UUID { return it.ActorID })
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("dòng thời gian của khách: %w", err)
	}
	out := &gen.TimelinePage{Items: make([]gen.TimelineItem, 0, len(items))}
	for _, it := range items {
		out.Items = append(out.Items, timelineItemOut(timelineEntry{
			At: it.At, Kind: it.Kind, RefID: it.RefID, ActorID: it.ActorID, Summary: it.Summary,
			Note: string(it.Note), IsNote: it.Kind == customer.TimelineNote,
		}, names))
	}
	if next != nil {
		out.NextCursor = gen.NewOptString(next.String())
	}
	return out, nil
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

// timelineEntry là một mục dòng thời gian (khách, đơn) trước khi dựng thân API; Note chỉ gửi khi IsNote.
type timelineEntry struct {
	At      time.Time
	Kind    string
	RefID   uuid.UUID
	ActorID *uuid.UUID
	Summary map[string]string
	Note    string
	IsNote  bool
}

// timelineItemOut dựng TimelineItem của e, tên người làm lấy từ names.
func timelineItemOut(e timelineEntry, names map[uuid.UUID]string) gen.TimelineItem {
	item := gen.TimelineItem{At: e.At, Kind: e.Kind, RefId: e.RefID}
	if e.ActorID != nil {
		item.Actor = gen.NewOptActor(gen.Actor{ID: *e.ActorID, Name: names[*e.ActorID]})
	}
	if len(e.Summary) > 0 {
		item.Summary = gen.NewOptTimelineItemSummary(gen.TimelineItemSummary(e.Summary))
	}
	if e.IsNote {
		item.Note = gen.NewOptString(e.Note)
	}
	return item
}
