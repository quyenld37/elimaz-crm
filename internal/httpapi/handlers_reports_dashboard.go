package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng báo cáo: tổng quan (BC01, Task M6-06 = 229).
// Khung của Task 225 (M6-02): mỗi operation trả 501 not_implemented (tests/arch/testdata/m6_pending.txt); task
// của vùng thay thân hàm trong tệp này và xóa dòng của mình khỏi danh sách, không sửa tệp của vùng khác.

// GetSalesDashboard (GET /api/v1/reports/sales/dashboard).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetSalesDashboard(_ context.Context, _ gen.GetSalesDashboardParams) (gen.GetSalesDashboardRes, error) {
	return nil, ht.ErrNotImplemented
}
