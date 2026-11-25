package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/httpapi/web"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/queries/weborders"
)

// Đơn hàng của website (kế hoạch M5; x-auth: service_key): handler mỏng, mọi việc ở web.Orders.

// WebQuote (POST /api/v1/web/quote, web.orders.create, x-idempotency: exempt) → 200 báo giá theo số CRM; không ghi
// PostgreSQL (tests/arch webexempt quét thân hàm). Có mã (Task 482): SĐT hay IP đã quá ngưỡng nhập mã sai → 429
// coupon_attempts_exceeded trước khi báo giá; mã not_found, bound_other được đếm là một lần sai.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebQuote(ctx context.Context, req *gen.WebQuoteRequest) (gen.WebQuoteRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	ship, err := h.shipChoice(ctx, req.ShippingChoice, nil)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.CouponCode.Or("")) == "" {
		return h.d.webOrders.Quote(ctx, u.TenantID, req, ship) //nolint:wrapcheck // lỗi của order, đổi mã ở errors_order.go
	}
	ph, _ := customer.NormalizePhone(req.CustomerPhone.Or("")) // sai hay thiếu: chỉ đếm theo IP
	s := CouponSubject{Phone: ph, IP: req.ClientIp.Or(netip.Addr{}), ServiceKeyID: u.ServiceKeyID}
	if err := h.couponAllowed(ctx, s); err != nil {
		return nil, err
	}
	q, err := h.d.webOrders.Quote(ctx, u.TenantID, req, ship)
	if err != nil {
		return nil, err //nolint:wrapcheck // lỗi của order, đổi mã ở errors_order.go
	}
	c, _ := q.Coupon.Get()
	h.d.CouponGuard.Record(ctx, s, !c.Applied && couponGuessFailed(string(c.Reason.Or(""))))
	return q, nil
}

// couponAllowed: s chưa quá ngưỡng nhập mã sai (Deps.CouponGuard); quá → tooFastError coupon_attempts_exceeded.
func (h *handler) couponAllowed(ctx context.Context, s CouponSubject) error {
	if blocked, wait := h.d.CouponGuard.Blocked(ctx, s); blocked {
		return &tooFastError{retryAfter: wait, code: codeCouponAttemptsExceeded}
	}
	return nil
}

// WebCreateOrder (POST /api/v1/web/orders, web.orders.create, Idempotency-Key bắt buộc; Task 194): đọc thân, chuẩn
// hóa SĐT (sai → 422 invalid_phone) → có mã: SĐT hay IP quá ngưỡng nhập mã sai (Task 482) → 429
// coupon_attempts_exceeded, không lưu → giới hạn đơn theo SĐT (quá → 429 too_fast, không lưu) → WriteTx: tạo đơn
// (web.Orders.Create, savepoint) và audit order.create của khóa dịch vụ cùng tx. Kết quả cuối được lưu theo khóa: 201
// (cả khi đơn của cùng externalRef đã có), từ chối dứt khoát webOrderRefusal (422 quote_changed, item_unavailable,
// coupon_invalid, mixed_sellers; 409 out_of_stock). Lỗi đầu vào khác (isOrderRefusal) và 503 web_orders_paused (shop
// tạm dừng nhận đơn web, Task 456: mở lại rồi gửi lại cùng khóa thì xử lý lại) trả lời nhưng không lưu; 503
// retry_later không lưu (WriteTx). Có shippingChoice (Task 490): đối chiếu trước WriteTx (shipChoice, ngoài
// transaction); CreateFromWeb xét kết quả sau khi tìm đơn theo externalRef — sync sai → 422 lưu theo khóa, chưa kiểm
// được → 503 retry_later không lưu; queued → priceReview.
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
	guarded := strings.TrimSpace(in.CouponCode) != ""
	s := CouponSubject{Phone: in.Customer.Phone, IP: req.ClientIp.Or(netip.Addr{}), ServiceKeyID: u.ServiceKeyID}
	if guarded {
		if err := h.couponAllowed(ctx, s); err != nil {
			return nil, err
		}
	}
	if allowed, wait := h.d.WebOrderLimiter.AllowWebOrder(ctx, in.Customer.Phone); !allowed {
		return nil, &tooFastError{retryAfter: wait}
	}
	if in.Shipping, err = h.shipChoice(ctx, req.ShippingChoice, &in.ShipAddress); err != nil {
		return nil, err
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
		case isOrderRefusal(err), errors.Is(err, order.ErrWebIntakePaused), errors.Is(err, order.ErrShipUnverified):
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
	if guarded { // sau transaction (không gọi Valkey trong RetryTx)
		ci, isCoupon := errors.AsType[*order.CouponInvalidError](refused)
		h.d.CouponGuard.Record(ctx, s, isCoupon && couponGuessFailed(ci.Reason))
	}
	switch {
	case err != nil:
		return nil, err
	case refused != nil:
		// Từ chối đã lưu: middleware chống trùng ghi đúng thân đã lưu; chưa lưu: writeHandlerError đổi mã.
		return nil, fmt.Errorf("tạo đơn web: %w", refused)
	}
	return out, nil
}

// shipChoice đối chiếu lựa chọn giao hàng c của website (Task 490; web.Shipping.CheckChoice, ngoài transaction) với
// địa chỉ giao addr (nil ở báo giá); không gửi → nil (như trước Task 490).
func (h *handler) shipChoice(ctx context.Context, c gen.OptWebShippingChoice, addr *order.ShipAddress,
) (*order.WebShipChoice, error) {
	v, ok := c.Get()
	if !ok {
		return nil, nil
	}
	sh, u, err := h.webShipping(ctx)
	if err != nil {
		return nil, err
	}
	return sh.CheckChoice(ctx, u.TenantID, &v, addr) //nolint:wrapcheck // web đã bọc; lỗi database đổi mã ở errors.go
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

// WebGetOrder (GET /api/v1/web/orders/{externalRef}, web.orders.read; Task 196): khách tra đơn web của mình; đơn tay,
// đơn của shop khác → 404. Không có địa chỉ, SĐT, tên, ghi chú nội bộ (QĐ25).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebGetOrder(ctx context.Context, params gen.WebGetOrderParams) (gen.WebGetOrderRes, error) {
	var out *gen.WebOrderView
	if err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		seller, err := catalog.DefaultSeller(ctx, tx)
		if err != nil {
			return fmt.Errorf("người bán của đơn web: %w", err)
		}
		v, err := order.GetWebOrder(ctx, tx, seller, params.ExternalRef, h.now().UTC())
		if err != nil {
			return err //nolint:wrapcheck // sentinel của order
		}
		var sh *weborders.Shipment
		if v.CurrentShipmentID != nil {
			s, err := weborders.ShipmentOf(ctx, tx, seller, *v.CurrentShipmentID)
			if err != nil {
				return err //nolint:wrapcheck // weborders đã bọc
			}
			sh = &s
		}
		out = web.OrderViewOut(v, sh)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("tra đơn web: %w", err)
	}
	return out, nil
}

// WebOrderChanges (GET /api/v1/web/orders/changes, web.orders.read; Task 196): đơn web đổi sau con trỏ (since,
// afterId), mặc định 100 dòng.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebOrderChanges(ctx context.Context, params gen.WebOrderChangesParams) (gen.WebOrderChangesRes, error) {
	var out *gen.WebOrderChanges
	if err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		seller, err := catalog.DefaultSeller(ctx, tx)
		if err != nil {
			return fmt.Errorf("người bán của đơn web: %w", err)
		}
		p, err := weborders.Changes(ctx, tx, seller, weborders.Cursor{
			Since: params.Since, AfterID: params.AfterId.Or(uuid.Nil),
		}, int(params.Limit.Or(webChangesDefaultLimit)))
		if err != nil {
			return err //nolint:wrapcheck // sentinel của weborders
		}
		out = web.ChangesOut(p)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("đọc đơn web đã đổi: %w", err)
	}
	return out, nil
}

// webChangesDefaultLimit là số dòng mặc định của webOrderChanges (hợp đồng: default 100).
const webChangesDefaultLimit = 100

// WebCancelOrder (POST /api/v1/web/orders/{externalRef}/cancel, web.orders.cancel, Idempotency-Key; Task 196, QĐ18):
// order.Writer.CancelFromWeb trong webRequestWrite. Hủy ngay → audit order.cancel (trước, sau; lý do, có lời nhắn hay
// không — không nội dung); vừa tạo yêu cầu hủy → audit order.request.create. 200 lưu theo khóa; từ chối (422
// cannot_cancel, 404, 429 too_fast…) trả lời, không lưu.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebCancelOrder(ctx context.Context, req gen.OptWebCancelOrderBody, params gen.WebCancelOrderParams,
) (gen.WebCancelOrderRes, error) {
	note := ""
	if b, ok := req.Get(); ok {
		note = b.Note.Or("")
	}
	out, err := h.webRequestWrite(ctx, http.StatusOK, "khách hủy đơn web",
		func(tx pgx.Tx, u identity.AuthUser, seller uuid.UUID, w order.Writer) (any, error) {
			res, err := w.CancelFromWeb(ctx, tx, seller, params.ExternalRef, note, webActor(u), h.now().UTC())
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của order
			}
			body := &gen.WebCancelOutcome{Cancelled: res.Cancelled}
			switch {
			case res.Cancelled:
				after := res.After.AuditFields()
				after["cancelReason"], after["hasNote"] = order.CancelCustomerChangedMind, note != ""
				return body, auditService(ctx, tx, u, "order.cancel", "order", res.After.ID, res.Before.AuditFields(), after)
			case res.Request != nil:
				body.RequestRef = gen.NewOptString(res.RequestRef)
				after := res.Request.AuditFields()
				if note != "" {
					after["changed_fields"] = []string{"customer_note"}
				}
				return body, auditService(ctx, tx, u, "order.request.create", "order_request", res.Request.ID, nil, after)
			}
			body.RequestRef = gen.NewOptString(res.RequestRef)
			return body, nil
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.WebCancelOutcome), nil //nolint:forcetypeassert // fn trên trả *gen.WebCancelOutcome
}
