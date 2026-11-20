package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng customer_stats (kế hoạch M3, Task M3-21): mức "khách hay hoàn".
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.

// GetCustomerDeliveryStats (GET /api/v1/customers/{id}/delivery-stats).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetCustomerDeliveryStats(_ context.Context, _ gen.GetCustomerDeliveryStatsParams) (gen.GetCustomerDeliveryStatsRes, error) {
	return nil, ht.ErrNotImplemented
}
