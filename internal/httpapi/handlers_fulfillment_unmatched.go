package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vận chuyển: sự kiện hãng của đơn ngoài hệ thống (MO07, Task M7-20 = 265).
// Khung của Task 260 (M7-15): mỗi operation trả 501 not_implemented (tests/arch/testdata/m7_pending.txt); task của
// vùng thay thân hàm trong tệp này và xóa dòng của mình khỏi danh sách, không sửa tệp của vùng khác.

// ListUnmatchedCarrierEvents (GET /api/v1/fulfillment/unmatched-events).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListUnmatchedCarrierEvents(_ context.Context, _ gen.ListUnmatchedCarrierEventsParams) (gen.ListUnmatchedCarrierEventsRes, error) {
	return nil, ht.ErrNotImplemented
}
