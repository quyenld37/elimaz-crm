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

// CreateCustomer (POST /api/v1/customers, customer.write) tạo khách (Task M2-09); SĐT đã thuộc khách khác → 409
// identity_taken kèm ownerId. Trả hồ sơ theo gốc, 201.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateCustomer(ctx context.Context, req *gen.CreateCustomerRequest, _ gen.CreateCustomerParams) (gen.CreateCustomerRes, error) {
	in := customer.CreateInput{FullName: req.FullName, Phone: string(req.Phone.Or("")), Source: customer.SourceStaff}
	if bd, ok := req.Birthday.Get(); ok {
		in.Birthday = &bd
	}
	if a, ok := req.Address.Get(); ok {
		addr := addressIn(a)
		in.Address = &addr
	}
	return h.customerWrite(ctx, http.StatusCreated, func(tx pgx.Tx, w customer.Writer, a customer.Actor, now time.Time) (uuid.UUID, error) {
		c, err := w.CreateCustomer(ctx, tx, in, a, now)
		return c.ID, err //nolint:wrapcheck // sentinel của customer
	})
}

// GetCustomer (GET /api/v1/customers/{id}, customer.read): hồ sơ theo gốc của id; không có hay của tenant khác → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetCustomer(ctx context.Context, params gen.GetCustomerParams) (gen.GetCustomerRes, error) {
	var p customer.Profile
	err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		var err error
		p, err = customer.Get(ctx, tx, params.ID)
		return err //nolint:wrapcheck // lỗi của customer đã bọc
	})
	if err != nil {
		return nil, fmt.Errorf("đọc khách: %w", err)
	}
	return customerOut(p), nil
}

// UpdateCustomer (PATCH /api/v1/customers/{id}, customer.write): sửa tên, ngày sinh khi version còn khớp (409
// version_conflict).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UpdateCustomer(ctx context.Context, req *gen.UpdateCustomerRequest, params gen.UpdateCustomerParams) (gen.UpdateCustomerRes, error) {
	var p customer.UpdateInput
	if n, ok := req.FullName.Get(); ok {
		p.FullName = &n
	}
	if bd, ok := req.Birthday.Get(); ok {
		p.Birthday = &bd
	}
	p.ClearBirthday = req.ClearBirthday.Or(false)
	return h.customerWrite(ctx, http.StatusOK, func(tx pgx.Tx, w customer.Writer, a customer.Actor, now time.Time) (uuid.UUID, error) {
		_, err := w.Update(ctx, tx, params.ID, req.Version, p, a, now)
		return params.ID, err //nolint:wrapcheck // sentinel của customer
	})
}

// AddCustomerIdentity (POST /api/v1/customers/{id}/identities, customer.write): thêm SĐT, email, Zalo; đã thuộc khách
// khác → 409 identity_taken.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) AddCustomerIdentity(ctx context.Context, req *gen.AddCustomerIdentityRequest, params gen.AddCustomerIdentityParams) (gen.AddCustomerIdentityRes, error) {
	in := customer.IdentityInput{Kind: customer.IdentityKind(req.Kind), Value: req.Value, Source: customer.SourceStaff}
	return h.customerWrite(ctx, http.StatusCreated, func(tx pgx.Tx, w customer.Writer, a customer.Actor, _ time.Time) (uuid.UUID, error) {
		_, err := w.AddIdentity(ctx, tx, params.ID, in, a)
		return params.ID, err //nolint:wrapcheck // sentinel của customer
	})
}

// RemoveCustomerIdentity (DELETE /api/v1/customers/{id}/identities/{identityId}, customer.write): gỡ danh tính của
// nhóm (không gỡ fb_psid, web_account: 400 invalid_input).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) RemoveCustomerIdentity(ctx context.Context, params gen.RemoveCustomerIdentityParams) (gen.RemoveCustomerIdentityRes, error) {
	return h.customerWrite(ctx, http.StatusOK, func(tx pgx.Tx, w customer.Writer, a customer.Actor, _ time.Time) (uuid.UUID, error) {
		return params.ID, w.RemoveIdentity(ctx, tx, params.ID, params.IdentityId, a)
	})
}

// AddCustomerAddress (POST /api/v1/customers/{id}/addresses, customer.write): mã tỉnh, xã kiểm theo danh mục (422
// unknown_admin_unit, ward_not_in_province).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) AddCustomerAddress(ctx context.Context, req *gen.CustomerAddressInput, params gen.AddCustomerAddressParams) (gen.AddCustomerAddressRes, error) {
	in, isDefault := addressIn(req.Address), req.IsDefault.Or(false)
	return h.customerWrite(ctx, http.StatusCreated, func(tx pgx.Tx, w customer.Writer, a customer.Actor, now time.Time) (uuid.UUID, error) {
		_, err := w.AddAddress(ctx, tx, params.ID, in, isDefault, a, now)
		return params.ID, err //nolint:wrapcheck // sentinel của customer
	})
}

// UpdateCustomerAddress (PATCH /api/v1/customers/{id}/addresses/{addressId}, customer.write): sửa khi version còn khớp.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UpdateCustomerAddress(ctx context.Context, req *gen.UpdateCustomerAddressRequest, params gen.UpdateCustomerAddressParams) (gen.UpdateCustomerAddressRes, error) {
	var p customer.AddressPatch
	if a, ok := req.Address.Get(); ok {
		addr := addressIn(a)
		p.Address = &addr
	}
	if d, ok := req.IsDefault.Get(); ok {
		p.IsDefault = &d
	}
	return h.customerWrite(ctx, http.StatusOK, func(tx pgx.Tx, w customer.Writer, a customer.Actor, now time.Time) (uuid.UUID, error) {
		return params.ID, w.UpdateAddress(ctx, tx, params.ID, params.AddressId, req.Version, p, a, now)
	})
}

// DeleteCustomerAddress (DELETE /api/v1/customers/{id}/addresses/{addressId}, customer.write): xóa mềm.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) DeleteCustomerAddress(ctx context.Context, params gen.DeleteCustomerAddressParams) (gen.DeleteCustomerAddressRes, error) {
	return h.customerWrite(ctx, http.StatusOK, func(tx pgx.Tx, w customer.Writer, a customer.Actor, now time.Time) (uuid.UUID, error) {
		return params.ID, w.DeleteAddress(ctx, tx, params.ID, params.AddressId, a, now)
	})
}

// customerWrite chạy change trong refusableWrite với một customer.Writer của yêu cầu (sự kiện, báo hiệu, audit do
// customer ghi trong cùng transaction), rồi đọc hồ sơ theo gốc của mã change trả về làm câu trả lời. Lỗi nghiệp vụ của
// customer là câu trả lời không đổi gì (không lưu vào khóa chống trùng).
func (h *handler) customerWrite(ctx context.Context, status int,
	change func(tx pgx.Tx, w customer.Writer, a customer.Actor, now time.Time) (uuid.UUID, error),
) (*gen.Customer, error) {
	w := customer.Writer{Events: h.d.Events, Queue: queue.NewQueue(), Realtime: h.d.Realtime}
	out, err := h.refusableWrite(ctx, status, isCustomerRefusal, "ghi khách", func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		a := customer.Actor{Type: customer.ActorStaff, ID: u.UserID, RequestID: RequestID(ctx)}
		id, err := change(tx, w, a, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		p, err := customer.Get(ctx, tx, id)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của customer
		}
		return customerOut(p), nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Customer), nil //nolint:forcetypeassert // change trên trả *gen.Customer
}

func isCustomerRefusal(err error) bool {
	_, _, ok := customerError(err)
	return ok || errors.Is(err, customer.ErrInvalidInput)
}

func addressIn(a gen.AddressInput) customer.AddressInput {
	return customer.AddressInput{
		Name: a.Name, Phone: string(a.Phone), Line: a.Line, ProvinceCode: a.ProvinceCode, WardCode: a.WardCode,
	}
}

// customerOut đổi hồ sơ theo gốc sang Customer của hợp đồng (nhãn của cả nhóm: Task M2-12).
func customerOut(p customer.Profile) *gen.Customer {
	out := &gen.Customer{
		ID: p.ID, RootId: p.RootID, Code: p.Code, FullName: string(p.FullName),
		Lifecycle: gen.CustomerLifecycle(p.Lifecycle), Stats: statsOut(p.Stats), MemberIds: p.MemberIDs,
		Identities: make([]gen.CustomerIdentity, 0, len(p.Identities)),
		Addresses:  make([]gen.CustomerAddress, 0, len(p.Addresses)),
		Tags:       make([]gen.CustomerTagLink, 0, len(p.Tags)), Version: p.Version, CreatedAt: p.CreatedAt,
	}
	for _, l := range p.Tags {
		out.Tags = append(out.Tags, gen.CustomerTagLink{Tag: tagOut(l.Tag), Source: gen.CustomerTagLinkSource(l.Source)})
	}
	if p.Birthday != nil {
		out.Birthday = gen.NewOptDate(time.Time(*p.Birthday))
	}
	for _, i := range p.Identities {
		item := gen.CustomerIdentity{
			ID: i.ID, CustomerId: i.CustomerID, Kind: gen.CustomerIdentityKind(i.Kind), Value: string(i.Value),
			Source: gen.CustomerIdentitySource(i.Source), CreatedAt: i.CreatedAt,
		}
		if i.Scope != "" {
			item.Scope = gen.NewOptString(i.Scope)
		}
		if i.VerifiedAt != nil {
			item.VerifiedAt = gen.NewOptDateTime(*i.VerifiedAt)
		}
		out.Identities = append(out.Identities, item)
	}
	for _, a := range p.Addresses {
		addr := gen.Address{
			Name: string(a.Name), Phone: string(a.Phone), Line: string(a.Line),
			ProvinceCode: gen.NewOptString(a.ProvinceCode), WardCode: gen.NewOptString(a.WardCode),
		}
		if a.ProvinceName != "" {
			addr.ProvinceName = gen.NewOptString(a.ProvinceName)
		}
		if a.WardName != "" {
			addr.WardName = gen.NewOptString(a.WardName)
		}
		out.Addresses = append(out.Addresses, gen.CustomerAddress{
			ID: a.ID, CustomerId: a.CustomerID, Address: addr, IsDefault: a.IsDefault, Version: a.Version,
		})
	}
	return out
}

func statsOut(s customer.Stats) gen.CustomerStats {
	out := gen.CustomerStats{
		OrderCount: int32(min(s.OrderCount, 1<<31-1)), DeliveredCount: int32(min(s.DeliveredCount, 1<<31-1)), //nolint:gosec // đã chặn
		ReturnedCount: int32(min(s.ReturnedCount, 1<<31-1)), TotalSpent: gen.Money(s.TotalSpent), //nolint:gosec // đã chặn
		ReturnRateBp: int32(min(s.ReturnRateBP, 1<<31-1)), //nolint:gosec // đã chặn
	}
	if s.LastOrderAt != nil {
		out.LastOrderAt = gen.NewOptDateTime(*s.LastOrderAt)
	}
	return out
}
