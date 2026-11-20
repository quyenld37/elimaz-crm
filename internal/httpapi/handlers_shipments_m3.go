package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng shipments_m3 (kế hoạch M3, Task M3-12): vận đơn của đơn, gửi lại, xử lý "không rõ".
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.

// ListOrderShipments (GET /api/v1/orders/{id}/shipments).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListOrderShipments(_ context.Context, _ gen.ListOrderShipmentsParams) (gen.ListOrderShipmentsRes, error) {
	return nil, ht.ErrNotImplemented
}

// ResendShipment (POST /api/v1/shipments/{id}/resend).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ResendShipment(_ context.Context, _ gen.ResendShipmentParams) (gen.ResendShipmentRes, error) {
	return nil, ht.ErrNotImplemented
}

// ResolveShipmentCreate (POST /api/v1/shipments/{id}/resolve-create).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ResolveShipmentCreate(_ context.Context, _ *gen.ResolveShipmentCreateRequest, _ gen.ResolveShipmentCreateParams) (gen.ResolveShipmentCreateRes, error) {
	return nil, ht.ErrNotImplemented
}
