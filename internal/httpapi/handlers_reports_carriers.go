package httpapi

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/reporting"
)

// Vùng báo cáo: báo cáo hãng vận chuyển (Task M6-08 = 231). Đọc kỳ, chạy tại chỗ qua runReport (hết chỗ, quá hạn →
// run_in_background), đổi reporting.CarriersReport sang gen.SalesCarriersReport.

// GetSalesCarriersReport (GET /api/v1/reports/sales/carriers).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetSalesCarriersReport(ctx context.Context, params gen.GetSalesCarriersReportParams) (gen.GetSalesCarriersReportRes, error) {
	p := &reporting.CarriersParams{PeriodParams: reporting.PeriodParams{From: reportDay(params.From), To: reportDay(params.To)}}
	res, bg, err := h.runReport(ctx, reporting.KeyCarriers, p)
	if err != nil {
		return nil, err
	}
	if bg != nil {
		r := gen.NewRunInBackgroundSalesCarriersReportResponse(*bg)
		return &r, nil
	}
	out, err := carriersOut(res)
	if err != nil {
		return nil, err
	}
	r := gen.NewSalesCarriersReportSalesCarriersReportResponse(out)
	return &r, nil
}

// carriersOut đổi báo cáo hãng sang kiểu của hợp đồng (GET và lần chạy nền dùng chung); kỳ là kỳ module đã đọc từ
// tham số.
func carriersOut(res any) (gen.SalesCarriersReport, error) {
	rep, ok := res.(reporting.CarriersReport)
	if !ok {
		return gen.SalesCarriersReport{}, fmt.Errorf("báo cáo hãng: kết quả kiểu %T", res)
	}
	out := gen.SalesCarriersReport{
		Status: gen.SalesCarriersReportStatusReady,
		Period: reportPeriod(rep.Period),
		Rows:   make([]gen.SalesCarrierRow, 0, len(rep.Rows)),
		Totals: carrierRowOut(rep.Totals),
	}
	for _, row := range rep.Rows {
		out.Rows = append(out.Rows, carrierRowOut(row))
	}
	return out, nil
}

// carrierRowOut đổi một dòng sang kiểu của hợp đồng: dòng tổng (mã tài khoản rỗng) không có carrierAccountId,
// accountName; tỷ lệ, trung bình nil → null.
func carrierRowOut(r reporting.CarrierRow) gen.SalesCarrierRow {
	out := gen.SalesCarrierRow{
		CarrierKey: r.CarrierKey,
		Created:    gen.ReportCount(r.Created), Cancelled: gen.ReportCount(r.Cancelled),
		InProgress: gen.ReportCount(r.InProgress), Returning: gen.ReportCount(r.Returning),
		DeliveryFailed: gen.ReportCount(r.DeliveryFailed), Delivered: gen.ReportCount(r.Delivered),
		Returned:      gen.ReportCount(r.Returned),
		SuccessRateBp: gen.NilReportRateBp{Null: true},
		FeeTotal:      gen.Money(r.FeeTotal), FeeAvg: gen.NilReportMoneyOrNull{Null: true},
		CodTotal: gen.Money(r.CODTotal),
	}
	if r.CarrierAccountID != uuid.Nil {
		out.CarrierAccountId = gen.NewOptUUID(r.CarrierAccountID)
		out.AccountName = gen.NewOptString(r.AccountName)
	}
	if r.SuccessRateBp != nil {
		out.SuccessRateBp = gen.NewNilReportRateBp(gen.ReportRateBp(*r.SuccessRateBp))
	}
	if r.FeeAvg != nil {
		out.FeeAvg = gen.NewNilReportMoneyOrNull(gen.ReportMoneyOrNull(*r.FeeAvg))
	}
	return out
}
