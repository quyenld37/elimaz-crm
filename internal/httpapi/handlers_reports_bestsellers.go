package httpapi

import (
	"context"
	"fmt"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/reporting"
)

// Vùng báo cáo: bán chạy (Task M6-09 = 232). Đọc kỳ, mức, cơ sở, limit; chạy tại chỗ qua runReport; đổi kết quả sang
// gen.SalesBestSellersReport. limit ngoài 1–500 do ogen chặn theo hợp đồng (400 bad_request).

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
	rep, ok := res.(reporting.BestSellersReport)
	if !ok {
		return nil, fmt.Errorf("kết quả bán chạy kiểu %T", res)
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
	out := gen.NewSalesBestSellersReportSalesBestSellersResponse(gen.SalesBestSellersReport{
		Status: gen.SalesBestSellersReportStatusReady,
		Period: gen.ReportPeriod{From: params.From, To: params.To},
		Level:  gen.SalesBestSellersReportLevel(p.Level), Basis: gen.SalesBestSellersReportBasis(p.Basis),
		Rows: rows, TotalRows: gen.ReportCount(rep.TotalRows),
	})
	return &out, nil
}

// optStringOut đổi chuỗi có thể vắng sang trường không bắt buộc của hợp đồng.
func optStringOut(s *string) gen.OptString {
	if s == nil {
		return gen.OptString{}
	}
	return gen.NewOptString(*s)
}
