package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng báo cáo: báo cáo hãng vận chuyển (Task M6-08 = 231).
// Khung của Task 225 (M6-02): mỗi operation trả 501 not_implemented (tests/arch/testdata/m6_pending.txt); task
// của vùng thay thân hàm trong tệp này và xóa dòng của mình khỏi danh sách, không sửa tệp của vùng khác.

// GetSalesCarriersReport (GET /api/v1/reports/sales/carriers).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetSalesCarriersReport(_ context.Context, _ gen.GetSalesCarriersReportParams) (gen.GetSalesCarriersReportRes, error) {
	return nil, ht.ErrNotImplemented
}
