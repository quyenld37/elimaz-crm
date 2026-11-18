package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// orderKeyPrefix tách không gian tên của khóa nghiệp vụ đơn (UUID v5) khỏi mọi chỗ khác dùng uuid.NewSHA1.
const orderKeyPrefix = "elimaz-crm/order.create:"

// orderBusinessKey là khóa nghiệp vụ uuid của đơn (ord.orders.idempotency_key, 04 mục 5.5) suy ra từ nhân viên và
// Idempotency-Key của yêu cầu (chuỗi tự do theo hợp đồng M0): cùng người, cùng khóa → cùng uuid.
func orderBusinessKey(userID uuid.UUID, requestKey string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(orderKeyPrefix+userID.String()+":"+requestKey))
}

// CreateOrder (POST /api/v1/orders, order.create): tạo khách mới và đơn nhập tay của người bán mặc định trong một
// transaction (giữ hàng cùng lúc) → 201. Từ chối nghiệp vụ (hết hàng, ngừng bán, khác người bán, tràn tổng, dữ liệu
// sai) không lưu theo khóa: gửi lại cùng khóa được xử lý lại. Hợp đồng M2 (Task M2-02) đã thêm trường; nghiệp vụ của
// chúng có ở Task M2-16, M2-17, nên tới lúc đó thân có trường M2 → 501 (không bỏ qua lặng lẽ). Thân kiểu M1 phải có
// customer và shipAddress (nay tùy chọn trong hợp đồng) → thiếu là 400 invalid_input.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateOrder(ctx context.Context, req *gen.CreateOrderRequest, params gen.CreateOrderParams) (gen.CreateOrderRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	if hasM2OrderFields(req) {
		return nil, fmt.Errorf("tạo đơn có trường M2: %w", ht.ErrNotImplemented)
	}
	cust, hasCust := req.Customer.Get()
	addr, hasAddr := req.ShipAddress.Get()
	if !hasCust || !hasAddr {
		return nil, fmt.Errorf("tạo đơn kiểu M1 thiếu khách hay địa chỉ: %w", order.ErrInvalidInput)
	}
	in := order.CreateManualInput{
		IdempotencyKey: orderBusinessKey(u.UserID, params.IdempotencyKey),
		WarehouseID:    req.WarehouseId.Or(uuid.Nil),
		ShipAddress: order.ShipAddress{
			Name: addr.Name, Phone: logging.Phone(addr.Phone), Line: logging.Address(addr.Line),
		},
		ShippingFee: int64(req.ShippingFee),
	}
	for _, it := range req.Items {
		in.Items = append(in.Items, order.ItemInput{VariantID: it.VariantId, Qty: it.Qty})
	}
	w := order.Writer{Events: h.d.Events, Queue: queue.NewQueue(), Realtime: h.d.Realtime}
	var out *gen.Order
	var refused error
	err := WriteTx(ctx, h.d, u.TenantID, func(tx pgx.Tx) (int, any, bool, error) {
		out, refused = nil, nil // fn có thể chạy lại (ADR-46): gán lại mỗi lần
		o, err := createOrder(ctx, tx, w, in, cust.FullName, u)
		switch {
		case isOrderRefusal(err):
			refused = err
			return 0, nil, false, nil
		case err != nil:
			return 0, nil, false, err
		}
		out = orderOut(o)
		return http.StatusCreated, out, true, nil
	})
	switch {
	case err != nil:
		return nil, err
	case refused != nil:
		return nil, fmt.Errorf("tạo đơn: %w", refused)
	}
	return (*gen.CreateOrderCreated)(out), nil
}

// hasM2OrderFields: thân createOrder có trường mà hợp đồng M2 thêm (Task M2-02) nhưng nghiệp vụ chưa có.
func hasM2OrderFields(req *gen.CreateOrderRequest) bool {
	if c, ok := req.Customer.Get(); ok && (c.Phone.Set || c.PhoneIsCustomers.Set) {
		return true
	}
	if a, ok := req.ShipAddress.Get(); ok && (a.ProvinceCode.Set || a.WardCode.Set) {
		return true
	}
	for _, it := range req.Items {
		if it.LineDiscount.Set {
			return true
		}
	}
	return req.CustomerId.Set || req.Intent.Set || req.OrderDiscount.Set || req.CouponCode.Set ||
		req.TrafficSourceId.Set || req.CarrierNote.Set || req.CareBy.Set || req.Channel.Set || req.ExternalRef.Set ||
		req.ReferralRef.Set
}

func createOrder(ctx context.Context, tx pgx.Tx, w order.Writer, in order.CreateManualInput, fullName string,
	u identity.AuthUser,
) (order.Order, error) {
	seller, err := catalog.DefaultSeller(ctx, tx)
	if err != nil {
		return order.Order{}, fmt.Errorf("người bán của đơn: %w", err)
	}
	in.SellerID = seller
	if in.CustomerID, err = customer.Create(ctx, tx, fullName); err != nil {
		return order.Order{}, err //nolint:wrapcheck // sentinel của customer
	}
	o, err := w.CreateManual(ctx, tx, in, order.Actor{TenantID: u.TenantID, UserID: u.UserID})
	if err != nil {
		return order.Order{}, err //nolint:wrapcheck // sentinel của order
	}
	// order không nhập identity (04 mục 3): audit ghi ở đây, cùng tx.
	if err := identity.RecordAudit(ctx, tx, identity.AuditEntry{
		TenantID: u.TenantID, ActorType: "staff", ActorID: u.UserID.String(), Action: "order.create",
		EntityType: "order", EntityID: o.ID.String(), After: o.AuditFields(), RequestID: RequestID(ctx),
	}); err != nil {
		return order.Order{}, fmt.Errorf("ghi audit order.create: %w", err)
	}
	return o, nil
}

// isOrderRefusal: lỗi nghiệp vụ của lần tạo đơn (trả cho client, không lưu theo khóa).
func isOrderRefusal(err error) bool {
	for _, e := range []error{
		order.ErrOutOfStock, order.ErrNotForSale, order.ErrMixedSellers, order.ErrTotalOverflow, order.ErrInvalidInput,
		order.ErrDuplicateOrder, customer.ErrInvalidInput,
	} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// GetOrder (GET /api/v1/orders/{id}, order.read): đơn của người bán mặc định kèm dòng đơn.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetOrder(ctx context.Context, params gen.GetOrderParams) (gen.GetOrderRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var o order.Order
	err := h.readAsDefaultSeller(ctx, u, func(tx pgx.Tx, seller uuid.UUID) error {
		var err error
		o, err = order.Get(ctx, tx, seller, params.ID)
		return err //nolint:wrapcheck // sentinel của order
	})
	if err != nil {
		return nil, fmt.Errorf("đọc đơn: %w", err)
	}
	return orderOut(o), nil
}

func orderOut(o order.Order) *gen.Order {
	out := &gen.Order{
		ID: o.ID, Code: o.Code, Status: gen.OrderStatus(o.Status), CustomerId: o.CustomerID, Subtotal: gen.Money(o.Subtotal),
		ShippingFee: gen.Money(o.ShippingFee), GrandTotal: gen.Money(o.GrandTotal), CodAmount: gen.Money(o.CODAmount),
		CreatedAt: o.CreatedAt, Items: make([]gen.OrderItem, 0, len(o.Items)),
	}
	for _, it := range o.Items {
		out.Items = append(out.Items, gen.OrderItem{
			ID: it.ID, Position: it.Position, VariantId: it.VariantID, Sku: it.SKU, Name: it.Name, Qty: it.Qty,
			UnitPrice: gen.Money(it.UnitPrice), NetAmount: gen.Money(it.NetAmount),
		})
	}
	return out
}
