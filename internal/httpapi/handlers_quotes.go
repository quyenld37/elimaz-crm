package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng quotes (kế hoạch M3, Task M3-13): so phí các hãng trên đơn.
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.

// RequestShippingQuotes (POST /api/v1/orders/{id}/shipping-quotes).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) RequestShippingQuotes(_ context.Context, _ *gen.RequestShippingQuotesRequest, _ gen.RequestShippingQuotesParams) (gen.RequestShippingQuotesRes, error) {
	return nil, ht.ErrNotImplemented
}

// ListShippingQuotes (GET /api/v1/orders/{id}/shipping-quotes).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListShippingQuotes(_ context.Context, _ gen.ListShippingQuotesParams) (gen.ListShippingQuotesRes, error) {
	return nil, ht.ErrNotImplemented
}
