package reporting

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/queries"
	"elimaz.com.vn/crm/internal/queries/orderview"
	"elimaz.com.vn/crm/internal/testkit/reportseed"
)

// inReportTx chạy fn trong transaction chỉ đọc của báo cáo (TxReport), ngữ cảnh tenant của s.
func inReportTx(t *testing.T, d *db.DB, s DataScope, fn func(ctx context.Context, tx pgx.Tx) error) error {
	t.Helper()
	ctx := t.Context()
	return tenancy.WithTenant(ctx, d, s.TenantID, func(tx pgx.Tx) error { //nolint:wrapcheck // lỗi của test
		return fn(ctx, tx)
	}, TxReport(30*time.Second, time.Second))
}

func runOrders(t *testing.T, d *db.DB, s DataScope, p OrdersParams) (*OrdersReport, error) {
	t.Helper()
	var res *OrdersReport
	err := inReportTx(t, d, s, func(ctx context.Context, tx pgx.Tx) error {
		r, err := ordersReport(ctx, tx, s, &p)
		res = r
		return err
	})
	return res, err
}

// nineDay là ngày của kịch bản chín đơn trong test báo cáo đơn.
const nineDay = "2026-11-17"

// newNineEnv dựng tenant có kịch bản chín đơn ngày nineDay.
func newNineEnv(t *testing.T) (setEnv, reportseed.Scenario) {
	t.Helper()
	e := newSetEnv(t)
	return e, reportseed.NineOrders(e.seed, nineDay)
}

func mustOrders(t *testing.T, d *db.DB, s DataScope, p OrdersParams) *OrdersReport {
	t.Helper()
	r, err := runOrders(t, d, s, p)
	if err != nil {
		t.Fatalf("báo cáo đơn %+v: %v", p, err)
	}
	return r
}

func dayParams(day string, basis Basis, dim Dimension) OrdersParams {
	return OrdersParams{
		SalesFilterParams: SalesFilterParams{PeriodParams: PeriodParams{From: day, To: day}, Basis: basis}, GroupBy: dim,
	}
}

func rowByKey(t *testing.T, r *OrdersReport, key string) OrdersRow {
	t.Helper()
	for _, row := range r.Rows {
		if row.Key == key {
			return row
		}
	}
	t.Fatalf("không có dòng %q trong %+v", key, r.Rows)
	return OrdersRow{}
}

func i64(v int64) *int64 { return &v }
func i32(v int32) *int32 { return &v }

// wantRow so mọi cột của một dòng; con trỏ nil là "null".
func wantRow(t *testing.T, name string, got, want OrdersRow) {
	t.Helper()
	eq64 := func(a, b *int64) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
	eq32 := func(a, b *int32) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
	if got.Key != want.Key || got.Label != want.Label || got.Orders != want.Orders || !eq64(got.Placed, want.Placed) ||
		!eq64(got.Active, want.Active) || !eq64(got.Cancelled, want.Cancelled) || got.GoodsAmount != want.GoodsAmount ||
		got.ShippingFeeCharged != want.ShippingFeeCharged || !eq64(got.AvgGoodsAmount, want.AvgGoodsAmount) ||
		got.Delivered != want.Delivered || got.Returned != want.Returned || !eq32(got.SuccessRateBp, want.SuccessRateBp) {
		t.Errorf("%s:\n got %s\nwant %s", name, fmtRow(got), fmtRow(want))
	}
}

func fmtRow(r OrdersRow) string {
	p := func(v *int64) string {
		if v == nil {
			return "nil"
		}
		return fmt.Sprint(*v)
	}
	rate := "nil"
	if r.SuccessRateBp != nil {
		rate = fmt.Sprint(*r.SuccessRateBp)
	}
	return fmt.Sprintf("{%s %q orders=%d placed=%s active=%s cancelled=%s goods=%d ship=%d avg=%s delivered=%d returned=%d rate=%s}",
		r.Key, r.Label, r.Orders, p(r.Placed), p(r.Active), p(r.Cancelled), r.GoodsAmount, r.ShippingFeeCharged,
		p(r.AvgGoodsAmount), r.Delivered, r.Returned, rate)
}

// Kịch bản chín đơn, cơ sở ngày lên đơn (kế hoạch M6, Task 230 Step 1): nhóm theo nhân viên phụ trách, nguồn, hãng,
// nhãn trạng thái, kênh, Page, ngày; dòng tổng; nhãn tiếng Việt của nhóm không xác định.
func TestOrdersReport_NineOrdersPlaced(t *testing.T) {
	t.Parallel()
	e, sc := newNineEnv(t)

	r := mustOrders(t, e.st.App, e.all, dayParams(nineDay, BasisPlaced, DimCareBy))
	if r.Status != "ready" || r.Basis != BasisPlaced || r.GroupBy != DimCareBy || r.Period.FromDay != nineDay || r.Period.ToDay != nineDay {
		t.Fatalf("đầu báo cáo: %+v", r)
	}
	wantRow(t, "An", rowByKey(t, r, sc.An.String()), OrdersRow{
		Key: sc.An.String(), Label: "An", Orders: 4, Placed: i64(4), Active: i64(4), Cancelled: i64(0),
		GoodsAmount: 1_150_000, ShippingFeeCharged: 120_000, AvgGoodsAmount: i64(287_500), Delivered: 2, Returned: 1,
		SuccessRateBp: i32(6666),
	})
	wantRow(t, "Bình", rowByKey(t, r, sc.Binh.String()), OrdersRow{
		Key: sc.Binh.String(), Label: "Bình", Orders: 3, Placed: i64(3), Active: i64(2), Cancelled: i64(1),
		GoodsAmount: 400_000, AvgGoodsAmount: i64(200_000),
	})
	wantRow(t, "chưa phân công", rowByKey(t, r, UnknownKey), OrdersRow{
		Key: UnknownKey, Label: "Chưa phân công", Orders: 1, Placed: i64(1), Active: i64(1), Cancelled: i64(0),
		GoodsAmount: 360_000, ShippingFeeCharged: 30_000, AvgGoodsAmount: i64(360_000),
	})
	if len(r.Rows) != 3 {
		t.Errorf("số dòng nhân viên = %d, mong 3", len(r.Rows))
	}
	wantRow(t, "tổng", r.Totals, OrdersRow{
		Key: TotalKey, Label: "Tổng", Orders: 8, Placed: i64(8), Active: i64(7), Cancelled: i64(1),
		GoodsAmount: 1_910_000, ShippingFeeCharged: 150_000, AvgGoodsAmount: i64(272_857), Delivered: 2, Returned: 1,
		SuccessRateBp: i32(6666),
	})

	orders := func(dim Dimension) map[string]int64 {
		out := map[string]int64{}
		for _, row := range mustOrders(t, e.st.App, e.all, dayParams(nineDay, BasisPlaced, dim)).Rows {
			out[row.Key] = row.Orders
		}
		return out
	}
	expectCounts(t, "nguồn", orders(DimSource), map[string]int64{
		sc.LiveP1.String(): 4, sc.MesP2.String(): 2, sc.Website.String(): 1, UnknownKey: 1,
	})
	expectCounts(t, "hãng", orders(DimCarrier), map[string]int64{"vtp": 3, "best": 1, "spx": 1, UnknownKey: 3})
	expectCounts(t, "kênh", orders(DimChannel), map[string]int64{"facebook": 6, "web": 1, "manual": 1})
	expectCounts(t, "Page", orders(DimPage), map[string]int64{sc.P1.String(): 4, sc.P2.String(): 2, UnknownKey: 2})
	expectCounts(t, "người tạo", orders(DimCreatedBy), map[string]int64{sc.An.String(): 4, sc.Binh.String(): 3, UnknownKey: 1})
	expectCounts(t, "ngày", orders(DimDay), map[string]int64{nineDay: 8})
	expectCounts(t, "nhãn", orders(DimStatus), map[string]int64{
		"new": 1, "confirmed": 1, "shipping": 1, "cancel_requested": 1, "cancelled": 1, "delivered": 2, "returned": 1,
	})

	// Nhãn: hãng, kênh, ngày, nguồn, Page, trạng thái; dòng trạng thái theo thứ tự thanh nhãn.
	labels := func(dim Dimension) map[string]string {
		out := map[string]string{}
		for _, row := range mustOrders(t, e.st.App, e.all, dayParams(nineDay, BasisPlaced, dim)).Rows {
			out[row.Key] = row.Label
		}
		return out
	}
	expectLabels(t, labels(DimCarrier), map[string]string{"vtp": "Viettel Post", "best": "BEST Express", "spx": "SPX Express", UnknownKey: "Chưa gửi hãng"})
	expectLabels(t, labels(DimChannel), map[string]string{"facebook": "Facebook", "web": "Website", "manual": "Nhập tay"})
	expectLabels(t, labels(DimDay), map[string]string{nineDay: "17/11/2026"})
	expectLabels(t, labels(DimSource), map[string]string{sc.LiveP1.String(): "Live trưa P1", UnknownKey: "Không xác định"})
	expectLabels(t, labels(DimPage), map[string]string{sc.P1.String(): "P1", UnknownKey: "Không xác định"})
	expectLabels(t, labels(DimStatus), map[string]string{"cancel_requested": "Đang yêu cầu hủy", "shipping": "Đang chuyển", "delivered": "Thành công"})
	st := mustOrders(t, e.st.App, e.all, dayParams(nineDay, BasisPlaced, DimStatus))
	var keys []string
	for _, row := range st.Rows {
		keys = append(keys, row.Key)
	}
	if want := []string{"new", "confirmed", "shipping", "cancel_requested", "cancelled", "delivered", "returned"}; !slices.Equal(keys, want) {
		t.Errorf("thứ tự dòng nhãn = %v, mong %v", keys, want)
	}
	// Nhãn của O8 bằng đúng orderview.Label của bộ trục của nó.
	if l := orderview.Label(orderview.Axes{Status: "cancel_requested", PackingStatus: "none", ShipmentStatus: "cancel_requested"}); rowByKey(t, st, string(l)).Orders != 1 {
		t.Errorf("dòng %s không có O8", l)
	}

	// Mặc định: cơ sở placed, nhóm theo trạng thái.
	def := mustOrders(t, e.st.App, e.all, OrdersParams{SalesFilterParams: SalesFilterParams{PeriodParams: PeriodParams{From: nineDay, To: nineDay}}})
	if def.Basis != BasisPlaced || def.GroupBy != DimStatus || def.Totals.Orders != 8 {
		t.Errorf("mặc định: basis %s, groupBy %s, tổng %d", def.Basis, def.GroupBy, def.Totals.Orders)
	}
}

func expectCounts(t *testing.T, name string, got, want map[string]int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: %v, mong %v", name, got, want)
		return
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %v, mong %v", name, got, want)
			return
		}
	}
}

func expectLabels(t *testing.T, got, want map[string]string) {
	t.Helper()
	for k, v := range want {
		if got[k] != v {
			t.Errorf("nhãn %q = %q, mong %q (tất cả: %v)", k, got[k], v, got)
		}
	}
}

// Cơ sở ngày hoàn tất: O1, O9 giao xong, O2 hoàn; doanh số của đơn giao xong; ba cột lên đơn là null. Phạm vi P1, lọc
// Page ngoài phạm vi (403), tham số lạ.
func TestOrdersReport_NineOrdersCompletedAndScope(t *testing.T) {
	t.Parallel()
	e, sc := newNineEnv(t)

	r := mustOrders(t, e.st.App, e.all, dayParams(nineDay, BasisCompleted, DimCareBy))
	wantRow(t, "tổng theo ngày hoàn tất", r.Totals, OrdersRow{
		Key: TotalKey, Label: "Tổng", Orders: 3, GoodsAmount: 750_000, ShippingFeeCharged: 60_000,
		AvgGoodsAmount: i64(375_000), Delivered: 2, Returned: 1, SuccessRateBp: i32(6666),
	})
	wantRow(t, "An theo ngày hoàn tất", rowByKey(t, r, sc.An.String()), OrdersRow{
		Key: sc.An.String(), Label: "An", Orders: 3, GoodsAmount: 750_000, ShippingFeeCharged: 60_000,
		AvgGoodsAmount: i64(375_000), Delivered: 2, Returned: 1, SuccessRateBp: i32(6666),
	})
	if len(r.Rows) != 1 {
		t.Errorf("ngày hoàn tất: %d dòng, mong 1 (An)", len(r.Rows))
	}

	p1 := DataScope{TenantID: e.tenant, SellerID: e.all.SellerID, PageIDs: []uuid.UUID{sc.P1}}
	rp := mustOrders(t, e.st.App, p1, dayParams(nineDay, BasisPlaced, DimChannel))
	if rp.Totals.Orders != 4 || rp.Totals.GoodsAmount != 1_150_000 || len(rp.Rows) != 1 || rp.Rows[0].Key != "facebook" {
		t.Errorf("phạm vi P1: %+v", rp)
	}

	bad := dayParams(nineDay, BasisPlaced, DimStatus)
	bad.PageIDs = []uuid.UUID{sc.P2}
	if _, err := runOrders(t, e.st.App, p1, bad); !errors.Is(err, ErrPageForbidden) {
		t.Errorf("lọc Page P2 trong phạm vi P1: %v, mong ErrPageForbidden", err)
	}
	inScope := dayParams(nineDay, BasisPlaced, DimStatus)
	inScope.PageIDs = []uuid.UUID{sc.P1}
	if r, err := runOrders(t, e.st.App, p1, inScope); err != nil || r.Totals.Orders != 4 {
		t.Errorf("lọc Page P1 trong phạm vi P1: %v, %+v", err, r)
	}
	unknownCarrier := dayParams(nineDay, BasisPlaced, DimStatus)
	unknownCarrier.CarrierKeys = []string{UnknownKey, "best"}
	if r := mustOrders(t, e.st.App, e.all, unknownCarrier); r.Totals.Orders != 4 { // O4, O5, O6 chưa gửi hãng + O3 best
		t.Errorf("lọc hãng unknown + best: %d, mong 4", r.Totals.Orders)
	}

	for _, p := range []OrdersParams{
		{SalesFilterParams: SalesFilterParams{PeriodParams: PeriodParams{From: nineDay, To: nineDay}}, GroupBy: "foo"},
		{SalesFilterParams: SalesFilterParams{PeriodParams: PeriodParams{From: nineDay, To: nineDay}, Basis: "x"}},
		{SalesFilterParams: SalesFilterParams{PeriodParams: PeriodParams{From: nineDay, To: nineDay}, Channels: []string{"zalo"}}},
		{SalesFilterParams: SalesFilterParams{PeriodParams: PeriodParams{From: nineDay, To: nineDay}, CarrierKeys: []string{"VTP"}}},
	} {
		if err := p.Validate(); !errors.Is(err, ErrBadParams) {
			t.Errorf("Validate(%+v) = %v, mong ErrBadParams", p, err)
		}
	}
	if err := (OrdersParams{SalesFilterParams: SalesFilterParams{PeriodParams: PeriodParams{From: "2026-13-01", To: nineDay}}}).Validate(); !errors.Is(err, ErrBadDate) {
		t.Errorf("ngày sai: %v", err)
	}

	// Ngày không có đơn: mọi số 0, trung bình và tỷ lệ null, không dòng.
	empty := mustOrders(t, e.st.App, e.all, dayParams("2026-11-10", BasisPlaced, DimStatus))
	wantRow(t, "ngày trống", empty.Totals, OrdersRow{Key: TotalKey, Label: "Tổng", Placed: i64(0), Active: i64(0), Cancelled: i64(0)})
	if len(empty.Rows) != 0 {
		t.Errorf("ngày trống có dòng: %+v", empty.Rows)
	}
}

// Chiều ngược của nhãn: trên đủ miền bốn trục (7 trạng thái × 4 lý do chờ × 3 trạng thái đóng gói × 10 trạng thái vận
// đơn kể cả chưa có), điều kiện SQL queries.OrderLabelCond của mỗi nhãn chọn đúng các bộ mà orderview.Label cho ra nhãn
// đó; mỗi bộ thuộc đúng một nhãn (hợp các tập phủ đủ miền).
func TestStatusLabelCond_Domain(t *testing.T) {
	t.Parallel()
	e := newSetEnv(t)
	st, pr, pk, ss, axes := labelDomain()
	if len(axes) != 840 {
		t.Fatalf("miền có %d bộ", len(axes))
	}
	seen := make([]int, len(axes))
	for _, l := range orderview.Labels() {
		cond, err := queries.OrderLabelCond(l)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := e.st.App.Pool.Query(t.Context(), `SELECT t.i FROM unnest($1::text[], $2::text[], $3::text[], $4::text[])
				WITH ORDINALITY AS t(st, pr, pk, ss, i)
			CROSS JOIN LATERAL (SELECT t.st AS status, t.pr AS pending_reason, t.pk AS packing_status) AS o
			CROSS JOIN LATERAL (SELECT nullif(t.ss, '') AS status) AS s
			WHERE `+cond, st, pr, pk, ss)
		if err != nil {
			t.Fatal(err)
		}
		got, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			t.Fatal(err)
		}
		for _, i := range got {
			seen[i-1]++
			if a := axes[i-1]; orderview.Label(a) != l {
				t.Errorf("điều kiện của %s chọn %+v, Label = %s", l, a, orderview.Label(a))
			}
		}
	}
	for i, n := range seen {
		if n != 1 {
			t.Errorf("bộ %+v thuộc %d nhãn, mong 1", axes[i], n)
		}
	}
}

// labelDomain là đủ miền bốn trục của nhãn gộp (cột theo CHECK của ord.orders và fulfillment.shipments; vận đơn "" là
// chưa có vận đơn hiện hành), dạng bốn mảng song song và bộ trục.
func labelDomain() (st, pr, pk, ss []string, axes []orderview.Axes) {
	for _, s := range []string{"draft", "new", "confirmed", "cancel_requested", "cancelled", "delivered", "returned"} {
		for _, p := range []string{"", "awaiting_customer", "out_of_stock", "discount_approval"} {
			for _, k := range []string{"none", "picking", "packed"} {
				for _, h := range []string{
					"", "creating", "awaiting_pickup", "shipping", "cancel_requested", "returning",
					"delivery_failed", "delivered", "returned", "cancelled",
				} {
					st, pr, pk, ss = append(st, s), append(pr, p), append(pk, k), append(ss, h)
					axes = append(axes, orderview.Axes{Status: s, PendingReason: p, PackingStatus: k, ShipmentStatus: h})
				}
			}
		}
	}
	return st, pr, pk, ss, axes
}
