package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng inventory_aged (kế hoạch M3, Task M3-22): giữ hàng quá hạn.
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.

// ListAgedReservations (GET /api/v1/inventory/aged-reservations).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListAgedReservations(_ context.Context, _ gen.ListAgedReservationsParams) (gen.ListAgedReservationsRes, error) {
	return nil, ht.ErrNotImplemented
}
