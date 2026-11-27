package httpapi

import (
	"context"
	"fmt"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/reporting"
)

// Vùng báo cáo: lựa chọn của bộ lọc báo cáo bán hàng trong phạm vi người xem (Task M6-07 = 230).

// GetSalesReportFilters (GET /api/v1/reports/sales/filters).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetSalesReportFilters(ctx context.Context) (gen.GetSalesReportFiltersRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	f, err := h.d.Reports.Filters(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("bộ lọc báo cáo: %w", err)
	}
	opts := func(in []reporting.FilterOption) []gen.ReportFilterOption {
		out := make([]gen.ReportFilterOption, 0, len(in))
		for _, o := range in {
			out = append(out, gen.ReportFilterOption{ID: o.ID, Name: o.Name})
		}
		return out
	}
	return &gen.SalesReportFilters{
		Sources: opts(f.Sources), Staff: opts(f.Staff), Pages: opts(f.Pages), CarrierKeys: f.CarrierKeys,
	}, nil
}
