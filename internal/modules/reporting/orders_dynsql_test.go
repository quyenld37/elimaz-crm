package reporting

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
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

// setRow là một dòng của order_set mà test đọc.
type setRow struct {
	ID      uuid.UUID
	EventAt time.Time
}

// readSet dựng order_set rồi đọc (order_id, event_at) bằng crm_app trong ngữ cảnh tenant. Lỗi dựng câu trả về trước khi
// gửi câu nào.
func readSet(ctx context.Context, d *db.DB, s DataScope, f OrderFilter, m Metric, cell *Cell) ([]setRow, error) {
	b := &sqlBuilder{}
	cte, err := orderSet(s, f, m, cell, b)
	if err != nil {
		return nil, err
	}
	var out []setRow
	err = tenancy.WithTenant(ctx, d, s.TenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "WITH "+cte+" SELECT order_id, event_at FROM order_set ORDER BY order_id", b.args...)
		if err != nil {
			return fmt.Errorf("đọc tập đơn: %w", err)
		}
		if out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[setRow]); err != nil {
			return fmt.Errorf("đọc dòng tập đơn: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("tập đơn: %w", err)
	}
	return out, nil
}

func ids(rows []setRow) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

func sorted(in []uuid.UUID) []uuid.UUID {
	out := slices.Clone(in)
	slices.SortFunc(out, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	return out
}

func mustPeriod(t *testing.T, from, to string) Period {
	t.Helper()
	p, err := ParsePeriod(from, to, 366)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type setEnv struct {
	st     *testkit.Staff
	tenant uuid.UUID
	seed   *reportseed.Seed
	all    DataScope
}

func newSetEnv(t *testing.T) setEnv {
	t.Helper()
	st := testkit.OpenStaff(t)
	tenant := st.Tenant(t, "a")
	seed := reportseed.New(t, st.Owner, tenant)
	return setEnv{st: st, tenant: tenant, seed: seed, all: DataScope{TenantID: tenant, SellerID: seed.Own(), AllPages: true}}
}

func (e setEnv) want(t *testing.T, name string, s DataScope, f OrderFilter, m Metric, cell *Cell, want ...uuid.UUID) {
	t.Helper()
	rows, err := readSet(t.Context(), e.st.App, s, f, m, cell)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	got := ids(rows)
	if len(got) != len(slices.Compact(sorted(got))) {
		t.Errorf("%s: một đơn xuất hiện hai lần: %v", name, got)
	}
	if !slices.Equal(sorted(got), sorted(want)) {
		t.Errorf("%s: tập = %v, mong %v", name, got, want)
	}
}

// Biên ngày giờ Việt Nam: lên đơn lúc 23:59:59 ngày 01/11 (16:59:59Z) thuộc ngày 01/11, lúc 00:00 ngày 02/11 (17:00Z)
// thuộc ngày 02/11.
func TestOrderSet_DayBoundary(t *testing.T) {
	t.Parallel()
	e := newSetEnv(t)
	v := e.seed.Variant(e.seed.Own(), "Áo A", "AOA-S")
	at := func(s string) time.Time { v, _ := time.Parse(time.RFC3339, s); return v }
	before := e.seed.Order(reportseed.OrderSpec{
		Channel: "manual", Items: []reportseed.Item{{Variant: v, Qty: 1, Price: 150_000}},
		History: []reportseed.Step{{To: "new", At: at("2026-11-01T16:59:59Z")}},
	})
	after := e.seed.Order(reportseed.OrderSpec{
		Channel: "manual", Items: []reportseed.Item{{Variant: v, Qty: 1, Price: 150_000}},
		History: []reportseed.Step{{To: "new", At: at("2026-11-01T17:00:00Z")}},
	})
	e.want(t, "ngày 02/11", e.all, OrderFilter{Period: mustPeriod(t, "2026-11-02", "2026-11-02"), Basis: BasisPlaced}, MetricPlaced, nil, after)
	e.want(t, "ngày 01/11", e.all, OrderFilter{Period: mustPeriod(t, "2026-11-01", "2026-11-01"), Basis: BasisPlaced}, MetricPlaced, nil, before)
	e.want(t, "ô ngày 02/11 trong kỳ hai ngày", e.all, OrderFilter{Period: mustPeriod(t, "2026-11-01", "2026-11-02"), Basis: BasisPlaced},
		MetricPlaced, &Cell{Dimension: DimDay, Key: "2026-11-02"}, after)
}

// Kịch bản chín đơn (kế hoạch M6, Task 227): tập theo từng Metric, theo phạm vi Page, theo ô; người bán thứ hai và
// tenant khác không lọt vào tập nào.
func TestOrderSet_NineOrders(t *testing.T) {
	t.Parallel()
	e := newSetEnv(t)
	sc := reportseed.NineOrders(e.seed, "2026-11-17")
	o := sc.IDs
	day := mustPeriod(t, "2026-11-17", "2026-11-17")
	placed := OrderFilter{Period: day, Basis: BasisPlaced}
	completed := OrderFilter{Period: day, Basis: BasisCompleted}

	partnerOrder, partner := e.outsiders(t, sc.P1)

	all := e.all
	e.want(t, "placed", all, placed, MetricPlaced, nil, o("O1", "O2", "O3", "O4", "O5", "O6", "O8", "O9")...)
	e.want(t, "placed_active", all, placed, MetricPlacedActive, nil, o("O1", "O2", "O3", "O4", "O5", "O8", "O9")...)
	e.want(t, "cancelled", all, placed, MetricCancelled, nil, o("O6")...)
	e.want(t, "delivered theo ngày lên", all, placed, MetricDelivered, nil, o("O1", "O9")...)
	e.want(t, "delivered theo ngày hoàn tất", all, completed, MetricDelivered, nil, o("O1", "O9")...)
	e.want(t, "returned theo ngày hoàn tất", all, completed, MetricReturned, nil, o("O2")...)
	e.want(t, "customer_returned", all, placed, MetricCustomerReturned, nil, o("O9")...)
	e.want(t, "pending_confirm", all, placed, MetricPendingConfirm, nil, o("O4")...)
	e.want(t, "pending_ship", all, placed, MetricPendingShip, nil, o("O5")...)
	e.want(t, "cancel_requested", all, placed, MetricCancelRequested, nil, o("O8")...)

	p1 := DataScope{TenantID: e.tenant, SellerID: all.SellerID, PageIDs: []uuid.UUID{sc.P1}}
	e.want(t, "P1 placed", p1, placed, MetricPlaced, nil, o("O1", "O2", "O8", "O9")...)
	e.want(t, "P1 pending_confirm", p1, placed, MetricPendingConfirm, nil)
	e.want(t, "P1 pending_ship", p1, placed, MetricPendingShip, nil)
	e.want(t, "P1 cancel_requested", p1, placed, MetricCancelRequested, nil, o("O8")...)
	e.want(t, "không phạm vi Page nào", DataScope{TenantID: e.tenant, SellerID: all.SellerID}, placed, MetricPlaced, nil)
	e.want(t, "người bán partner chỉ có đơn của mình", DataScope{TenantID: e.tenant, SellerID: partner, AllPages: true},
		placed, MetricPlaced, nil, partnerOrder)

	// Bộ lọc.
	e.want(t, "kênh facebook", all, OrderFilter{Period: day, Basis: BasisPlaced, Channels: []string{"facebook"}}, MetricPlaced, nil,
		o("O1", "O2", "O3", "O6", "O8", "O9")...)
	e.want(t, "nguồn Mes P2", all, OrderFilter{Period: day, Basis: BasisPlaced, SourceIDs: []uuid.UUID{sc.MesP2}}, MetricPlaced, nil, o("O3", "O6")...)
	e.want(t, "phụ trách Bình", all, OrderFilter{Period: day, Basis: BasisPlaced, CareBy: []uuid.UUID{sc.Binh}}, MetricPlaced, nil, o("O3", "O5", "O6")...)
	e.want(t, "người tạo An", all, OrderFilter{Period: day, Basis: BasisPlaced, CreatedBy: []uuid.UUID{sc.An}}, MetricPlaced, nil, o("O1", "O2", "O8", "O9")...)
	e.want(t, "lọc Page P2", all, OrderFilter{Period: day, Basis: BasisPlaced, PageIDs: []uuid.UUID{sc.P2}}, MetricPlaced, nil, o("O3", "O6")...)
	e.want(t, "lọc Page P2 trong phạm vi P1", p1, OrderFilter{Period: day, Basis: BasisPlaced, PageIDs: []uuid.UUID{sc.P2}}, MetricPlaced, nil)
	e.want(t, "hãng vtp", all, OrderFilter{Period: day, Basis: BasisPlaced, CarrierKeys: []string{"vtp"}}, MetricPlaced, nil, o("O1", "O2", "O9")...)

	// Ô (chiều, khóa); nhóm không xác định là "unknown".
	cells := []struct {
		name string
		cell Cell
		want []uuid.UUID
	}{
		{"Page không xác định", Cell{DimPage, UnknownKey}, o("O4", "O5")},
		{"Page P1", Cell{DimPage, sc.P1.String()}, o("O1", "O2", "O8", "O9")},
		{"kênh web", Cell{DimChannel, "web"}, o("O4")},
		{"nguồn không xác định", Cell{DimSource, UnknownKey}, o("O5")},
		{"nguồn Live trưa P1", Cell{DimSource, sc.LiveP1.String()}, o("O1", "O2", "O8", "O9")},
		{"phụ trách không xác định", Cell{DimCareBy, UnknownKey}, o("O4")},
		{"người tạo Bình", Cell{DimCreatedBy, sc.Binh.String()}, o("O3", "O5", "O6")},
		{"hãng vtp", Cell{DimCarrier, "vtp"}, o("O1", "O2", "O9")},
		{"hãng best (vận đơn hiện hành của O3)", Cell{DimCarrier, "best"}, o("O3")},
		{"chưa gửi hãng", Cell{DimCarrier, UnknownKey}, o("O4", "O5", "O6")},
		{"ngày", Cell{DimDay, "2026-11-17"}, o("O1", "O2", "O3", "O4", "O5", "O6", "O8", "O9")},
		{"ngày khác", Cell{DimDay, "2026-11-18"}, nil},
		{"trạng thái thô confirmed", Cell{DimStatus, "confirmed"}, o("O3", "O5")},
	}
	for _, c := range cells {
		e.want(t, "ô "+c.name, all, placed, MetricPlaced, &c.cell, c.want...)
	}
	e.want(t, "ô Page không xác định trong phạm vi P1", p1, placed, MetricPlaced, &Cell{DimPage, UnknownKey})
}

// Một đơn có ba dòng hàng, hai vận đơn và lịch sử quay confirmed → cancel_requested → confirmed xuất hiện đúng một
// lần, với mốc lên đơn là dòng draft → confirmed; lần quay lại confirmed (ngày hôm sau) không phải lần lên đơn.
func TestOrderSet_OneRowPerOrder(t *testing.T) {
	t.Parallel()
	e := newSetEnv(t)
	own := e.seed.Own()
	v1, v2, v3 := e.seed.Variant(own, "Áo A", "AOA-S"), e.seed.Variant(own, "Áo A", "AOA-M"), e.seed.Variant(own, "Quần B", "QB-1")
	t0 := time.Date(2026, 11, 17, 2, 0, 0, 0, time.UTC)
	id := e.seed.Order(reportseed.OrderSpec{
		Channel: "manual",
		Items:   []reportseed.Item{{Variant: v1, Qty: 1, Price: 150_000}, {Variant: v2, Qty: 2, Price: 150_000}, {Variant: v3, Qty: 1, Price: 250_000}},
		History: []reportseed.Step{
			{To: "draft", At: t0},
			{To: "confirmed", At: t0.Add(time.Minute)},
			{To: "cancel_requested", At: t0.Add(2 * time.Minute)},
			{To: "confirmed", At: t0.Add(24 * time.Hour)}, // quay lại ngày hôm sau: không phải lần lên đơn
		},
		Shipments: []reportseed.ShipmentSpec{
			{CarrierKey: "spx", Status: "cancelled", Fee: 22_000, CreatedAt: t0.Add(90 * time.Second)},
			{CarrierKey: "vtp", Status: "awaiting_pickup", Fee: 22_000, CreatedAt: t0.Add(4 * time.Minute)},
		},
	})
	rows, err := readSet(t.Context(), e.st.App, e.all, OrderFilter{Period: mustPeriod(t, "2026-11-17", "2026-11-17"), Basis: BasisPlaced}, MetricPlaced, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != id || !rows[0].EventAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("tập = %+v, mong một dòng %s lúc %s", rows, id, t0.Add(time.Minute))
	}
	e.want(t, "ngày quay lại confirmed", e.all, OrderFilter{Period: mustPeriod(t, "2026-11-18", "2026-11-18"), Basis: BasisPlaced}, MetricPlaced, nil)
	e.want(t, "ô hãng vtp", e.all, OrderFilter{Period: mustPeriod(t, "2026-11-17", "2026-11-18"), Basis: BasisPlaced}, MetricPlaced,
		&Cell{DimCarrier, "vtp"}, id)
}

// Giá trị lạ của chiều, Metric, Basis, khóa ô, bộ lọc → ErrBadParams, không câu SQL nào được gửi.
func TestOrderSet_BadParams(t *testing.T) {
	t.Parallel()
	if _, err := ParseDimension("status; DROP TABLE x"); !errors.Is(err, ErrBadParams) {
		t.Fatalf("ParseDimension: %v", err)
	}
	if _, err := ParseMetric("placed'--"); !errors.Is(err, ErrBadParams) {
		t.Fatalf("ParseMetric: %v", err)
	}
	if _, err := ParseBasis("x"); !errors.Is(err, ErrBadParams) {
		t.Fatalf("ParseBasis: %v", err)
	}
	if d, err := ParseDimension("care_by"); err != nil || d != DimCareBy {
		t.Fatalf("ParseDimension(care_by) = %q, %v", d, err)
	}
	e := newSetEnv(t)
	var queries atomic.Int64
	cfg := e.st.App.Pool.Config()
	cfg.ConnConfig.Tracer = countTracer{&queries}
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	traced := &db.DB{Pool: pool}
	day := mustPeriod(t, "2026-11-17", "2026-11-17")
	ok := OrderFilter{Period: day, Basis: BasisPlaced}
	bad := []struct {
		name string
		f    OrderFilter
		m    Metric
		cell *Cell
	}{
		{"chiều lạ", ok, MetricPlaced, &Cell{Dimension: "status; DROP TABLE x", Key: "a"}},
		{"Metric lạ", ok, "placed; DROP TABLE x", nil},
		{"Basis lạ", OrderFilter{Period: day, Basis: "x"}, MetricPlaced, nil},
		{"Basis rỗng", OrderFilter{Period: day}, MetricPlaced, nil},
		{"completed với placed", OrderFilter{Period: day, Basis: BasisCompleted}, MetricPlaced, nil},
		{"không có kỳ", OrderFilter{Basis: BasisPlaced}, MetricPlaced, nil},
		{"khóa Page không phải uuid", ok, MetricPlaced, &Cell{Dimension: DimPage, Key: "1' OR '1'='1"}},
		{"khóa hãng lạ", ok, MetricPlaced, &Cell{Dimension: DimCarrier, Key: "vtp' OR 1=1 --"}},
		{"khóa kênh lạ", ok, MetricPlaced, &Cell{Dimension: DimChannel, Key: "zalo"}},
		{"khóa ngày sai", ok, MetricPlaced, &Cell{Dimension: DimDay, Key: "2026-02-30"}},
		{"khóa trạng thái lạ", ok, MetricPlaced, &Cell{Dimension: DimStatus, Key: "x"}},
		{"khóa rỗng", ok, MetricPlaced, &Cell{Dimension: DimSource, Key: ""}},
		{"lọc kênh lạ", OrderFilter{Period: day, Basis: BasisPlaced, Channels: []string{"facebook", "x"}}, MetricPlaced, nil},
		{"lọc hãng lạ", OrderFilter{Period: day, Basis: BasisPlaced, CarrierKeys: []string{"VTP"}}, MetricPlaced, nil},
		{"thiếu người bán", ok, MetricPlaced, nil},
	}
	for _, c := range bad {
		s := e.all
		if c.name == "thiếu người bán" {
			s.SellerID = uuid.Nil
		}
		if _, err := readSet(t.Context(), traced, s, c.f, c.m, c.cell); !errors.Is(err, ErrBadParams) {
			t.Errorf("%s: err = %v, mong ErrBadParams", c.name, err)
		}
	}
	if n := queries.Load(); n != 0 {
		t.Fatalf("%d câu SQL đã gửi cho tham số sai", n)
	}
	if _, err := readSet(t.Context(), traced, e.all, ok, MetricPlaced, nil); err != nil {
		t.Fatal(err)
	}
	if queries.Load() == 0 {
		t.Fatal("bộ đếm câu SQL không đếm được câu hợp lệ")
	}
}

type countTracer struct{ n *atomic.Int64 }

func (c countTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}

func (countTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// EXPLAIN bằng crm_app của tập placed 31 ngày (phạm vi all và phạm vi Page) trên 100.000 đơn: không Seq Scan.
func TestOrderSet_ExplainNoSeqScan(t *testing.T) {
	t.Parallel()
	e := newSetEnv(t)
	e.bulk()
	pages := e.seed.Pages()
	f := OrderFilter{Period: mustPeriod(t, "2026-03-01", "2026-03-31"), Basis: BasisPlaced}
	for _, c := range []struct {
		name string
		s    DataScope
		m    Metric
	}{
		{"all placed", e.all, MetricPlaced},
		{"Page placed", DataScope{TenantID: e.tenant, SellerID: e.all.SellerID, PageIDs: pages[:1]}, MetricPlaced},
		{"all delivered", e.all, MetricDelivered},
	} {
		b := &sqlBuilder{}
		cte, err := orderSet(c.s, f, c.m, nil, b)
		if err != nil {
			t.Fatal(err)
		}
		nodes, err := testkit.ExplainAsApp(t.Context(), e.st.App, e.tenant, "WITH "+cte+" SELECT count(*), count(carrier_key), sum(goods_amount) FROM order_set", b.args...)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		testkit.RequireNoSeqScan(t, nodes)
	}
}
