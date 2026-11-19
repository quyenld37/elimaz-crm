package order_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/queries/orderview"
	"elimaz.com.vn/crm/internal/testkit"
)

// listExplainSeed: tenant A có người bán S, kho MAIN, 20.000 khách và 200.000 đơn (sinh bằng generate_series: trạng
// thái, SĐT người nhận, giờ tạo rải đều, 3 kênh), tenant B 2.000 đơn; ANALYZE. Chạy bằng chủ bảng trong ngữ cảnh từng
// tenant (RLS lọc như ứng dụng). Đơn không có dòng (tổng 0) để trigger tổng tiền đạt.
func listExplainSeed(a, b, seller uuid.UUID) testkit.Seed {
	q := []string{
		"SET ROLE crm_owner",
		fmt.Sprintf("INSERT INTO platform.tenants (id, name) VALUES ('%s', 'A'), ('%s', 'B')", a, b),
	}
	for _, c := range []struct {
		tenant, seller uuid.UUID
		customers      int
		orders         int
	}{{a, seller, 20000, 200000}, {b, testkit.FixedUUID("order-list-explain/seller-B"), 200, 2000}} {
		q = append(q,
			fmt.Sprintf("SELECT set_config('app.tenant_id', '%s', false)", c.tenant),
			fmt.Sprintf(`INSERT INTO catalog.sellers (id, tenant_id, code, name, kind)
				VALUES ('%s', current_setting('app.tenant_id')::uuid, 'ELIMAZ', 'Elimaz', 'own')`, c.seller),
			fmt.Sprintf(`INSERT INTO inventory.warehouses (tenant_id, operated_by_seller_id, code, name)
				VALUES (current_setting('app.tenant_id')::uuid, '%s', 'MAIN', 'Kho chính')`, c.seller),
			fmt.Sprintf(`INSERT INTO customer.customers (tenant_id, full_name, name_search)
				SELECT current_setting('app.tenant_id')::uuid, 'khach ' || g, 'khach ' || g
				FROM generate_series(1, %d) AS g`, c.customers),
			fmt.Sprintf(`INSERT INTO ord.orders (tenant_id, seller_id, code, customer_id, channel, idempotency_key, status,
					pending_reason, packing_status, ship_address, subtotal, cod_amount, grand_total, warehouse_id,
					ship_phone, ship_phone_reversed, created_at, confirmed_at)
				SELECT current_setting('app.tenant_id')::uuid, '%[1]s', 'DH' || lpad((g + %[3]d)::text, 7, '0'),
				       cs.ids[1 + g %% %[4]d], (ARRAY['manual','facebook','web'])[1 + g %% 3], gen_random_uuid(), st.s,
				       CASE WHEN st.s = 'new' AND g %% 5 = 0 THEN 'awaiting_customer' ELSE '' END, 'none', '{}', 0, 0, 0,
				       w.id, '+849' || lpad(g::text, 8, '0'), reverse('849' || lpad(g::text, 8, '0')),
				       timestamptz '2026-01-01' + g * interval '1 minute',
				       CASE WHEN st.s IN ('confirmed','delivered') THEN timestamptz '2026-01-01' END
				FROM generate_series(1, %[2]d) AS g
				CROSS JOIN (SELECT array_agg(id ORDER BY id) AS ids FROM customer.customers) AS cs
				CROSS JOIN (SELECT id FROM inventory.warehouses WHERE code = 'MAIN') AS w
				CROSS JOIN LATERAL (SELECT (ARRAY['new','confirmed','confirmed','delivered','delivered','delivered',
				       'cancelled','returned','cancel_requested','confirmed'])[1 + g %% 10] AS s) AS st`,
				c.seller, c.orders, map[bool]int{true: 0, false: 5000000}[c.tenant == a], c.customers),
		)
	}
	return testkit.Seed{Name: "order-list-explain", SQL: append(q,
		"ANALYZE ord.orders, customer.customers, ord.order_items")}
}

// listExplainFixture lấy database thử có dữ liệu của listExplainSeed (dựng một lần làm database mẫu) và kiểm số đơn
// của A; trả pool crm_app, tenant A, người bán của A, một khách của A.
func listExplainFixture(t *testing.T) (app *db.DB, a, seller, cust uuid.UUID) {
	t.Helper()
	a, b := testkit.FixedUUID("order-list-explain/A"), testkit.FixedUUID("order-list-explain/B")
	seller = testkit.FixedUUID("order-list-explain/seller")
	s := testkit.OpenStaffSeeded(t, listExplainSeed(a, b, seller))
	app = s.App
	var n int
	s.Exec(t, a, func(tx pgx.Tx) error {
		if err := tx.QueryRow(t.Context(), "SELECT count(*) FROM ord.orders").Scan(&n); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(t.Context(), "SELECT customer_id FROM ord.orders WHERE code = 'DH0000042'").Scan(&cust)
	})
	if n != 200000 {
		t.Fatalf("đơn của A: %d, muốn 200.000", n)
	}
	return app, a, seller, cust
}

// Kế hoạch câu danh sách đơn bằng crm_app trong ngữ cảnh tenant (200.000 đơn): mặc định, lọc trạng thái, lọc nhãn, lọc
// khách (cả nhóm), đuôi SĐT, SĐT đủ, mã đơn, trang sau → không Seq Scan.
func TestListPlans(t *testing.T) {
	app, a, seller, cust := listExplainFixture(t)
	mid := order.Cursor{CreatedAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), ID: uuid.Max}
	cases := []struct {
		name   string
		f      order.ListFilter
		q      string
		cursor order.Cursor
	}{
		{"mặc định", order.ListFilter{}, "", order.Cursor{}},
		{"mặc định trang sau", order.ListFilter{}, "", mid},
		{"lọc trạng thái", order.ListFilter{Statuses: []string{order.StatusNew}}, "", order.Cursor{}},
		{"lọc nhãn chờ khách", order.ListFilter{Labels: []orderview.LabelKey{orderview.LabelAwaitingCustomer}}, "", order.Cursor{}},
		{"lọc khách", order.ListFilter{CustomerID: &cust}, "", order.Cursor{}},
		{"đuôi SĐT", order.ListFilter{}, "5678", order.Cursor{}},
		{"SĐT đủ", order.ListFilter{}, "0900012345", order.Cursor{}},
		{"mã đơn", order.ListFilter{}, "DH0000042", order.Cursor{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.f.SellerID = seller
			var roots []uuid.UUID
			if c.f.CustomerID != nil {
				roots = []uuid.UUID{*c.f.CustomerID}
			}
			sql, args, err := order.ListSQL(c.f, c.q, roots, c.cursor, 50)
			if err != nil {
				t.Fatal(err)
			}
			if seq := seqScans(t, app, a, sql, args...); slices.Contains(seq, "orders") {
				t.Errorf("kế hoạch Seq Scan trên ord.orders (bảng Seq Scan: %v)", seq)
			}
		})
	}
}

// seqScans chạy EXPLAIN (ANALYZE, FORMAT JSON) của sql bằng crm_app trong ngữ cảnh tenant và trả tên mọi bảng bị Seq
// Scan. testkit.ExplainAsApp chỉ trả loại nút; ở đây cần tên bảng vì ord.order_items của dữ liệu mẫu rỗng (Seq Scan
// bảng rỗng là đúng) còn tiêu chí là không Seq Scan trên ord.orders.
func seqScans(t *testing.T, app *db.DB, tenant uuid.UUID, sql string, args ...any) []string {
	t.Helper()
	var raw []byte
	if err := tenancy.WithTenant(t.Context(), app, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), "EXPLAIN (ANALYZE, FORMAT JSON, COSTS OFF, TIMING OFF) "+sql, args...).Scan(&raw)
	}); err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	var top []struct {
		Plan explainNode `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	var out []string
	var walk func(n explainNode)
	walk = func(n explainNode) {
		if n.NodeType == "Seq Scan" {
			out = append(out, n.Relation)
		}
		for _, c := range n.Plans {
			walk(c)
		}
	}
	for _, p := range top {
		walk(p.Plan)
	}
	return out
}

type explainNode struct {
	NodeType string        `json:"Node Type"`
	Relation string        `json:"Relation Name"`
	Plans    []explainNode `json:"Plans"`
}
