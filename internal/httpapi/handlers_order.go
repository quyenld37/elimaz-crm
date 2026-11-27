package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/queries/orderview"
)

// permOrderCreate: quyền lên đơn; ngoài guard, handler kiểm nó ở kho giữ hàng của đơn (orderWarehouseAt, Task 411).
const permOrderCreate = identity.Permission("order.create")

// permDiscountApprove: người có quyền này tạo đơn giảm tay vượt ngưỡng không phải chờ duyệt (Q3).
const permDiscountApprove = "order.discount.approve"

// orderKeyPrefix tách không gian tên của khóa nghiệp vụ đơn (UUID v5) khỏi mọi chỗ khác dùng uuid.NewSHA1.
const orderKeyPrefix = "elimaz-crm/order.create:"

// orderBusinessKey là khóa nghiệp vụ uuid của đơn (ord.orders.idempotency_key, 04 mục 5.5) suy ra từ nhân viên và
// Idempotency-Key của yêu cầu (chuỗi tự do theo hợp đồng M0): cùng người, cùng khóa → cùng uuid.
func orderBusinessKey(userID uuid.UUID, requestKey string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(orderKeyPrefix+userID.String()+":"+requestKey))
}

// CreateOrder (POST /api/v1/orders, order.create): tìm-hoặc-tạo khách và tạo đơn của người bán mặc định trong một
// transaction (giữ hàng cùng lúc, order.Writer.Create) → 201; khóa nghiệp vụ (từ Idempotency-Key và nhân viên) đã có
// đơn cùng nội dung → 200 trả đơn đó. Thân kiểu M1 (không intent) tạo thẳng confirmed như M1. Từ chối nghiệp vụ (hết
// hàng, dữ liệu sai, mã giảm giá bị từ chối…) không lưu theo khóa: gửi lại cùng khóa được xử lý lại. Người có quyền
// order.discount.approve không phải chờ duyệt giảm giá (Task M2-17; máy chủ tự tính từ grant). Kho giữ hàng (bỏ trống
// → kho chính) phải nằm trong phạm vi order.create của người gọi, không thì 403 (orderWarehouseAt, Task 411).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateOrder(ctx context.Context, req *gen.CreateOrderRequest, params gen.CreateOrderParams) (gen.CreateOrderRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	in, err := createOrderInput(req, orderBusinessKey(u.UserID, params.IdempotencyKey))
	if err != nil {
		return nil, err
	}
	w := order.Writer{Events: h.d.Events, Queue: queue.NewQueue(), Realtime: h.d.Realtime}
	var out *gen.Order
	var replayed bool
	var refused error
	err = WriteTx(ctx, h.d, u.TenantID, func(tx pgx.Tx) (int, any, bool, error) {
		out, replayed, refused = nil, false, nil // fn có thể chạy lại (ADR-46): gán lại mỗi lần
		var res order.CreateResult
		run := in // kho đích, bối cảnh hội thoại tính lại mỗi lần chạy
		err := orderConversationAt(ctx, tx, u, req.ConversationId, &run)
		var wh uuid.UUID
		if err == nil {
			wh, err = orderWarehouseAt(ctx, tx, u, in.WarehouseID)
		}
		if err == nil {
			run.WarehouseID = wh
			res, err = createOrder(ctx, tx, w, run, u, h.now().UTC())
		}
		switch {
		case isOrderRefusal(err):
			refused = err
			return 0, nil, false, nil
		case err != nil:
			return 0, nil, false, err
		}
		out, replayed = orderOut(res.Order), res.Replayed
		out.Warnings = make([]gen.OrderWarning, 0, len(res.Warnings))
		for _, wn := range res.Warnings {
			out.Warnings = append(out.Warnings, gen.OrderWarning{Code: wn.Code, RefId: gen.NewOptUUID(wn.RefID)})
		}
		if replayed {
			return http.StatusOK, out, true, nil
		}
		return http.StatusCreated, out, true, nil
	})
	switch {
	case err != nil:
		return nil, err
	case refused != nil:
		return nil, fmt.Errorf("tạo đơn: %w", refused)
	case replayed:
		return (*gen.CreateOrderOK)(out), nil
	}
	return (*gen.CreateOrderCreated)(out), nil
}

// orderConversationAt gắn đơn sắp tạo vào hội thoại conv (khi có; lên đơn trong khung chat, Task 176, 01 mục 6):
// conversation.OrderContext kiểm u có conversation.read trên Page của hội thoại (không → conversation.ErrNotFound, 404)
// và trả Page, người bán của Page. Đơn lưu hội thoại, Page, người bán của Page (ADR-29: hàng người bán khác bị từ chối
// như M2) và kênh facebook (thay kênh bên gọi gửi). Khách của đơn phải cùng nhóm với khách của hội thoại
// (conversation.CheckOrderCustomer, Task 501; lệch → 422 conversation_customer_mismatch).
func orderConversationAt(ctx context.Context, tx pgx.Tx, u identity.AuthUser, conv gen.OptUUID, in *order.CreateInput,
) error {
	id, ok := conv.Get()
	if !ok {
		return nil
	}
	c, err := conversation.OrderContext(ctx, tx, u, id)
	if err != nil {
		return fmt.Errorf("đọc hội thoại của đơn: %w", err)
	}
	if err := conversation.CheckOrderCustomer(ctx, tx, c, in.Customer.CustomerID); err != nil {
		return fmt.Errorf("kiểm khách của đơn từ hội thoại: %w", err)
	}
	// M4 chỉ có Page Facebook: đơn từ hội thoại luôn mang kênh facebook (form lên đơn của M2 gửi sẵn manual).
	in.ConversationID, in.ChannelAccountID, in.SellerID = &c.ConversationID, &c.ChannelAccountID, c.SellerID
	in.Channel = order.ChannelFacebook
	return nil
}

// orderWarehouseAt là kho giữ hàng của đơn sắp tạo (wh; uuid.Nil → kho chính) sau khi kiểm u có order.create ở kho
// đó: grant phạm vi all hoặc warehouse:<kho> (identity.GrantedAt; order.create là quyền none nên Can bỏ qua đích).
// Ngoài phạm vi → errForbiddenTarget (403), kể cả khi kho không có (không cho biết kho có hay không). Task 411, D-02,
// docs/quyen.md.
func orderWarehouseAt(ctx context.Context, tx pgx.Tx, u identity.AuthUser, wh uuid.UUID) (uuid.UUID, error) {
	if wh == uuid.Nil {
		var err error
		if wh, err = inventory.DefaultWarehouse(ctx, tx); err != nil {
			return uuid.Nil, fmt.Errorf("đọc kho chính: %w", err)
		}
	}
	if !identity.GrantedAt(u, permOrderCreate, *warehouseScope(wh)) {
		return uuid.Nil, errForbiddenTarget
	}
	return wh, nil
}

// createOrderInput dịch thân createOrder sang order.CreateInput. Đúng một trong customerId, customer; không gửi intent
// (kiểu M1) hay intent confirmed thì phải có shipAddress (hợp đồng) — sai → ErrInvalidInput (400).
func createOrderInput(req *gen.CreateOrderRequest, key uuid.UUID) (order.CreateInput, error) {
	in := order.CreateInput{
		IdempotencyKey: key, WarehouseID: req.WarehouseId.Or(uuid.Nil), Channel: string(req.Channel.Or("")),
		ExternalRef: req.ExternalRef.Or(""), TrafficSourceID: optUUID(req.TrafficSourceId),
		OrderDiscount: int64(req.OrderDiscount.Or(0)), ShippingFee: int64(req.ShippingFee),
		Intent: string(req.Intent.Or("")), CarrierNote: req.CarrierNote.Or(""), CareBy: optUUID(req.CareBy),
		CouponCode: req.CouponCode.Or(""),
	}
	c, hasCust := req.Customer.Get()
	switch {
	case hasCust == req.CustomerId.Set:
		return order.CreateInput{}, fmt.Errorf("tạo đơn cần đúng một trong customerId, customer: %w", order.ErrInvalidInput)
	case hasCust && req.PhoneIsCustomers.Set:
		return order.CreateInput{}, fmt.Errorf("phoneIsCustomers chỉ đi cùng customerId: %w", order.ErrInvalidInput)
	case hasCust:
		in.Customer = customer.OrderCustomerInput{
			FullName: c.FullName, Phone: string(c.Phone.Or("")), PhoneIsCustomers: c.PhoneIsCustomers.Or(false),
		}
	default:
		in.Customer = customer.OrderCustomerInput{
			CustomerID: optUUID(req.CustomerId), PhoneIsCustomers: req.PhoneIsCustomers.Or(false),
		}
	}
	if a, ok := req.ShipAddress.Get(); ok {
		in.ShipAddress = shipAddressIn(a)
	} else if in.Intent == "" || in.Intent == order.IntentConfirmed {
		return order.CreateInput{}, fmt.Errorf("tạo đơn confirmed thiếu địa chỉ giao: %w", order.ErrInvalidInput)
	}
	for _, it := range req.Items {
		in.Lines = append(in.Lines, order.LineInput{
			VariantID: it.VariantId, Quantity: it.Qty, LineDiscount: int64(it.LineDiscount.Or(0)),
		})
	}
	if r, ok := req.ReferralRef.Get(); ok {
		raw, err := json.Marshal(r)
		if err != nil {
			return order.CreateInput{}, fmt.Errorf("đọc referralRef: %w", err)
		}
		in.ReferralRef = raw
	}
	return in, nil
}

// shipAddressIn dịch địa chỉ giao của hợp đồng (createOrder, updateOrder, đơn thay thế của createReturn) sang
// order.ShipAddress.
func shipAddressIn(a gen.ShipAddress) *order.ShipAddress {
	return &order.ShipAddress{
		Name: a.Name, Phone: logging.Phone(a.Phone), Line: logging.Address(a.Line),
		ProvinceCode: a.ProvinceCode.Or(""), WardCode: a.WardCode.Or(""),
	}
}

// orderActor là người làm việc trên đơn từ người dùng đã xác thực u; quyền duyệt giảm giá máy chủ tự tính từ grant
// (không bao giờ lấy từ thân yêu cầu).
func orderActor(u identity.AuthUser) order.Actor {
	return order.Actor{TenantID: u.TenantID, UserID: u.UserID, CanApproveDiscount: identity.Can(u, permDiscountApprove, nil)}
}

func optUUID(o gen.OptUUID) *uuid.UUID {
	if v, ok := o.Get(); ok {
		return &v
	}
	return nil
}

func createOrder(ctx context.Context, tx pgx.Tx, w order.Writer, in order.CreateInput, u identity.AuthUser,
	now time.Time,
) (order.CreateResult, error) {
	res, err := w.Create(ctx, tx, in, orderActor(u), now)
	if err != nil || res.Replayed {
		return res, err //nolint:wrapcheck // sentinel của order
	}
	// order không nhập identity (04 mục 3): audit ghi ở đây, cùng tx.
	if err := identity.RecordAudit(ctx, tx, identity.AuditEntry{
		TenantID: u.TenantID, ActorType: "staff", ActorID: u.UserID.String(), Action: "order.create",
		EntityType: "order", EntityID: res.Order.ID.String(), After: res.Order.AuditFields(), RequestID: RequestID(ctx),
	}); err != nil {
		return order.CreateResult{}, fmt.Errorf("ghi audit order.create: %w", err)
	}
	return res, nil
}

// isOrderRefusal: lỗi nghiệp vụ của lần tạo đơn (trả cho client, không lưu theo khóa), kể cả kho giữ hàng ngoài phạm
// vi của người gọi (errForbiddenTarget, Task 411: được cấp kho rồi gửi lại cùng khóa thì xử lý lại).
func isOrderRefusal(err error) bool {
	for _, e := range []error{
		errForbiddenTarget, conversation.ErrNotFound, conversation.ErrOrderCustomerMismatch,
		order.ErrOutOfStock, order.ErrNotForSale, order.ErrMixedSellers, order.ErrTotalOverflow, order.ErrInvalidInput,
		order.ErrDuplicateOrder, order.ErrVariantNotFound, order.ErrLineDiscountExceeds, order.ErrOrderDiscountExceeds,
		order.ErrAddressIncomplete, order.ErrTrafficSourceInactive, customer.ErrInvalidInput, customer.ErrNotFound,
		customer.ErrInvalidPhone, customer.ErrUnknownAdminUnit, customer.ErrWardNotInProvince,
		promotion.ErrCouponRejected,
	} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// GetOrder (GET /api/v1/orders/{id}, order.read): chi tiết đơn của người bán mặc định (Task M2-24, order.GetDetail):
// dòng đơn, nhãn, phạm vi sửa, khách, mã giảm giá, cảnh báo đơn trùng, phiếu trả, tên người phụ trách và người tạo.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetOrder(ctx context.Context, params gen.GetOrderParams) (gen.GetOrderRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var d order.OrderDetail
	var names map[uuid.UUID]string
	err := h.readAsDefaultSeller(ctx, u, func(tx pgx.Tx, seller uuid.UUID) error {
		var err error
		if d, err = order.GetDetail(ctx, tx, seller, params.ID); err != nil {
			return err //nolint:wrapcheck // sentinel của order
		}
		names, err = actorNamesOf(ctx, tx, []*uuid.UUID{d.CareBy, d.CreatedBy}, func(p *uuid.UUID) *uuid.UUID { return p })
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("đọc đơn: %w", err)
	}
	return orderDetailOut(d, names), nil
}

// orderOut là thân đơn (createOrder, xác nhận…): trường M1 và trường M2 đọc được từ order.Order (Task M2-16); getOrder
// thêm khách, người phụ trách, phạm vi sửa, phiếu trả bằng orderDetailOut (Task M2-24).
func orderOut(o order.Order) *gen.Order {
	out := &gen.Order{
		ID: o.ID, Code: o.Code, Status: gen.OrderStatus(o.Status), CustomerId: o.CustomerID, Subtotal: gen.Money(o.Subtotal),
		ShippingFee: gen.Money(o.ShippingFee), GrandTotal: gen.Money(o.GrandTotal), CodAmount: gen.Money(o.CODAmount),
		CreatedAt: o.CreatedAt, Items: make([]gen.OrderItem, 0, len(o.Items)),

		Version: gen.NewOptInt64(o.Version), AddressComplete: gen.NewOptBool(o.AddressComplete),
		Label: gen.NewOptOrderLabel(gen.OrderLabel(orderview.Label(orderview.Axes{
			Status: o.Status, PendingReason: o.PendingReason, PackingStatus: o.PackingStatus,
		}))),
		WarehouseId: gen.NewOptUUID(o.WarehouseID), Channel: gen.NewOptString(o.Channel),
		CarrierNote: gen.NewOptString(o.CarrierNote), OrderType: gen.NewOptOrderOrderType(gen.OrderOrderType(o.OrderType)),
		PackingStatus:     gen.NewOptOrderPackingStatus(gen.OrderPackingStatus(o.PackingStatus)),
		PaymentStatus:     gen.NewOptOrderPaymentStatus(gen.OrderPaymentStatus(o.PaymentStatus)),
		LineDiscountTotal: gen.NewOptMoney(gen.Money(o.LineDiscountTotal)), OrderDiscount: gen.NewOptMoney(gen.Money(o.OrderDiscount)),
		CouponDiscount: gen.NewOptMoney(gen.Money(o.CouponDiscount)), DiscountTotal: gen.NewOptMoney(gen.Money(o.DiscountTotal)),
		PrepaidTotal: gen.NewOptMoney(gen.Money(o.PrepaidTotal)), DuplicateSuspect: gen.NewOptBool(o.DuplicateSuspect),
		ShipAddress: gen.NewOptAddress(gen.Address{
			Name: o.ShipAddress.Name, Phone: string(o.ShipAddress.Phone), Line: string(o.ShipAddress.Line),
			ProvinceCode: nonEmpty(o.ShipAddress.ProvinceCode), WardCode: nonEmpty(o.ShipAddress.WardCode),
		}),
	}
	if o.PendingReason != "" {
		out.PendingReason = gen.NewOptPendingReason(gen.PendingReason(o.PendingReason))
	}
	setOpt(&out.TrafficSourceId, o.TrafficSourceID)
	setOpt(&out.CurrentShipmentId, o.CurrentShipmentID)
	setOpt(&out.ReplacedReturnId, o.ReplacedReturnID)
	setOptTime(&out.DraftExpiresAt, o.DraftExpiresAt)
	setOptTime(&out.ConfirmedAt, o.ConfirmedAt)
	setOptTime(&out.CancelledAt, o.CancelledAt)
	if o.ExternalRef != nil {
		out.ExternalRef = gen.NewOptString(*o.ExternalRef)
	}
	if c := o.WebShipping; c != nil {
		out.WebShipping = gen.NewOptOrderWebShipping(webShippingOut(*c))
	}
	for _, it := range o.Items {
		out.Items = append(out.Items, gen.OrderItem{
			ID: it.ID, Position: it.Position, VariantId: it.VariantID, Sku: it.SKU, Name: it.Name, Qty: it.Qty,
			UnitPrice: gen.Money(it.UnitPrice), NetAmount: gen.Money(it.NetAmount),
			LineDiscount:           gen.NewOptMoney(gen.Money(it.LineDiscount)),
			AllocatedOrderDiscount: gen.NewOptMoney(gen.Money(it.AllocatedOrderDiscount)),
		})
	}
	return out
}

// webShippingOut là hãng/dịch vụ khách chọn trên website (Task 490) theo hợp đồng.
func webShippingOut(c order.WebShipChoice) gen.OrderWebShipping {
	g := gen.OrderWebShipping{
		CarrierKey: nonEmpty(c.CarrierKey), CarrierName: nonEmpty(c.CarrierName), Service: nonEmpty(c.Service),
		Fee: gen.Money(c.Fee), Estimated: c.Estimated(),
	}
	setOpt(&g.AccountId, c.AccountID)
	if c.Problem != "" {
		g.Problem = gen.NewOptOrderWebShippingProblem(gen.OrderWebShippingProblem(c.Problem))
	}
	return g
}

func nonEmpty(s string) gen.OptString {
	if s == "" {
		return gen.OptString{}
	}
	return gen.NewOptString(s)
}

func setOpt(dst *gen.OptUUID, v *uuid.UUID) {
	if v != nil {
		*dst = gen.NewOptUUID(*v)
	}
}

func setOptTime(dst *gen.OptDateTime, v *time.Time) {
	if v != nil {
		*dst = gen.NewOptDateTime(*v)
	}
}
