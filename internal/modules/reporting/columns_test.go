package reporting

import (
	"slices"
	"testing"

	"elimaz.com.vn/crm/internal/platform/xlsx"
)

// Tỷ lệ phần vạn ra chữ làm tròn xuống như API (66,66% cho 2/3), "—" khi nil.
func TestRateText(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		bp   *int32
		want string
	}{
		{nil, "—"},
		{new(int32(6666)), "66,66%"},
		{new(int32(0)), "0,00%"},
		{new(int32(10000)), "100,00%"},
		{new(int32(5)), "0,05%"},
		{new(int32(1250)), "12,50%"},
	} {
		if got := rateText(c.bp); got != c.want {
			t.Errorf("rateText(%v) = %q, mong %q", c.bp, got, c.want)
		}
	}
}

func titles(cols []xlsx.Column) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, c.Title)
	}
	return out
}

// Mọi báo cáo xuất được (trừ danh sách khách của Task 238) có đủ Rows, Columns, FileName; tên tệp theo kỳ hay ngày.
func TestDefs_XLSX(t *testing.T) {
	t.Parallel()
	period := PeriodParams{From: "2026-11-01", To: "2026-11-30"}
	for _, c := range []struct {
		k    Key
		p    Params
		file string
	}{
		{KeyDashboard, &DashboardParams{PeriodParams: period}, "tong-quan_2026-11-01_2026-11-30.xlsx"},
		{KeyOrders, &OrdersParams{SalesFilterParams: SalesFilterParams{PeriodParams: period}}, "bao-cao-don_2026-11-01_2026-11-30.xlsx"},
		{
			KeyOrderDrilldown, &DrilldownParams{SalesFilterParams: SalesFilterParams{PeriodParams: period}, Metric: MetricPlaced},
			"chi-tiet-don_2026-11-01_2026-11-30.xlsx",
		},
		{KeyCarriers, &CarriersParams{PeriodParams: period}, "hang-van-chuyen_2026-11-01_2026-11-30.xlsx"},
		{KeyBestSellers, &BestSellersParams{PeriodParams: period}, "ban-chay_2026-11-01_2026-11-30.xlsx"},
		{KeyStock, &StockParams{Date: "2026-11-18"}, "ton-kho_2026-11-18.xlsx"},
		{KeyStockMovements, &MovementsParams{PeriodParams: period}, "xuat-nhap-ton_2026-11-01_2026-11-30.xlsx"},
	} {
		d := Defs()[c.k]
		if d.Rows == nil || d.Columns == nil || d.FileName == nil {
			t.Errorf("%s thiếu phần xuất", c.k)
			continue
		}
		if got := d.FileName(c.p); got != c.file {
			t.Errorf("%s: tên tệp %q, mong %q", c.k, got, c.file)
		}
		if len(d.Columns(c.p)) == 0 {
			t.Errorf("%s không có cột", c.k)
		}
	}
	placed := Defs()[KeyOrders].Columns(&OrdersParams{})
	completed := Defs()[KeyOrders].Columns(&OrdersParams{SalesFilterParams: SalesFilterParams{Basis: BasisCompleted}})
	if !slices.Contains(titles(placed), "Chưa hủy") || slices.Contains(titles(completed), "Chưa hủy") ||
		slices.Contains(titles(completed), "Hủy") {
		t.Errorf("cột theo cơ sở: placed %q, completed %q", titles(placed), titles(completed))
	}
	if got := titles(Defs()[KeyBestSellers].Columns(&BestSellersParams{Level: LevelProduct})); slices.Contains(got, "SKU") {
		t.Errorf("bán chạy mức mẫu có cột SKU: %q", got)
	}
}
