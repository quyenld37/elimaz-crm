package reporting

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/queries"
	"elimaz.com.vn/crm/internal/queries/orderview"
)

// sqlBuilder gom tham số $n của một câu báo cáo dựng lúc chạy. Giá trị người dùng gửi chỉ đi vào câu qua Arg; biểu
// thức SQL chỉ lấy từ hằng trong tệp này (kế hoạch M6, Global Constraints).
type sqlBuilder struct{ args []any }

// Arg thêm v và trả chỗ giữ $n của nó.
func (b *sqlBuilder) Arg(v any) string {
	b.args = append(b.args, v)
	return "$" + strconv.Itoa(len(b.args))
}

// anchor là dòng sự kiện quyết định một đơn có trong tập không và mốc của nó (event_at).
type anchor int

const (
	anchorPlaced         anchor = iota // dòng lịch sử lên đơn
	anchorDelivered                    // dòng lịch sử giao thành công
	anchorReturned                     // dòng lịch sử hoàn về kho
	anchorCustomerReturn               // phiếu khách trả đã nhận
	anchorCurrent                      // trạng thái hiện tại, không theo kỳ
)

// metricRule là cách một cột số chọn đơn: dòng sự kiện và điều kiện hằng trên trạng thái hiện tại của đơn.
type metricRule struct {
	anchor anchor
	cond   string
}

// metricRules: cột số theo cơ sở ngày (bảng hằng). Cơ sở placed: đơn lên trong kỳ, các cột sau lọc theo trạng thái
// hiện tại (hủy = đơn đã lên đang cancelled, để Placed = Active + Cancelled, Task 229). Cơ sở completed: đơn giao xong
// hay hoàn trong kỳ; cột lên đơn không có (Task 230). Ba cột chờ xử lý là số hiện tại, không theo kỳ; phiếu khách trả
// theo ngày nhận phiếu (cả hai cơ sở).
//
//nolint:gochecknoglobals // bảng hằng chỉ đọc
var metricRules = map[Basis]map[Metric]metricRule{
	BasisPlaced: {
		MetricPlaced:       {anchorPlaced, ""},
		MetricPlacedActive: {anchorPlaced, "o.status <> 'cancelled'"},
		MetricCancelled:    {anchorPlaced, "o.status = 'cancelled'"},
		MetricDelivered:    {anchorPlaced, "o.status = 'delivered'"},
		MetricReturned:     {anchorPlaced, "o.status = 'returned'"},
	},
	BasisCompleted: {
		MetricDelivered: {anchorDelivered, ""},
		MetricReturned:  {anchorReturned, ""},
	},
}

// currentRules: cột số không phụ thuộc cơ sở ngày.
//
//nolint:gochecknoglobals // bảng hằng chỉ đọc
var currentRules = map[Metric]metricRule{
	MetricCustomerReturned: {anchorCustomerReturn, ""},
	MetricPendingConfirm:   {anchorCurrent, "o.status = 'new'"},
	MetricPendingShip:      {anchorCurrent, "o.status = 'confirmed' AND o.current_shipment_id IS NULL"},
	MetricCancelRequested:  {anchorCurrent, "o.status = 'cancel_requested'"},
}

// anchorSQL: bảng sự kiện (bí danh ev), điều kiện hằng của dòng sự kiện, cột thời gian của mốc. Dòng lịch sử lúc tạo
// của M2 có from_status = ” (view trả coalesce, G8); đơn tạo từ nháp lên ở dòng draft → new/confirmed.
//
//nolint:gochecknoglobals // bảng hằng chỉ đọc
var anchorSQL = map[anchor]struct{ from, cond, at string }{
	anchorPlaced: {
		"ord.order_status_history_v ev",
		"ev.to_status IN ('new', 'confirmed') AND ev.from_status IN ('', 'draft')", "ev.occurred_at",
	},
	anchorDelivered:      {"ord.order_status_history_v ev", "ev.to_status = 'delivered'", "ev.occurred_at"},
	anchorReturned:       {"ord.order_status_history_v ev", "ev.to_status = 'returned'", "ev.occurred_at"},
	anchorCustomerReturn: {"ord.returns_v ev", "ev.kind = 'customer_return' AND ev.status <> 'cancelled'", "ev.received_at"},
}

// uuidDimensions: chiều có khóa là mã (cột của đơn; NULL là nhóm không xác định).
//
//nolint:gochecknoglobals // bảng hằng chỉ đọc
var uuidDimensions = map[Dimension]string{
	DimSource:    "o.traffic_source_id",
	DimCareBy:    "o.care_by",
	DimCreatedBy: "o.created_by",
	DimPage:      "o.channel_account_id",
}

// orderSetName là tên CTE mà orderSet dựng; câu dùng tập (báo cáo, danh sách chi tiết) đọc qua hằng này.
const orderSetName = "order_set"

// drillPageName là tên CTE của một trang danh sách chi tiết (drilldownSQL).
const drillPageName = "drill_page"

// orderSetColumns: cột của order_set (một dòng mỗi đơn). Không có SĐT, địa chỉ, tên khách (Global Constraints M6).
const orderSetColumns = `o.id AS order_id, %s AS event_at, o.code, o.customer_id, o.status, o.pending_reason,
	o.packing_status, o.channel, o.channel_account_id AS page_id, o.traffic_source_id AS source_id, o.care_by,
	o.created_by, o.subtotal - o.discount_total AS goods_amount, o.shipping_fee_charged, o.created_at,
	s.carrier_key, s.status AS ship_status`

// orderSet dựng CTE "order_set AS (…)", hàm dựng tập đơn dùng chung của mọi báo cáo bán hàng (kế hoạch M6, Task
// 227): số tổng, số của từng ô, danh sách chi tiết và tệp xuất gọi cùng hàm này. Tập gồm đơn của người bán s.SellerID
// (ADR-29), trong phạm vi Page của s (không AllPages thì đơn không gắn Page bị loại), thỏa điều kiện thời gian của m
// theo f.Basis trên bảng sự kiện bằng khoảng UTC (dùng chỉ mục), các bộ lọc của f và điều kiện của cell. Mỗi đơn có
// tối đa một dòng, mốc event_at là dòng sự kiện sớm nhất khớp. Giá trị lạ → ErrBadParams, trước khi có câu nào.
func orderSet(s DataScope, f OrderFilter, m Metric, cell *Cell, b *sqlBuilder) (string, error) {
	rule, err := ruleFor(f, m)
	if err != nil {
		return "", err
	}
	if s.SellerID == uuid.Nil {
		return "", fmt.Errorf("tập đơn thiếu người bán: %w", ErrBadParams)
	}
	from, conds, at := orderSetFrom(s, f, rule, b)
	scopeConds, err := filterConds(s, f, b)
	if err != nil {
		return "", err
	}
	conds = append(conds, scopeConds...)
	outer := ""
	if cell != nil {
		c, dayCond, err := cellCond(*cell, b)
		if err != nil {
			return "", err
		}
		if c != "" {
			conds = append(conds, c)
		}
		outer = dayCond
	}
	inner := "SELECT DISTINCT ON (o.id) " + fmt.Sprintf(orderSetColumns, at) + "\n\tFROM " + from +
		"\n\tWHERE " + strings.Join(conds, "\n\t  AND ") + "\n\tORDER BY o.id, " + at
	if outer == "" {
		return orderSetName + " AS (\n\t" + inner + "\n)", nil
	}
	return orderSetName + " AS (\n\tSELECT * FROM (" + inner + ") AS first_event WHERE " + outer + "\n)", nil
}

// ruleFor chọn quy tắc của cột số m theo cơ sở ngày; kiểm cơ sở ngày, cột số, kỳ.
func ruleFor(f OrderFilter, m Metric) (metricRule, error) {
	if !basisSet[f.Basis] {
		return metricRule{}, fmt.Errorf("cơ sở ngày %q: %w", string(f.Basis), ErrBadParams)
	}
	rule, ok := currentRules[m]
	if !ok {
		rule, ok = metricRules[f.Basis][m]
	}
	if !ok {
		return metricRule{}, fmt.Errorf("cột số %q với cơ sở %s: %w", string(m), f.Basis, ErrBadParams)
	}
	if rule.anchor != anchorCurrent && (f.Period.IsZero() || !f.Period.Start.Before(f.Period.End)) {
		return metricRule{}, fmt.Errorf("tập đơn thiếu kỳ: %w", ErrBadParams)
	}
	return rule, nil
}

// orderSetFrom dựng phần FROM, các điều kiện của sự kiện và người bán, và cột mốc. Điều kiện seller_id nằm ở mọi lần
// nhắc bảng đơn (ADR-29); vận đơn hiện hành đọc cùng người bán.
func orderSetFrom(s DataScope, f OrderFilter, rule metricRule, b *sqlBuilder) (from string, conds []string, at string) {
	seller := b.Arg(s.SellerID)
	shipments := "\n\tLEFT JOIN fulfillment.shipments_v s ON s.id = o.current_shipment_id AND s.seller_id = " + seller
	if rule.anchor == anchorCurrent {
		from = "ord.orders_v o" + shipments
		conds = []string{"o.seller_id = " + seller}
		at = "o.created_at"
	} else {
		a := anchorSQL[rule.anchor]
		from = a.from + "\n\tJOIN ord.orders_v o ON o.id = ev.order_id AND o.seller_id = " + seller + shipments
		at = a.at
		conds = []string{a.cond, at + " >= " + b.Arg(f.Period.Start), at + " < " + b.Arg(f.Period.End)}
	}
	if rule.cond != "" {
		conds = append(conds, rule.cond)
	}
	return from, conds, at
}

// filterConds dựng điều kiện phạm vi Page và các bộ lọc (giá trị luôn là tham số).
func filterConds(s DataScope, f OrderFilter, b *sqlBuilder) ([]string, error) {
	var conds []string
	if !s.AllPages {
		conds = append(conds, "o.channel_account_id = ANY("+b.Arg(append([]uuid.UUID{}, s.PageIDs...))+"::uuid[])") // nil → mảng rỗng, không NULL
	}
	for _, c := range f.Channels {
		if !channelSet[c] {
			return nil, fmt.Errorf("lọc kênh %q: %w", c, ErrBadParams)
		}
	}
	for _, k := range f.CarrierKeys {
		if !carrierKeyRe.MatchString(k) {
			return nil, fmt.Errorf("lọc hãng %q: %w", k, ErrBadParams)
		}
	}
	if len(f.Channels) > 0 {
		conds = append(conds, "o.channel = ANY("+b.Arg(f.Channels)+"::text[])")
	}
	for _, u := range []struct {
		col string
		ids []uuid.UUID
	}{
		{"o.traffic_source_id", f.SourceIDs},
		{"o.care_by", f.CareBy},
		{"o.created_by", f.CreatedBy},
		{"o.channel_account_id", f.PageIDs},
	} {
		if len(u.ids) > 0 {
			conds = append(conds, u.col+" = ANY("+b.Arg(u.ids)+"::uuid[])")
		}
	}
	if len(f.CarrierKeys) > 0 {
		keys := b.Arg(f.CarrierKeys)
		// unknown: đơn chưa có vận đơn hiện hành (hợp đồng: "unknown là đơn chưa gửi hãng").
		conds = append(conds, "(s.carrier_key = ANY("+keys+"::text[]) OR (s.carrier_key IS NULL AND '"+UnknownKey+"' = ANY("+keys+"::text[])))")
	}
	return conds, nil
}

// cellCond dựng điều kiện của ô: cond trên cột của đơn, hoặc dayCond trên mốc event_at của tập (chiều day: ngày giờ
// VN của mốc theo cơ sở, xét sau khi đã chọn dòng sự kiện sớm nhất, để ô khớp với cách nhóm theo ngày). Chiều status:
// khóa là nhãn gộp (orderview.LabelKey).
func cellCond(c Cell, b *sqlBuilder) (cond, dayCond string, err error) {
	bad := func() (string, string, error) {
		return "", "", fmt.Errorf("ô %s = %q: %w", string(c.Dimension), c.Key, ErrBadParams)
	}
	if col, ok := uuidDimensions[c.Dimension]; ok {
		if c.Key == UnknownKey {
			return col + " IS NULL", "", nil
		}
		id, err := uuid.Parse(c.Key)
		if err != nil || id.String() != c.Key {
			return bad()
		}
		return col + " = " + b.Arg(id), "", nil
	}
	switch c.Dimension {
	case DimChannel:
		if !channelSet[c.Key] {
			return bad()
		}
		return "o.channel = " + b.Arg(c.Key), "", nil
	case DimCarrier:
		if c.Key == UnknownKey {
			return "s.carrier_key IS NULL", "", nil
		}
		if !carrierKeyRe.MatchString(c.Key) {
			return bad()
		}
		return "s.carrier_key = " + b.Arg(c.Key), "", nil
	case DimStatus:
		// Nhãn gộp: điều kiện SQL của chính danh sách đơn (queries.OrderLabelCond, bí danh o và s), cùng nguồn với
		// orderview.Label mà báo cáo dùng để nhóm (G9, QĐ-223-4). Khóa chỉ là một nhãn của orderview.Labels().
		if !slices.Contains(orderview.Labels(), orderview.LabelKey(c.Key)) {
			return bad()
		}
		cond, err := queries.OrderLabelCond(orderview.LabelKey(c.Key))
		if err != nil {
			return bad()
		}
		return cond, "", nil
	case DimDay:
		start, err := dayStart(c.Key)
		if err != nil {
			return bad()
		}
		return "", "event_at >= " + b.Arg(start.UTC()) + " AND event_at < " + b.Arg(start.AddDate(0, 0, 1).UTC()), nil
	case DimSource, DimCareBy, DimCreatedBy, DimPage:
	}
	return bad()
}

// groupKeySQL: bốn cột khóa của nhóm theo chiều, trên cột của order_set (bảng hằng). Chiều status nhóm theo bốn trục
// của nhãn gộp (Go đổi bằng orderview.Label); chiều day là ngày giờ VN của mốc theo cơ sở (cộng 7 giờ trên giờ UTC,
// không phụ thuộc TimeZone của phiên); chiều mã: NULL là nhóm không xác định.
//
//nolint:gochecknoglobals // bảng hằng chỉ đọc
var groupKeySQL = map[Dimension][4]string{
	DimStatus:    {"status", "pending_reason", "packing_status", "coalesce(ship_status, '')"},
	DimSource:    {"source_id::text", "''", "''", "''"},
	DimCareBy:    {"care_by::text", "''", "''", "''"},
	DimCreatedBy: {"created_by::text", "''", "''", "''"},
	DimPage:      {"page_id::text", "''", "''", "''"},
	DimChannel:   {"channel", "''", "''", "''"},
	DimCarrier:   {"carrier_key", "''", "''", "''"},
	DimDay:       {"to_char((event_at AT TIME ZONE 'UTC') + interval '7 hours', 'YYYY-MM-DD')", "''", "''", "''"},
}

// groupNameSQL: tên của khóa g.k1 đọc từ view công bố (G14, G15), sau khi đã nhóm.
//
//nolint:gochecknoglobals // bảng hằng chỉ đọc
var groupNameSQL = map[Dimension]string{
	DimSource:    "(SELECT ts.name FROM ord.traffic_sources_v ts WHERE ts.id = g.k1::uuid)",
	DimCareBy:    "(SELECT u.display_name FROM identity.users_v u WHERE u.id = g.k1::uuid)",
	DimCreatedBy: "(SELECT u.display_name FROM identity.users_v u WHERE u.id = g.k1::uuid)",
	DimPage:      "(SELECT a.name FROM channel.channel_accounts_v a WHERE a.id = g.k1::uuid)",
}

// groupPart là một phần của báo cáo: tập của cột số m và bảy cột số của phần (n, active, cancelled, delivered,
// returned, goods, ship) trên order_set.
type groupPart struct {
	m    Metric
	aggs string
}

// groupParts: phần theo cơ sở ngày (QĐ-230-1). placed: một tập đơn lên, cột theo trạng thái hiện tại, tiền của đơn chưa
// hủy. completed: tập giao xong (đếm giao xong, tiền) và tập hoàn (đếm hoàn).
//
//nolint:gochecknoglobals // bảng hằng chỉ đọc
var groupParts = map[Basis][]groupPart{
	BasisPlaced: {{MetricPlaced, `count(*), count(*) FILTER (WHERE status <> 'cancelled'), count(*) FILTER (WHERE status = 'cancelled'),
		count(*) FILTER (WHERE status = 'delivered'), count(*) FILTER (WHERE status = 'returned'),
		coalesce(sum(goods_amount) FILTER (WHERE status <> 'cancelled'), 0),
		coalesce(sum(shipping_fee_charged) FILTER (WHERE status <> 'cancelled'), 0)`}},
	BasisCompleted: {
		{MetricDelivered, "0, 0, 0, count(*), 0, coalesce(sum(goods_amount), 0), coalesce(sum(shipping_fee_charged), 0)"},
		{MetricReturned, "0, 0, 0, 0, count(*), 0, 0"},
	},
}

// orderGroupsSQL dựng một câu cho mọi nhóm của báo cáo đơn: mỗi phần dựng tập bằng orderSet (CTE trong truy vấn con
// riêng), nhóm theo groupKeySQL, rồi cộng các phần cùng khóa và đọc tên của khóa.
func orderGroupsSQL(s DataScope, f OrderFilter, dim Dimension, b *sqlBuilder) (string, error) {
	keys, ok := groupKeySQL[dim]
	if !ok {
		return "", fmt.Errorf("chiều nhóm %q: %w", string(dim), ErrBadParams)
	}
	parts, ok := groupParts[f.Basis]
	if !ok {
		return "", fmt.Errorf("cơ sở ngày %q: %w", string(f.Basis), ErrBadParams)
	}
	sel := keys[0] + " AS k1, " + keys[1] + " AS k2, " + keys[2] + " AS k3, " + keys[3] + " AS k4"
	subs := make([]string, 0, len(parts))
	for _, p := range parts {
		cte, err := orderSet(s, f, p.m, nil, b)
		if err != nil {
			return "", err
		}
		subs = append(subs, "SELECT "+sel+", "+p.aggs+"\nFROM (WITH "+cte+"\nSELECT * FROM "+orderSetName+") AS os\nGROUP BY 1, 2, 3, 4")
	}
	name := "NULL::text"
	if n, ok := groupNameSQL[dim]; ok {
		name = n
	}
	return "SELECT g.k1, g.k2, g.k3, g.k4, " + name + ", g.n, g.active, g.cancelled, g.delivered, g.returned, g.goods, g.ship\n" +
		"FROM (SELECT k1, k2, k3, k4, sum(n)::bigint AS n, sum(active)::bigint AS active, sum(cancelled)::bigint AS cancelled,\n" +
		"\tsum(delivered)::bigint AS delivered, sum(returned)::bigint AS returned, sum(goods)::bigint AS goods,\n" +
		"\tsum(ship)::bigint AS ship\n" +
		"FROM (" + strings.Join(subs, "\nUNION ALL\n") + ") AS p(k1, k2, k3, k4, n, active, cancelled, delivered, returned, goods, ship)\n" +
		"GROUP BY k1, k2, k3, k4) AS g", nil
}

// orderGroups chạy orderGroupsSQL và đọc các nhóm.
func orderGroups(ctx context.Context, tx pgx.Tx, s DataScope, f OrderFilter, dim Dimension) ([]orderGroup, error) {
	b := &sqlBuilder{}
	q, err := orderGroupsSQL(s, f, dim, b)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, q, b.args...)
	if err != nil {
		return nil, fmt.Errorf("đọc nhóm báo cáo đơn: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (orderGroup, error) {
		var g orderGroup
		var k [4]*string
		var name *string
		a := &g.agg
		if err := r.Scan(&k[0], &k[1], &k[2], &k[3], &name, &a.n, &a.active, &a.cancelled, &a.delivered, &a.returned,
			&a.goods, &a.ship); err != nil {
			return g, err //nolint:wrapcheck // CollectRows bọc dưới đây
		}
		for i, v := range k {
			if v != nil {
				g.keys[i] = *v
			}
		}
		if name != nil {
			g.name = *name
		}
		return g, nil
	})
	if err != nil {
		return nil, fmt.Errorf("đọc dòng nhóm báo cáo đơn: %w", err)
	}
	return out, nil
}

// drilldownSQL dựng câu một trang của danh sách chi tiết: tập của ô bằng orderSet (cùng hàm với báo cáo), trang theo
// keyset (event_at, order_id) giảm dần lấy limit + 1 dòng (dòng thừa báo còn trang sau), Total bằng count(*) OVER ()
// trước LIMIT; mã khách, tên nguồn, tên người phụ trách, mốc lên đơn và hoàn tất chỉ đọc cho các dòng của trang.
func drilldownSQL(s DataScope, f OrderFilter, m Metric, cell *Cell, cursor *DrillCursor, limit int, b *sqlBuilder) (string, error) {
	cte, err := orderSet(s, f, m, cell, b)
	if err != nil {
		return "", err
	}
	where := "TRUE"
	if cursor != nil {
		where = "(os.event_at, os.order_id) < (" + b.Arg(cursor.At) + "::timestamptz, " + b.Arg(cursor.ID) + "::uuid)"
	}
	placed := anchorSQL[anchorPlaced]
	return "WITH " + cte + ",\n" + drillPageName + " AS (\n\tSELECT os.*, count(*) OVER () AS total FROM " + orderSetName + " os WHERE " + where +
		"\n\tORDER BY os.event_at DESC, os.order_id DESC LIMIT " + b.Arg(limit+1) + `
)
SELECT p.order_id, p.event_at, p.code, c.code,
	(SELECT min(ev.occurred_at) FROM ` + placed.from + ` WHERE ev.order_id = p.order_id AND ` + placed.cond + `),
	(SELECT max(ev.occurred_at) FROM ord.order_status_history_v ev
		WHERE ev.order_id = p.order_id AND ev.to_status IN ('delivered', 'returned')),
	p.status, p.pending_reason, p.packing_status, coalesce(p.ship_status, ''), p.channel, ts.name, u.display_name,
	p.carrier_key, p.goods_amount, p.shipping_fee_charged, p.total
FROM ` + drillPageName + ` p
LEFT JOIN customer.customers_v c ON c.id = p.customer_id
LEFT JOIN ord.traffic_sources_v ts ON ts.id = p.source_id
LEFT JOIN identity.users_v u ON u.id = p.care_by
ORDER BY p.event_at DESC, p.order_id DESC`, nil
}

// drillRows chạy câu của drilldownSQL và đọc các dòng (nhãn gộp bằng orderview.Label) cùng Total của trang đầu.
func drillRows(ctx context.Context, tx pgx.Tx, q string, args []any) (items []DrillItem, total int64, err error) {
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("đọc danh sách chi tiết: %w", err)
	}
	items, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (DrillItem, error) {
		var it DrillItem
		var ax orderview.Axes
		if err := r.Scan(&it.OrderID, &it.EventAt, &it.Code, &it.CustomerCode, &it.PlacedAt, &it.CompletedAt, &ax.Status,
			&ax.PendingReason, &ax.PackingStatus, &ax.ShipmentStatus, &it.Channel, &it.SourceName, &it.CareByName,
			&it.CarrierKey, &it.GoodsAmount, &it.ShippingFeeCharged, &total); err != nil {
			return it, err //nolint:wrapcheck // CollectRows bọc dưới đây
		}
		it.StatusLabel = orderview.Label(ax)
		return it, nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("đọc dòng danh sách chi tiết: %w", err)
	}
	return items, total, nil
}
