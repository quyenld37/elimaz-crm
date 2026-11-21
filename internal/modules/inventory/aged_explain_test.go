package inventory_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// agedExplainSeed: một tenant, người bán, kho MAIN, 100 biến thể có số dư và 100.000 dòng giữ hàng (dòng đơn rỗng:
// liên kết dòng đơn cho phép NULL) rải đều 2 năm trước agedNow; 1/10 held, 1/10 in_transit, còn lại consumed/released
// (như shop chạy lâu: phần lớn giữ hàng đã xong); ANALYZE.
func agedExplainSeed() testkit.Seed {
	t := testkit.FixedUUID("inventory-aged-explain/A")
	s := testkit.FixedUUID("inventory-aged-explain/seller")
	return testkit.Seed{Name: "inventory-aged-explain", SQL: []string{
		"SET ROLE crm_owner",
		fmt.Sprintf("INSERT INTO platform.tenants (id, name) VALUES ('%s', 'A')", t),
		fmt.Sprintf("SELECT set_config('app.tenant_id', '%s', false)", t),
		fmt.Sprintf(`INSERT INTO catalog.sellers (id, tenant_id, code, name, kind)
			VALUES ('%s', current_setting('app.tenant_id')::uuid, 'ELIMAZ', 'Elimaz', 'own')`, s),
		fmt.Sprintf(`INSERT INTO inventory.warehouses (tenant_id, operated_by_seller_id, code, name)
			VALUES (current_setting('app.tenant_id')::uuid, '%s', 'MAIN', 'Kho chính')`, s),
		fmt.Sprintf(`INSERT INTO catalog.products (tenant_id, seller_id, code, name, name_search, status)
			VALUES (current_setting('app.tenant_id')::uuid, '%s', 'AO01', 'Áo thử', 'ao thu', 'active')`, s),
		`INSERT INTO catalog.variants (tenant_id, seller_id, product_id, sku, barcode, color, size, price)
			SELECT p.tenant_id, p.seller_id, p.id, 'AO01-' || g, 'BC' || lpad(g::text, 6, '0'), 'Đỏ', 'S' || g, 100000
			FROM catalog.products AS p, generate_series(1, 100) AS g`,
		`INSERT INTO inventory.stock_balances (tenant_id, variant_id, warehouse_id, seller_id, on_hand)
			SELECT v.tenant_id, v.id, w.id, v.seller_id, 1000000 FROM catalog.variants AS v, inventory.warehouses AS w`,
		`INSERT INTO inventory.stock_reservations (tenant_id, variant_id, warehouse_id, quantity, state, state_changed_at)
			SELECT b.tenant_id, b.variant_id, b.warehouse_id, 1,
			       (ARRAY['held','in_transit','consumed','consumed','consumed','consumed','consumed','released',
			              'released','consumed'])[1 + g % 10],
			       timestamptz '2026-12-21 00:00+07' - (g % 730) * interval '1 day' - (g % 1440) * interval '1 minute'
			FROM generate_series(1, 100000) AS g
			JOIN LATERAL (SELECT * FROM inventory.stock_balances ORDER BY variant_id OFFSET g % 100 LIMIT 1) AS b ON true`,
		"ANALYZE inventory.stock_reservations, inventory.stock_balances",
	}}
}

// capturingTx ghi câu và tham số của lần Query cuối (câu sqlc của AgedReservations) để EXPLAIN đúng câu ứng dụng chạy.
type capturingTx struct {
	pgx.Tx
	sql  string
	args []any
}

//nolint:ireturn // hiện thực pgx.Tx
func (c *capturingTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	c.sql, c.args = sql, args
	return c.Tx.Query(ctx, sql, args...) //nolint:wrapcheck // test
}

// TestAgedReservations_Plan (kế hoạch Task M3-22): 100.000 dòng giữ hàng, câu danh sách (mọi kho, theo kho, lọc
// trạng thái, trang sau) bằng crm_app → kế hoạch không có Seq Scan.
func TestAgedReservations_Plan(t *testing.T) {
	staff := testkit.OpenStaffSeeded(t, agedExplainSeed())
	tenant := testkit.FixedUUID("inventory-aged-explain/A")
	var n int
	var wh uuid.UUID
	staff.Exec(t, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM inventory.stock_reservations),
			(SELECT id FROM inventory.warehouses WHERE code = 'MAIN')`).Scan(&n, &wh)
	})
	if n != 100000 {
		t.Fatalf("giữ hàng: %d, muốn 100.000", n)
	}
	mid := &inventory.AgedCursor{Since: agedNow().AddDate(0, -6, 0), ID: uuid.Max}
	cases := []struct {
		name string
		whs  []uuid.UUID
		page inventory.AgedPage
	}{
		{"mọi kho", nil, inventory.AgedPage{}},
		{"theo kho", []uuid.UUID{wh}, inventory.AgedPage{}},
		{"chỉ held", nil, inventory.AgedPage{State: inventory.StateHeld}},
		{"chỉ in_transit", nil, inventory.AgedPage{State: inventory.StateInTransit}},
		{"trang sau", nil, inventory.AgedPage{After: mid}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ct := &capturingTx{}
			if err := tenancy.WithTenant(t.Context(), staff.App, tenant, func(tx pgx.Tx) error {
				ct.Tx = tx
				rows, _, err := inventory.AgedReservations(t.Context(), ct, agedNow(), 20, 180, c.whs, c.page)
				if err == nil && len(rows) == 0 {
					err = fmt.Errorf("không dòng nào: dữ liệu mẫu phải có dòng quá hạn")
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if ct.sql == "" {
				t.Fatal("không bắt được câu")
			}
			nodes, err := testkit.ExplainAsApp(t.Context(), staff.App, tenant, ct.sql, ct.args...)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("kế hoạch: %v", nodes)
			if slices.Contains(nodes, "Seq Scan") {
				t.Errorf("kế hoạch có Seq Scan: %v", nodes)
			}
		})
	}
}
