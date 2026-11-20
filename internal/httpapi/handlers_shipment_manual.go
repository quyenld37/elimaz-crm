package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng shipment_manual (kế hoạch M3, Task M3-15): cập nhật tay trạng thái vận đơn.
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.

// SetShipmentManualStatus (POST /api/v1/shipments/{id}/manual-status).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) SetShipmentManualStatus(_ context.Context, _ *gen.SetShipmentManualStatusRequest, _ gen.SetShipmentManualStatusParams) (gen.SetShipmentManualStatusRes, error) {
	return nil, ht.ErrNotImplemented
}
