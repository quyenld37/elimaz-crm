package reportseed_test

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/reportseed"
)

// count đếm bằng chủ bảng trong ngữ cảnh tenant.
func count(t *testing.T, s *testkit.Staff, tenant uuid.UUID, q string, args ...any) int64 {
	t.Helper()
	var n int64
	s.Exec(t, tenant, func(tx pgx.Tx) error { return tx.QueryRow(t.Context(), q, args...).Scan(&n) })
	return n
}

// Kịch bản chín đơn dựng được qua mọi CHECK và trigger tổng tiền của bảng đơn; các số tổng khớp bảng của kế hoạch.
func TestNineOrders(t *testing.T) {
	t.Parallel()
	st := testkit.OpenStaff(t)
	tenant := st.Tenant(t, "a")
	seed := reportseed.New(t, st.Owner, tenant)
	sc := reportseed.NineOrders(seed, "2026-11-17")
	if len(sc.Orders) != 9 {
		t.Fatalf("số đơn = %d", len(sc.Orders))
	}
	checks := []struct {
		name string
		q    string
		want int64
	}{
		{"đơn", "SELECT count(*) FROM ord.orders", 9},
		{"đơn đã lên", `SELECT count(*) FROM ord.orders WHERE status <> 'draft'`, 8},
		{"tiền hàng đơn lên chưa hủy", `SELECT coalesce(sum(subtotal - discount_total), 0) FROM ord.orders
			WHERE status NOT IN ('draft', 'cancelled')`, 1_910_000},
		{"ship thu khách đơn lên chưa hủy", `SELECT coalesce(sum(shipping_fee_charged), 0) FROM ord.orders
			WHERE status NOT IN ('draft', 'cancelled')`, 150_000},
		{"đơn hủy", `SELECT coalesce(sum(subtotal - discount_total), 0) FROM ord.orders WHERE status = 'cancelled'`, 200_000},
		{"giao xong", `SELECT count(*) FROM ord.orders WHERE status = 'delivered'`, 2},
		{"hoàn", `SELECT count(*) FROM ord.orders WHERE status = 'returned'`, 1},
		{"chờ xác nhận", `SELECT count(*) FROM ord.orders WHERE status = 'new'`, 1},
		{"chờ gửi hãng", `SELECT count(*) FROM ord.orders WHERE status = 'confirmed' AND current_shipment_id IS NULL`, 1},
		{"yêu cầu hủy", `SELECT count(*) FROM ord.orders WHERE status = 'cancel_requested'`, 1},
		{"khách trả nhận", `SELECT coalesce(sum(ri.sellable_qty + ri.damaged_qty), 0) FROM ord.return_items ri
			JOIN ord.returns r ON r.id = ri.return_id WHERE r.kind = 'customer_return' AND r.received_at IS NOT NULL`, 1},
		{"tiền hoàn khách", `SELECT coalesce(sum(refund_amount), 0) FROM ord.returns WHERE kind = 'customer_return'`, 150_000},
		{"dòng lịch sử lúc tạo có from rỗng", `SELECT count(*) FROM ord.order_status_history_v WHERE from_status = ''`, 9},
		{"phí giả của hãng", `SELECT count(*) FROM fulfillment.shipments WHERE fee <> 22000`, 0},
	}
	for _, c := range checks {
		if got := count(t, st, tenant, c.q); got != c.want {
			t.Errorf("%s = %d, mong %d", c.name, got, c.want)
		}
	}
	// O3: hai vận đơn (SPX hủy, Best đang đi), vận đơn hiện hành là Best.
	if got := count(t, st, tenant, `SELECT count(*) FROM fulfillment.shipments WHERE order_id = $1`, sc.Orders["O3"]); got != 2 {
		t.Errorf("vận đơn của O3 = %d", got)
	}
	if got := count(t, st, tenant, `SELECT count(*) FROM ord.orders o JOIN fulfillment.shipments s ON s.id = o.current_shipment_id
		WHERE o.id = $1 AND s.carrier_key = 'best' AND s.status = 'shipping'`, sc.Orders["O3"]); got != 1 {
		t.Errorf("vận đơn hiện hành của O3 không phải Best đang đi")
	}
	// O4: giảm 40.000 phân bổ vào dòng; net_amount cộng lại bằng tiền hàng.
	if got := count(t, st, tenant, `SELECT sum(net_amount) FROM ord.order_items WHERE order_id = $1`, sc.Orders["O4"]); got != 360_000 {
		t.Errorf("net_amount của O4 = %d", got)
	}
	// Mọi lần đổi trạng thái đều ở ngày của kịch bản (giờ VN).
	if got := count(t, st, tenant, `SELECT count(*) FROM ord.order_status_history
		WHERE occurred_at < '2026-11-16T17:00:00Z' OR occurred_at >= '2026-11-17T17:00:00Z'`); got != 0 {
		t.Errorf("%d dòng lịch sử ngoài ngày", got)
	}
}

// Ledger cập nhật cả sổ và số dư: số dư của mỗi ngăn bằng tổng sổ.
func TestLedgerKeepsBalance(t *testing.T) {
	t.Parallel()
	st := testkit.OpenStaff(t)
	tenant := st.Tenant(t, "a")
	seed := reportseed.New(t, st.Owner, tenant)
	v := seed.Variant(seed.Own(), "Áo A", "AOA-S")
	w1, w2 := seed.Warehouse("Kho 1"), seed.Warehouse("Kho 2")
	at := time.Date(2026, 11, 17, 3, 0, 0, 0, time.UTC)
	seed.Ledger(v, w1, "on_hand", 10, "import", at)
	seed.Ledger(v, w1, "on_hand", -3, "order", at.Add(time.Hour))
	seed.Ledger(v, w1, "damaged", 1, "damage", at.Add(2*time.Hour))
	seed.Ledger(v, w2, "on_hand", 4, "import", at)
	if got := count(t, st, tenant, `SELECT count(*) FROM inventory.stock_balances b
		LEFT JOIN (SELECT variant_id, warehouse_id,
			coalesce(sum(delta) FILTER (WHERE bucket = 'on_hand'), 0) AS on_hand,
			coalesce(sum(delta) FILTER (WHERE bucket = 'held'), 0) AS held,
			coalesce(sum(delta) FILTER (WHERE bucket = 'in_transit'), 0) AS in_transit,
			coalesce(sum(delta) FILTER (WHERE bucket = 'damaged'), 0) AS damaged
			FROM inventory.stock_ledger GROUP BY variant_id, warehouse_id) l USING (variant_id, warehouse_id)
		WHERE (b.on_hand, b.held, b.in_transit, b.damaged) IS DISTINCT FROM (l.on_hand, l.held, l.in_transit, l.damaged)`); got != 0 {
		t.Fatalf("%d số dư lệch sổ", got)
	}
	if got := count(t, st, tenant, `SELECT on_hand FROM inventory.stock_balances WHERE variant_id = $1 AND warehouse_id = $2`, v, w1); got != 7 {
		t.Fatalf("on_hand kho 1 = %d", got)
	}
	if got := count(t, st, tenant, `SELECT count(*) FROM inventory.stock_ledger WHERE created_at = $1`, at); got != 2 {
		t.Fatalf("mốc sổ không theo at: %d", got)
	}
}

// Bulk dựng n đơn ngẫu nhiên qua mọi ràng buộc; mỗi đơn có dòng lịch sử lúc tạo (from rỗng) trong khoảng [from, to).
func TestBulk(t *testing.T) {
	t.Parallel()
	st := testkit.OpenStaff(t)
	tenant := st.Tenant(t, "a")
	seed := reportseed.New(t, st.Owner, tenant)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 3, 0)
	seed.Bulk(500, rand.New(rand.NewPCG(1, 2)), from, to) //nolint:gosec // G404: dữ liệu thử lặp lại được
	if got := count(t, st, tenant, "SELECT count(*) FROM ord.orders"); got != 500 {
		t.Fatalf("số đơn = %d", got)
	}
	if got := count(t, st, tenant, `SELECT count(DISTINCT order_id) FROM ord.order_status_history_v
		WHERE from_status = '' AND occurred_at >= $1 AND occurred_at < $2`, from, to); got != 500 {
		t.Fatalf("đơn có dòng lúc tạo trong khoảng = %d", got)
	}
	if got := count(t, st, tenant, `SELECT count(*) FROM ord.orders o WHERE status <> (
		SELECT to_status FROM ord.order_status_history h WHERE h.order_id = o.id ORDER BY occurred_at DESC, recorded_at DESC LIMIT 1)`); got != 0 {
		t.Fatalf("%d đơn có trạng thái khác dòng lịch sử cuối", got)
	}
	for _, q := range []string{
		"SELECT count(*) FROM ord.order_items",
		"SELECT count(*) FROM fulfillment.shipments",
		"SELECT count(*) FROM ord.orders WHERE channel_account_id IS NOT NULL",
		"SELECT count(*) FROM ord.orders WHERE status = 'delivered'",
	} {
		if count(t, st, tenant, q) == 0 {
			t.Errorf("%s = 0: dữ liệu ngẫu nhiên thiếu loại này", q)
		}
	}
}
