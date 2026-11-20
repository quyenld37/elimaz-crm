package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng corrections (kế hoạch M3, Task M3-27): đảo "Thành công".
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.

// PreviewShipmentCorrection (POST /api/v1/shipments/{id}/corrections/preview).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) PreviewShipmentCorrection(_ context.Context, _ *gen.ShipmentCorrectionRequest, _ gen.PreviewShipmentCorrectionParams) (gen.PreviewShipmentCorrectionRes, error) {
	return nil, ht.ErrNotImplemented
}

// ApplyShipmentCorrection (POST /api/v1/shipments/{id}/corrections).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ApplyShipmentCorrection(_ context.Context, _ *gen.ApplyShipmentCorrectionRequest, _ gen.ApplyShipmentCorrectionParams) (gen.ApplyShipmentCorrectionRes, error) {
	return nil, ht.ErrNotImplemented
}
