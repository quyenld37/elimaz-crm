package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng shipment_cancel (kế hoạch M3, Task M3-14): hủy vận đơn, đổi hãng.
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.

// CancelShipment (POST /api/v1/shipments/{id}/cancel).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) CancelShipment(_ context.Context, _ *gen.CancelShipmentRequest, _ gen.CancelShipmentParams) (gen.CancelShipmentRes, error) {
	return nil, ht.ErrNotImplemented
}
