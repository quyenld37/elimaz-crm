package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Góp ý của khách — phía nhân viên (kế hoạch M5, Task 455 / M5-43). Đọc cần customer.feedback.read, sửa cần
// customer.feedback.manage (bảng route). Audit do customer ghi (chỉ tên trường). Thân trả về có nội dung, liên lạc của
// khách: không log.

// ListFeedback (GET /api/v1/feedback): một trang góp ý, mới nhất trước; con trỏ hỏng → 400; khách lạ → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListFeedback(ctx context.Context, params gen.ListFeedbackParams) (gen.ListFeedbackRes, error) {
	f := customer.FeedbackFilter{
		State: string(params.State.Or("")), Topic: string(params.Topic.Or("")), CustomerID: optUUID(params.CustomerId),
		Limit: int(params.Limit.Or(0)),
	}
	if s, ok := params.Cursor.Get(); ok {
		c, err := customer.ParseListCursor(s)
		if err != nil {
			return nil, fmt.Errorf("danh sách góp ý: %w", err)
		}
		f.After = &c
	}
	out := &gen.FeedbackPage{Items: []gen.Feedback{}}
	if err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		items, next, err := customer.ListFeedback(ctx, tx, f)
		if err != nil {
			return err //nolint:wrapcheck // lỗi của customer đã bọc
		}
		names, err := actorNamesOf(ctx, tx, items, func(it customer.Feedback) *uuid.UUID { return it.UpdatedBy })
		if err != nil {
			return err
		}
		out.Items = out.Items[:0]
		for _, it := range items {
			out.Items = append(out.Items, feedbackOut(it, names))
		}
		out.NextCursor = gen.OptString{}
		if next != nil {
			out.NextCursor = gen.NewOptString(next.String())
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("danh sách góp ý: %w", err)
	}
	return out, nil
}

// UpdateFeedback (PATCH /api/v1/feedback/{id}): đổi trạng thái, ghi chú khi version khớp; version cũ → 409.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UpdateFeedback(ctx context.Context, req *gen.UpdateFeedbackRequest, params gen.UpdateFeedbackParams,
) (gen.UpdateFeedbackRes, error) {
	var state, note *string
	if s, ok := req.State.Get(); ok {
		v := string(s)
		state = &v
	}
	if n, ok := req.Note.Get(); ok {
		note = &n
	}
	w := customer.Writer{Events: h.d.Events, Queue: queue.NewQueue(), Realtime: h.d.Realtime}
	out, err := h.refusableWrite(ctx, http.StatusOK, isCustomerRefusal, "sửa góp ý",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			a := customer.Actor{Type: customer.ActorStaff, ID: u.UserID, RequestID: RequestID(ctx)}
			f, err := w.UpdateFeedback(ctx, tx, params.ID, int(req.Version), state, note, a, h.now())
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của customer
			}
			names, err := staffNames(ctx, tx, []uuid.UUID{u.UserID})
			if err != nil {
				return nil, err
			}
			body := feedbackOut(f, names)
			return &body, nil
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Feedback), nil //nolint:forcetypeassert // fn trên trả *gen.Feedback
}

// feedbackOut đổi góp ý sang kiểu API; names: tên nhân viên theo mã (staffNames).
// CountFeedback (GET /api/v1/feedback/counts, customer.feedback.read): số góp ý theo trạng thái (đủ 3) và tổng, cho
// thanh lọc của màn Góp ý (Task 486).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CountFeedback(ctx context.Context) (gen.CountFeedbackRes, error) {
	var c customer.FeedbackCounts
	if err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		var err error
		c, err = customer.CountFeedback(ctx, tx)
		return err //nolint:wrapcheck // customer đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đếm góp ý: %w", err)
	}
	out := &gen.FeedbackCounts{Total: int32(c.Total)} //nolint:gosec // G115: số dòng một tenant
	for _, s := range []string{customer.FeedbackNew, customer.FeedbackSeen, customer.FeedbackAnswered} {
		out.States = append(out.States, gen.FeedbackCountsStatesItem{
			State: gen.FeedbackState(s), Count: int32(c.ByState[s]), //nolint:gosec // G115: số dòng một tenant
		})
	}
	return out, nil
}

func feedbackOut(f customer.Feedback, names map[uuid.UUID]string) gen.Feedback {
	out := gen.Feedback{
		ID: f.ID, Topic: gen.FeedbackTopic(f.Topic), Body: string(f.Body), Name: nonEmpty(string(f.Name)),
		Contact: nonEmpty(string(f.Contact)), OrderRef: nonEmpty(f.OrderRef), Page: nonEmpty(f.Page),
		Device: nonEmpty(f.Device), State: gen.FeedbackState(f.State), Note: nonEmpty(string(f.Note)),
		CustomerName: nonEmpty(string(f.CustomerName)), CustomerCode: nonEmpty(f.CustomerCode),
		CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt,
		Version: int32(f.Version), //nolint:gosec // G115: version nhỏ
	}
	if f.Mood != nil {
		out.Mood = gen.NewOptInt32(int32(*f.Mood)) //nolint:gosec // G115: 1–5
	}
	if f.CustomerID != nil {
		out.CustomerId = gen.NewOptUUID(*f.CustomerID)
	}
	if f.UpdatedBy != nil {
		out.UpdatedByName = nonEmpty(names[*f.UpdatedBy])
	}
	return out
}
