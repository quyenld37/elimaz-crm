package httpapi

import (
	"context"
	"fmt"
	"time"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/reporting"
)

// Vùng báo cáo: tổng quan (BC01, Task 228 khung chạy tại chỗ, Task 229 kết quả).

// GetSalesDashboard (GET /api/v1/reports/sales/dashboard).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetSalesDashboard(ctx context.Context, params gen.GetSalesDashboardParams) (gen.GetSalesDashboardRes, error) {
	p := &reporting.DashboardParams{PeriodParams: reporting.PeriodParams{From: reportDay(params.From), To: reportDay(params.To)}}
	res, bg, err := h.runReport(ctx, reporting.KeyDashboard, p)
	if err != nil {
		return nil, err
	}
	if bg != nil {
		r := gen.NewRunInBackgroundSalesDashboardResponse(*bg)
		return &r, nil
	}
	out, err := dashboardOut(res)
	if err != nil {
		return nil, err
	}
	r := gen.NewSalesDashboardReportSalesDashboardResponse(out)
	return &r, nil
}

// dashboardOut đổi kết quả tổng quan của module sang kiểu của hợp đồng (GET và lần chạy nền dùng chung).
func dashboardOut(res any) (gen.SalesDashboardReport, error) {
	rep, ok := res.(reporting.DashboardReport)
	if !ok {
		return gen.SalesDashboardReport{}, fmt.Errorf("kết quả tổng quan %T", res)
	}
	return dashboardResponse(rep)
}

// dashboardResponse đổi kết quả của module sang kiểu của hợp đồng.
func dashboardResponse(r reporting.DashboardReport) (gen.SalesDashboardReport, error) {
	from, err := reportDate(r.Period.FromDay)
	if err != nil {
		return gen.SalesDashboardReport{}, err
	}
	to, err := reportDate(r.Period.ToDay)
	if err != nil {
		return gen.SalesDashboardReport{}, err
	}
	out := gen.SalesDashboardReport{
		Status: gen.SalesDashboardReportStatusReady, Period: gen.ReportPeriod{From: from, To: to}, GeneratedAt: r.GeneratedAt,
		Placed: reportAmounts(r.Placed), Active: reportAmounts(r.Active), Cancelled: reportAmounts(r.Cancelled),
		Delivered: reportAmounts(r.Delivered), Returned: reportAmounts(r.Returned),
		CustomerReturns: gen.SalesDashboardReportCustomerReturns{
			Items: gen.ReportCount(r.CustomerReturns.Items), RefundAmount: gen.Money(r.CustomerReturns.RefundAmount),
		},
		Pending: gen.SalesDashboardReportPending{
			Confirm: gen.ReportCount(r.Pending.Confirm), Ship: gen.ReportCount(r.Pending.Ship),
			CancelRequested: gen.ReportCount(r.Pending.CancelRequested),
		},
		ByChannel: make([]gen.SalesDashboardChannelRow, 0, len(r.ByChannel)),
		ByDay:     make([]gen.SalesDashboardDayRow, 0, len(r.ByDay)),
	}
	if r.AvgGoodsAmount == nil {
		out.AvgGoodsAmount.SetToNull()
	} else {
		out.AvgGoodsAmount.SetTo(gen.ReportMoneyOrNull(*r.AvgGoodsAmount))
	}
	for _, c := range r.ByChannel {
		out.ByChannel = append(out.ByChannel, gen.SalesDashboardChannelRow{
			Channel: c.Channel, Placed: reportAmounts(c.Placed), Active: reportAmounts(c.Active),
		})
	}
	for _, d := range r.ByDay {
		day, err := reportDate(d.Day)
		if err != nil {
			return gen.SalesDashboardReport{}, err
		}
		out.ByDay = append(out.ByDay, gen.SalesDashboardDayRow{
			Day: day, Active: reportAmounts(d.Active), Delivered: reportAmounts(d.Delivered),
		})
	}
	return out, nil
}

// reportDate đổi ngày YYYY-MM-DD của module sang giá trị format: date của ogen (00:00 UTC).
func reportDate(day string) (time.Time, error) {
	t, err := time.Parse(reportDayLayout, day)
	if err != nil {
		return time.Time{}, fmt.Errorf("ngày báo cáo %q: %w", day, err)
	}
	return t, nil
}

func reportAmounts(a reporting.Amounts) gen.ReportAmounts {
	return gen.ReportAmounts{
		Orders: gen.ReportCount(a.Orders), GoodsAmount: gen.Money(a.GoodsAmount), ShippingFeeCharged: gen.Money(a.ShippingFeeCharged),
	}
}
