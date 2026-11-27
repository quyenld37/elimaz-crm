package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng báo cáo: bộ lọc báo cáo bán hàng (Task M6-07 = 230).
// Khung của Task 225 (M6-02): mỗi operation trả 501 not_implemented (tests/arch/testdata/m6_pending.txt); task
// của vùng thay thân hàm trong tệp này và xóa dòng của mình khỏi danh sách, không sửa tệp của vùng khác.

// GetSalesReportFilters (GET /api/v1/reports/sales/filters).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetSalesReportFilters(_ context.Context) (gen.GetSalesReportFiltersRes, error) {
	return nil, ht.ErrNotImplemented
}
