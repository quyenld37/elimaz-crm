package httpapi

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/reporting"
)

// Vùng báo cáo: báo cáo đơn và danh sách chi tiết (BC02, Task M6-07 = 230). Handler chỉ dựng tham số, chạy qua
// runReport (chỗ, hạn giờ, run_in_background của Task 228) và đổi kết quả sang kiểu của hợp đồng.

// GetSalesOrdersReport (GET /api/v1/reports/sales/orders).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetSalesOrdersReport(ctx context.Context, params gen.GetSalesOrdersReportParams) (gen.GetSalesOrdersReportRes, error) {
	p := &reporting.OrdersParams{
		SalesFilterParams: salesFilterParams(params.From, params.To, params.Basis, params.Channel, params.SourceId,
			params.CareBy, params.CreatedBy, params.PageId, params.CarrierKey),
		GroupBy: reporting.Dimension(params.GroupBy.Or("")),
	}
	res, bg, err := h.runReport(ctx, reporting.KeyOrders, p)
	if err != nil {
		return nil, err
	}
	if bg != nil {
		r := gen.NewRunInBackgroundSalesOrdersReportResponse(*bg)
		return &r, nil
	}
	out, err := ordersOut(res)
	if err != nil {
		return nil, err
	}
	r := gen.NewSalesOrdersReportSalesOrdersReportResponse(out)
	return &r, nil
}

// ordersOut đổi kết quả báo cáo đơn sang kiểu của hợp đồng (GET và lần chạy nền dùng chung).
func ordersOut(res any) (gen.SalesOrdersReport, error) {
	rep, ok := res.(*reporting.OrdersReport)
	if !ok {
		return gen.SalesOrdersReport{}, fmt.Errorf("kết quả báo cáo đơn %T", res)
	}
	out := gen.SalesOrdersReport{
		Status: gen.SalesOrdersReportStatusReady,
		Period: reportPeriod(rep.Period), Basis: gen.SalesOrdersReportBasis(rep.Basis),
		GroupBy: gen.ReportDimension(rep.GroupBy), Rows: make([]gen.SalesOrdersRow, 0, len(rep.Rows)),
		Totals: ordersRow(rep.Totals),
	}
	for _, r := range rep.Rows {
		out.Rows = append(out.Rows, ordersRow(r))
	}
	return out, nil
}

// ListSalesOrdersDrilldown (GET /api/v1/reports/sales/orders/drilldown).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListSalesOrdersDrilldown(ctx context.Context, params gen.ListSalesOrdersDrilldownParams) (gen.ListSalesOrdersDrilldownRes, error) {
	p := &reporting.DrilldownParams{
		SalesFilterParams: salesFilterParams(params.From, params.To, params.Basis, params.Channel, params.SourceId,
			params.CareBy, params.CreatedBy, params.PageId, params.CarrierKey),
		CellDimension: reporting.Dimension(params.CellDimension.Or("")), CellKey: params.CellKey.Or(""),
		Metric: reporting.Metric(params.Metric), Cursor: params.Cursor.Or(""), Limit: int(params.Limit.Or(0)),
	}
	if !params.CellKey.Set {
		// ReportDimension của hợp đồng có default status, nên ogen điền cellDimension cả khi người gọi không gửi; không
		// có cellKey là cả bảng (hợp đồng: cellKey "cần cellDimension", bỏ trống: cả bảng).
		p.CellDimension = ""
	}
	res, bg, err := h.runReport(ctx, reporting.KeyOrderDrilldown, p)
	if err != nil {
		return nil, err
	}
	if bg != nil {
		r := gen.NewRunInBackgroundSalesOrdersDrilldownResponse(*bg)
		return &r, nil
	}
	out, err := drilldownOut(res)
	if err != nil {
		return nil, err
	}
	r := gen.NewSalesOrdersDrilldownSalesOrdersDrilldownResponse(out)
	return &r, nil
}

// drilldownOut đổi một trang danh sách chi tiết sang kiểu của hợp đồng (GET và lần chạy nền dùng chung).
func drilldownOut(res any) (gen.SalesOrdersDrilldown, error) {
	page, ok := res.(*reporting.DrillPage)
	if !ok {
		return gen.SalesOrdersDrilldown{}, fmt.Errorf("kết quả danh sách chi tiết %T", res)
	}
	out := gen.SalesOrdersDrilldown{
		Status: gen.SalesOrdersDrilldownStatusReady, Total: gen.ReportCount(page.Total),
		Items: make([]gen.SalesOrdersDrilldownItem, 0, len(page.Items)),
	}
	for _, it := range page.Items {
		item := gen.SalesOrdersDrilldownItem{
			OrderId: it.OrderID, Code: it.Code, StatusLabel: gen.OrderLabel(it.StatusLabel), Channel: it.Channel,
			GoodsAmount: gen.Money(it.GoodsAmount), ShippingFeeCharged: gen.Money(it.ShippingFeeCharged),
		}
		for _, f := range []struct {
			v   *string
			dst *gen.OptString
		}{{it.CustomerCode, &item.CustomerCode}, {it.SourceName, &item.SourceName}, {it.CareByName, &item.CareByName}, {it.CarrierKey, &item.CarrierKey}} {
			if f.v != nil {
				f.dst.SetTo(*f.v)
			}
		}
		for _, f := range []struct {
			v   *time.Time
			dst *gen.OptDateTime
		}{{it.PlacedAt, &item.PlacedAt}, {it.CompletedAt, &item.CompletedAt}} {
			if f.v != nil {
				f.dst.SetTo(*f.v)
			}
		}
		out.Items = append(out.Items, item)
	}
	if page.Next != nil {
		out.NextCursor = gen.NewOptString(page.Next.String())
	}
	return out, nil
}

// salesFilterParams dựng bộ lọc chung của báo cáo đơn và danh sách chi tiết từ tham số query.
func salesFilterParams(from, to time.Time, basis gen.OptReportBasis, channels []gen.OrderChannel,
	sources, careBy, createdBy, pages []uuid.UUID, carriers []string,
) reporting.SalesFilterParams {
	p := reporting.SalesFilterParams{
		PeriodParams: reporting.PeriodParams{From: reportDay(from), To: reportDay(to)},
		Basis:        reporting.Basis(basis.Or("")), SourceIDs: sources, CareBy: careBy, CreatedBy: createdBy,
		PageIDs: pages, CarrierKeys: carriers,
	}
	for _, c := range channels {
		p.Channels = append(p.Channels, string(c))
	}
	return p
}

func reportPeriod(p reporting.Period) gen.ReportPeriod {
	from, _ := time.Parse(reportDayLayout, p.FromDay)
	to, _ := time.Parse(reportDayLayout, p.ToDay)
	return gen.ReportPeriod{From: from, To: to}
}

func ordersRow(r reporting.OrdersRow) gen.SalesOrdersRow {
	nil64 := func(v *int64) gen.NilInt64 {
		if v == nil {
			return gen.NilInt64{Null: true}
		}
		return gen.NewNilInt64(*v)
	}
	out := gen.SalesOrdersRow{
		Key: r.Key, Label: r.Label, Orders: gen.ReportCount(r.Orders), Placed: nil64(r.Placed), Active: nil64(r.Active),
		Cancelled: nil64(r.Cancelled), GoodsAmount: gen.Money(r.GoodsAmount), ShippingFeeCharged: gen.Money(r.ShippingFeeCharged),
		AvgGoodsAmount: gen.NilReportMoneyOrNull{Null: true}, Delivered: gen.ReportCount(r.Delivered),
		Returned: gen.ReportCount(r.Returned), SuccessRateBp: gen.NilReportRateBp{Null: true},
	}
	if r.AvgGoodsAmount != nil {
		out.AvgGoodsAmount = gen.NewNilReportMoneyOrNull(gen.ReportMoneyOrNull(*r.AvgGoodsAmount))
	}
	if r.SuccessRateBp != nil {
		out.SuccessRateBp = gen.NewNilReportRateBp(gen.ReportRateBp(*r.SuccessRateBp))
	}
	return out
}
