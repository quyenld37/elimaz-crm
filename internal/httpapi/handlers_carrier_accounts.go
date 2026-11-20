package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng carrier_accounts (kế hoạch M3, Task M3-09, M3-18): tài khoản hãng, khóa API, kiểm kết nối, đồng bộ địa giới.
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.

// ListCarrierAccounts (GET /api/v1/carrier-accounts).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListCarrierAccounts(_ context.Context, _ gen.ListCarrierAccountsParams) (gen.ListCarrierAccountsRes, error) {
	return nil, ht.ErrNotImplemented
}

// CreateCarrierAccount (POST /api/v1/carrier-accounts).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) CreateCarrierAccount(_ context.Context, _ *gen.CreateCarrierAccountRequest, _ gen.CreateCarrierAccountParams) (gen.CreateCarrierAccountRes, error) {
	return nil, ht.ErrNotImplemented
}

// UpdateCarrierAccount (PATCH /api/v1/carrier-accounts/{id}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) UpdateCarrierAccount(_ context.Context, _ *gen.UpdateCarrierAccountRequest, _ gen.UpdateCarrierAccountParams) (gen.UpdateCarrierAccountRes, error) {
	return nil, ht.ErrNotImplemented
}

// PutCarrierCredentials (PUT /api/v1/carrier-accounts/{id}/credentials).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) PutCarrierCredentials(_ context.Context, _ *gen.PutCarrierCredentialsRequest, _ gen.PutCarrierCredentialsParams) (gen.PutCarrierCredentialsRes, error) {
	return nil, ht.ErrNotImplemented
}

// CheckCarrierAccount (POST /api/v1/carrier-accounts/{id}/check).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) CheckCarrierAccount(_ context.Context, _ gen.CheckCarrierAccountParams) (gen.CheckCarrierAccountRes, error) {
	return nil, ht.ErrNotImplemented
}

// SetCarrierAccountStatus (POST /api/v1/carrier-accounts/{id}/status).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) SetCarrierAccountStatus(_ context.Context, _ *gen.SetCarrierAccountStatusRequest, _ gen.SetCarrierAccountStatusParams) (gen.SetCarrierAccountStatusRes, error) {
	return nil, ht.ErrNotImplemented
}

// SyncCarrierLocations (POST /api/v1/carrier-accounts/{id}/locations/sync).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) SyncCarrierLocations(_ context.Context, _ gen.SyncCarrierLocationsParams) (gen.SyncCarrierLocationsRes, error) {
	return nil, ht.ErrNotImplemented
}

// ListUnmappedLocations (GET /api/v1/carrier-locations/unmapped).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListUnmappedLocations(_ context.Context, _ gen.ListUnmappedLocationsParams) (gen.ListUnmappedLocationsRes, error) {
	return nil, ht.ErrNotImplemented
}
