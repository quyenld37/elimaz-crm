package reporting_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/reportseed"
)

// carriersEnv là một tenant có dữ liệu dựng bằng reportseed và bộ chạy tại chỗ trên crm_app.
type carriersEnv struct {
	staff  *testkit.Staff
	tenant uuid.UUID
	seed   *reportseed.Seed
	run    *reporting.Interactive
}

func newCarriersEnv(t *testing.T) carriersEnv {
	t.Helper()
	st := testkit.OpenStaff(t)
	tenant := st.Tenant(t, "a")
	seed := reportseed.New(t, st.Owner, tenant)
	seed.Own()
	return carriersEnv{staff: st, tenant: tenant, seed: seed, run: reporting.NewInteractive(st.App, reporting.InteractiveOptions{})}
}

// salesUser là người xem báo cáo bán hàng: không có trang nào → phạm vi all; có trang → chỉ các Page đó.
func (e carriersEnv) salesUser(pages ...uuid.UUID) identity.AuthUser {
	u := identity.AuthUser{TenantID: e.tenant, UserID: uuid.New()}
	if len(pages) == 0 {
		u.Grants = []identity.Grant{{Permission: "report.sales.read", Scope: identity.Scope{Type: identity.ScopeAll}}}
	}
	for _, p := range pages {
		u.Grants = append(u.Grants, identity.Grant{Permission: "report.sales.read", Scope: identity.Scope{Type: identity.ScopePage, ID: p.String()}})
	}
	return u
}

func (e carriersEnv) report(t *testing.T, u identity.AuthUser, from, to string) reporting.CarriersReport {
	t.Helper()
	res, err := e.run.Run(t.Context(), u, reporting.KeyCarriers, &reporting.CarriersParams{PeriodParams: reporting.PeriodParams{From: from, To: to}})
	if err != nil {
		t.Fatalf("báo cáo hãng %s – %s: %v", from, to, err)
	}
	r, ok := res.(reporting.CarriersReport)
	if !ok {
		t.Fatalf("kết quả kiểu %T, mong reporting.CarriersReport", res)
	}
	if r.Status != "ready" || r.Period.FromDay != from || r.Period.ToDay != to {
		t.Errorf("status %q, kỳ %s – %s", r.Status, r.Period.FromDay, r.Period.ToDay)
	}
	checkCarrierSums(t, r)
	return r
}

// checkCarrierSums: mỗi dòng Created = tổng năm nhóm chưa hủy; dòng tổng = tổng các dòng, tỷ lệ và trung bình tính
// lại từ tổng.
func checkCarrierSums(t *testing.T, r reporting.CarriersReport) {
	t.Helper()
	var sum reporting.CarrierRow
	for _, row := range r.Rows {
		if row.Created != row.InProgress+row.Returning+row.DeliveryFailed+row.Delivered+row.Returned {
			t.Errorf("%s: Created %d khác tổng các trạng thái chưa hủy", row.CarrierKey, row.Created)
		}
		sum.Created += row.Created
		sum.Cancelled += row.Cancelled
		sum.InProgress += row.InProgress
		sum.Returning += row.Returning
		sum.DeliveryFailed += row.DeliveryFailed
		sum.Delivered += row.Delivered
		sum.Returned += row.Returned
		sum.FeeTotal += row.FeeTotal
		sum.CODTotal += row.CODTotal
	}
	got := r.Totals
	if got.CarrierKey != "" || got.CarrierAccountID != uuid.Nil || got.AccountName != "" {
		t.Errorf("dòng tổng có khóa hãng hay tài khoản: %+v", got)
	}
	if got.Created != sum.Created || got.Cancelled != sum.Cancelled || got.InProgress != sum.InProgress ||
		got.Returning != sum.Returning || got.DeliveryFailed != sum.DeliveryFailed || got.Delivered != sum.Delivered ||
		got.Returned != sum.Returned || got.FeeTotal != sum.FeeTotal || got.CODTotal != sum.CODTotal {
		t.Errorf("dòng tổng %+v khác tổng các dòng %+v", got, sum)
	}
}

func rowOf(t *testing.T, r reporting.CarriersReport, key string) reporting.CarrierRow {
	t.Helper()
	for _, row := range r.Rows {
		if row.CarrierKey == key {
			return row
		}
	}
	t.Fatalf("không có dòng hãng %q trong %+v", key, r.Rows)
	return reporting.CarrierRow{}
}

func p32(v int32) *int32 { return &v }
func p64(v int64) *int64 { return &v }

// want là phần số của một dòng (bỏ khóa, tài khoản).
type want struct {
	Created, Cancelled, InProgress, Returning, DeliveryFailed, Delivered, Returned int64
	Rate                                                                           *int32
	FeeTotal                                                                       int64
	FeeAvg                                                                         *int64
	COD                                                                            int64
}

func expectRow(t *testing.T, name string, got reporting.CarrierRow, w want) {
	t.Helper()
	g := want{
		got.Created, got.Cancelled, got.InProgress, got.Returning, got.DeliveryFailed, got.Delivered, got.Returned,
		got.SuccessRateBp, got.FeeTotal, got.FeeAvg, got.CODTotal,
	}
	eq := func(a, b *int64) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
	eq32 := func(a, b *int32) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
	if g.Created != w.Created || g.Cancelled != w.Cancelled || g.InProgress != w.InProgress || g.Returning != w.Returning ||
		g.DeliveryFailed != w.DeliveryFailed || g.Delivered != w.Delivered || g.Returned != w.Returned ||
		!eq32(g.Rate, w.Rate) || g.FeeTotal != w.FeeTotal || !eq(g.FeeAvg, w.FeeAvg) || g.COD != w.COD {
		t.Errorf("%s: được %s, mong %s", name, fmtWant(g), fmtWant(w))
	}
}

func fmtWant(w want) string {
	ptr := func(p any) string {
		switch v := p.(type) {
		case *int64:
			if v != nil {
				return fmt.Sprint(*v)
			}
		case *int32:
			if v != nil {
				return fmt.Sprint(*v)
			}
		}
		return "nil"
	}
	return fmt.Sprintf("{Created %d Cancelled %d InProgress %d Returning %d DeliveryFailed %d Delivered %d Returned %d Rate %s Fee %d FeeAvg %s COD %d}",
		w.Created, w.Cancelled, w.InProgress, w.Returning, w.DeliveryFailed, w.Delivered, w.Returned, ptr(w.Rate), w.FeeTotal, ptr(w.FeeAvg), w.COD)
}

// Kịch bản chín đơn (Task 227) ngày 02/11, phí giả 22.000 mỗi vận đơn, COD = grand_total: số từng hãng, dòng tổng; thêm
// một đơn tự giao phí 0 thành dòng self. Tỷ lệ thành công làm tròn xuống (hợp đồng ReportRateBp; QĐ-231-1): 2/3 → 6666.
func TestCarriersNineOrders(t *testing.T) {
	t.Parallel()
	e := newCarriersEnv(t)
	sc := reportseed.NineOrders(e.seed, "2026-11-02")
	all := e.salesUser()
	r := e.report(t, all, "2026-11-02", "2026-11-02")
	if len(r.Rows) != 3 {
		t.Fatalf("số dòng %d, mong 3 (best, spx, vtp): %+v", len(r.Rows), r.Rows)
	}
	for i, k := range []string{"best", "spx", "vtp"} {
		if r.Rows[i].CarrierKey != k || r.Rows[i].AccountName != k || r.Rows[i].CarrierAccountID == uuid.Nil {
			t.Errorf("dòng %d: %q tài khoản %q (%s), mong %q", i, r.Rows[i].CarrierKey, r.Rows[i].AccountName, r.Rows[i].CarrierAccountID, k)
		}
	}
	expectRow(t, "vtp", rowOf(t, r, "vtp"), want{Created: 3, Delivered: 2, Returned: 1, Rate: p32(6666), FeeTotal: 66_000, FeeAvg: p64(22_000), COD: 1_090_000})
	expectRow(t, "spx", rowOf(t, r, "spx"), want{Created: 1, Cancelled: 1, InProgress: 1, FeeTotal: 22_000, FeeAvg: p64(22_000), COD: 180_000})
	expectRow(t, "best", rowOf(t, r, "best"), want{Created: 1, InProgress: 1, FeeTotal: 22_000, FeeAvg: p64(22_000), COD: 150_000})
	expectRow(t, "tổng", r.Totals, want{
		Created: 5, Cancelled: 1, InProgress: 2, Delivered: 2, Returned: 1, Rate: p32(6666),
		FeeTotal: 110_000, FeeAvg: p64(22_000), COD: 1_420_000,
	})

	// Người chỉ có Page P2: chỉ vận đơn của O3 (SPX hủy, Best đang đi).
	p2 := e.report(t, e.salesUser(sc.P2), "2026-11-02", "2026-11-02")
	if len(p2.Rows) != 2 {
		t.Fatalf("P2: số dòng %d, mong 2: %+v", len(p2.Rows), p2.Rows)
	}
	expectRow(t, "P2 spx", rowOf(t, p2, "spx"), want{Cancelled: 1})
	expectRow(t, "P2 best", rowOf(t, p2, "best"), want{Created: 1, InProgress: 1, FeeTotal: 22_000, FeeAvg: p64(22_000), COD: 150_000})
	// Không có grant báo cáo bán hàng nào → không thấy gì.
	none := e.report(t, identity.AuthUser{TenantID: e.tenant}, "2026-11-02", "2026-11-02")
	if len(none.Rows) != 0 || none.Totals.Created != 0 || none.Totals.FeeAvg != nil || none.Totals.SuccessRateBp != nil {
		t.Errorf("không phạm vi: %+v", none)
	}

	// Đơn tự giao, giao xong, phí 0.
	v := e.seed.Variant(e.seed.Own(), "Áo tự giao", "AOTG")
	at := time.Date(2026, 11, 2, 5, 0, 0, 0, time.UTC) // 12:00 giờ VN
	e.seed.Order(reportseed.OrderSpec{
		Channel: "manual", Items: []reportseed.Item{{Variant: v, Qty: 1, Price: 100_000}},
		History:   []reportseed.Step{{To: "new", At: at}, {To: "confirmed", At: at.Add(time.Minute)}, {To: "delivered", At: at.Add(time.Hour)}},
		Shipments: []reportseed.ShipmentSpec{{CarrierKey: "self", Status: "delivered", Fee: 0, COD: 100_000, CreatedAt: at.Add(2 * time.Minute)}},
	})
	r = e.report(t, all, "2026-11-02", "2026-11-02")
	expectRow(t, "self", rowOf(t, r, "self"), want{Created: 1, Delivered: 1, Rate: p32(10_000), FeeTotal: 0, FeeAvg: p64(0), COD: 100_000})
	if r.Totals.Created != 6 || r.Totals.Cancelled != 1 {
		t.Errorf("tổng sau tự giao: Created %d Cancelled %d, mong 6, 1", r.Totals.Created, r.Totals.Cancelled)
	}
	// Đơn tự giao không gắn Page: người chỉ có P2 không thấy.
	if p2 = e.report(t, e.salesUser(sc.P2), "2026-11-02", "2026-11-02"); len(p2.Rows) != 2 {
		t.Errorf("P2 thấy đơn không gắn Page: %+v", p2.Rows)
	}
}

// Kỳ theo ngày tạo vận đơn giờ Việt Nam: tạo lúc 23:59:59 ngày 01/11 không nằm trong kỳ 02/11, lúc 00:00:00 ngày 02/11
// thì nằm; vận đơn của người bán khác không bao giờ vào báo cáo (ADR-29).
func TestCarriersDayBoundaryAndSeller(t *testing.T) {
	t.Parallel()
	e := newCarriersEnv(t)
	v := e.seed.Variant(e.seed.Own(), "Áo A", "AOA-S")
	ship := func(seller uuid.UUID, variant uuid.UUID, created time.Time, cod int64) {
		e.seed.Order(reportseed.OrderSpec{
			Seller: seller, Channel: "manual", Items: []reportseed.Item{{Variant: variant, Qty: 1, Price: cod}},
			History:   []reportseed.Step{{To: "new", At: created.Add(-time.Hour)}, {To: "confirmed", At: created.Add(-time.Minute)}},
			Shipments: []reportseed.ShipmentSpec{{CarrierKey: "vtp", Status: "awaiting_pickup", Fee: 22_000, COD: cod, CreatedAt: created}},
		})
	}
	ship(e.seed.Own(), v, time.Date(2026, 11, 1, 16, 59, 59, 0, time.UTC), 100_000) // 23:59:59 ngày 01/11 giờ VN
	ship(e.seed.Own(), v, time.Date(2026, 11, 1, 17, 0, 0, 0, time.UTC), 200_000)   // 00:00:00 ngày 02/11 giờ VN
	ship(e.seed.Own(), v, time.Date(2026, 11, 2, 16, 59, 59, 0, time.UTC), 400_000) // 23:59:59 ngày 02/11 giờ VN
	other := e.seed.Seller("DOITAC", "partner")
	ship(other, e.seed.Variant(other, "Áo đối tác", "DT-1"), time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC), 800_000)

	all := e.salesUser()
	r := e.report(t, all, "2026-11-02", "2026-11-02")
	if len(r.Rows) != 1 {
		t.Fatalf("số dòng %d, mong 1 (vtp của người bán own): %+v", len(r.Rows), r.Rows)
	}
	expectRow(t, "02/11", r.Totals, want{Created: 2, InProgress: 2, FeeTotal: 44_000, FeeAvg: p64(22_000), COD: 600_000})
	expectRow(t, "01/11", e.report(t, all, "2026-11-01", "2026-11-01").Totals,
		want{Created: 1, InProgress: 1, FeeTotal: 22_000, FeeAvg: p64(22_000), COD: 100_000})
	expectRow(t, "03/11", e.report(t, all, "2026-11-03", "2026-11-03").Totals, want{})
}

// Phí của vận đơn = coalesce(fee_actual, fee, 0); vận đơn hủy không cộng phí, COD; vận đơn creating vào InProgress; đủ
// chín trạng thái vào đúng cột; trung bình làm tròn nửa lên.
func TestCarriersFeesAndStatuses(t *testing.T) {
	t.Parallel()
	e := newCarriersEnv(t)
	v := e.seed.Variant(e.seed.Own(), "Áo A", "AOA-S")
	base := time.Date(2026, 11, 2, 2, 0, 0, 0, time.UTC)
	n := 0
	ship := func(key, status string, fee int64, feeActual *int64, cod int64) uuid.UUID {
		n++
		at := base.Add(time.Duration(n) * time.Minute)
		return e.seed.Order(reportseed.OrderSpec{
			Channel: "manual", Items: []reportseed.Item{{Variant: v, Qty: 1, Price: cod}},
			History:   []reportseed.Step{{To: "new", At: at.Add(-time.Hour)}, {To: "confirmed", At: at.Add(-time.Minute)}},
			Shipments: []reportseed.ShipmentSpec{{CarrierKey: key, Status: status, Fee: fee, FeeActual: feeActual, COD: cod, CreatedAt: at}},
		})
	}
	ship("ghn", "delivered", 22_000, p64(25_000), 100_000) // phí thật 25.000
	ship("ghn", "returned", 22_000, nil, 100_000)          // chỉ có phí báo: 22.000
	noFee := ship("ghn", "creating", 0, nil, 100_000)      // cả hai NULL → 0
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE fulfillment.shipments SET fee = NULL, fee_actual = NULL WHERE order_id = $1", noFee)
		return err //nolint:wrapcheck // test
	})
	ship("ghn", "cancelled", 22_000, p64(30_000), 999_000) // hủy: không cộng phí, COD
	r := e.report(t, e.salesUser(), "2026-11-02", "2026-11-02")
	// 47.000 / 3 = 15.666,67 → 15.667 (nửa lên).
	expectRow(t, "ghn", rowOf(t, r, "ghn"), want{
		Created: 3, Cancelled: 1, InProgress: 1, Delivered: 1, Returned: 1, Rate: p32(5000),
		FeeTotal: 47_000, FeeAvg: p64(15_667), COD: 300_000,
	})

	// Mỗi trạng thái còn lại một vận đơn (hãng spx): đúng cột.
	for _, st := range []string{"awaiting_pickup", "shipping", "cancel_requested", "returning", "delivery_failed"} {
		ship("spx", st, 1, nil, 10_000)
	}
	ship("spx", "delivered", 2, nil, 10_000)
	// Phí 1+1+1+1+1+2 = 7 trên 6 vận đơn → 1,17 → 1; tỷ lệ 1/1.
	r = e.report(t, e.salesUser(), "2026-11-02", "2026-11-02")
	expectRow(t, "spx", rowOf(t, r, "spx"), want{
		Created: 6, InProgress: 3, Returning: 1, DeliveryFailed: 1, Delivered: 1, Rate: p32(10_000),
		FeeTotal: 7, FeeAvg: p64(1), COD: 60_000,
	})
}
