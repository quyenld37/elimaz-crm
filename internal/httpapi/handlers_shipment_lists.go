package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng shipment_lists (kế hoạch M3, Task M3-20): đơn kẹt, "Cần xem".
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.

// ListStuckShipments (GET /api/v1/shipments/stuck).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListStuckShipments(_ context.Context, _ gen.ListStuckShipmentsParams) (gen.ListStuckShipmentsRes, error) {
	return nil, ht.ErrNotImplemented
}

// ListShipmentsForReview (GET /api/v1/shipments/review).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListShipmentsForReview(_ context.Context, _ gen.ListShipmentsForReviewParams) (gen.ListShipmentsForReviewRes, error) {
	return nil, ht.ErrNotImplemented
}

// ResolveShipmentAnomaly (POST /api/v1/shipment-anomalies/{id}/resolve).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ResolveShipmentAnomaly(_ context.Context, _ *gen.ResolveShipmentAnomalyRequest, _ gen.ResolveShipmentAnomalyParams) (gen.ResolveShipmentAnomalyRes, error) {
	return nil, ht.ErrNotImplemented
}
