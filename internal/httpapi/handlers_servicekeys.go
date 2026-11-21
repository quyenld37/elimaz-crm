package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Quản lý khóa dịch vụ của website (kế hoạch M5 Task 189; quyền identity.service_keys.manage). Bí mật của khóa chỉ có
// trong câu trả lời 201 của createServiceKey (route no-replay: thân không lưu vào khóa chống trùng, gửi lại → 409).

// ListServiceKeys (GET /api/v1/identity/service-keys): mọi khóa của tenant người gọi, không bí mật, không băm.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListServiceKeys(ctx context.Context) (gen.ListServiceKeysRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var keys []identity.ServiceKey
	err := tenancy.WithTenant(ctx, h.d.DB, u.TenantID, func(tx pgx.Tx) error {
		var err error
		keys, err = identity.ListServiceKeys(ctx, tx)
		return err //nolint:wrapcheck // identity đã bọc
	}, db.TxAPI)
	if err != nil {
		return nil, fmt.Errorf("liệt kê khóa dịch vụ: %w", err)
	}
	out := make(gen.ListServiceKeysOKApplicationJSON, 0, len(keys))
	for _, k := range keys {
		out = append(out, serviceKeyOut(k))
	}
	return &out, nil
}

// CreateServiceKey (POST /api/v1/identity/service-keys, no-replay) tạo khóa → 201 kèm khóa đầy đủ (secret) đúng một
// lần (luật ở identity.CreateServiceKey). Lỗi đầu vào: tx lùi, khóa chống trùng được trả lại.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateServiceKey(ctx context.Context, req *gen.CreateServiceKeyRequest, _ gen.CreateServiceKeyParams) (gen.CreateServiceKeyRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	in := identity.CreateServiceKeyInput{Name: req.Name, Actor: u}
	for _, p := range req.Permissions {
		in.Permissions = append(in.Permissions, identity.Permission(p))
	}
	for _, c := range req.AllowedCidrs {
		p, err := netip.ParsePrefix(c)
		if err != nil {
			return nil, fmt.Errorf("CIDR của khóa: %w", identity.ErrInvalidInput)
		}
		in.AllowedCIDRs = append(in.AllowedCIDRs, p)
	}
	var (
		key     identity.ServiceKey
		full    string
		refused error
	)
	err := WriteTx(ctx, h.d, u.TenantID, func(tx pgx.Tx) (int, any, bool, error) {
		key, full, refused = identity.ServiceKey{}, "", nil // fn có thể chạy lại: gán lại mỗi lần
		k, f, err := identity.CreateServiceKey(ctx, tx, in)
		if isKeyRefusal(err) {
			refused = err
			return 0, nil, false, nil
		}
		if err != nil {
			return 0, nil, false, err //nolint:wrapcheck // identity đã bọc
		}
		key, full = k, f
		return http.StatusCreated, nil, true, nil // no-replay: thân không lưu
	})
	switch {
	case err != nil:
		return nil, err
	case refused != nil:
		return nil, fmt.Errorf("tạo khóa dịch vụ: %w", refused)
	}
	o := serviceKeyOut(key)
	return &gen.CreatedServiceKey{
		ID: o.ID, Name: o.Name, KeyPrefix: o.KeyPrefix, Permissions: o.Permissions, AllowedCidrs: o.AllowedCidrs,
		CreatedAt: o.CreatedAt, RevokedAt: o.RevokedAt, LastUsedAt: o.LastUsedAt, Version: o.Version, Secret: full,
	}, nil
}

// RevokeServiceKey (POST /api/v1/identity/service-keys/{id}/revoke) thu hồi khóa khi version còn khớp và phát báo hiệu
// hệ thống identity.TopicServiceKeyRevoked trong cùng transaction (bộ kiểm khóa của mọi tiến trình api xóa đệm sau
// commit). identity không phát được (realtime nhập identity), nên lớp HTTP phát.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) RevokeServiceKey(ctx context.Context, req *gen.RevokeServiceKeyRequest, params gen.RevokeServiceKeyParams) (gen.RevokeServiceKeyRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var (
		out     *gen.ServiceKey
		refused error
	)
	err := WriteTx(ctx, h.d, u.TenantID, func(tx pgx.Tx) (int, any, bool, error) {
		out, refused = nil, nil // fn có thể chạy lại: gán lại mỗi lần
		k, err := identity.RevokeServiceKey(ctx, tx, params.ID, req.Version, u)
		if isKeyRefusal(err) {
			refused = err
			return 0, nil, false, nil
		}
		if err != nil {
			return 0, nil, false, err //nolint:wrapcheck // identity đã bọc
		}
		if h.d.Realtime != nil {
			if err := h.d.Realtime.Publish(ctx, tx, realtime.Signal{
				TenantID: u.TenantID, Topic: identity.TopicServiceKeyRevoked, System: true,
				Entity: realtime.Entity{Type: identity.RouteKindServiceKey, ID: k.KeyPrefix},
			}); err != nil {
				return 0, nil, false, fmt.Errorf("báo hiệu thu hồi khóa: %w", err)
			}
		}
		o := serviceKeyOut(k)
		out = &o
		return http.StatusOK, out, true, nil
	})
	switch {
	case err != nil:
		return nil, err
	case refused != nil:
		return nil, fmt.Errorf("thu hồi khóa dịch vụ: %w", refused)
	}
	return out, nil
}

// isKeyRefusal: lỗi nghiệp vụ của khóa dịch vụ — câu trả lời không đổi gì (không lưu vào khóa chống trùng).
func isKeyRefusal(err error) bool {
	for _, e := range []error{
		identity.ErrInvalidInput, identity.ErrPermissionNotForKey, identity.ErrVersionConflict, identity.ErrServiceKeyNotFound,
	} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

func serviceKeyOut(k identity.ServiceKey) gen.ServiceKey {
	o := gen.ServiceKey{
		ID: k.ID, Name: k.Name, KeyPrefix: k.KeyPrefix, Permissions: make([]string, 0, len(k.Permissions)),
		AllowedCidrs: make([]string, 0, len(k.AllowedCIDRs)), CreatedAt: k.CreatedAt, Version: k.Version,
	}
	for _, p := range k.Permissions {
		o.Permissions = append(o.Permissions, string(p))
	}
	for _, c := range k.AllowedCIDRs {
		o.AllowedCidrs = append(o.AllowedCidrs, c.String())
	}
	if k.RevokedAt != nil {
		o.RevokedAt = gen.NewOptDateTime(*k.RevokedAt)
	}
	if k.LastUsedAt != nil {
		o.LastUsedAt = gen.NewOptDateTime(*k.LastUsedAt)
	}
	return o
}
