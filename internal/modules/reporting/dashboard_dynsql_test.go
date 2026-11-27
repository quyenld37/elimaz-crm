package reporting

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/reportseed"
)

// runDashboard chạy tổng quan của kỳ from–to bằng crm_app trong ngữ cảnh tenant (như Interactive.Run, không giới hạn chỗ).
func runDashboard(ctx context.Context, d *db.DB, s DataScope, from, to string) (DashboardReport, error) {
	var out DashboardReport
	err := tenancy.WithTenant(ctx, d, s.TenantID, func(tx pgx.Tx) error {
		r, err := dashboard(ctx, tx, s, &DashboardParams{PeriodParams{From: from, To: to}})
		out = r
		return err
	})
	if err != nil {
		return DashboardReport{}, fmt.Errorf("tổng quan: %w", err)
	}
	return out, nil
}

func mustDashboard(t *testing.T, e setEnv, s DataScope, from, to string) DashboardReport {
	t.Helper()
	r, err := runDashboard(t.Context(), e.st.App, s, from, to)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func expectAmounts(t *testing.T, name string, got, want Amounts) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %+v, mong %+v", name, got, want)
	}
}

// outsiders thêm một đơn của người bán partner trên Page page và một đơn của tenant B, cùng ngày 17/11/2026 giờ VN:
// không lọt vào tập nào của người bán own. Trả mã đơn và mã người bán partner.
func (e setEnv) outsiders(t *testing.T, page uuid.UUID) (partnerOrder, partner uuid.UUID) {
	t.Helper()
	partner = e.seed.Seller("NCC1", "partner")
	pv := e.seed.Variant(partner, "Hàng ký gửi", "KG-1")
	at := time.Date(2026, 11, 17, 3, 0, 0, 0, time.UTC)
	partnerOrder = e.seed.Order(reportseed.OrderSpec{
		Seller: partner, Channel: "facebook", Page: page, Items: []reportseed.Item{{Variant: pv, Qty: 1, Price: 99_000}},
		History: []reportseed.Step{{To: "new", At: at}, {To: "confirmed", At: at.Add(time.Minute)}},
	})
	sb := reportseed.New(t, e.st.Owner, e.st.Tenant(t, "b"))
	bv := sb.Variant(sb.Own(), "Áo B", "AOB-1")
	sb.Order(reportseed.OrderSpec{
		Channel: "manual", Items: []reportseed.Item{{Variant: bv, Qty: 1, Price: 10_000}},
		History: []reportseed.Step{{To: "new", At: at}},
	})
	return partnerOrder, partner
}

// bulk dựng 100.000 đơn ngẫu nhiên (lặp lại được) lên trong 2025–2026 cho các test EXPLAIN.
func (e setEnv) bulk() {
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	e.seed.Bulk(100_000, rand.New(rand.NewPCG(7, 11)), //nolint:gosec // G404: dữ liệu thử lặp lại được, không phải bí mật
		from, from.AddDate(2, 0, 0))
}

// ownerCount đếm bằng role chủ bảng trên bảng gốc (không qua view, không qua orderSet), trong ngữ cảnh tenant.
func ownerCount(t *testing.T, e setEnv, q string, args ...any) int64 {
	t.Helper()
	var n int64
	e.st.Exec(t, e.tenant, func(tx pgx.Tx) error { return tx.QueryRow(t.Context(), q, args...).Scan(&n) })
	return n
}

// Câu đếm độc lập trên bảng gốc cho từng ô (phạm vi all, người bán own, kỳ $2–$3).
const (
	indepPlaced = `SELECT count(*) FROM ord.orders o WHERE o.seller_id = $1 AND EXISTS (
		SELECT 1 FROM ord.order_status_history h WHERE h.order_id = o.id AND h.to_status IN ('new', 'confirmed')
		  AND coalesce(h.from_status, '') IN ('', 'draft') AND h.occurred_at >= $2 AND h.occurred_at < $3)`
	indepDone = `SELECT count(*) FROM ord.orders o WHERE o.seller_id = $1 AND EXISTS (
		SELECT 1 FROM ord.order_status_history h WHERE h.order_id = o.id AND h.to_status = $4
		  AND h.occurred_at >= $2 AND h.occurred_at < $3)`
	indepCustomerReturned = `SELECT count(*) FROM ord.orders o WHERE o.seller_id = $1 AND EXISTS (
		SELECT 1 FROM ord.returns r WHERE r.order_id = o.id AND r.kind = 'customer_return' AND r.status <> 'cancelled'
		  AND r.received_at >= $2 AND r.received_at < $3)`
)

// Kịch bản chín đơn (kế hoạch M6, Task 229): mọi ô của người phạm vi all và của người chỉ có P1; mỗi ô bằng số đơn của
// orderSet cùng Metric và bằng câu đếm độc lập trên bảng gốc; người bán partner, tenant khác không lọt vào.
func TestDashboard_NineOrders(t *testing.T) {
	t.Parallel()
	e := newSetEnv(t)
	sc := reportseed.NineOrders(e.seed, "2026-11-17")
	e.outsiders(t, sc.P1)

	r := mustDashboard(t, e, e.all, "2026-11-17", "2026-11-17")
	if r.Status != "ready" || r.Period.FromDay != "2026-11-17" || r.Period.ToDay != "2026-11-17" || r.GeneratedAt.IsZero() {
		t.Fatalf("đầu báo cáo: %q %+v %v", r.Status, r.Period, r.GeneratedAt)
	}
	expectAmounts(t, "Placed", r.Placed, Amounts{8, 2_110_000, 150_000})
	expectAmounts(t, "Active", r.Active, Amounts{7, 1_910_000, 150_000})
	expectAmounts(t, "Cancelled", r.Cancelled, Amounts{1, 200_000, 0})
	expectAmounts(t, "Delivered", r.Delivered, Amounts{2, 750_000, 60_000})
	expectAmounts(t, "Returned", r.Returned, Amounts{1, 250_000, 30_000})
	if r.AvgGoodsAmount == nil || *r.AvgGoodsAmount != 272_857 {
		t.Errorf("AvgGoodsAmount = %v, mong 272.857", r.AvgGoodsAmount)
	}
	if r.CustomerReturns != (CustomerReturns{Items: 1, RefundAmount: 150_000}) {
		t.Errorf("CustomerReturns = %+v", r.CustomerReturns)
	}
	if r.Pending != (Pending{Confirm: 1, Ship: 1, CancelRequested: 1}) {
		t.Errorf("Pending = %+v", r.Pending)
	}
	wantCh := []ChannelRow{
		{Channel: "facebook", Placed: Amounts{6, 1_500_000, 120_000}, Active: Amounts{5, 1_300_000, 120_000}},
		{Channel: "web", Placed: Amounts{1, 360_000, 30_000}, Active: Amounts{1, 360_000, 30_000}},
		{Channel: "manual", Placed: Amounts{1, 250_000, 0}, Active: Amounts{1, 250_000, 0}},
	}
	if fmt.Sprint(r.ByChannel) != fmt.Sprint(wantCh) {
		t.Errorf("ByChannel = %+v\nmong %+v", r.ByChannel, wantCh)
	}
	var sum Amounts
	for _, c := range r.ByChannel {
		sum = sum.add(c.Active)
	}
	expectAmounts(t, "tổng Active các kênh", sum, r.Active)
	if r.ByDay == nil || len(r.ByDay) != 0 {
		t.Errorf("kỳ một ngày: ByDay = %#v, mong rỗng không nil", r.ByDay)
	}

	e.checkCells(t, r)

	e.checkScoped(t, sc.P1)
}

// checkScoped: người chỉ có report.sales.read trên P1 thấy số của P1 (bằng orderSet của phạm vi đó); không grant nào
// thì mọi số 0.
func (e setEnv) checkScoped(t *testing.T, page uuid.UUID) {
	t.Helper()
	// Người chỉ có report.sales.read trên P1.
	p1 := DataScope{TenantID: e.tenant, SellerID: e.all.SellerID, PageIDs: []uuid.UUID{page}}
	r1 := mustDashboard(t, e, p1, "2026-11-17", "2026-11-17")
	if r1.Placed.Orders != 4 {
		t.Errorf("P1: Placed.Orders = %d, mong 4", r1.Placed.Orders)
	}
	expectAmounts(t, "P1 Active", r1.Active, Amounts{4, 1_150_000, 120_000})
	if r1.Pending != (Pending{Confirm: 0, Ship: 0, CancelRequested: 1}) {
		t.Errorf("P1: Pending = %+v", r1.Pending)
	}
	for _, m := range []Metric{MetricPlaced, MetricPlacedActive} {
		rows, err := readSet(t.Context(), e.st.App, p1, OrderFilter{Period: mustPeriod(t, "2026-11-17", "2026-11-17"), Basis: BasisPlaced}, m, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 4 {
			t.Errorf("P1 orderSet %s = %d đơn", m, len(rows))
		}
	}
	// Không grant nào: mọi số 0.
	none := mustDashboard(t, e, DataScope{TenantID: e.tenant, SellerID: e.all.SellerID}, "2026-11-17", "2026-11-17")
	if none.Placed != (Amounts{}) || none.Active != (Amounts{}) || none.Delivered != (Amounts{}) || none.Returned != (Amounts{}) ||
		none.CustomerReturns != (CustomerReturns{}) || none.Pending != (Pending{}) || len(none.ByChannel) != 0 || none.AvgGoodsAmount != nil {
		t.Errorf("không phạm vi: %+v", none)
	}
}

// checkCells: mỗi ô của r (kỳ 17/11/2026, phạm vi all) = số đơn của orderSet cùng Metric (cơ sở của ô) = câu đếm độc lập
// trên bảng gốc.
func (e setEnv) checkCells(t *testing.T, r DashboardReport) {
	t.Helper()
	period := mustPeriod(t, "2026-11-17", "2026-11-17")
	pStart, pEnd := period.Start, period.End
	cells := []struct {
		m     Metric
		basis Basis
		got   int64
		indep string
		args  []any
	}{
		{MetricPlaced, BasisPlaced, r.Placed.Orders, indepPlaced, nil},
		{MetricPlacedActive, BasisPlaced, r.Active.Orders, indepPlaced + ` AND o.status <> 'cancelled'`, nil},
		{MetricCancelled, BasisPlaced, r.Cancelled.Orders, indepPlaced + ` AND o.status = 'cancelled'`, nil},
		{MetricDelivered, BasisCompleted, r.Delivered.Orders, indepDone, []any{"delivered"}},
		{MetricReturned, BasisCompleted, r.Returned.Orders, indepDone, []any{"returned"}},
		{MetricCustomerReturned, BasisPlaced, r.CustomerReturns.Items, indepCustomerReturned, nil},
		{MetricPendingConfirm, BasisPlaced, r.Pending.Confirm, `SELECT count(*) FROM ord.orders WHERE seller_id = $1 AND status = 'new' AND $2::timestamptz < $3`, nil},
		{MetricPendingShip, BasisPlaced, r.Pending.Ship, `SELECT count(*) FROM ord.orders WHERE seller_id = $1 AND status = 'confirmed'
			AND current_shipment_id IS NULL AND $2::timestamptz < $3`, nil},
		{MetricCancelRequested, BasisPlaced, r.Pending.CancelRequested, `SELECT count(*) FROM ord.orders WHERE seller_id = $1
			AND status = 'cancel_requested' AND $2::timestamptz < $3`, nil},
	}
	for _, c := range cells {
		rows, err := readSet(t.Context(), e.st.App, e.all, OrderFilter{Period: period, Basis: c.basis}, c.m, nil)
		if err != nil {
			t.Fatal(err)
		}
		if int64(len(rows)) != c.got {
			t.Errorf("ô %s = %d, orderSet có %d đơn", c.m, c.got, len(rows))
		}
		if n := ownerCount(t, e, c.indep, append([]any{e.all.SellerID, pStart, pEnd}, c.args...)...); n != c.got {
			t.Errorf("ô %s = %d, câu độc lập đếm %d", c.m, c.got, n)
		}
	}
}

// Ngày không có đơn: mọi số 0, AvgGoodsAmount nil, ByChannel rỗng không nil.
func TestDashboard_EmptyDay(t *testing.T) {
	t.Parallel()
	e := newSetEnv(t)
	reportseed.NineOrders(e.seed, "2026-11-17")
	r := mustDashboard(t, e, e.all, "2026-11-20", "2026-11-20")
	for name, a := range map[string]Amounts{
		"Placed": r.Placed, "Active": r.Active, "Cancelled": r.Cancelled,
		"Delivered": r.Delivered, "Returned": r.Returned,
	} {
		expectAmounts(t, name, a, Amounts{})
	}
	if r.AvgGoodsAmount != nil || r.CustomerReturns != (CustomerReturns{}) {
		t.Errorf("ngày rỗng: avg %v, trả %+v", r.AvgGoodsAmount, r.CustomerReturns)
	}
	// Số chờ xử lý là số hiện tại, không theo kỳ.
	if r.Pending != (Pending{Confirm: 1, Ship: 1, CancelRequested: 1}) {
		t.Errorf("Pending = %+v", r.Pending)
	}
	if r.ByChannel == nil || len(r.ByChannel) != 0 {
		t.Errorf("ByChannel = %#v", r.ByChannel)
	}
}

// Kỳ 3 ngày: ByDay đủ 3 dòng (ngày không đơn là 0), tổng bằng Active và Delivered; mốc sát biên ngày giờ VN.
func TestDashboard_ByDay(t *testing.T) {
	t.Parallel()
	e := newSetEnv(t)
	reportseed.NineOrders(e.seed, "2026-11-17")
	v := e.seed.Variant(e.seed.Own(), "Áo D", "AOD-1")
	// Lên đơn 23:59:59 ngày 15/11 giờ VN, giao 00:00 ngày 16/11 giờ VN (không trong kỳ 16–18 với lên đơn).
	late := time.Date(2026, 11, 15, 16, 59, 59, 0, time.UTC)
	e.seed.Order(reportseed.OrderSpec{
		Channel: "manual", Items: []reportseed.Item{{Variant: v, Qty: 1, Price: 100_000}}, ShippingFee: 30_000,
		History: []reportseed.Step{{To: "new", At: late}, {To: "confirmed", At: late.Add(time.Second / 2)}, {To: "delivered", At: late.Add(time.Second)}},
		Shipments: []reportseed.ShipmentSpec{{
			CarrierKey: "vtp", Status: "delivered", Fee: reportseed.FakeCarrierFee,
			COD: 130_000, CreatedAt: late.Add(time.Second / 2),
		}},
	})
	r := mustDashboard(t, e, e.all, "2026-11-16", "2026-11-18")
	if len(r.ByDay) != 3 || r.ByDay[0].Day != "2026-11-16" || r.ByDay[1].Day != "2026-11-17" || r.ByDay[2].Day != "2026-11-18" {
		t.Fatalf("ByDay = %+v", r.ByDay)
	}
	expectAmounts(t, "16/11 Active", r.ByDay[0].Active, Amounts{})
	expectAmounts(t, "16/11 Delivered", r.ByDay[0].Delivered, Amounts{1, 100_000, 30_000})
	expectAmounts(t, "17/11 Active", r.ByDay[1].Active, Amounts{7, 1_910_000, 150_000})
	expectAmounts(t, "18/11", r.ByDay[2].Active, Amounts{})
	var act, del Amounts
	for _, d := range r.ByDay {
		act, del = act.add(d.Active), del.add(d.Delivered)
	}
	expectAmounts(t, "tổng Active theo ngày", act, r.Active)
	expectAmounts(t, "tổng Delivered theo ngày", del, r.Delivered)
	expectAmounts(t, "Delivered", r.Delivered, Amounts{3, 850_000, 90_000})
}

// EXPLAIN bằng crm_app (thiết lập của TxReport) của hai câu tổng quan 31 ngày trên 100.000 đơn, phạm vi all: không Seq
// Scan. Phạm vi một Page: nhánh giao thành công của orderSet (Task 227) có thể ra Hash Join + Seq Scan đơn (chi phí sát
// nhau), ghi ở "Việc phát hiện thêm" của Task 229.
func TestDashboard_ExplainNoSeqScan(t *testing.T) {
	t.Parallel()
	e := newSetEnv(t)
	e.bulk()
	// EXPLAIN trên kết nối có đúng thiết lập của TxReport (không chạy song song, work_mem 32MB) như lúc chạy thật.
	cfg := e.st.App.Pool.Config()
	for _, set := range TxReport(time.Minute, time.Second).Settings {
		if set.Name != "transaction_read_only" {
			cfg.ConnConfig.RuntimeParams[set.Name] = set.Value
		}
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	report := &db.DB{Pool: pool}
	period := mustPeriod(t, "2026-03-01", "2026-03-31")
	for _, s := range []DataScope{e.all} {
		for name, build := range map[string]func(DataScope, Period, *sqlBuilder) (string, error){
			"ô số": dashboardSQL, "theo ngày": dashboardDaySQL,
		} {
			b := &sqlBuilder{}
			q, err := build(s, period, b)
			if err != nil {
				t.Fatal(err)
			}
			nodes, err := testkit.ExplainAsApp(t.Context(), report, e.tenant, q, b.args...)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			testkit.RequireNoSeqScan(t, nodes)
		}
	}
}
