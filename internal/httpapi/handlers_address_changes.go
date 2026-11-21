package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Vùng address_changes (kế hoạch M3, Task M3-17, DH15): đổi địa chỉ sau khi đã gửi hãng. Quyền fulfillment.ship theo kho
// của tài khoản hãng của vận đơn, kiểm dưới khóa; thiếu phạm vi → 403. Địa chỉ trên đơn chỉ đổi khi hãng đã nhận (lệnh
// sửa địa chỉ, hay nhân viên xác nhận ở resolveAddressChange). Audit chỉ có tên trường, mã; địa chỉ bị che.

// RequestAddressChange (POST /api/v1/shipments/{id}/address-changes) → 201: yêu cầu pending (api: lệnh sửa địa chỉ ở
// hãng; manual: nhân viên gọi hãng rồi xác nhận). Vận đơn không đổi được địa chỉ → 409 invalid_transition; còn yêu cầu
// mở → 409 already_exists; địa chỉ chưa khớp mã hãng → 422 address_unmapped; thiếu xã → 422 address_incomplete.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) RequestAddressChange(ctx context.Context, req *gen.RequestAddressChangeRequest, params gen.RequestAddressChangeParams) (gen.RequestAddressChangeRes, error) {
	svc, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	a := req.Address
	in := fulfillment.AddressChangeInput{
		Address: order.ShipAddress{
			Name: a.Name, Phone: logging.Phone(a.Phone), Line: logging.Address(a.Line), ProvinceCode: a.ProvinceCode,
			WardCode: a.WardCode,
		},
		Note: req.Note.Or(""),
	}
	out, err := h.refusableWrite(ctx, http.StatusCreated, isAddressChangeRefusal, "yêu cầu đổi địa chỉ",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			in.Authorize = shipAuthorize(u, permShip, nil)
			c, err := svc.RequestAddressChange(ctx, tx, params.ID, in, orderActor(u))
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment, order
			}
			return addressChangeOut(c), h.audit(ctx, tx, u, "fulfillment.address_change.request", "address_change", c.ID,
				nil, c.AuditFields())
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.AddressChange), nil //nolint:forcetypeassert // fn trên trả *gen.AddressChange
}

// ResolveAddressChange (POST /api/v1/address-changes/{id}/resolve) → 200: nhân viên xác nhận kết quả (yêu cầu manual,
// sent hay unknown); confirmed → địa chỉ đơn mới, audit order.address_change (changed_fields ship_address). Yêu cầu đã
// đóng hay còn chờ lệnh → 409 invalid_transition; không có → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ResolveAddressChange(ctx context.Context, req *gen.ResolveAddressChangeRequest, params gen.ResolveAddressChangeParams) (gen.ResolveAddressChangeRes, error) {
	svc, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	in := fulfillment.AddressResolveInput{Result: string(req.Result), Note: req.Note.Or("")}
	out, err := h.refusableWrite(ctx, http.StatusOK, isAddressChangeRefusal, "xác nhận đổi địa chỉ",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			in.Authorize = shipAuthorize(u, permShip, nil)
			r, err := svc.ResolveAddressChange(ctx, tx, params.ID, in, orderActor(u))
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment, order
			}
			if r.OrderAudit != nil {
				if err := RecordFulfillmentAudit(ctx, tx, *r.OrderAudit); err != nil {
					return nil, err
				}
			}
			return addressChangeOut(r.Change), h.audit(ctx, tx, u, "fulfillment.address_change.resolve", "address_change",
				r.Change.ID, nil, r.Change.AuditFields())
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.AddressChange), nil //nolint:forcetypeassert // fn trên trả *gen.AddressChange
}

// ListShipmentAddressChanges (GET /api/v1/shipments/{id}/address-changes, fulfillment.read ở kho của tài khoản hãng
// của vận đơn) → 200: yêu cầu đang mở (nhiều nhất một), không có địa chỉ; thiếu phạm vi ở kho hay vận đơn lạ → 404
// (Task 134).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListShipmentAddressChanges(ctx context.Context, params gen.ListShipmentAddressChangesParams) (gen.ListShipmentAddressChangesRes, error) {
	var open []fulfillment.AddressChange
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) error {
		s, err := fulfillment.Get(ctx, tx, params.ID)
		if err != nil {
			return err //nolint:wrapcheck // sentinel của fulfillment
		}
		if canAt(u, s.WarehouseID, permShipRead) != nil {
			return fulfillment.ErrNotFound
		}
		open, err = fulfillment.OpenAddressChanges(ctx, tx, s)
		return err //nolint:wrapcheck // fulfillment đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc yêu cầu đổi địa chỉ: %w", err)
	}
	out := &gen.AddressChangeList{Items: make([]gen.AddressChange, 0, len(open))}
	for _, c := range open {
		out.Items = append(out.Items, *addressChangeOut(c))
	}
	return out, nil
}

// RecordFulfillmentAudit ghi dòng audit fulfillment dựng (đổi địa chỉ đơn theo yêu cầu đổi địa chỉ) bằng
// identity.RecordAudit trong tx của bên gọi; cmd/crm truyền hàm này cho lệnh sửa địa chỉ (fulfillment.AuditFunc).
// request id lấy từ ctx (rỗng ở worker).
func RecordFulfillmentAudit(ctx context.Context, tx pgx.Tx, r fulfillment.AuditRecord) error {
	actor := ""
	if r.ActorID != uuid.Nil {
		actor = r.ActorID.String()
	}
	if err := identity.RecordAudit(ctx, tx, identity.AuditEntry{
		TenantID: r.TenantID, ActorType: r.ActorType, ActorID: actor, Action: r.Action, EntityType: r.EntityType,
		EntityID: r.EntityID.String(), Before: r.Before, After: r.After, RequestID: RequestID(ctx),
	}); err != nil {
		return fmt.Errorf("ghi audit %s: %w", r.Action, err)
	}
	return nil
}

// isAddressChangeRefusal: lỗi nghiệp vụ của vận chuyển, của địa chỉ (order, danh mục địa giới của customer).
func isAddressChangeRefusal(err error) bool {
	if isShipmentRefusal(err) || isCustomerRefusal(err) {
		return true
	}
	_, _, ok := orderError(err)
	return ok
}

func addressChangeOut(c fulfillment.AddressChange) *gen.AddressChange {
	out := &gen.AddressChange{
		ID: c.ID, ShipmentId: c.ShipmentID, Mode: gen.CarrierAccountMode(c.Mode), Status: gen.AddressChangeStatus(c.Status),
		CreatedAt: c.CreatedAt,
	}
	if c.Note != "" {
		out.Note = gen.NewOptString(c.Note)
	}
	if c.ResolvedAt != nil {
		out.ResolvedAt = gen.NewOptDateTime(*c.ResolvedAt)
	}
	return out
}
