package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng shipment_sync (kế hoạch M3, Task M3-11): tra lại trạng thái vận đơn.
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.

// SyncShipment (POST /api/v1/shipments/{id}/sync).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) SyncShipment(_ context.Context, _ gen.SyncShipmentParams) (gen.SyncShipmentRes, error) {
	return nil, ht.ErrNotImplemented
}
