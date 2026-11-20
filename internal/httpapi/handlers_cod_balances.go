package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng cod_balances (kế hoạch M3, Task M3-29): tiền COD của vận đơn.
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.

// GetShipmentCod (GET /api/v1/shipments/{id}/cod).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetShipmentCod(_ context.Context, _ gen.GetShipmentCodParams) (gen.GetShipmentCodRes, error) {
	return nil, ht.ErrNotImplemented
}
