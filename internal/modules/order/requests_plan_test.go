package order_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// requestPlanSeed: tenant A có 2.000 yêu cầu, tenant B 20.000 (trên 50 đơn web mỗi tenant, rải 8 trạng thái); ANALYZE.
// Như máy chủ nhiều shop: câu đếm của A không được quét cả bảng.
func requestPlanSeed(a, b uuid.UUID) testkit.Seed {
	q := []string{
		"SET ROLE crm_owner",
		fmt.Sprintf("INSERT INTO platform.tenants (id, name) VALUES ('%s', 'A'), ('%s', 'B')", a, b),
	}
	for _, c := range []struct {
		tenant   uuid.UUID
		requests int
	}{{a, 2000}, {b, 20000}} {
		seller := testkit.FixedUUID("order-request-plan/seller/" + c.tenant.String())
		q = append(q,
			fmt.Sprintf("SELECT set_config('app.tenant_id', '%s', false)", c.tenant),
			fmt.Sprintf(`INSERT INTO catalog.sellers (id, tenant_id, code, name, kind)
				VALUES ('%s', current_setting('app.tenant_id')::uuid, 'ELIMAZ', 'Elimaz', 'own')`, seller),
			fmt.Sprintf(`INSERT INTO inventory.warehouses (tenant_id, operated_by_seller_id, code, name)
				VALUES (current_setting('app.tenant_id')::uuid, '%s', 'MAIN', 'Kho chính')`, seller),
			`INSERT INTO customer.customers (tenant_id, full_name, name_search)
				VALUES (current_setting('app.tenant_id')::uuid, 'khach', 'khach')`,
			fmt.Sprintf(`INSERT INTO ord.orders (tenant_id, seller_id, code, customer_id, channel, idempotency_key, status,
					pending_reason, packing_status, ship_address, subtotal, cod_amount, grand_total, warehouse_id,
					ship_phone, ship_phone_reversed, external_ref, created_at)
				SELECT current_setting('app.tenant_id')::uuid, '%s', 'DH' || lpad(g::text, 7, '0'), cs.id, 'web',
				       gen_random_uuid(), 'delivered', '', 'none', '{}', 0, 0, 0, w.id, '+849' || lpad(g::text, 8, '0'),
				       reverse('849' || lpad(g::text, 8, '0')), 'W-' || g, timestamptz '2026-01-01' + g * interval '1 hour'
				FROM generate_series(1, 50) AS g
				CROSS JOIN (SELECT id FROM customer.customers LIMIT 1) AS cs
				CROSS JOIN (SELECT id FROM inventory.warehouses WHERE code = 'MAIN') AS w`, seller),
			fmt.Sprintf(`INSERT INTO ord.order_requests (tenant_id, order_id, ref, kind, status, reason_code, source,
					ship_back, created_at, updated_at)
				SELECT current_setting('app.tenant_id')::uuid, o.ids[1 + g %% 50], 'YC-' || lpad(g::text, 8, '0'),
				       CASE WHEN st.s IN ('confirmed', 'items_received') THEN 'exchange' ELSE 'question' END, st.s,
				       'OTHER', 'web', CASE WHEN st.s IN ('confirmed', 'items_received') THEN '{}'::jsonb END,
				       timestamptz '2026-02-01' + g * interval '1 minute', timestamptz '2026-02-01' + g * interval '1 minute'
				FROM generate_series(1, %d) AS g
				CROSS JOIN (SELECT array_agg(id ORDER BY id) AS ids FROM ord.orders) AS o
				CROSS JOIN LATERAL (SELECT (ARRAY['received','in_review','needs_info','confirmed','items_received',
				       'resolved','rejected','cancelled'])[1 + g %% 8] AS s) AS st`, c.requests),
		)
	}
	return testkit.Seed{Name: "order-request-plan", SQL: append(q, "ANALYZE ord.order_requests", "ANALYZE ord.orders")}
}

// sqlcQuery trả câu SQL sqlc sinh cho truy vấn name trong internal/store/requests.sql.go (đúng câu ứng dụng chạy).
func sqlcQuery(t *testing.T, name string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("internal", "store", "requests.sql.go"))
	if err != nil {
		t.Fatal(err)
	}
	_, rest, found := strings.Cut(string(src), "const "+name+" = `")
	q, _, closed := strings.Cut(rest, "`")
	if !found || !closed {
		t.Fatalf("không thấy câu %s", name)
	}
	return q
}

// [Task 486] Câu đếm yêu cầu theo trạng thái (countOrderRequests) bằng crm_app trên tenant A (2.000 dòng, bảng có
// 22.000): kế hoạch không có Seq Scan (index order_requests_status); CountRequests đếm đúng.
func TestCountRequests_Plan(t *testing.T) {
	t.Parallel()
	a, b := testkit.FixedUUID("order-request-plan/A"), testkit.FixedUUID("order-request-plan/B")
	staff := testkit.OpenStaffSeeded(t, requestPlanSeed(a, b))
	err := tenancy.WithTenant(t.Context(), staff.App, a, func(tx pgx.Tx) error {
		c, err := order.CountRequests(t.Context(), tx)
		if err == nil && (c.Total != 2000 || c.ByStatus[order.RequestReceived] != 250 || c.Open != 1250) {
			err = fmt.Errorf("đếm sai: %+v", c)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := testkit.ExplainAsApp(t.Context(), staff.App, a, sqlcQuery(t, "countOrderRequests"))
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		t.Fatal("không có kế hoạch")
	}
	testkit.RequireNoSeqScan(t, nodes)
}
