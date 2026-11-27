package reporting

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Tổng quan trong ngày (BC01, Task 229 = M6-06): đơn lên, doanh số, hủy, giao thành công, hoàn, khách trả, chờ xử lý,
// theo kênh, theo ngày. Mỗi ô là một Metric của danh sách chi tiết và tính bằng đúng orderSet (dashboard_dynsql.go).

// DashboardParams là tham số của tổng quan: kỳ from, to.
type DashboardParams struct {
	PeriodParams
}

// Amounts là số đơn, doanh số tiền hàng (tiền hàng sau giảm, Q3) và phí ship thu khách của một tập đơn.
type Amounts struct {
	Orders             int64 `json:"orders"`
	GoodsAmount        int64 `json:"goodsAmount"`
	ShippingFeeCharged int64 `json:"shippingFeeCharged"`
}

func (a Amounts) add(b Amounts) Amounts {
	return Amounts{a.Orders + b.Orders, a.GoodsAmount + b.GoodsAmount, a.ShippingFeeCharged + b.ShippingFeeCharged}
}

// CustomerReturns là đơn có phiếu khách trả nhận trong kỳ (Items: số đơn, Metric customer_returned) và tổng tiền hoàn
// của các phiếu đó.
type CustomerReturns struct {
	Items        int64 `json:"items"`
	RefundAmount int64 `json:"refundAmount"`
}

// Pending là số đơn chờ xử lý hiện tại, không theo kỳ: chờ xác nhận (new), chờ gửi hãng (confirmed chưa có vận đơn
// hiện hành), yêu cầu hủy.
type Pending struct {
	Confirm         int64 `json:"confirm"`
	Ship            int64 `json:"ship"`
	CancelRequested int64 `json:"cancelRequested"`
}

// ChannelRow là số của một kênh (facebook, web, manual, unknown): đơn lên và đơn lên chưa hủy.
type ChannelRow struct {
	Channel string  `json:"channel"`
	Placed  Amounts `json:"placed"`
	Active  Amounts `json:"active"`
}

// DayRow là số của một ngày giờ VN: đơn lên chưa hủy theo ngày lên, đơn giao thành công theo ngày giao.
type DayRow struct {
	Day       string  `json:"day"`
	Active    Amounts `json:"active"`
	Delivered Amounts `json:"delivered"`
}

// DashboardReport là kết quả của tổng quan; JSON đúng SalesDashboardReport của hợp đồng (lần chạy nền dạng json).
// Placed: đơn lên trong kỳ; Active: Placed trừ đơn đang hủy; Cancelled: Placed đang hủy; Delivered, Returned: theo ngày
// hoàn tất; AvgGoodsAmount = Active.GoodsAmount / Active.Orders làm tròn nửa lên, nil khi 0 đơn; ByDay chỉ có khi kỳ
// dài hơn một ngày (đủ mọi ngày của kỳ).
type DashboardReport struct {
	Status          string          `json:"status"`
	Period          Period          `json:"period"`
	GeneratedAt     time.Time       `json:"generatedAt"`
	Placed          Amounts         `json:"placed"`
	Active          Amounts         `json:"active"`
	Cancelled       Amounts         `json:"cancelled"`
	Delivered       Amounts         `json:"delivered"`
	Returned        Amounts         `json:"returned"`
	AvgGoodsAmount  *int64          `json:"avgGoodsAmount"`
	CustomerReturns CustomerReturns `json:"customerReturns"`
	Pending         Pending         `json:"pending"`
	ByChannel       []ChannelRow    `json:"byChannel"`
	ByDay           []DayRow        `json:"byDay"`
}

// ReportRows là số dòng của kết quả cho thuộc tính report.rows của span.
func (r DashboardReport) ReportRows() int { return len(r.ByChannel) + len(r.ByDay) }

// StatusReady là trạng thái của kết quả xem được ngay.
const StatusReady = "ready"

// channelOrder là thứ tự các dòng kênh (C03: kênh rỗng là unknown, không tự gán Facebook).
//
//nolint:gochecknoglobals // bảng hằng chỉ đọc
var channelOrder = []string{"facebook", "web", "manual", UnknownKey}

func dashboardDef() Def {
	d := newDef[DashboardParams](KeyDashboard, FamilySales, permSalesRead)
	d.JSON = func(ctx context.Context, tx pgx.Tx, s DataScope, p Params) (any, error) {
		dp, ok := p.(*DashboardParams)
		if !ok {
			return nil, fmt.Errorf("tham số tổng quan %T: %w", p, ErrBadParams)
		}
		return dashboard(ctx, tx, s, dp)
	}
	return d
}

func newDashboardReport(p Period) DashboardReport {
	return DashboardReport{Status: StatusReady, Period: p, GeneratedAt: time.Now().UTC(), ByChannel: []ChannelRow{}, ByDay: []DayRow{}}
}

// dashboard tính tổng quan của kỳ p cho phạm vi s: một câu cho mọi ô số, một câu cho ByDay khi kỳ dài hơn một ngày.
func dashboard(ctx context.Context, tx pgx.Tx, s DataScope, p *DashboardParams) (DashboardReport, error) {
	period, err := p.Period()
	if err != nil {
		return DashboardReport{}, err
	}
	rows, err := dashboardCells(ctx, tx, s, period)
	if err != nil {
		return DashboardReport{}, err
	}
	r := newDashboardReport(period)
	fillCells(&r, rows)
	if period.Days() > 1 {
		days, err := dashboardDays(ctx, tx, s, period)
		if err != nil {
			return DashboardReport{}, err
		}
		r.ByDay = fillDays(period, days)
	}
	return r, nil
}

// cellRow là một dòng của câu ô số: tập (Metric) × kênh.
type cellRow struct {
	Metric  Metric
	Channel string
	Amounts Amounts
	Refund  int64
}

// fillCells cộng các dòng (Metric × kênh) vào các ô và dựng ByChannel theo channelOrder (chỉ kênh có đơn lên).
func fillCells(r *DashboardReport, rows []cellRow) {
	placed, active := map[string]Amounts{}, map[string]Amounts{}
	for _, row := range rows {
		a := row.Amounts
		switch row.Metric {
		case MetricPlaced:
			r.Placed = r.Placed.add(a)
			placed[row.Channel] = placed[row.Channel].add(a)
		case MetricPlacedActive:
			r.Active = r.Active.add(a)
			active[row.Channel] = active[row.Channel].add(a)
		case MetricCancelled:
			r.Cancelled = r.Cancelled.add(a)
		case MetricDelivered:
			r.Delivered = r.Delivered.add(a)
		case MetricReturned:
			r.Returned = r.Returned.add(a)
		case MetricCustomerReturned:
			r.CustomerReturns.Items += a.Orders
			r.CustomerReturns.RefundAmount += row.Refund
		case MetricPendingConfirm:
			r.Pending.Confirm += a.Orders
		case MetricPendingShip:
			r.Pending.Ship += a.Orders
		case MetricCancelRequested:
			r.Pending.CancelRequested += a.Orders
		}
	}
	r.AvgGoodsAmount = avgGoods(r.Active)
	for _, c := range channelOrder {
		if p, ok := placed[c]; ok {
			r.ByChannel = append(r.ByChannel, ChannelRow{Channel: c, Placed: p, Active: active[c]})
		}
	}
}

// dayRow là một dòng của câu theo ngày: tập (placed_active hay delivered) × ngày giờ VN.
type dayRow struct {
	Metric  Metric
	Day     string
	Amounts Amounts
}

// fillDays dựng đủ mọi ngày của kỳ (ngày không có đơn là 0).
func fillDays(p Period, rows []dayRow) []DayRow {
	out := make([]DayRow, 0, p.Days())
	idx := map[string]int{}
	for d := p.Start.In(VN); d.Before(p.End); d = d.AddDate(0, 0, 1) {
		idx[d.Format(dayLayout)] = len(out)
		out = append(out, DayRow{Day: d.Format(dayLayout)})
	}
	for _, row := range rows {
		i, ok := idx[row.Day]
		if !ok {
			continue // không xảy ra: mốc của tập nằm trong kỳ
		}
		switch row.Metric {
		case MetricPlacedActive:
			out[i].Active = out[i].Active.add(row.Amounts)
		case MetricDelivered:
			out[i].Delivered = out[i].Delivered.add(row.Amounts)
		default:
		}
	}
	return out
}

// avgGoods là doanh số trung bình một đơn làm tròn nửa lên tới đồng (số nguyên, tiền không âm); 0 đơn → nil.
func avgGoods(a Amounts) *int64 {
	if a.Orders <= 0 {
		return nil
	}
	v := (2*a.GoodsAmount + a.Orders) / (2 * a.Orders)
	return &v
}
