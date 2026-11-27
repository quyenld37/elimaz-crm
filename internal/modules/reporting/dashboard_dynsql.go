package reporting

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// dashboardCell là một ô số của tổng quan: Metric và cơ sở ngày mà orderSet dựng tập của ô.
type dashboardCell struct {
	m     Metric
	basis Basis
}

// dashboardCellSet: mọi ô số của tổng quan (Delivered, Returned theo ngày hoàn tất; các ô còn lại theo ngày lên đơn hay
// không theo kỳ).
//
//nolint:gochecknoglobals // bảng hằng chỉ đọc
var dashboardCellSet = []dashboardCell{
	{MetricPlaced, BasisPlaced},
	{MetricPlacedActive, BasisPlaced},
	{MetricCancelled, BasisPlaced},
	{MetricDelivered, BasisCompleted},
	{MetricReturned, BasisCompleted},
	{MetricCustomerReturned, BasisPlaced},
	{MetricPendingConfirm, BasisPlaced},
	{MetricPendingShip, BasisPlaced},
	{MetricCancelRequested, BasisPlaced},
}

// dashboardDaySet: hai tập của ByDay (đơn lên chưa hủy theo ngày lên, giao thành công theo ngày giao).
//
//nolint:gochecknoglobals // bảng hằng chỉ đọc
var dashboardDaySet = []dashboardCell{{MetricPlacedActive, BasisPlaced}, {MetricDelivered, BasisCompleted}}

// cellBranch là một nhánh của câu ô số: tập order_set (thân từ orderSetSelect), rồi Metric, kênh (rỗng → unknown), số
// đơn, doanh số, ship, tiền hoàn; %s lần lượt: thân tập, tham số Metric, tham số khóa unknown, cột tiền hoàn, phần nối.
const cellBranch = `(WITH order_set AS (
	%s
)
	SELECT %s::text, coalesce(os.channel, %s::text), count(*)::bigint, coalesce(sum(os.goods_amount), 0)::bigint,
		coalesce(sum(os.shipping_fee_charged), 0)::bigint, %s
	FROM order_set os%s
	GROUP BY 2)`

// dayBranch là một nhánh của câu theo ngày: tập order_set, Metric, ngày giờ VN của mốc (UTC+7 cố định như reporting.VN),
// số đơn, doanh số, ship; %s lần lượt: thân tập, tham số Metric.
const dayBranch = `(WITH order_set AS (
	%s
)
	SELECT %s::text, to_char((os.event_at AT TIME ZONE 'UTC') + interval '7 hours', 'YYYY-MM-DD'), count(*)::bigint,
		coalesce(sum(os.goods_amount), 0)::bigint, coalesce(sum(os.shipping_fee_charged), 0)::bigint
	FROM order_set os
	GROUP BY 2)`

// dashboardSQL dựng một câu cho mọi ô số: mỗi ô một nhánh cellBranch nối UNION ALL, tập của nhánh dựng bằng
// orderSetSelect (thân của orderSet) với Metric của ô. Nhánh customer_returned cộng thêm tiền hoàn của các phiếu khách
// trả nhận trong kỳ (cùng điều kiện anchorSQL của tập).
func dashboardSQL(s DataScope, p Period, b *sqlBuilder) (string, error) {
	parts := make([]string, 0, len(dashboardCellSet))
	for _, c := range dashboardCellSet {
		sel, err := orderSetSelect(s, OrderFilter{Period: p, Basis: c.basis}, c.m, nil, b)
		if err != nil {
			return "", err
		}
		refund, join := "0::bigint", ""
		if c.m == MetricCustomerReturned {
			a := anchorSQL[anchorCustomerReturn]
			refund = "coalesce(sum(rf.amount), 0)::bigint"
			join = "\n\tLEFT JOIN LATERAL (SELECT sum(ev.refund_amount) AS amount FROM " + a.from +
				" WHERE ev.order_id = os.order_id AND " + a.cond + " AND " + a.at + " >= " + b.Arg(p.Start) +
				" AND " + a.at + " < " + b.Arg(p.End) + ") rf ON true"
		}
		parts = append(parts, fmt.Sprintf(cellBranch, sel, b.Arg(string(c.m)), b.Arg(UnknownKey), refund, join))
	}
	return strings.Join(parts, "\nUNION ALL\n"), nil
}

// dashboardDaySQL dựng câu của ByDay: hai nhánh dayBranch (dashboardDaySet) nhóm theo ngày giờ VN của mốc.
func dashboardDaySQL(s DataScope, p Period, b *sqlBuilder) (string, error) {
	parts := make([]string, 0, len(dashboardDaySet))
	for _, c := range dashboardDaySet {
		sel, err := orderSetSelect(s, OrderFilter{Period: p, Basis: c.basis}, c.m, nil, b)
		if err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf(dayBranch, sel, b.Arg(string(c.m))))
	}
	return strings.Join(parts, "\nUNION ALL\n"), nil
}

// dashboardCells chạy câu ô số.
func dashboardCells(ctx context.Context, tx pgx.Tx, s DataScope, p Period) ([]cellRow, error) {
	return dashboardQuery(ctx, tx, s, p, dashboardSQL, "ô số tổng quan", func(r pgx.CollectableRow) (cellRow, error) {
		var c cellRow
		err := r.Scan(&c.Metric, &c.Channel, &c.Amounts.Orders, &c.Amounts.GoodsAmount, &c.Amounts.ShippingFeeCharged, &c.Refund)
		return c, err //nolint:wrapcheck // dashboardQuery bọc
	})
}

// dashboardDays chạy câu theo ngày.
func dashboardDays(ctx context.Context, tx pgx.Tx, s DataScope, p Period) ([]dayRow, error) {
	return dashboardQuery(ctx, tx, s, p, dashboardDaySQL, "tổng quan theo ngày", func(r pgx.CollectableRow) (dayRow, error) {
		var d dayRow
		err := r.Scan(&d.Metric, &d.Day, &d.Amounts.Orders, &d.Amounts.GoodsAmount, &d.Amounts.ShippingFeeCharged)
		return d, err //nolint:wrapcheck // dashboardQuery bọc
	})
}

// dashboardQuery dựng câu bằng build rồi đọc mọi dòng bằng scan.
func dashboardQuery[T any](ctx context.Context, tx pgx.Tx, s DataScope, p Period,
	build func(DataScope, Period, *sqlBuilder) (string, error), what string, scan pgx.RowToFunc[T],
) ([]T, error) {
	b := &sqlBuilder{}
	q, err := build(s, p, b)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, q, b.args...)
	if err != nil {
		return nil, fmt.Errorf("đọc %s: %w", what, err)
	}
	out, err := pgx.CollectRows(rows, scan)
	if err != nil {
		return nil, fmt.Errorf("đọc dòng %s: %w", what, err)
	}
	return out, nil
}
