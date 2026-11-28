package reporting

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/queries/orderview"
)

// Báo cáo đơn theo chiều và danh sách chi tiết (BC02, Task 230 = M6-07). Mọi số của bảng, mọi ô và danh sách chi tiết
// dựng tập bằng cùng orderSet (Global Constraints M6: một hàm dựng điều kiện cho một tập).

// maxFilterItems là số giá trị tối đa của một bộ lọc (maxItems của hợp đồng: 50 mã, 10 kênh, 20 hãng; lấy trần chung
// cho tham số của lần chạy nền, ogen đã chặn đúng từng trần trên GET).
const maxFilterItems = 50

// SalesFilterParams là kỳ, cơ sở ngày và bộ lọc chung của báo cáo đơn và danh sách chi tiết; tên trường JSON cùng tên
// tham số query của GET (cột params của report_runs). Basis rỗng là placed; lát rỗng là không lọc.
type SalesFilterParams struct {
	PeriodParams
	Basis       Basis       `json:"basis,omitempty"`
	Channels    []string    `json:"channel,omitempty"`
	SourceIDs   []uuid.UUID `json:"sourceId,omitempty"`
	CareBy      []uuid.UUID `json:"careBy,omitempty"`
	CreatedBy   []uuid.UUID `json:"createdBy,omitempty"`
	PageIDs     []uuid.UUID `json:"pageId,omitempty"`
	CarrierKeys []string    `json:"carrierKey,omitempty"`
}

// Validate kiểm kỳ, cơ sở ngày, kênh, khóa hãng, số giá trị của mỗi bộ lọc.
func (p SalesFilterParams) Validate() error {
	_, err := p.Filter()
	return err
}

// Filter đổi tham số thành OrderFilter đã kiểm (kỳ: ErrBadDate, ErrFromAfterTo, ErrRangeTooLong; giá trị lạ:
// ErrBadParams).
func (p SalesFilterParams) Filter() (OrderFilter, error) {
	period, err := p.Period()
	if err != nil {
		return OrderFilter{}, err
	}
	basis := p.Basis
	if basis == "" {
		basis = BasisPlaced
	}
	if err := checkEnum(basis, basisSet, "cơ sở ngày"); err != nil {
		return OrderFilter{}, err
	}
	for _, n := range []int{len(p.Channels), len(p.SourceIDs), len(p.CareBy), len(p.CreatedBy), len(p.PageIDs), len(p.CarrierKeys)} {
		if n > maxFilterItems {
			return OrderFilter{}, fmt.Errorf("bộ lọc %d giá trị, tối đa %d: %w", n, maxFilterItems, ErrBadParams)
		}
	}
	for _, c := range p.Channels {
		if !channelSet[c] {
			return OrderFilter{}, fmt.Errorf("lọc kênh %q: %w", c, ErrBadParams)
		}
	}
	for _, k := range p.CarrierKeys {
		if !carrierKeyRe.MatchString(k) {
			return OrderFilter{}, fmt.Errorf("lọc hãng %q: %w", k, ErrBadParams)
		}
	}
	return OrderFilter{
		Period: period, Basis: basis, Channels: p.Channels, SourceIDs: p.SourceIDs, CareBy: p.CareBy,
		CreatedBy: p.CreatedBy, PageIDs: p.PageIDs, CarrierKeys: p.CarrierKeys,
	}, nil
}

// OrdersParams là tham số của báo cáo đơn: bộ lọc chung và chiều nhóm (rỗng là status).
type OrdersParams struct {
	SalesFilterParams
	GroupBy Dimension `json:"groupBy,omitempty"`
}

// Validate kiểm bộ lọc và chiều nhóm.
func (p OrdersParams) Validate() error {
	if _, err := p.Filter(); err != nil {
		return err
	}
	_, err := p.dimension()
	return err
}

func (p OrdersParams) dimension() (Dimension, error) {
	if p.GroupBy == "" {
		return DimStatus, nil
	}
	if err := checkEnum(p.GroupBy, dimensionSet, "chiều nhóm"); err != nil {
		return "", err
	}
	return p.GroupBy, nil
}

// OrdersRow là một dòng (hay dòng tổng) của báo cáo đơn. Cơ sở placed: Orders = Placed = đơn lên trong kỳ; Active,
// Cancelled theo trạng thái hiện tại; GoodsAmount, ShippingFeeCharged của đơn chưa hủy; Delivered, Returned là đơn
// trong tập đang ở trạng thái đó. Cơ sở completed: Delivered, Returned là đơn có dòng giao xong, hoàn trong kỳ; Orders
// = Delivered + Returned (QĐ-230-1); GoodsAmount, ShippingFeeCharged của đơn giao xong; Placed, Active, Cancelled nil.
// AvgGoodsAmount = GoodsAmount ÷ (Active hay Delivered) làm tròn nửa lên, nil khi mẫu số 0. SuccessRateBp = Delivered
// × 10000 ÷ (Delivered + Returned) làm tròn xuống, nil khi mẫu số 0.
type OrdersRow struct {
	Key, Label                      string
	Orders                          int64
	Placed, Active, Cancelled       *int64
	GoodsAmount, ShippingFeeCharged int64
	AvgGoodsAmount                  *int64
	Delivered, Returned             int64
	SuccessRateBp                   *int32
}

// OrdersReport là kết quả của báo cáo đơn (Status "ready").
type OrdersReport struct {
	Status  string
	Period  Period
	Basis   Basis
	GroupBy Dimension
	Rows    []OrdersRow
	Totals  OrdersRow
}

// ReportRows là số dòng cho thuộc tính report.rows của span.
func (r *OrdersReport) ReportRows() int { return len(r.Rows) }

func ordersDef() Def {
	d := newDef[OrdersParams](KeyOrders, FamilySales, permSalesRead)
	d.JSON = func(ctx context.Context, tx pgx.Tx, s DataScope, p Params) (any, error) {
		op, ok := p.(*OrdersParams)
		if !ok {
			return nil, fmt.Errorf("tham số báo cáo đơn %T: %w", p, ErrBadParams)
		}
		return ordersReport(ctx, tx, s, op)
	}
	d.Rows, d.Columns, d.FileName = ordersRows, ordersColumns, periodFile("bao-cao-don")
	return d
}

// ordersReport chạy báo cáo đơn cho phạm vi s: một câu SQL (orderGroupsSQL) cho mọi dòng, Go đổi khóa thành dòng (nhãn
// gộp của chiều status cộng các bộ trục cùng nhãn), tính trung bình, tỷ lệ, dòng tổng.
func ordersReport(ctx context.Context, tx pgx.Tx, s DataScope, p *OrdersParams) (*OrdersReport, error) {
	f, err := p.Filter()
	if err != nil {
		return nil, err
	}
	dim, err := p.dimension()
	if err != nil {
		return nil, err
	}
	if err := checkPageFilter(s, f); err != nil {
		return nil, err
	}
	groups, err := orderGroups(ctx, tx, s, f, dim)
	if err != nil {
		return nil, err
	}
	rows := mergeGroups(dim, groups)
	total := orderAgg{}
	for _, g := range groups {
		total.add(g.agg)
	}
	out := &OrdersReport{Status: "ready", Period: f.Period, Basis: f.Basis, GroupBy: dim, Rows: make([]OrdersRow, 0, len(rows))}
	for _, r := range rows {
		out.Rows = append(out.Rows, r.agg.row(f.Basis, r.key, r.label))
	}
	out.Totals = total.row(f.Basis, TotalKey, totalLabel)
	return out, nil
}

// checkPageFilter: lọc Page ngoài phạm vi report.sales.read → ErrPageForbidden (hợp đồng: 403 forbidden).
func checkPageFilter(s DataScope, f OrderFilter) error {
	if s.AllPages {
		return nil
	}
	for _, id := range f.PageIDs {
		if !slices.Contains(s.PageIDs, id) {
			return ErrPageForbidden
		}
	}
	return nil
}

// orderAgg là các số đếm, tổng tiền của một nhóm (hay dòng). Cơ sở placed: n là đơn lên; cơ sở completed: delivered,
// returned đếm theo phần (tập giao xong, tập hoàn), goods, ship chỉ của tập giao xong.
type orderAgg struct {
	n, active, cancelled, delivered, returned, goods, ship int64
}

func (a *orderAgg) add(b orderAgg) {
	a.n += b.n
	a.active += b.active
	a.cancelled += b.cancelled
	a.delivered += b.delivered
	a.returned += b.returned
	a.goods += b.goods
	a.ship += b.ship
}

func (a orderAgg) row(basis Basis, key, label string) OrdersRow {
	r := OrdersRow{
		Key: key, Label: label, GoodsAmount: a.goods, ShippingFeeCharged: a.ship, Delivered: a.delivered, Returned: a.returned,
	}
	div := a.delivered
	if basis == BasisPlaced {
		r.Orders = a.n
		r.Placed, r.Active, r.Cancelled = new(a.n), new(a.active), new(a.cancelled)
		div = a.active
	} else {
		r.Orders = a.delivered + a.returned
	}
	r.AvgGoodsAmount = avgHalfUp(a.goods, div)
	if d := a.delivered + a.returned; d > 0 {
		r.SuccessRateBp = new(int32(a.delivered * 10000 / d)) //nolint:gosec // G115: delivered ≤ d nên giá trị trong 0..10000
	}
	return r
}

// orderGroup là một nhóm của câu SQL: khóa của chiều (status: bốn trục) và số của nhóm.
type orderGroup struct {
	keys [4]string
	name string
	agg  orderAgg
}

type mergedRow struct {
	key, label string
	agg        orderAgg
}

// mergeGroups đổi nhóm của SQL thành dòng: chiều status đổi bộ trục thành nhãn gộp (orderview.Label, cùng hàm với
// danh sách đơn) rồi cộng các bộ cùng nhãn, dòng theo thứ tự thanh nhãn; chiều day theo ngày tăng dần; còn lại theo
// số đơn giảm dần, nhóm không xác định cuối.
func mergeGroups(dim Dimension, groups []orderGroup) []mergedRow {
	var rows []mergedRow
	idx := map[string]int{}
	for _, g := range groups {
		key, name := g.keys[0], g.name
		if dim == DimStatus {
			key = string(orderview.Label(orderview.Axes{
				Status: g.keys[0], PendingReason: g.keys[1], PackingStatus: g.keys[2], ShipmentStatus: g.keys[3],
			}))
		}
		if key == "" {
			key = UnknownKey
		}
		i, ok := idx[key]
		if !ok {
			i = len(rows)
			idx[key] = i
			rows = append(rows, mergedRow{key: key, label: rowLabel(dim, key, name)})
		}
		rows[i].agg.add(g.agg)
	}
	order := map[string]int{}
	for i, l := range orderview.Labels() {
		order[string(l)] = i
	}
	slices.SortStableFunc(rows, func(a, b mergedRow) int {
		switch {
		case dim == DimStatus:
			return order[a.key] - order[b.key]
		case dim == DimDay:
			return cmp.Compare(a.key, b.key)
		case (a.key == UnknownKey) != (b.key == UnknownKey):
			if a.key == UnknownKey {
				return 1
			}
			return -1
		case a.agg.n+a.agg.delivered+a.agg.returned != b.agg.n+b.agg.delivered+b.agg.returned:
			return int((b.agg.n + b.agg.delivered + b.agg.returned) - (a.agg.n + a.agg.delivered + a.agg.returned))
		}
		return cmp.Compare(a.label+a.key, b.label+b.key)
	})
	return rows
}
