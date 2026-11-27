package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng báo cáo: tồn kho, xuất nhập tồn (BC03, Task M6-11 = 234).
// Khung của Task 225 (M6-02): mỗi operation trả 501 not_implemented (tests/arch/testdata/m6_pending.txt); task
// của vùng thay thân hàm trong tệp này và xóa dòng của mình khỏi danh sách, không sửa tệp của vùng khác.

// GetInventoryStock (GET /api/v1/reports/inventory/stock).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetInventoryStock(_ context.Context, _ gen.GetInventoryStockParams) (gen.GetInventoryStockRes, error) {
	return nil, ht.ErrNotImplemented
}

// GetInventoryMovements (GET /api/v1/reports/inventory/movements).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetInventoryMovements(_ context.Context, _ gen.GetInventoryMovementsParams) (gen.GetInventoryMovementsRes, error) {
	return nil, ht.ErrNotImplemented
}
