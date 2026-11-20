package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng partial_delivery (kế hoạch M3, Task M3-26): giao một phần.
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.

// RecordPartialDelivery (POST /api/v1/shipments/{id}/partial-delivery).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) RecordPartialDelivery(_ context.Context, _ *gen.RecordPartialDeliveryRequest, _ gen.RecordPartialDeliveryParams) (gen.RecordPartialDeliveryRes, error) {
	return nil, ht.ErrNotImplemented
}
