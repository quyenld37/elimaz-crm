package reporting

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/queries/orderview"
	"elimaz.com.vn/crm/internal/testkit/reportseed"
)

func runDrill(t *testing.T, d *db.DB, s DataScope, f OrderFilter, m Metric, cell *Cell, cur *DrillCursor, limit int) (DrillPage, error) {
	t.Helper()
	var page DrillPage
	err := inReportTx(t, d, s, func(ctx context.Context, tx pgx.Tx) error {
		p, err := Drilldown(ctx, tx, s, f, m, cell, cur, limit)
		page = p
		return err
	})
	return page, err
}

// drillAll đọc mọi trang của danh sách chi tiết; kiểm Total giữ nguyên qua các trang và thứ tự mốc giảm dần.
func drillAll(t *testing.T, d *db.DB, s DataScope, f OrderFilter, m Metric, cell *Cell, limit int) (total int64, items []DrillItem) {
	t.Helper()
	var cur *DrillCursor
	for page := 0; ; page++ {
		p, err := runDrill(t, d, s, f, m, cell, cur, limit)
		if err != nil {
			t.Fatalf("danh sách chi tiết %s %+v trang %d: %v", m, cell, page, err)
		}
		if page == 0 {
			total = p.Total
		} else if p.Total != total {
			t.Fatalf("Total trang %d = %d, trang đầu %d", page, p.Total, total)
		}
		if len(p.Items) > limit {
			t.Fatalf("trang %d có %d dòng > limit %d", page, len(p.Items), limit)
		}
		items = append(items, p.Items...)
		if p.Next == nil {
			return total, items
		}
		if page > 1000 {
			t.Fatal("danh sách chi tiết không dừng")
		}
		next := *p.Next
		cur = &next
	}
}

func itemIDs(items []DrillItem) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(items))
	for _, it := range items {
		out = append(out, it.OrderID)
	}
	return out
}

// Danh sách chi tiết của kịch bản chín đơn (kế hoạch M6, Task 230 Step 1): ô An, cột đơn lên, hai dòng mỗi trang →
// hai trang, Total 4, không trùng, đúng tập, mốc giảm dần; trường của một dòng.
func TestDrilldown_NineOrders(t *testing.T) {
	t.Parallel()
	e, sc := newNineEnv(t)
	o := sc.IDs
	f := OrderFilter{Period: mustPeriod(t, nineDay, nineDay), Basis: BasisPlaced}
	an := &Cell{Dimension: DimCareBy, Key: sc.An.String()}

	p1, err := runDrill(t, e.st.App, e.all, f, MetricPlaced, an, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if p1.Total != 4 || len(p1.Items) != 2 || p1.Next == nil || p1.Next.Total != 4 {
		t.Fatalf("trang 1: total %d, %d dòng, next %+v", p1.Total, len(p1.Items), p1.Next)
	}
	parsed, err := ParseDrillCursor(p1.Next.String())
	if err != nil || !parsed.At.Equal(p1.Next.At) || parsed.ID != p1.Next.ID || parsed.Total != 4 {
		t.Fatalf("con trỏ đi về: %+v, %v", parsed, err)
	}
	p2, err := runDrill(t, e.st.App, e.all, f, MetricPlaced, an, &parsed, 2)
	if err != nil {
		t.Fatal(err)
	}
	if p2.Total != 4 || len(p2.Items) != 2 || p2.Next != nil {
		t.Fatalf("trang 2: total %d, %d dòng, next %+v", p2.Total, len(p2.Items), p2.Next)
	}
	all := append(slices.Clone(p1.Items), p2.Items...)
	if got := sorted(itemIDs(all)); !slices.Equal(got, sorted(o("O1", "O2", "O8", "O9"))) {
		t.Fatalf("tập = %v", got)
	}
	for i := 1; i < len(all); i++ {
		if all[i].EventAt.After(all[i-1].EventAt) {
			t.Fatalf("thứ tự mốc không giảm: %v rồi %v", all[i-1].EventAt, all[i].EventAt)
		}
	}

	checkItemFields(t, e, f, sc, all)
}

// Trường của một dòng danh sách chi tiết: O9 (đủ trường, mốc lên đơn và giao xong), O4 (chưa hoàn tất, chưa gửi hãng,
// chưa phân công).
func checkItemFields(t *testing.T, e setEnv, f OrderFilter, sc reportseed.Scenario, all []DrillItem) {
	t.Helper()
	// Trường của O9: nhãn, kênh, nguồn, người phụ trách, hãng, tiền, mốc lên đơn và giao xong, mã khách.
	var o9 DrillItem
	for _, it := range all {
		if it.OrderID == sc.Orders["O9"] {
			o9 = it
		}
	}
	base := time.Date(2026, 11, 17, 1, 0, 0, 0, time.UTC) // 08:00 giờ VN
	placedAt, doneAt := base.Add(160*time.Minute), base.Add(164*time.Minute)
	if o9.Code == "" || o9.CustomerCode == nil || *o9.CustomerCode == "" || o9.StatusLabel != orderview.LabelDelivered ||
		o9.Channel != "facebook" || o9.SourceName == nil || *o9.SourceName != "Live trưa P1" || o9.CareByName == nil ||
		*o9.CareByName != "An" || o9.CarrierKey == nil || *o9.CarrierKey != "vtp" || o9.GoodsAmount != 450_000 ||
		o9.ShippingFeeCharged != 30_000 || o9.PlacedAt == nil || !o9.PlacedAt.Equal(placedAt) || o9.CompletedAt == nil ||
		!o9.CompletedAt.Equal(doneAt) || !o9.EventAt.Equal(placedAt) {
		t.Fatalf("dòng O9: %+v", o9)
	}
	// Đơn chưa hoàn tất, chưa gửi hãng, chưa phân công: không có completedAt, carrierKey, careByName.
	_, o4 := drillAll(t, e.st.App, e.all, f, MetricPendingConfirm, nil, 50)
	if len(o4) != 1 || o4[0].OrderID != sc.Orders["O4"] || o4[0].CompletedAt != nil || o4[0].CarrierKey != nil ||
		o4[0].CareByName != nil || o4[0].StatusLabel != orderview.LabelNew || o4[0].GoodsAmount != 360_000 {
		t.Fatalf("dòng O4: %+v", o4)
	}
}

// Danh sách chi tiết theo phạm vi P1, theo ô nhãn gộp; tham số lạ (ô nhãn lạ, con trỏ hỏng, limit ngoài miền, Page
// ngoài phạm vi).
func TestDrilldown_CellsScopeAndBadInput(t *testing.T) {
	t.Parallel()
	e, sc := newNineEnv(t)
	o := sc.IDs
	f := OrderFilter{Period: mustPeriod(t, nineDay, nineDay), Basis: BasisPlaced}
	p1Scope := DataScope{TenantID: e.tenant, SellerID: e.all.SellerID, PageIDs: []uuid.UUID{sc.P1}}
	if tot, items := drillAll(t, e.st.App, p1Scope, f, MetricPlaced, &Cell{Dimension: DimChannel, Key: "web"}, 50); tot != 0 || len(items) != 0 {
		t.Fatalf("P1, ô kênh web: total %d, %d dòng", tot, len(items))
	}
	if tot, items := drillAll(t, e.st.App, p1Scope, f, MetricPlaced, nil, 50); tot != 4 || len(items) != 4 {
		t.Fatalf("P1, cả bảng: total %d, %d dòng", tot, len(items))
	}

	for _, c := range []struct {
		key  orderview.LabelKey
		want []uuid.UUID
	}{
		{orderview.LabelShipping, o("O3")},
		{orderview.LabelConfirmed, o("O5")},
		{orderview.LabelCancelRequested, o("O8")},
		{orderview.LabelDelivered, o("O1", "O9")},
		{orderview.LabelPacked, nil},
	} {
		_, items := drillAll(t, e.st.App, e.all, f, MetricPlaced, &Cell{Dimension: DimStatus, Key: string(c.key)}, 50)
		if got := sorted(itemIDs(items)); !slices.Equal(got, sorted(c.want)) {
			t.Errorf("ô nhãn %s: %v, mong %v", c.key, got, c.want)
		}
	}

	if _, err := runDrill(t, e.st.App, e.all, f, MetricPlaced, &Cell{Dimension: DimStatus, Key: "confirmed'--"}, nil, 10); !errors.Is(err, ErrBadParams) {
		t.Errorf("ô nhãn lạ: %v", err)
	}
	for _, bad := range []string{"", "!!", "MTIz", "YWJj.ZGVm", DrillCursor{Total: -1}.String()} {
		if _, err := ParseDrillCursor(bad); !errors.Is(err, ErrBadParams) {
			t.Errorf("con trỏ %q: %v", bad, err)
		}
	}
	if _, err := runDrill(t, e.st.App, e.all, f, MetricPlaced, nil, nil, 0); !errors.Is(err, ErrBadParams) {
		t.Errorf("limit 0: %v", err)
	}
	if _, err := runDrill(t, e.st.App, e.all, f, MetricPlaced, nil, nil, MaxDrillLimit+1); !errors.Is(err, ErrBadParams) {
		t.Errorf("limit quá: %v", err)
	}
	bad := f
	bad.PageIDs = []uuid.UUID{sc.P2}
	if _, err := runDrill(t, e.st.App, p1Scope, bad, MetricPlaced, nil, nil, 10); !errors.Is(err, ErrPageForbidden) {
		t.Errorf("lọc Page ngoài phạm vi: %v", err)
	}
}

// Tham số của danh sách chi tiết: ô thiếu khóa hay khóa thiếu chiều, cột số lạ, con trỏ hỏng, limit ngoài 1..200 →
// ErrBadParams; giá trị đúng → đọc được bộ lọc, ô, con trỏ.
func TestDrilldownParams_Validate(t *testing.T) {
	t.Parallel()
	ok := DrilldownParams{SalesFilterParams: SalesFilterParams{PeriodParams: PeriodParams{From: "2026-11-01", To: "2026-11-02"}}, Metric: MetricPlaced}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	cur := DrillCursor{At: time.Date(2026, 11, 1, 3, 4, 5, 6000, time.UTC), ID: uuid.New(), Total: 9}
	withCell := ok
	withCell.CellDimension, withCell.CellKey, withCell.Cursor, withCell.Limit = DimChannel, "web", cur.String(), 20
	if err := withCell.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]DrilldownParams{
		"ô thiếu khóa":  func() DrilldownParams { p := ok; p.CellDimension = DimChannel; return p }(),
		"khóa thiếu ô":  func() DrilldownParams { p := ok; p.CellKey = "web"; return p }(),
		"cột số lạ":     func() DrilldownParams { p := ok; p.Metric = "x"; return p }(),
		"thiếu cột số":  func() DrilldownParams { p := ok; p.Metric = ""; return p }(),
		"con trỏ hỏng":  func() DrilldownParams { p := ok; p.Cursor = "%%"; return p }(),
		"limit âm":      func() DrilldownParams { p := ok; p.Limit = -1; return p }(),
		"limit quá 200": func() DrilldownParams { p := ok; p.Limit = 201; return p }(),
		"chiều lạ":      func() DrilldownParams { p := ok; p.CellDimension, p.CellKey = "x", "y"; return p }(),
	} {
		if err := p.Validate(); !errors.Is(err, ErrBadParams) {
			t.Errorf("%s: %v, mong ErrBadParams", name, err)
		}
	}
}
