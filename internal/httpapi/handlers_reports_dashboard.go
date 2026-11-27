package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/reporting"
)

// Vùng báo cáo: tổng quan (BC01, Task M6-06 = 229).
// Khung chạy tại chỗ (Task 228): đọc kỳ, chạy qua runReport, trả run_in_background. Task 229 đổi kết quả sang
// gen.SalesDashboardReport trong tệp này và xóa dòng getSalesDashboard khỏi tests/arch/testdata/m6_pending.txt.

// GetSalesDashboard (GET /api/v1/reports/sales/dashboard).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetSalesDashboard(ctx context.Context, params gen.GetSalesDashboardParams) (gen.GetSalesDashboardRes, error) {
	p := &reporting.DashboardParams{PeriodParams: reporting.PeriodParams{From: reportDay(params.From), To: reportDay(params.To)}}
	_, bg, err := h.runReport(ctx, reporting.KeyDashboard, p)
	if err != nil {
		return nil, err
	}
	if bg != nil {
		r := gen.NewRunInBackgroundSalesDashboardResponse(*bg)
		return &r, nil
	}
	return nil, ht.ErrNotImplemented // Task 229: đổi kết quả sang gen.SalesDashboardReport
}
