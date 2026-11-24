package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/httpapi/web"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
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

// WebCreateOrder (POST /api/v1/web/orders, web.orders.create, Idempotency-Key bắt buộc; Task 194): đọc thân, chuẩn
// hóa SĐT (sai → 422 invalid_phone) → giới hạn đơn theo SĐT (quá → 429 too_fast, không lưu) → WriteTx: tạo đơn
// (web.Orders.Create, savepoint) và audit order.create của khóa dịch vụ cùng tx. Kết quả cuối được lưu theo khóa: 201
// (cả khi đơn của cùng externalRef đã có), từ chối dứt khoát webOrderRefusal (422 quote_changed, item_unavailable,
// coupon_invalid, mixed_sellers; 409 out_of_stock). Lỗi đầu vào khác (isOrderRefusal) trả lời nhưng không lưu; 503
// retry_later không lưu (WriteTx).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebCreateOrder(ctx context.Context, req *gen.WebOrderRequest, _ gen.WebCreateOrderParams) (gen.WebCreateOrderRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	in, err := web.OrderInput(req)
	if err != nil {
		return nil, fmt.Errorf("đọc đơn web: %w", err)
	}
	if allowed, wait := h.d.WebOrderLimiter.AllowWebOrder(ctx, in.Customer.Phone); !allowed {
		return nil, &tooFastError{retryAfter: wait}
	}
	a := order.Actor{TenantID: u.TenantID, UserID: u.ServiceKeyID, Type: order.ActorService}
	var out *gen.WebOrderCreated
	var refused error
	err = WriteTx(ctx, h.d, u.TenantID, func(tx pgx.Tx) (int, any, bool, error) {
		out, refused = nil, nil // fn có thể chạy lại (ADR-46): gán lại mỗi lần
		res, err := h.d.webOrders.Create(ctx, tx, in, a)
		if body, status, final := webOrderRefusal(err); final {
			refused = err
			body.RequestID = RequestID(ctx)
			return status, body, true, nil
		}
		if err == nil && !res.Replayed {
			err = auditWebOrder(ctx, tx, u, res.OrderID)
		}
		switch {
		case isOrderRefusal(err):
			refused = err
			return 0, nil, false, nil
		case err != nil:
			return 0, nil, false, err
		}
		out = &gen.WebOrderCreated{
			OrderId: res.OrderID, Code: res.Code, Status: gen.OrderStatus(res.Status), GrandTotal: res.GrandTotal,
			PriceReview: res.PriceReview,
		}
		return http.StatusCreated, out, true, nil
	})
	switch {
	case err != nil:
		return nil, err
	case refused != nil:
		// Từ chối đã lưu: middleware chống trùng ghi đúng thân đã lưu; chưa lưu: writeHandlerError đổi mã.
		return nil, fmt.Errorf("tạo đơn web: %w", refused)
	}
	return out, nil
}

// auditWebOrder ghi audit order.create của đơn web id (người làm: khóa dịch vụ) trong tx; order không nhập identity
// (04 mục 3) nên ghi ở lớp HTTP như createOrder.
func auditWebOrder(ctx context.Context, tx pgx.Tx, u identity.AuthUser, id uuid.UUID) error {
	seller, err := catalog.DefaultSeller(ctx, tx)
	if err != nil {
		return fmt.Errorf("người bán của đơn web: %w", err)
	}
	o, err := order.Get(ctx, tx, seller, id)
	if err != nil {
		return fmt.Errorf("đọc đơn web để ghi audit: %w", err)
	}
	if err := identity.RecordAudit(ctx, tx, identity.AuditEntry{
		TenantID: u.TenantID, ActorType: order.ActorService, ActorID: u.ServiceKeyID.String(), Action: "order.create",
		EntityType: "order", EntityID: id.String(), After: o.AuditFields(), RequestID: RequestID(ctx),
	}); err != nil {
		return fmt.Errorf("ghi audit order.create: %w", err)
	}
	return nil
}
