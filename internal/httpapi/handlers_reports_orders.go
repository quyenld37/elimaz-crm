package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng báo cáo: báo cáo đơn và danh sách chi tiết (BC02, Task M6-07 = 230).
// Khung của Task 225 (M6-02): mỗi operation trả 501 not_implemented (tests/arch/testdata/m6_pending.txt); task
// của vùng thay thân hàm trong tệp này và xóa dòng của mình khỏi danh sách, không sửa tệp của vùng khác.

// GetSalesOrdersReport (GET /api/v1/reports/sales/orders).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetSalesOrdersReport(_ context.Context, _ gen.GetSalesOrdersReportParams) (gen.GetSalesOrdersReportRes, error) {
	return nil, ht.ErrNotImplemented
}

// ListSalesOrdersDrilldown (GET /api/v1/reports/sales/orders/drilldown).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListSalesOrdersDrilldown(_ context.Context, _ gen.ListSalesOrdersDrilldownParams) (gen.ListSalesOrdersDrilldownRes, error) {
	return nil, ht.ErrNotImplemented
}
