package httpapi

import (
	"cmp"
	"context"
	"fmt"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/reporting"
)

// Vùng báo cáo: bán chạy (Task M6-09 = 232). Đọc kỳ, mức, cơ sở, limit; chạy tại chỗ qua runReport; đổi kết quả sang
// gen.SalesBestSellersReport. limit ngoài 1–500 do ogen chặn theo hợp đồng (400 bad_params).

// GetSalesBestSellers (GET /api/v1/reports/sales/best-sellers).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetSalesBestSellers(ctx context.Context, params gen.GetSalesBestSellersParams) (gen.GetSalesBestSellersRes, error) {
	p := &reporting.BestSellersParams{
		PeriodParams: reporting.PeriodParams{From: reportDay(params.From), To: reportDay(params.To)},
		Level:        string(params.Level.Or(gen.GetSalesBestSellersLevelSku)),
		Basis:        string(params.Basis.Or(gen.GetSalesBestSellersBasisPlaced)),
		Limit:        int(params.Limit.Or(reporting.DefaultBestSellersLimit)),
	}
	res, bg, err := h.runReport(ctx, reporting.KeyBestSellers, p)
	if err != nil {
		return nil, err
	}
	if bg != nil {
		r := gen.NewRunInBackgroundSalesBestSellersResponse(*bg)
		return &r, nil
	}
	out, err := bestSellersOut(p, res)
	if err != nil {
		return nil, err
	}
	r := gen.NewSalesBestSellersReportSalesBestSellersResponse(out)
	return &r, nil
}

// bestSellersOut đổi kết quả bán chạy (tham số p) sang kiểu của hợp đồng (GET và lần chạy nền dùng chung); mức, cơ
// sở bỏ trống là sku, placed như module.
func bestSellersOut(p *reporting.BestSellersParams, res any) (gen.SalesBestSellersReport, error) {
	rep, ok := res.(reporting.BestSellersReport)
	if !ok {
		return gen.SalesBestSellersReport{}, fmt.Errorf("kết quả bán chạy kiểu %T", res)
	}
	period, err := paramsPeriod(p.PeriodParams)
	if err != nil {
		return gen.SalesBestSellersReport{}, err
	}
	rows := make([]gen.SalesBestSellerRow, 0, len(rep.Rows))
	for _, r := range rep.Rows {
		rows = append(rows, gen.SalesBestSellerRow{
			ProductId: r.ProductID, VariantId: optUUIDOut(r.VariantID), Sku: optStringOut(r.SKU), Name: r.Name,
			Color: optStringOut(r.Color), Size: optStringOut(r.Size), Quantity: gen.ReportCount(r.Quantity),
			Orders: gen.ReportCount(r.Orders), GoodsAmount: gen.Money(r.GoodsAmount),
			ReturnedQuantity: gen.ReportCount(r.ReturnedQuantity),
		})
	}
	return gen.SalesBestSellersReport{
		Status: gen.SalesBestSellersReportStatusReady, Period: period,
		Level: gen.SalesBestSellersReportLevel(cmp.Or(p.Level, reporting.LevelSKU)),
		Basis: gen.SalesBestSellersReportBasis(cmp.Or(p.Basis, reporting.SalesBasisPlaced)),
		Rows:  rows, TotalRows: gen.ReportCount(rep.TotalRows),
	}, nil
}

// paramsPeriod là kỳ của hợp đồng đọc từ tham số from, to (đã kiểm) của module.
func paramsPeriod(p reporting.PeriodParams) (gen.ReportPeriod, error) {
	from, err := reportDate(p.From)
	if err != nil {
		return gen.ReportPeriod{}, err
	}
	to, err := reportDate(p.To)
	if err != nil {
		return gen.ReportPeriod{}, err
	}
	return gen.ReportPeriod{From: from, To: to}, nil
}

// optStringOut đổi chuỗi có thể vắng sang trường không bắt buộc của hợp đồng.
func optStringOut(s *string) gen.OptString {
	if s == nil {
		return gen.OptString{}
	}
	return gen.NewOptString(*s)
}
