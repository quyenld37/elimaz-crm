package reporting

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/reporting/internal/store"
	"elimaz.com.vn/crm/internal/platform/xlsx"
)

// Cột, dòng và tên tệp XLSX theo báo cáo (Task 237). Mỗi Rows gọi đúng hàm đọc của bản xem tại chỗ (cùng số với GET),
// đọc hết tập theo trang (keyset) thay cho giới hạn trang của bản xem; tiêu đề cột theo chữ của màn hình.

// typed đổi Params về kiểu tham số của báo cáo; sai kiểu → ErrBadParams.
func typed[P any](p Params) (*P, error) {
	v, ok := any(p).(*P)
	if !ok {
		return nil, fmt.Errorf("tham số kiểu %T: %w", p, ErrBadParams)
	}
	return v, nil
}

func col(title string, k xlsx.Kind) xlsx.Column { return xlsx.Column{Title: title, Kind: k} }

// optInt là ô số, "—" khi nil (như màn hình).
func optInt(v *int64) xlsx.Cell {
	if v == nil {
		return xlsx.Str("—")
	}
	return xlsx.Int(*v)
}

// optMoney là ô tiền, "—" khi nil.
func optMoney(v *int64) xlsx.Cell {
	if v == nil {
		return xlsx.Str("—")
	}
	return xlsx.Money(*v)
}

func optStr(v *string) xlsx.Cell {
	if v == nil || *v == "" {
		return xlsx.Str("—")
	}
	return xlsx.Str(*v)
}

// periodFile là tên tệp "<tên>_<from>_<to>.xlsx" của báo cáo theo kỳ.
func periodFile(base string) func(p Params) string {
	return func(p Params) string {
		if pp, ok := p.(interface{ Period() (Period, error) }); ok {
			if period, err := pp.Period(); err == nil {
				return base + "_" + period.FromDay + "_" + period.ToDay + ".xlsx"
			}
		}
		return base + ".xlsx"
	}
}

// amountCells là số đơn, doanh số, phí ship của một tập.
func amountCells(label string, a Amounts) []xlsx.Cell {
	return []xlsx.Cell{xlsx.Str(label), xlsx.Int(a.Orders), xlsx.Money(a.GoodsAmount), xlsx.Money(a.ShippingFeeCharged)}
}

// ---------------------------------------------------------------- Tổng quan

func dashboardColumns(Params) []xlsx.Column {
	return []xlsx.Column{
		col("Chỉ số", xlsx.KindString), col("Số đơn", xlsx.KindInt), col("Doanh số", xlsx.KindMoney),
		col("Phí ship thu khách", xlsx.KindMoney),
	}
}

// dashboardRows: bảng chỉ số, rồi bảng theo kênh (đơn lên, chưa hủy), rồi bảng theo ngày (khi kỳ dài hơn một ngày);
// nhãn dòng ghi rõ kênh, ngày.
func dashboardRows(ctx context.Context, tx pgx.Tx, s DataScope, p Params, emit func([]xlsx.Cell) error) error {
	dp, err := typed[DashboardParams](p)
	if err != nil {
		return err
	}
	r, err := dashboard(ctx, tx, s, dp)
	if err != nil {
		return err
	}
	none := xlsx.Empty()
	rows := [][]xlsx.Cell{
		amountCells("Đơn lên", r.Placed), amountCells("Đơn chưa hủy", r.Active), amountCells("Đơn hủy", r.Cancelled),
		amountCells("Thành công", r.Delivered), amountCells("Hoàn", r.Returned),
		{xlsx.Str("Giá trị TB (đơn chưa hủy)"), none, optMoney(r.AvgGoodsAmount), none},
		{xlsx.Str("Hàng khách trả (tiền hoàn)"), xlsx.Int(r.CustomerReturns.Items), xlsx.Money(r.CustomerReturns.RefundAmount), none},
		{xlsx.Str("Chờ xác nhận"), xlsx.Int(r.Pending.Confirm), none, none},
		{xlsx.Str("Chờ gửi hãng"), xlsx.Int(r.Pending.Ship), none, none},
		{xlsx.Str("Đang yêu cầu hủy"), xlsx.Int(r.Pending.CancelRequested), none, none},
	}
	for _, c := range r.ByChannel {
		name := rowLabel(DimChannel, c.Channel, "")
		rows = append(rows, amountCells("Kênh "+name+" · Đơn lên", c.Placed), amountCells("Kênh "+name+" · Chưa hủy", c.Active))
	}
	for _, d := range r.ByDay {
		day := rowLabel(DimDay, d.Day, "")
		rows = append(rows, amountCells(day+" · Chưa hủy", d.Active), amountCells(day+" · Thành công", d.Delivered))
	}
	for _, row := range rows {
		if err := emit(row); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- Báo cáo đơn

//nolint:gochecknoglobals // bảng hằng chỉ đọc
var dimensionTitles = map[Dimension]string{
	DimCareBy: "Nhân viên phụ trách", DimStatus: "Trạng thái", DimSource: "Nguồn", DimCreatedBy: "Người tạo",
	DimChannel: "Kênh", DimPage: "Page", DimCarrier: "Hãng", DimDay: "Ngày",
}

func ordersColumns(p Params) []xlsx.Column {
	op, err := typed[OrdersParams](p)
	if err != nil {
		op = &OrdersParams{}
	}
	dim, err := op.dimension()
	if err != nil {
		dim = DimStatus
	}
	cols := []xlsx.Column{col(dimensionTitles[dim], xlsx.KindString), col("Số đơn", xlsx.KindInt)}
	if op.Basis != BasisCompleted {
		cols = append(cols, col("Chưa hủy", xlsx.KindInt), col("Hủy", xlsx.KindInt))
	}
	return append(cols, col("Tiền hàng", xlsx.KindMoney), col("Phí ship thu khách", xlsx.KindMoney),
		col("Giá trị TB", xlsx.KindMoney), col("Giao thành công", xlsx.KindInt), col("Hoàn", xlsx.KindInt),
		col("Tỷ lệ thành công", xlsx.KindString))
}

// ordersRows: các dòng của báo cáo đơn rồi dòng tổng (cùng cột với ordersColumns).
func ordersRows(ctx context.Context, tx pgx.Tx, s DataScope, p Params, emit func([]xlsx.Cell) error) error {
	op, err := typed[OrdersParams](p)
	if err != nil {
		return err
	}
	r, err := ordersReport(ctx, tx, s, op)
	if err != nil {
		return err
	}
	for _, row := range append(append([]OrdersRow{}, r.Rows...), r.Totals) {
		cells := []xlsx.Cell{xlsx.Str(row.Label), xlsx.Int(row.Orders)}
		if r.Basis != BasisCompleted {
			cells = append(cells, optInt(row.Active), optInt(row.Cancelled))
		}
		cells = append(cells, xlsx.Money(row.GoodsAmount), xlsx.Money(row.ShippingFeeCharged), optMoney(row.AvgGoodsAmount),
			xlsx.Int(row.Delivered), xlsx.Int(row.Returned), xlsx.Str(rateText(row.SuccessRateBp)))
		if err := emit(cells); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- Danh sách chi tiết

func drilldownColumns(p Params) []xlsx.Column {
	at := "Lên lúc"
	if dp, err := typed[DrilldownParams](p); err == nil && dp.Basis == BasisCompleted {
		at = "Hoàn tất lúc"
	}
	return []xlsx.Column{
		col("Mã đơn", xlsx.KindString), col("Mã khách", xlsx.KindString), col(at, xlsx.KindDate),
		col("Trạng thái", xlsx.KindString), col("Kênh", xlsx.KindString), col("Nguồn", xlsx.KindString),
		col("Phụ trách", xlsx.KindString), col("Hãng", xlsx.KindString), col("Tiền hàng", xlsx.KindMoney),
		col("Ship", xlsx.KindMoney),
	}
}

// drilldownRows: mọi đơn của tập (theo trang MaxDrillLimit, con trỏ của Drilldown; bỏ cursor, limit của tham số).
func drilldownRows(ctx context.Context, tx pgx.Tx, s DataScope, p Params, emit func([]xlsx.Cell) error) error {
	dp, err := typed[DrilldownParams](p)
	if err != nil {
		return err
	}
	a, err := dp.resolve()
	if err != nil {
		return err
	}
	completed := a.f.Basis == BasisCompleted
	var cursor *DrillCursor
	for {
		page, err := Drilldown(ctx, tx, s, a.f, dp.Metric, a.cell, cursor, MaxDrillLimit)
		if err != nil {
			return err
		}
		for _, it := range page.Items {
			if err := emit(drillCells(it, completed)); err != nil {
				return err
			}
		}
		if page.Next == nil {
			return nil
		}
		cursor = page.Next
	}
}

// drillCells là một dòng của danh sách chi tiết: mốc hoàn tất khi cơ sở completed (không có thì mốc lên đơn, như màn).
func drillCells(it DrillItem, completed bool) []xlsx.Cell {
	at := it.PlacedAt
	if completed && it.CompletedAt != nil {
		at = it.CompletedAt
	}
	when := xlsx.Empty()
	if at != nil {
		when = xlsx.Date(*at)
	}
	carrier := xlsx.Str("—")
	if it.CarrierKey != nil {
		carrier = xlsx.Str(rowLabel(DimCarrier, *it.CarrierKey, ""))
	}
	care := xlsx.Str(unknownLabels[DimCareBy])
	if it.CareByName != nil && *it.CareByName != "" {
		care = xlsx.Str(*it.CareByName)
	}
	return []xlsx.Cell{
		xlsx.Str(it.Code), optStr(it.CustomerCode), when, xlsx.Str(rowLabel(DimStatus, string(it.StatusLabel), "")),
		xlsx.Str(rowLabel(DimChannel, it.Channel, "")), optStr(it.SourceName), care, carrier,
		xlsx.Money(it.GoodsAmount), xlsx.Money(it.ShippingFeeCharged),
	}
}

// ---------------------------------------------------------------- Hãng vận chuyển

func carriersColumns(Params) []xlsx.Column {
	cols := []xlsx.Column{col("Hãng / tài khoản", xlsx.KindString)}
	for _, t := range []string{"Vận đơn tạo", "Đã hủy", "Đang đi", "Đang hoàn", "Giao thất bại chờ xử lý", "Thành công", "Hoàn"} {
		cols = append(cols, col(t, xlsx.KindInt))
	}
	return append(cols, col("Tỷ lệ thành công", xlsx.KindString), col("Tổng phí", xlsx.KindMoney),
		col("Phí TB", xlsx.KindMoney), col("Tổng COD", xlsx.KindMoney))
}

func carriersRows(ctx context.Context, tx pgx.Tx, s DataScope, p Params, emit func([]xlsx.Cell) error) error {
	cp, err := typed[CarriersParams](p)
	if err != nil {
		return err
	}
	r, err := carriersReport(ctx, tx, s, *cp)
	if err != nil {
		return err
	}
	for _, row := range append(append([]CarrierRow{}, r.Rows...), r.Totals) {
		label := totalLabel
		if row.CarrierKey != "" {
			label = rowLabel(DimCarrier, row.CarrierKey, "")
			if row.AccountName != "" {
				label += " · " + row.AccountName
			}
		}
		if err := emit([]xlsx.Cell{
			xlsx.Str(label), xlsx.Int(row.Created), xlsx.Int(row.Cancelled), xlsx.Int(row.InProgress), xlsx.Int(row.Returning),
			xlsx.Int(row.DeliveryFailed), xlsx.Int(row.Delivered), xlsx.Int(row.Returned), xlsx.Str(rateText(row.SuccessRateBp)),
			xlsx.Money(row.FeeTotal), optMoney(row.FeeAvg), xlsx.Money(row.CODTotal),
		}); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- Bán chạy

func bestSellersColumns(p Params) []xlsx.Column {
	cols := []xlsx.Column{col("#", xlsx.KindInt)}
	if bp, err := typed[BestSellersParams](p); err == nil && bp.level() == LevelProduct {
		cols = append(cols, col("Mẫu", xlsx.KindString))
	} else {
		cols = append(cols, col("SKU", xlsx.KindString), col("Tên", xlsx.KindString), col("Màu", xlsx.KindString),
			col("Size", xlsx.KindString))
	}
	return append(cols, col("Số lượng", xlsx.KindInt), col("Số đơn", xlsx.KindInt), col("Doanh số", xlsx.KindMoney),
		col("Hàng khách trả", xlsx.KindInt))
}

// bestSellersRows: mọi dòng (bestSellerRows limit 0, không giới hạn 500 của bản xem).
func bestSellersRows(ctx context.Context, tx pgx.Tx, s DataScope, p Params, emit func([]xlsx.Cell) error) error {
	bp, err := typed[BestSellersParams](p)
	if err != nil {
		return err
	}
	rows, _, err := bestSellerRows(ctx, tx, s, bp, 0)
	if err != nil {
		return err
	}
	product := bp.level() == LevelProduct
	for i, r := range rows {
		cells := []xlsx.Cell{xlsx.Int(int64(i + 1))}
		if product {
			cells = append(cells, xlsx.Str(r.Name))
		} else {
			cells = append(cells, optStr(r.SKU), xlsx.Str(r.Name), optStr(r.Color), optStr(r.Size))
		}
		cells = append(cells, xlsx.Int(r.Quantity), xlsx.Int(r.Orders), xlsx.Money(r.GoodsAmount), xlsx.Int(r.ReturnedQuantity))
		if err := emit(cells); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- Tồn, xuất nhập tồn

// skuColumns là năm cột đầu của hai báo cáo tồn (SKU, Tên, Màu, Size, Kho) rồi các cột số counts.
func skuColumns(counts ...string) []xlsx.Column {
	cols := make([]xlsx.Column, 0, 5+len(counts))
	for _, t := range []string{"SKU", "Tên", "Màu", "Size", "Kho"} {
		cols = append(cols, col(t, xlsx.KindString))
	}
	for _, t := range counts {
		cols = append(cols, col(t, xlsx.KindInt))
	}
	return cols
}

// skuCells là dòng của skuColumns.
func skuCells(sku, name, color, size, warehouse string, counts ...int64) []xlsx.Cell {
	cells := []xlsx.Cell{xlsx.Str(sku), xlsx.Str(name), xlsx.Str(color), xlsx.Str(size), xlsx.Str(warehouse)}
	for _, n := range counts {
		cells = append(cells, xlsx.Int(n))
	}
	return cells
}

func stockColumns(Params) []xlsx.Column {
	return skuColumns("Tồn sổ", "Đang giữ", "Đang giao", "Hàng lỗi", "Có thể bán")
}

func stockFileName(p Params) string {
	day := Today(time.Now())
	if sp, err := typed[StockParams](p); err == nil && sp.Date != "" {
		day = sp.Date
	}
	return "ton-kho_" + day + ".xlsx"
}

// stockExportRows đọc hết báo cáo tồn theo trang MaxStockLimit bằng stockReport (bỏ cursor, limit của tham số; ngày giữ
// đúng ngày của trang đầu).
func stockExportRows(ctx context.Context, tx pgx.Tx, s DataScope, p Params, emit func([]xlsx.Cell) error) error {
	sp, err := typed[StockParams](p)
	if err != nil {
		return err
	}
	page := *sp
	page.Cursor, page.Limit = "", MaxStockLimit
	for {
		r, err := stockReport(ctx, tx, s, &page, time.Now())
		if err != nil {
			return err
		}
		for _, x := range r.Rows {
			if err := emit(skuCells(x.SKU, x.ProductName, x.Color, x.Size, x.WarehouseName,
				x.OnHand, x.Held, x.InTransit, x.Damaged, x.Available)); err != nil {
				return err
			}
		}
		if r.Next == nil {
			return nil
		}
		page.Date, page.Cursor = r.Date, r.Next.String()
	}
}

func movementsColumns(Params) []xlsx.Column {
	return skuColumns("Đầu kỳ", "Nhập", "Nhập trả", "Xuất bán", "Xuất khác", "Kiểm kho / điều chỉnh", "Cuối kỳ", "Hàng lỗi cuối kỳ")
}

// movementsExportRows đọc hết xuất nhập tồn theo trang MaxStockLimit bằng movementsReport; tên kho đọc một lần.
func movementsExportRows(ctx context.Context, tx pgx.Tx, s DataScope, p Params, emit func([]xlsx.Cell) error) error {
	mp, err := typed[MovementsParams](p)
	if err != nil {
		return err
	}
	page := *mp
	page.Cursor, page.Limit = "", MaxStockLimit
	names := map[uuid.UUID]string{}
	for {
		r, err := movementsReport(ctx, tx, s, &page)
		if err != nil {
			return err
		}
		if err := warehouseNames(ctx, tx, r.Rows, names); err != nil {
			return err
		}
		for _, x := range r.Rows {
			if err := emit(skuCells(x.SKU, x.ProductName, x.Color, x.Size, names[x.WarehouseID], x.Opening, x.Imported,
				x.ReturnedIn, x.Sold, x.ExportedOther, x.Adjusted, x.Closing, x.DamagedClosing)); err != nil {
				return err
			}
		}
		if r.Next == nil {
			return nil
		}
		page.Cursor = r.Next.String()
	}
}

// warehouseNames thêm vào names tên các kho của rows chưa có.
func warehouseNames(ctx context.Context, tx pgx.Tx, rows []MovementRow, names map[uuid.UUID]string) error {
	var missing []uuid.UUID
	for _, r := range rows {
		if _, ok := names[r.WarehouseID]; !ok && !slices.Contains(missing, r.WarehouseID) {
			missing = append(missing, r.WarehouseID)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	got, err := store.New(tx).WarehouseNames(ctx, missing)
	if err != nil {
		return fmt.Errorf("đọc tên kho: %w", err)
	}
	for _, w := range got {
		names[w.WarehouseID] = w.Name
	}
	return nil
}

// ---------------------------------------------------------------- Dòng đầu tệp

// rateText đổi tỷ lệ phần vạn (đã làm tròn xuống như API) ra chữ "66,66%"; nil → "—".
func rateText(bp *int32) string {
	if bp == nil {
		return "—"
	}
	v := *bp
	return strconv.Itoa(int(v/100)) + "," + fmt.Sprintf("%02d", v%100) + "%"
}

// groupDigits viết số nguyên có dấu chấm ngăn hàng nghìn: 200000 → "200.000".
func groupDigits(n int) string {
	s := strconv.Itoa(n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// sheetName là tên trang của tệp: danh sách sản phẩm dùng "Sản phẩm" như tệp mẫu nhập, báo cáo khác "Báo cáo".
func sheetName(k Key) string {
	name := "Báo cáo"
	if k == KeyProducts {
		name = "Sản phẩm"
	}
	return name
}

// exportMeta là dòng đầu tệp: kỳ, phạm vi, giờ tạo (giờ VN, lúc tạo lần chạy để chạy lại ra cùng nội dung). Danh sách
// sản phẩm không có dòng này (giữ tệp nhập lại được).
func exportMeta(ctx context.Context, tx pgx.Tx, def Def, p Params, s DataScope, created time.Time) (string, error) {
	if def.Key == KeyProducts {
		return "", nil
	}
	var parts []string
	if pp, ok := p.(interface{ Period() (Period, error) }); ok {
		if period, err := pp.Period(); err == nil {
			parts = append(parts, "Kỳ: "+viDay(period.FromDay)+" – "+viDay(period.ToDay))
		}
	}
	if sp, ok := p.(*StockParams); ok {
		parts = append(parts, "Ngày: "+viDay(cmp.Or(sp.Date, Today(time.Now())))) // cùng "hôm nay" với stockReport, stockFileName
	}
	if b := basisLabel(p); b != "" {
		parts = append(parts, "Cơ sở: "+b)
	}
	scope, err := scopeLabel(ctx, tx, def.Family, s)
	if err != nil {
		return "", err
	}
	parts = append(parts, "Phạm vi: "+scope, "Tạo lúc "+created.In(VN).Format("15:04 02/01/2006")+" (giờ VN)")
	return strings.Join(parts, " · "), nil
}

// basisLabel là chữ cơ sở ngày của báo cáo có cơ sở (đơn, danh sách chi tiết, bán chạy); rỗng khi không có.
func basisLabel(p Params) string {
	var b string
	switch v := p.(type) {
	case *OrdersParams:
		b = string(v.Basis)
	case *DrilldownParams:
		b = string(v.Basis)
	case *BestSellersParams:
		b = v.basis()
	default:
		return ""
	}
	switch b {
	case string(BasisCompleted):
		return "Ngày hoàn tất (giao thành công hoặc hoàn về kho)"
	case SalesBasisDelivered:
		return "Ngày giao thành công"
	}
	return "Ngày lên đơn"
}

func viDay(d string) string {
	t, err := time.Parse(dayLayout, d)
	if err != nil {
		return d
	}
	return t.Format("02/01/2006")
}

// scopeLabel là chữ phạm vi của người tạo: "Tất cả Page"/"Tất cả kho" hay danh sách tên; họ khách "Toàn shop".
func scopeLabel(ctx context.Context, tx pgx.Tx, f Family, s DataScope) (string, error) {
	st := store.New(tx)
	var names []string
	switch f {
	case FamilySales:
		if s.AllPages {
			return "Tất cả Page", nil
		}
		rows, err := st.FilterPages(ctx, store.FilterPagesParams{SellerID: s.SellerID, AllPages: false, PageIds: s.PageIDs})
		if err != nil {
			return "", fmt.Errorf("đọc tên Page của phạm vi: %w", err)
		}
		for _, r := range rows {
			names = append(names, r.Name)
		}
	case FamilyInventory:
		if s.AllWarehouses {
			return "Tất cả kho", nil
		}
		rows, err := st.WarehouseNames(ctx, s.WarehouseIDs)
		if err != nil {
			return "", fmt.Errorf("đọc tên kho của phạm vi: %w", err)
		}
		for _, r := range rows {
			names = append(names, r.Name)
		}
	case FamilyCustomers:
		return "Toàn shop", nil
	}
	return strings.Join(names, ", "), nil
}
