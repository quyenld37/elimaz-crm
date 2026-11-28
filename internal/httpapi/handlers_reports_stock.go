package httpapi

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/reporting"
)

// Vùng báo cáo: tồn kho, xuất nhập tồn (BC03, Task M6-11 = 234). Đọc tham số, chạy tại chỗ qua runReport (hết chỗ,
// quá hạn → run_in_background), đổi reporting.StockReport, reporting.MovementsReport sang kiểu của hợp đồng.

// GetInventoryStock (GET /api/v1/reports/inventory/stock).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetInventoryStock(ctx context.Context, params gen.GetInventoryStockParams) (gen.GetInventoryStockRes, error) {
	p := &reporting.StockParams{
		StockPage: stockPage(params.WarehouseId, params.Q, params.Cursor, params.Limit),
	}
	if d, ok := params.Date.Get(); ok {
		p.Date = reportDay(d)
	}
	res, bg, err := h.runReport(ctx, reporting.KeyStock, p)
	if err != nil {
		return nil, err
	}
	if bg != nil {
		r := gen.NewRunInBackgroundInventoryStockResponse(*bg)
		return &r, nil
	}
	out, err := stockOut(res)
	if err != nil {
		return nil, err
	}
	r := gen.NewInventoryStockInventoryStockResponse(out)
	return &r, nil
}

// stockOut đổi báo cáo tồn sang kiểu của hợp đồng (GET và lần chạy nền dùng chung).
func stockOut(res any) (gen.InventoryStock, error) {
	rep, ok := res.(reporting.StockReport)
	if !ok {
		return gen.InventoryStock{}, fmt.Errorf("báo cáo tồn: kết quả kiểu %T", res)
	}
	day, err := time.Parse(reportDayLayout, rep.Date)
	if err != nil {
		return gen.InventoryStock{}, fmt.Errorf("báo cáo tồn: ngày %q: %w", rep.Date, err)
	}
	out := gen.InventoryStock{
		Status: gen.InventoryStockStatusReady, Date: day, Source: gen.InventoryStockSource(rep.Source),
		Rows: make([]gen.InventoryStockRow, 0, len(rep.Rows)), NextCursor: nextCursor(rep.Next),
	}
	if rep.FirstSnapshotDay != nil {
		first, err := time.Parse(reportDayLayout, *rep.FirstSnapshotDay)
		if err != nil {
			return gen.InventoryStock{}, fmt.Errorf("báo cáo tồn: ngày chụp đầu %q: %w", *rep.FirstSnapshotDay, err)
		}
		out.FirstSnapshotDay = gen.NewOptDate(first)
	}
	for _, r := range rep.Rows {
		out.Rows = append(out.Rows, gen.InventoryStockRow{
			VariantId: r.VariantID, Sku: r.SKU, ProductName: r.ProductName, Color: r.Color, Size: r.Size,
			WarehouseId: r.WarehouseID, WarehouseName: r.WarehouseName,
			OnHand: r.OnHand, Held: r.Held, InTransit: r.InTransit, Damaged: r.Damaged, Available: r.Available,
		})
	}
	return out, nil
}

// GetInventoryMovements (GET /api/v1/reports/inventory/movements).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetInventoryMovements(ctx context.Context, params gen.GetInventoryMovementsParams) (gen.GetInventoryMovementsRes, error) {
	p := &reporting.MovementsParams{
		PeriodParams: reporting.PeriodParams{From: reportDay(params.From), To: reportDay(params.To)},
		StockPage:    stockPage(params.WarehouseId, params.Q, params.Cursor, params.Limit),
	}
	res, bg, err := h.runReport(ctx, reporting.KeyStockMovements, p)
	if err != nil {
		return nil, err
	}
	if bg != nil {
		r := gen.NewRunInBackgroundInventoryMovementsResponse(*bg)
		return &r, nil
	}
	out, err := movementsOut(res)
	if err != nil {
		return nil, err
	}
	r := gen.NewInventoryMovementsInventoryMovementsResponse(out)
	return &r, nil
}

// movementsOut đổi xuất nhập tồn sang kiểu của hợp đồng (GET và lần chạy nền dùng chung); kỳ là kỳ module đã đọc.
func movementsOut(res any) (gen.InventoryMovements, error) {
	rep, ok := res.(reporting.MovementsReport)
	if !ok {
		return gen.InventoryMovements{}, fmt.Errorf("xuất nhập tồn: kết quả kiểu %T", res)
	}
	out := gen.InventoryMovements{
		Status: gen.InventoryMovementsStatusReady, Period: reportPeriod(rep.Period),
		Rows: make([]gen.InventoryMovementRow, 0, len(rep.Rows)), NextCursor: nextCursor(rep.Next),
	}
	for _, r := range rep.Rows {
		out.Rows = append(out.Rows, gen.InventoryMovementRow{
			VariantId: r.VariantID, Sku: r.SKU, ProductName: r.ProductName, Color: r.Color, Size: r.Size,
			WarehouseId: r.WarehouseID, Opening: r.Opening, Imported: r.Imported, ReturnedIn: r.ReturnedIn, Sold: r.Sold,
			ExportedOther: r.ExportedOther, Adjusted: r.Adjusted, Closing: r.Closing, DamagedClosing: r.DamagedClosing,
		})
	}
	return out, nil
}

// stockPage đọc phần lọc kho, tiền tố SKU, con trỏ, limit chung của hai GET tồn (limit vắng → 0: module lấy mặc định).
func stockPage(wh gen.OptUUID, q, cursor gen.OptString, limit gen.OptInt32) reporting.StockPage {
	pg := reporting.StockPage{SKUPrefix: q.Or(""), Cursor: cursor.Or(""), Limit: int(limit.Or(0))}
	if id, ok := wh.Get(); ok {
		pg.WarehouseID = new(uuid.UUID)
		*pg.WarehouseID = id
	}
	return pg
}

// nextCursor đổi con trỏ trang sau sang tham số nextCursor (vắng khi hết trang).
func nextCursor(c *reporting.StockCursor) gen.OptString {
	if c == nil {
		return gen.OptString{}
	}
	return gen.NewOptString(c.String())
}
