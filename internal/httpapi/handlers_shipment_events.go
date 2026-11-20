package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng shipment_events (kế hoạch M3, Task M3-08): lịch sử sự kiện của vận đơn.
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.

// ListShipmentEvents (GET /api/v1/shipments/{id}/events).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListShipmentEvents(_ context.Context, _ gen.ListShipmentEventsParams) (gen.ListShipmentEventsRes, error) {
	return nil, ht.ErrNotImplemented
}
