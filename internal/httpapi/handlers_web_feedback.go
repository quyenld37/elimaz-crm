package httpapi

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// WebSubmitFeedback (POST /api/v1/web/feedback, x-auth service_key, web.feedback.write): website gửi góp ý của khách
// (kế hoạch M5, Task 455). Người làm là khóa dịch vụ; audit do customer ghi (chỉ tên trường). Trường sai → 400, không
// lưu theo khóa; gửi lại cùng khóa trả cùng mã (middleware chống trùng).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebSubmitFeedback(ctx context.Context, req *gen.WebFeedbackRequest, _ gen.WebSubmitFeedbackParams,
) (gen.WebSubmitFeedbackRes, error) {
	in := customer.FeedbackInput{
		Topic: string(req.Topic), Mood: optInt32(req.Mood), Body: req.Body, Name: req.Name.Or(""),
		Contact: req.Contact.Or(""), OrderRef: req.OrderRef.Or(""), Page: req.Page.Or(""), Device: req.Device.Or(""),
		ClientHash: req.ClientHash.Or(""),
	}
	w := customer.Writer{Events: h.d.Events, Queue: queue.NewQueue(), Realtime: h.d.Realtime}
	out, err := h.refusableWrite(ctx, http.StatusCreated, isCustomerRefusal, "lưu góp ý của khách",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			a := customer.Actor{Type: customer.ActorService, ID: u.ServiceKeyID, RequestID: RequestID(ctx)}
			id, err := w.SubmitFeedback(ctx, tx, in, a, h.now())
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của customer
			}
			return &gen.WebFeedbackResult{ID: id}, nil
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.WebFeedbackResult), nil //nolint:forcetypeassert // fn trên trả *gen.WebFeedbackResult
}
