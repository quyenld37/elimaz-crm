package httpapi

import (
	"context"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Đơn hàng của website (kế hoạch M5; x-auth: service_key): handler mỏng, mọi việc ở web.Orders.

// WebQuote (POST /api/v1/web/quote, web.orders.create, x-idempotency: exempt) → 200 báo giá theo số CRM; không ghi
// gì (tests/arch webexempt quét thân hàm).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebQuote(ctx context.Context, req *gen.WebQuoteRequest) (gen.WebQuoteRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	return h.d.webOrders.Quote(ctx, u.TenantID, req) //nolint:wrapcheck // lỗi của order, đổi mã ở errors_order.go
}
