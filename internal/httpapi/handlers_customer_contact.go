package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// SetCustomerDoNotContact (PUT /api/v1/customers/{id}/do-not-contact, customer.write; Task 152): bật, tắt cờ "không
// liên hệ" trên hồ sơ đại diện của id (chặn mọi tin tự động). 204 không thân; không có hay tenant khác → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SetCustomerDoNotContact(ctx context.Context, req *gen.SetCustomerDoNotContactRequest,
	params gen.SetCustomerDoNotContactParams,
) (gen.SetCustomerDoNotContactRes, error) {
	w := customer.Writer{Events: h.d.Events, Queue: queue.NewQueue(), Realtime: h.d.Realtime}
	if _, err := h.refusableWrite(ctx, http.StatusNoContent, isCustomerRefusal, "đặt cờ không liên hệ",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			a := customer.Actor{Type: customer.ActorStaff, ID: u.UserID, RequestID: RequestID(ctx)}
			return nil, w.SetDoNotContact(ctx, tx, params.ID, req.On, a, time.Now().UTC())
		}); err != nil {
		return nil, err
	}
	return &gen.SetCustomerDoNotContactNoContent{}, nil
}
