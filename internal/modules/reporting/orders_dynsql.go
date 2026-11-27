package reporting

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
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

// orderSetColumns: cột của order_set (một dòng mỗi đơn). Không có SĐT, địa chỉ, tên khách (Global Constraints M6).
const orderSetColumns = `o.id AS order_id, %s AS event_at, o.code, o.customer_id, o.status, o.pending_reason,
	o.packing_status, o.channel, o.channel_account_id AS page_id, o.traffic_source_id AS source_id, o.care_by,
	o.created_by, o.subtotal - o.discount_total AS goods_amount, o.shipping_fee_charged, o.created_at,
	sh.carrier_key, sh.status AS ship_status`

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
		return "order_set AS (\n\t" + inner + "\n)", nil
	}
	return "order_set AS (\n\tSELECT * FROM (" + inner + ") AS first_event WHERE " + outer + "\n)", nil
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
	shipments := "\n\tLEFT JOIN fulfillment.shipments_v sh ON sh.id = o.current_shipment_id AND sh.seller_id = " + seller
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
		conds = append(conds, "sh.carrier_key = ANY("+b.Arg(f.CarrierKeys)+"::text[])")
	}
	return conds, nil
}

// cellCond dựng điều kiện của ô: cond trên cột của đơn, hoặc dayCond trên mốc event_at của tập (chiều day: ngày giờ
// VN của mốc theo cơ sở, xét sau khi đã chọn dòng sự kiện sớm nhất, để ô khớp với cách nhóm theo ngày). Chiều status
// tạm so trạng thái thô của đơn; Task 230 đổi sang nhãn gộp của orderview (G9).
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
			return "sh.carrier_key IS NULL", "", nil
		}
		if !carrierKeyRe.MatchString(c.Key) {
			return bad()
		}
		return "sh.carrier_key = " + b.Arg(c.Key), "", nil
	case DimStatus:
		if !orderStatusSet[c.Key] {
			return bad()
		}
		return "o.status = " + b.Arg(c.Key), "", nil
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
