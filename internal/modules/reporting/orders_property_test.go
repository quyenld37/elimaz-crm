package reporting

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/testkit/reportseed"
)

// propPaths: diễn biến trạng thái hợp lệ của đơn (sơ đồ 05 mục 2), kể cả nháp chưa chốt và hãng sửa "đã giao" → hoàn.
//
//nolint:gochecknoglobals // bảng hằng của test
var propPaths = [][]string{
	{"draft"},
	{"new"},
	{"new", "cancelled"},
	{"draft", "cancelled"},
	{"new", "confirmed"},
	{"draft", "confirmed"},
	{"new", "confirmed", "delivered"},
	{"draft", "confirmed", "returned"},
	{"new", "confirmed", "cancel_requested"},
	{"new", "confirmed", "cancel_requested", "confirmed"},
	{"new", "confirmed", "delivered", "confirmed", "returned"},
}

//nolint:gochecknoglobals // bảng hằng của test
var propShipStatuses = []string{
	"creating", "awaiting_pickup", "shipping", "cancel_requested", "returning", "delivery_failed", "delivered", "returned",
	"cancelled",
}

// propOrder là một đơn sinh ngẫu nhiên và mô hình Go của nó (để so số của báo cáo với cách đếm độc lập).
type propOrder struct {
	spec   reportseed.OrderSpec
	placed *time.Time             // mốc lên đơn: bước đầu vào new/confirmed từ ” hay draft
	done   map[string][]time.Time // mốc giao xong, hoàn
	status string
	goods  int64
}

// propWorld là dữ liệu chung của một lượt: tenant mới, hai Page, hai nguồn, hai nhân viên (cộng "không có"), một biến
// thể; ngày 0 là 16/11/2026 giờ VN.
type propWorld struct {
	seed           *reportseed.Seed
	tenant, own    uuid.UUID
	pages          []uuid.UUID
	sources, staff []uuid.UUID
	variant        uuid.UUID
	day0           time.Time
	marks          []time.Time
}

func newPropWorld(t *testing.T, e setEnv, n int) propWorld {
	t.Helper()
	tenant := e.st.Tenant(t, fmt.Sprintf("p%d", n))
	seed := reportseed.New(t, e.st.Owner, tenant)
	w := propWorld{
		seed: seed, tenant: tenant, own: seed.Own(), pages: []uuid.UUID{seed.Page("P1"), seed.Page("P2")},
		sources: []uuid.UUID{uuid.Nil, seed.Source("S1"), seed.Source("S2")},
		staff:   []uuid.UUID{uuid.Nil, seed.Staff("A"), seed.Staff("B")},
		day0:    time.Date(2026, 11, 16, 0, 0, 0, 0, VN),
	}
	w.variant = seed.Variant(w.own, "Áo", "AO-1")
	// Mốc lên đơn sát biên 00:00 giờ VN (±1 giây) của bốn ngày.
	for d := range 4 {
		b := w.day0.AddDate(0, 0, d)
		w.marks = append(w.marks, b.Add(-time.Second), b, b.Add(time.Second))
	}
	return w
}

// history sinh lịch sử của đơn i theo một diễn biến hợp lệ, bắt đầu sát biên ngày hay bất kỳ, và ghi mô hình.
func (w propWorld) history(rt *rapid.T, i int, po *propOrder) []reportseed.Step {
	start := w.day0.Add(time.Duration(rapid.IntRange(-24*60, 4*24*60).Draw(rt, fmt.Sprintf("min%d", i))) * time.Minute)
	if rapid.Bool().Draw(rt, fmt.Sprintf("edge%d", i)) {
		start = rapid.SampledFrom(w.marks).Draw(rt, fmt.Sprintf("mark%d", i))
	}
	path := rapid.SampledFrom(propPaths).Draw(rt, fmt.Sprintf("path%d", i))
	po.status = path[len(path)-1]
	at, from := start.UTC(), ""
	steps := make([]reportseed.Step, 0, len(path))
	for k, to := range path {
		if k > 0 {
			at = at.Add(time.Duration(rapid.IntRange(1, 30*60).Draw(rt, fmt.Sprintf("gap%d.%d", i, k))) * time.Minute)
		}
		steps = append(steps, reportseed.Step{To: to, At: at})
		if po.placed == nil && (to == "new" || to == "confirmed") && (from == "" || from == "draft") {
			po.placed = new(at)
		}
		if to == "delivered" || to == "returned" {
			po.done[to] = append(po.done[to], at)
		}
		from = to
	}
	return steps
}

// shipments sinh 0–2 vận đơn cho đơn đã xác nhận; mọi vận đơn trừ cái cuối đã hủy (mỗi đơn tối đa một vận đơn chưa
// hủy, shipments_active_order).
func shipments(rt *rapid.T, i int, after time.Time) []reportseed.ShipmentSpec {
	n := rapid.IntRange(0, 2).Draw(rt, fmt.Sprintf("nship%d", i))
	out := make([]reportseed.ShipmentSpec, 0, n)
	for k := range n {
		status := rapid.SampledFrom(propShipStatuses).Draw(rt, fmt.Sprintf("shst%d.%d", i, k))
		if k < n-1 {
			status = "cancelled"
		}
		out = append(out, reportseed.ShipmentSpec{
			CarrierKey: rapid.SampledFrom([]string{"vtp", "spx", "best"}).Draw(rt, fmt.Sprintf("car%d.%d", i, k)),
			Status:     status, Fee: 22_000, CreatedAt: after.Add(time.Duration(k+1) * time.Second),
		})
	}
	return out
}

// order sinh và chèn đơn thứ i.
func (w propWorld) order(rt *rapid.T, i int) *propOrder {
	po := &propOrder{done: map[string][]time.Time{}}
	steps := w.history(rt, i, po)
	spec := reportseed.OrderSpec{
		Channel:   rapid.SampledFrom([]string{"facebook", "web", "manual"}).Draw(rt, fmt.Sprintf("ch%d", i)),
		Source:    rapid.SampledFrom(w.sources).Draw(rt, fmt.Sprintf("src%d", i)),
		CareBy:    rapid.SampledFrom(w.staff).Draw(rt, fmt.Sprintf("care%d", i)),
		CreatedBy: rapid.SampledFrom(w.staff).Draw(rt, fmt.Sprintf("by%d", i)),
		Items: []reportseed.Item{{
			Variant: w.variant, Qty: rapid.IntRange(1, 3).Draw(rt, fmt.Sprintf("qty%d", i)),
			Price: int64(rapid.IntRange(1, 9).Draw(rt, fmt.Sprintf("price%d", i))) * 50_000,
		}},
		ShippingFee: rapid.SampledFrom([]int64{0, 30_000}).Draw(rt, fmt.Sprintf("ship%d", i)),
		Discount:    rapid.SampledFrom([]int64{0, 10_000}).Draw(rt, fmt.Sprintf("disc%d", i)),
		History:     steps,
	}
	if spec.Channel == "facebook" && rapid.IntRange(0, 4).Draw(rt, fmt.Sprintf("nopage%d", i)) > 0 {
		spec.Page = rapid.SampledFrom(w.pages).Draw(rt, fmt.Sprintf("page%d", i))
	}
	if slices.ContainsFunc(steps, func(s reportseed.Step) bool { return s.To == "confirmed" }) {
		spec.Shipments = shipments(rt, i, steps[len(steps)-1].At)
	}
	po.goods = int64(spec.Items[0].Qty)*spec.Items[0].Price - spec.Discount
	po.spec = spec
	w.seed.Order(spec)
	return po
}

// query sinh kỳ (trong bốn ngày), cơ sở, chiều nhóm, phạm vi Page và bộ lọc.
func (w propWorld) query(rt *rapid.T) (DataScope, OrderFilter, Dimension) {
	fromDay := rapid.IntRange(0, 3).Draw(rt, "from")
	toDay := rapid.IntRange(fromDay, 3).Draw(rt, "to")
	period, err := ParsePeriod(w.day0.AddDate(0, 0, fromDay).Format(dayLayout), w.day0.AddDate(0, 0, toDay).Format(dayLayout), MaxDays)
	if err != nil {
		rt.Fatal(err)
	}
	f := OrderFilter{Period: period, Basis: rapid.SampledFrom([]Basis{BasisPlaced, BasisCompleted}).Draw(rt, "basis")}
	dim := rapid.SampledFrom([]Dimension{DimStatus, DimSource, DimCareBy, DimCreatedBy, DimChannel, DimPage, DimCarrier, DimDay}).Draw(rt, "dim")
	s := DataScope{TenantID: w.tenant, SellerID: w.own, AllPages: true}
	if rapid.Bool().Draw(rt, "pageScope") {
		s.AllPages = false
		for _, p := range w.pages {
			if rapid.Bool().Draw(rt, "scopePage") {
				s.PageIDs = append(s.PageIDs, p)
			}
		}
	}
	if rapid.Bool().Draw(rt, "filterChannel") {
		f.Channels = []string{rapid.SampledFrom([]string{"facebook", "web", "manual"}).Draw(rt, "channel")}
	}
	if rapid.Bool().Draw(rt, "filterCarrier") {
		f.CarrierKeys = []string{rapid.SampledFrom([]string{"vtp", "spx", "best", UnknownKey}).Draw(rt, "carrier")}
	}
	if rapid.Bool().Draw(rt, "filterCare") {
		f.CareBy = []uuid.UUID{rapid.SampledFrom(w.staff[1:]).Draw(rt, "careFilter")}
	}
	return s, f, dim
}

// Thuộc tính của báo cáo đơn (kế hoạch M6, Task 230 Step 1, BC02, Review Focus 2), 200 lượt: dữ liệu ngẫu nhiên 0–60
// đơn (kênh, Page, nguồn, nhân viên, 0–2 vận đơn, lịch sử có mốc sát biên ngày giờ VN), bộ lọc, chiều nhóm, cơ sở và
// phạm vi ngẫu nhiên →
// (0) dòng tổng bằng cách đếm độc lập bằng Go trên dữ liệu đã sinh;
// (1) tổng các dòng của mỗi cột = Totals;
// (2) với mỗi dòng và mỗi cột số đơn, Total của danh sách chi tiết cùng ô, cùng cột = giá trị cột (cả dòng tổng);
// (3) tổng goodsAmount qua mọi trang danh sách chi tiết của cột doanh số = GoodsAmount của dòng;
// (4) không đơn nào nằm ở hai dòng của cùng một cột, hợp các dòng = danh sách của dòng tổng.
func TestOrdersProperty(t *testing.T) {
	t.Parallel()
	e := newSetEnv(t)
	n := 0
	rapid.Check(t, func(rt *rapid.T) {
		n++
		w := newPropWorld(t, e, n)
		count := rapid.IntRange(0, 60).Draw(rt, "orders")
		orders := make([]*propOrder, 0, count)
		for i := range count {
			orders = append(orders, w.order(rt, i))
		}
		s, f, dim := w.query(rt)
		p := OrdersParams{SalesFilterParams: SalesFilterParams{
			PeriodParams: PeriodParams{From: f.Period.FromDay, To: f.Period.ToDay}, Basis: f.Basis, Channels: f.Channels,
			CareBy: f.CareBy, CarrierKeys: f.CarrierKeys,
		}, GroupBy: dim}
		rep, err := runOrders(t, e.st.App, s, p)
		if err != nil {
			rt.Fatalf("báo cáo: %v", err)
		}
		checkModel(rt, rep, orders, s, f)
		checkRowSums(rt, rep, f.Basis)
		d := drillCheck{t: t, rt: rt, e: e, s: s, f: f, limit: rapid.IntRange(1, 7).Draw(rt, "limit"), seen: map[Metric]map[uuid.UUID]string{}}
		for _, r := range rep.Rows {
			d.row(r, &Cell{Dimension: dim, Key: r.Key})
		}
		d.row(rep.Totals, nil)
	})
}

// checkRowSums: (1) tổng các dòng của mỗi cột = Totals.
func checkRowSums(rt *rapid.T, rep *OrdersReport, basis Basis) {
	var sum OrdersRow
	var placed, active, cancelled int64
	for _, r := range rep.Rows {
		sum.Orders += r.Orders
		sum.GoodsAmount += r.GoodsAmount
		sum.ShippingFeeCharged += r.ShippingFeeCharged
		sum.Delivered += r.Delivered
		sum.Returned += r.Returned
		placed, active, cancelled = placed+deref(r.Placed), active+deref(r.Active), cancelled+deref(r.Cancelled)
	}
	tt := rep.Totals
	if sum.Orders != tt.Orders || sum.GoodsAmount != tt.GoodsAmount || sum.ShippingFeeCharged != tt.ShippingFeeCharged ||
		sum.Delivered != tt.Delivered || sum.Returned != tt.Returned {
		rt.Fatalf("tổng các dòng %s ≠ Totals %s", fmtRow(sum), fmtRow(tt))
	}
	if basis == BasisPlaced && (placed != deref(tt.Placed) || active != deref(tt.Active) || cancelled != deref(tt.Cancelled)) {
		rt.Fatalf("tổng các dòng lên %d, chưa hủy %d, hủy %d ≠ Totals %s", placed, active, cancelled, fmtRow(tt))
	}
}

// drillCheck so một dòng với danh sách chi tiết của từng cột số đơn: (2), (3), (4). seen nhớ dòng của mỗi đơn theo cột.
type drillCheck struct {
	t     *testing.T
	rt    *rapid.T
	e     setEnv
	s     DataScope
	f     OrderFilter
	limit int
	seen  map[Metric]map[uuid.UUID]string
}

func (d drillCheck) row(r OrdersRow, cell *Cell) {
	for _, m := range columnMetrics(d.f.Basis) {
		total, items := drillAll(d.t, d.e.st.App, d.s, d.f, m, cell, d.limit)
		if want := columnValue(r, m); total != want || int64(len(items)) != want {
			d.rt.Fatalf("dòng %s cột %s: danh sách chi tiết total %d, %d dòng; ô %d", r.Key, m, total, len(items), want)
		}
		var goods int64
		for _, it := range items {
			goods += it.GoodsAmount
		}
		if m == goodsMetric(d.f.Basis) && goods != r.GoodsAmount {
			d.rt.Fatalf("dòng %s: tổng tiền danh sách chi tiết %d ≠ GoodsAmount %d", r.Key, goods, r.GoodsAmount)
		}
		if cell == nil {
			d.union(m, items)
		} else {
			d.mark(m, r.Key, items)
		}
	}
}

// mark ghi dòng của từng đơn; một đơn ở hai dòng của cùng cột → lỗi.
func (d drillCheck) mark(m Metric, key string, items []DrillItem) {
	if d.seen[m] == nil {
		d.seen[m] = map[uuid.UUID]string{}
	}
	for _, it := range items {
		if prev, dup := d.seen[m][it.OrderID]; dup {
			d.rt.Fatalf("cột %s: đơn %s ở hai dòng %s và %s", m, it.OrderID, prev, key)
		}
		d.seen[m][it.OrderID] = key
	}
}

// union: hợp các dòng của cột m bằng đúng danh sách của dòng tổng.
func (d drillCheck) union(m Metric, items []DrillItem) {
	if len(d.seen[m]) != len(items) {
		d.rt.Fatalf("cột %s: hợp các dòng %d đơn, dòng tổng %d", m, len(d.seen[m]), len(items))
	}
	for _, it := range items {
		if _, ok := d.seen[m][it.OrderID]; !ok {
			d.rt.Fatalf("cột %s: đơn %s ở dòng tổng nhưng không ở dòng nào", m, it.OrderID)
		}
	}
}

// modelCounts là số của dòng tổng đếm bằng Go trên mô hình.
type modelCounts struct {
	placed, active, cancelled, delivered, returned, goods int64
}

// inModel: đơn qua phạm vi và bộ lọc (không xét kỳ).
func inModel(sp reportseed.OrderSpec, s DataScope, f OrderFilter) bool {
	switch {
	case !s.AllPages && (sp.Page == uuid.Nil || !slices.Contains(s.PageIDs, sp.Page)):
		return false
	case len(f.Channels) > 0 && !slices.Contains(f.Channels, sp.Channel):
		return false
	case len(f.CareBy) > 0 && !slices.Contains(f.CareBy, sp.CareBy):
		return false
	case len(f.CarrierKeys) > 0 && !slices.Contains(f.CarrierKeys, currentCarrier(sp)):
		return false
	}
	return true
}

func (c *modelCounts) addPlaced(o *propOrder) {
	c.placed++
	if o.status == "cancelled" {
		c.cancelled++
	} else {
		c.active++
		c.goods += o.goods
	}
	switch o.status {
	case "delivered":
		c.delivered++
	case "returned":
		c.returned++
	}
}

// checkModel: (0) dòng tổng so với cách đếm bằng Go trên dữ liệu đã sinh (không qua orderSet).
func checkModel(rt *rapid.T, rep *OrdersReport, orders []*propOrder, s DataScope, f OrderFilter) {
	in := func(t time.Time) bool { return !t.Before(f.Period.Start) && t.Before(f.Period.End) }
	var c modelCounts
	for _, o := range orders {
		switch {
		case !inModel(o.spec, s, f):
		case f.Basis == BasisPlaced:
			if o.placed != nil && in(*o.placed) {
				c.addPlaced(o)
			}
		default:
			if slices.ContainsFunc(o.done["delivered"], in) {
				c.delivered++
				c.goods += o.goods
			}
			if slices.ContainsFunc(o.done["returned"], in) {
				c.returned++
			}
		}
	}
	got := rep.Totals
	okHead := got.Orders == c.delivered+c.returned && got.Placed == nil && got.Active == nil && got.Cancelled == nil
	if f.Basis == BasisPlaced {
		okHead = got.Orders == c.placed && deref(got.Active) == c.active && deref(got.Cancelled) == c.cancelled
	}
	if !okHead || got.GoodsAmount != c.goods || got.Delivered != c.delivered || got.Returned != c.returned {
		rt.Fatalf("mô hình %+v; báo cáo %s", c, fmtRow(got))
	}
}

// currentCarrier là hãng của vận đơn hiện hành theo cách reportseed gắn (vận đơn chưa hủy cuối cùng), unknown khi không có.
func currentCarrier(o reportseed.OrderSpec) string {
	key := UnknownKey
	for _, sh := range o.Shipments {
		if sh.Status != "cancelled" {
			key = sh.CarrierKey
		}
	}
	return key
}

func columnMetrics(b Basis) []Metric {
	if b == BasisCompleted {
		return []Metric{MetricDelivered, MetricReturned}
	}
	return []Metric{MetricPlaced, MetricPlacedActive, MetricCancelled, MetricDelivered, MetricReturned}
}

func goodsMetric(b Basis) Metric {
	if b == BasisCompleted {
		return MetricDelivered
	}
	return MetricPlacedActive
}

func columnValue(r OrdersRow, m Metric) int64 {
	switch m {
	case MetricPlaced:
		return deref(r.Placed)
	case MetricPlacedActive:
		return deref(r.Active)
	case MetricCancelled:
		return deref(r.Cancelled)
	case MetricDelivered:
		return r.Delivered
	case MetricReturned:
		return r.Returned
	case MetricCustomerReturned, MetricPendingConfirm, MetricPendingShip, MetricCancelRequested:
	}
	return -1
}

func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
