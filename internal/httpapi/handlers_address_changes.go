package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng address_changes (kế hoạch M3, Task M3-17): đổi địa chỉ sau khi đã gửi hãng.
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.

// RequestAddressChange (POST /api/v1/shipments/{id}/address-changes).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) RequestAddressChange(_ context.Context, _ *gen.RequestAddressChangeRequest, _ gen.RequestAddressChangeParams) (gen.RequestAddressChangeRes, error) {
	return nil, ht.ErrNotImplemented
}

// ResolveAddressChange (POST /api/v1/address-changes/{id}/resolve).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ResolveAddressChange(_ context.Context, _ *gen.ResolveAddressChangeRequest, _ gen.ResolveAddressChangeParams) (gen.ResolveAddressChangeRes, error) {
	return nil, ht.ErrNotImplemented
}
