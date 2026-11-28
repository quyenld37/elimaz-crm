package cash_test

import (
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/reportseed"
)

// m3BigTables là bảng lớn của M3 (tên không schema): kế hoạch câu danh sách không được quét cả bảng (Seq Scan) trên các
// bảng này.
func m3BigTables() []string {
	return []string{"orders", "shipments", "cod_transfers", "cod_ledger_entries", "cod_statement_lines", "callback_tasks"}
}

// Kế hoạch câu danh sách của M3 trên dữ liệu giả lớn (kế hoạch M3, Task M3-48): 320.000 đơn, khoảng 200.000 vận đơn
// (reportseed.Bulk), hơn 1.000.000 dòng sổ COD (dự kiến, đã thu cho vận đơn đã giao, hoàn cho vận đơn hoàn, đã trả cho
// 90% vận đơn đã giao), 100 bảng kê đã chốt mỗi bảng 1.000 dòng, 100.000 việc gọi lại đã xong và vài việc mở. Mỗi câu chạy
// EXPLAIN ANALYZE bằng crm_app trong ngữ cảnh tenant: không Seq Scan trên bảng lớn (m3BigTables). Câu danh sách đơn có
// bộ lọc vận đơn: TestOrderListPlans (internal/queries); đơn kẹt, cần xem: TestStuck_PlanUsesIndexes (fulfillment); giữ
// hàng quá hạn: TestAgedReservations_Plan (inventory).
func TestM3ListPlans_Large(t *testing.T) {
	// Không t.Parallel: nạp dữ liệu lớn chiếm database chung của máy test; chạy riêng để không làm chậm test khác của gói
	// (và không bị chúng làm quá hạn câu lệnh).
	st := testkit.OpenStaff(t)
	tenant := st.Tenant(t, "A")
	seed := reportseed.New(t, st.Owner, tenant)
	from := time.Now().UTC().AddDate(0, -6, 0)
	rnd := rand.New(rand.NewPCG(48, 139)) //nolint:gosec // G404: dữ liệu thử lặp lại được
	for range 8 {                         // 8 lô × 40.000 đơn: mỗi câu chèn dưới hạn 30 s của pool cả khi máy test bận
		seed.Bulk(40_000, rnd, from, time.Now().UTC().AddDate(0, 0, -20))
	}
	seedM3Money(t, st, tenant)

	statement, account := uuid.Nil, uuid.Nil
	ownerTx(t, st, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT id, carrier_account_id FROM cash.cod_statements LIMIT 1`).Scan(&statement, &account)
	})
	cases := []struct {
		name, file, query string
		args              []any
	}{
		{"lệch đối soát", "cash/lists", "listDiscrepancies", []any{nil, int32(51)}},
		{"lệch đối soát trang sau", "cash/lists", "listDiscrepancies", []any{uuid.Max, int32(51)}},
		{"quá hạn chưa có tiền", "cash/lists", "listOverdue", []any{time.Now().AddDate(0, 0, -7), nil, int32(51)}},
		{"quá hạn trang sau", "cash/lists", "listOverdue", []any{time.Now().AddDate(0, 0, -7), uuid.Nil, int32(51)}},
		{"bảng kê", "cash/statements", "listStatements", []any{nil, nil, nil, int32(51)}},
		{"bảng kê theo tài khoản", "cash/statements", "listStatements", []any{"confirmed", account, nil, int32(51)}},
		{"dòng bảng kê", "cash/statements", "listStatementLines", []any{statement, nil, int32(0), int32(51)}},
		{"dòng bảng kê lệch", "cash/statements", "listStatementLines", []any{statement, "amount_mismatch", int32(0), int32(51)}},
		{"nghĩa vụ hoàn", "cash/refunds", "listRefundObligations", []any{"open", nil, int32(51)}},
		{"gọi lại mở", "fulfillment/callbacks", "listCallbackTasks", []any{[]string{"open", "in_progress"}, nil, nil, nil, nil, int32(51)}},
		{"gọi lại tới hạn", "fulfillment/callbacks", "listCallbackTasks", []any{[]string{"open"}, time.Now(), nil, nil, nil, int32(51)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			module, base, _ := strings.Cut(c.file, "/")
			sql := testkit.StoreQuery(t, "../"+module+"/internal/store/"+base+".sql.go", c.query)
			seq, ms, err := testkit.SeqScanTables(t.Context(), st.App, tenant, m3BigTables(), sql, c.args...)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: %.1f ms", c.query, ms)
			if len(seq) > 0 {
				t.Errorf("%s: Seq Scan trên bảng lớn %v", c.query, seq)
			}
		})
	}
}

// seedM3Money nạp sổ COD, bảng kê, việc gọi lại cho vận đơn của Bulk (chủ bảng, trong tenant; mỗi loại một
// transaction): bút toán dự kiến cho mọi vận đơn, đã thu cho vận đơn delivered, hoàn cho vận đơn returned, đã trả (phí
// hãng 22.000) cho 90% vận đơn delivered kèm 100 bảng kê đã chốt, mỗi bảng 1.000 dòng (5% lệch tiền); 100.000 việc gọi lại
// done, 20 việc mở. Rồi ANALYZE.
func seedM3Money(t *testing.T, st *testkit.Staff, tenant uuid.UUID) {
	t.Helper()
	posting := func(kind, where string, legs string) string {
		return `WITH t AS (
			INSERT INTO cash.cod_transfers (tenant_id, shipment_id, kind, idempotency_key, actor_type, occurred_at)
			SELECT s.tenant_id, s.id, '` + kind + `', 'bulk-` + kind + `-' || s.id, 'system', s.created_at
			FROM fulfillment.shipments s WHERE ` + where + `
			RETURNING id, shipment_id)
		INSERT INTO cash.cod_ledger_entries (tenant_id, transfer_id, shipment_id, account, amount)
		SELECT s.tenant_id, t.id, s.id, l.account, l.amount
		FROM t JOIN fulfillment.shipments s ON s.id = t.shipment_id
		CROSS JOIN LATERAL (VALUES ` + legs + `) AS l(account, amount)`
	}
	paid := `s.status = 'delivered' AND abs(hashtext(s.id::text)) % 10 <> 0`
	for _, q := range []string{
		// Bulk không ghi giờ giao: vận đơn delivered giao 2 ngày sau khi tạo (đều trước hạn quá hạn 7 ngày).
		`UPDATE fulfillment.shipments SET delivered_at = created_at + interval '2 days' WHERE status = 'delivered'`,
		posting("expected", "true", `('to_collect', s.cod_amount), ('sales_clearing', -s.cod_amount)`),
		posting("collected", "s.status = 'delivered'", `('carrier_holding', s.cod_amount), ('to_collect', -s.cod_amount)`),
		posting("returned", "s.status = 'returned'", `('to_collect', -s.cod_amount), ('sales_clearing', s.cod_amount)`),
		posting("remitted", paid,
			`('carrier_holding', -s.cod_amount), ('shop_received', s.cod_amount - 22000), ('carrier_fee', 22000::bigint)`),
		`INSERT INTO cash.cod_statements (tenant_id, carrier_key, carrier_account_id, file_id, file_sha256, format, status,
			line_count, imported_by, confirmed_by, confirmed_at)
		SELECT s.tenant_id, s.carrier_key, s.carrier_account_id, gen_random_uuid(), lpad(to_hex(g), 64, '0'), 'elimaz_v1',
			'confirmed', 1000, gen_random_uuid(), gen_random_uuid(), now()
		FROM (SELECT * FROM fulfillment.shipments ORDER BY id LIMIT 1) s, generate_series(1, 100) g`,
		`INSERT INTO cash.cod_statement_lines (tenant_id, statement_id, line_no, tracking_code, cod_amount, fee_amount,
			net_amount, match_status, shipment_id, expected_amount, diff_amount)
		SELECT p.tenant_id, st.id, (p.n - 1) % 1000 + 1, p.tracking_code, p.cod_amount, 22000, p.cod_amount - 22000,
			CASE WHEN abs(hashtext(p.id::text)) % 20 = 0 THEN 'amount_mismatch' ELSE 'matched' END, p.id,
			p.cod_amount - 22000, 0
		FROM (SELECT s.*, row_number() OVER (ORDER BY s.id) AS n FROM fulfillment.shipments s WHERE ` + paid + `
			ORDER BY s.id LIMIT 100000) p
		JOIN (SELECT id, row_number() OVER (ORDER BY id) AS k FROM cash.cod_statements) st ON st.k = (p.n - 1) / 1000 + 1`,
		`INSERT INTO fulfillment.callback_tasks (tenant_id, shipment_id, order_id, attempt_no, failed_at, due_at, status)
		SELECT tenant_id, id, order_id, 1, created_at, created_at + interval '1 day', 'done'
		FROM fulfillment.shipments WHERE status = 'delivered' ORDER BY id LIMIT 100000`,
		`INSERT INTO fulfillment.callback_tasks (tenant_id, shipment_id, order_id, attempt_no, failed_at, due_at)
		SELECT tenant_id, id, order_id, 1, now(), now() + interval '1 day'
		FROM fulfillment.shipments WHERE status = 'returned' ORDER BY id LIMIT 20`,
	} {
		ownerTx(t, st, tenant, func(tx pgx.Tx) error {
			// Bước nạp (không phải câu đang kiểm) được chạy lâu hơn hạn 30 s của pool khi máy test bận.
			if _, err := tx.Exec(t.Context(), "SET LOCAL statement_timeout = '10min'"); err != nil {
				return err //nolint:wrapcheck // test
			}
			_, err := tx.Exec(t.Context(), q)
			return err //nolint:wrapcheck // test
		})
	}
	for _, tbl := range []string{
		"cash.cod_transfers", "cash.cod_ledger_entries", "cash.cod_statements", "cash.cod_statement_lines",
		"cash.cod_open_holdings", "fulfillment.callback_tasks", "fulfillment.shipments", "ord.orders",
	} {
		if _, err := st.Owner.Pool.Exec(t.Context(), "ANALYZE "+tbl); err != nil {
			t.Fatal(err)
		}
	}
	var entries int
	ownerTx(t, st, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT count(*) FROM cash.cod_ledger_entries`).Scan(&entries)
	})
	if entries < 1_000_000 {
		t.Fatalf("sổ COD có %d dòng, muốn ≥ 1.000.000", entries)
	}
}

// ownerTx chạy fn bằng chủ bảng trong ngữ cảnh tenant (RLS áp cả chủ bảng); lỗi làm test đỏ.
func ownerTx(t *testing.T, st *testkit.Staff, tenant uuid.UUID, fn func(tx pgx.Tx) error) {
	t.Helper()
	if err := tenancy.WithTenant(t.Context(), st.Owner, tenant, fn); err != nil {
		t.Fatal(err)
	}
}
