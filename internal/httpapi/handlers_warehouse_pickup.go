package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng warehouse_pickup (kế hoạch M3, Task M3-09): địa chỉ lấy hàng của kho.
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.

// SetWarehousePickup (PUT /api/v1/inventory/warehouses/{id}/pickup).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) SetWarehousePickup(_ context.Context, _ *gen.SetWarehousePickupRequest, _ gen.SetWarehousePickupParams) (gen.SetWarehousePickupRes, error) {
	return nil, ht.ErrNotImplemented
}
