package webcatalog_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/queries/webcatalog"
)

// variantsOf trả mã biến thể của mẫu product theo mã tăng dần.
func (e *env) variantsOf(product uuid.UUID) []uuid.UUID {
	e.t.Helper()
	var ids []uuid.UUID
	e.owner(func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), "SELECT id FROM catalog.variants WHERE product_id = $1 ORDER BY id", product)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err //nolint:wrapcheck // test
	})
	return ids
}

// stock nạp onHand cho biến thể v ở kho MAIN (invtest: dòng số dư và dòng sổ cùng tx).
func (e *env) stock(v uuid.UUID, onHand int32) {
	e.t.Helper()
	e.read(func(tx pgx.Tx) error { invtest.SeedStock(e.t, tx, v, e.warehouse, onHand); return nil })
}

// export ghi sổ phiếu xuất qty của v ở kho MAIN bằng chứng từ của M1 (giả lập phần đã giữ cho đợt flash sale: không
// còn trong available của kho).
func (e *env) export(v uuid.UUID, qty int32) {
	e.t.Helper()
	w := inventory.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &realtimetest.RecordingBus{}}
	e.read(func(tx pgx.Tx) error { invtest.Export(e.t, tx, w, e.warehouse, v, qty); return nil })
}

func (e *env) availability(ids []uuid.UUID) []webcatalog.WebAvailability {
	e.t.Helper()
	var out []webcatalog.WebAvailability
	e.read(func(tx pgx.Tx) error {
		var err error
		out, err = webcatalog.Availability(e.t.Context(), tx, ids)
		return err //nolint:wrapcheck // test
	})
	return out
}

// [WB02], [FS09]: tồn 2 → còn, sắp hết; 7 trừ 5 đã giữ cho đợt (phiếu xuất của M1) → 2 còn, sắp hết; 4 → còn, không
// sắp hết; 0 (không có dòng số dư) → hết; 3 → sắp hết (biên); mã lạ, mã trùng → bỏ / một lần; theo mã tăng dần.
func TestAvailability(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	vs := e.variantsOf(e.productsOfSeller[e.seller][0])
	ws := e.variantsOf(e.productsOfSeller[e.seller][1])
	two, flash, four := vs[0], vs[1], vs[2]
	none, three := ws[0], ws[1]
	e.stock(two, 2)
	e.stock(flash, 7)
	e.export(flash, 5)
	e.stock(four, 4)
	e.stock(three, 3)

	got := e.availability([]uuid.UUID{four, two, uuid.New(), flash, none, three, two})
	want := map[uuid.UUID]webcatalog.WebAvailability{
		two:   {VariantID: two, Available: true, Low: true},
		flash: {VariantID: flash, Available: true, Low: true},
		four:  {VariantID: four, Available: true, Low: false},
		none:  {VariantID: none, Available: false, Low: false},
		three: {VariantID: three, Available: true, Low: true},
	}
	if len(got) != len(want) {
		t.Fatalf("mong %d dòng (bỏ mã lạ, mã trùng), có %+v", len(want), got)
	}
	if !slices.IsSortedFunc(got, func(a, b webcatalog.WebAvailability) int { return slices.Compare(a.VariantID[:], b.VariantID[:]) }) {
		t.Errorf("không theo mã tăng dần: %+v", got)
	}
	for _, g := range got {
		if g != want[g.VariantID] {
			t.Errorf("%s: mong %+v, có %+v", g.VariantID, want[g.VariantID], g)
		}
	}
	// Bán hết phần còn lại của đợt thường → hết hàng.
	e.export(flash, 2)
	if got := e.availability([]uuid.UUID{flash}); len(got) != 1 || got[0].Available || got[0].Low {
		t.Fatalf("sau khi xuất hết: %+v", got)
	}
}

// Giới hạn: 100 mã được, 101 → ErrBadLimit; rỗng → rỗng, không lỗi.
func TestAvailability_Limit(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ids := make([]uuid.UUID, webcatalog.MaxAvailabilityIDs+1)
	for i := range ids {
		ids[i] = uuid.New()
	}
	e.read(func(tx pgx.Tx) error {
		if _, err := webcatalog.Availability(t.Context(), tx, ids); !errors.Is(err, webcatalog.ErrBadLimit) {
			t.Errorf("101 mã: mong ErrBadLimit, có %v", err)
		}
		if got, err := webcatalog.Availability(t.Context(), tx, ids[:100]); err != nil || len(got) != 0 {
			t.Errorf("100 mã lạ: %v, %+v", err, got)
		}
		if got, err := webcatalog.Availability(t.Context(), tx, nil); err != nil || got == nil || len(got) != 0 {
			t.Errorf("rỗng: %v, %+v", err, got)
		}
		return nil
	})
}

// orderSQL chèn một khách, một đơn ($3 người bán, $4 mã, $5 trạng thái, $6 lúc tạo) và các dòng đơn từ $7 (JSON
// {"<mã biến thể>": số lượng}) trong một câu.
const orderSQL = `WITH c AS (
	INSERT INTO customer.customers (id, tenant_id, root_id, full_name) VALUES ($1, $2, $1, 'Khách thử') RETURNING id
), o AS (
	INSERT INTO ord.orders (tenant_id, seller_id, code, customer_id, channel, idempotency_key, status, ship_address,
		subtotal, cod_amount, grand_total, warehouse_id, created_at, draft_expires_at)
	SELECT $2, $3, $4, c.id, 'web', gen_random_uuid(), $5, '{}', 0, 0, 0, $8, $6,
		CASE WHEN $5 = 'draft' THEN $6::timestamptz + interval '1 day' END
	FROM c RETURNING id
)
INSERT INTO ord.order_items (tenant_id, seller_id, order_id, position, variant_id, sku_snapshot, name_snapshot,
	quantity, unit_price, net_amount)
SELECT $2, $3, o.id, l.n, l.key::uuid, 'SKU', 'Dòng thử', l.value::int, 0, 0
FROM o CROSS JOIN LATERAL (SELECT key, value, row_number() OVER () AS n FROM jsonb_each_text($7::jsonb)) AS l`

// order chèn một đơn của người bán seller, trạng thái status, tạo lúc createdAt, với các dòng (biến thể, số lượng),
// bằng chủ bảng (gói chỉ đọc, không có API ghi đơn ở đây).
func (e *env) order(seller uuid.UUID, status string, createdAt time.Time, lines map[uuid.UUID]int) {
	e.t.Helper()
	e.n++
	e.exec(orderSQL, uuid.New(), e.tenant, seller, fmt.Sprintf("DH%07d", e.n), status, createdAt, lines, e.warehouse)
}

// "Đã bán 30 ngày" (docs/m5/doi-chieu-gia-dinh.md mục 4): cộng số lượng theo biến thể của đơn tạo trong 30 ngày, mọi
// kênh, mọi người bán, trừ đơn draft, cancelled, returned; đơn 31 ngày trước không tính; đúng now − 30 ngày tính, sớm
// hơn 1 µs không tính; đúng now không tính; kèm mã mẫu.
func TestSold30(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	p, q := e.productsOfSeller[e.seller][0], e.productsOfSeller[e.partner][0]
	pv, qv := e.variantsOf(p), e.variantsOf(q)
	now := e.dbNow()
	day := 24 * time.Hour
	s, ps := e.seller, e.partner
	e.order(s, "confirmed", now.Add(-time.Hour), map[uuid.UUID]int{pv[0]: 2, pv[1]: 1})
	e.order(s, "cancelled", now.Add(-time.Hour), map[uuid.UUID]int{pv[0]: 5})
	e.order(s, "delivered", now.Add(-31*day), map[uuid.UUID]int{pv[0]: 7})
	e.order(s, "delivered", now.Add(-webcatalog.SoldWindow), map[uuid.UUID]int{pv[0]: 3}) // đúng biên dưới: tính
	e.order(ps, "delivered", now.Add(-webcatalog.SoldWindow), map[uuid.UUID]int{qv[2]: 4})
	e.order(s, "delivered", now.Add(-webcatalog.SoldWindow-time.Microsecond), map[uuid.UUID]int{pv[1]: 8}) // ngay trước biên
	e.order(s, "new", now, map[uuid.UUID]int{pv[1]: 20})                                                   // đúng mốc now (biên trên, không tính)
	e.order(s, "draft", now.Add(-time.Hour), map[uuid.UUID]int{pv[0]: 9})
	e.order(s, "returned", now.Add(-time.Hour), map[uuid.UUID]int{pv[1]: 6})
	e.order(ps, "new", now.Add(-time.Minute), map[uuid.UUID]int{qv[2]: 1})

	var got []webcatalog.WebSold
	e.read(func(tx pgx.Tx) error {
		var err error
		got, err = webcatalog.Sold30(t.Context(), tx, now)
		return err //nolint:wrapcheck // test
	})
	want := []webcatalog.WebSold{
		{ProductID: p, VariantID: pv[0], Sold: 5},
		{ProductID: p, VariantID: pv[1], Sold: 1},
		{ProductID: q, VariantID: qv[2], Sold: 5},
	}
	slices.SortFunc(want, func(a, b webcatalog.WebSold) int { return slices.Compare(a.VariantID[:], b.VariantID[:]) })
	if !slices.Equal(got, want) {
		t.Fatalf("mong %+v\ncó   %+v", want, got)
	}
	// Mốc now lùi 2 ngày: đơn tạo sau mốc không tính; đơn 31 ngày trước và đơn ngay trước biên cũ vào khoảng (pv[0]:
	// 7 + 3, pv[1]: 8).
	e.read(func(tx pgx.Tx) error {
		got, err := webcatalog.Sold30(t.Context(), tx, now.Add(-2*day))
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		want := []webcatalog.WebSold{
			{ProductID: p, VariantID: pv[0], Sold: 10}, {ProductID: p, VariantID: pv[1], Sold: 8}, {ProductID: q, VariantID: qv[2], Sold: 4},
		}
		slices.SortFunc(want, func(a, b webcatalog.WebSold) int { return slices.Compare(a.VariantID[:], b.VariantID[:]) })
		if !slices.Equal(got, want) {
			t.Errorf("mốc lùi 2 ngày: mong %+v, có %+v", want, got)
		}
		return nil
	})
}

// InStock (Task 454): mẫu có một biến thể bán được còn hàng ở kho MAIN → có; tồn 0, đã xuất hết, biến thể ngừng bán
// còn hàng, mã lạ → không; mã trùng một lần, theo mã tăng dần; rỗng → rỗng.
func TestInStock(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ps := e.productsOfSeller[e.seller]
	if len(ps) < 4 {
		t.Fatalf("dữ liệu mẫu có %d mẫu của người bán, cần 4", len(ps))
	}
	stocked, empty, soldOut, stopped := ps[0], ps[1], ps[2], ps[3]
	e.stock(e.variantsOf(stocked)[1], 1)
	so := e.variantsOf(soldOut)[0]
	e.stock(so, 3)
	e.export(so, 3)
	st := e.variantsOf(stopped)[0]
	e.stock(st, 5)
	e.exec("UPDATE catalog.variants SET status = 'discontinued' WHERE id = $1", st)

	var got []uuid.UUID
	e.read(func(tx pgx.Tx) error {
		var err error
		got, err = webcatalog.InStock(t.Context(), tx, []uuid.UUID{empty, stocked, uuid.New(), soldOut, stopped, stocked})
		return err //nolint:wrapcheck // test
	})
	if !slices.Equal(got, []uuid.UUID{stocked}) {
		t.Fatalf("InStock = %v, muốn chỉ %s", got, stocked)
	}
	e.read(func(tx pgx.Tx) error {
		var err error
		got, err = webcatalog.InStock(t.Context(), tx, nil)
		return err //nolint:wrapcheck // test
	})
	if got == nil || len(got) != 0 {
		t.Fatalf("InStock rỗng = %#v, muốn mảng rỗng", got)
	}
}
