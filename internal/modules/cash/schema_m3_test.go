package cash_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// Sau 0303 database theo đủ luật schema (RLS, unique ghép tenant, view _v security_invoker…).
func TestSchemaM3Cash_Rules(t *testing.T) {
	t.Parallel()
	testkit.CheckSchemaRulesClean(t, testkit.OpenStaff(t).Owner)
}

// m1Transfer là một bút toán ghi bằng đúng các cột của M1 (0119) trước 0303.
type m1Transfer struct {
	id, shipment uuid.UUID
	kind         string
}

// Dữ liệu M1 (bút toán expected, collected như consumer M1 ghi, trên hai vận đơn của hai tenant) chạy 0303 không lỗi:
// mọi dòng sổ có shipment_id của bút toán, occurred_at = created_at, tổng vẫn 0, trigger chỉ ghi thêm bật lại.
func TestMigrateM1CashData(t *testing.T) {
	t.Parallel()
	_, ownerURL, appURL := testkit.CreateDatabase(t)
	testkit.MigrateBefore(t, ownerURL, "0303")
	owner, app := testkit.OpenDB(t, ownerURL), testkit.OpenDB(t, appURL)
	var tenants []uuid.UUID
	var transfers []m1Transfer
	for _, name := range []string{"A", "B"} {
		tenant, tr := seedM1Ledger(t, owner, app, name)
		tenants = append(tenants, tenant)
		transfers = append(transfers, tr...)
	}
	if _, err := db.Migrate(t.Context(), ownerURL); err != nil {
		t.Fatalf("migrate 0303 trên dữ liệu M1: %v", err)
	}
	for i, tenant := range tenants {
		checkM1Ledger(t, owner, tenant, transfers[i*2:i*2+2])
	}
	// Trigger chỉ ghi thêm của M1 đã bật lại sau khi điền.
	err := ownerExec(t, owner, tenants[0], "UPDATE cash.cod_ledger_entries SET amount = amount")
	if pgCode(err) != "42501" || !strings.Contains(err.Error(), "append-only") {
		t.Errorf("sau 0303 chủ bảng UPDATE dòng sổ: mong 42501 append-only, có %v", err)
	}
	testkit.CheckSchemaRulesClean(t, owner)
}

// checkM1Ledger: sau 0303 mỗi bút toán M1 của tenant có 2 dòng mang đúng vận đơn của bút toán; occurred_at =
// created_at; tổng sổ 0.
func checkM1Ledger(t *testing.T, owner *db.DB, tenant uuid.UUID, transfers []m1Transfer) {
	t.Helper()
	testkit.InTenant(t, owner, tenant, func(tx pgx.Tx) error {
		for _, tr := range transfers {
			var lines, sameShip int
			if err := tx.QueryRow(t.Context(), `SELECT count(*), count(*) FILTER (WHERE shipment_id = $2)
				FROM cash.cod_ledger_entries WHERE transfer_id = $1`, tr.id, tr.shipment).Scan(&lines, &sameShip); err != nil {
				return err //nolint:wrapcheck // test
			}
			if lines != 2 || sameShip != 2 {
				t.Errorf("bút toán %s %s: %d dòng, %d dòng có shipment_id đúng", tr.kind, tr.id, lines, sameShip)
			}
		}
		var badTime, total int64
		if err := tx.QueryRow(t.Context(), `SELECT
			(SELECT count(*) FROM cash.cod_transfers WHERE occurred_at IS DISTINCT FROM created_at),
			(SELECT coalesce(sum(amount), 0)::bigint FROM cash.cod_ledger_entries)`).Scan(&badTime, &total); err != nil {
			return err //nolint:wrapcheck // test
		}
		if badTime != 0 || total != 0 {
			t.Errorf("tenant %s: %d bút toán occurred_at khác created_at, tổng sổ %d", tenant, badTime, total)
		}
		return nil
	})
}

// ownerExec chạy câu q trong ngữ cảnh tenant trên d, trả lỗi của transaction.
func ownerExec(t *testing.T, d *db.DB, tenant uuid.UUID, q string) error {
	t.Helper()
	tx, err := d.Pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(t.Context()) }()
	if _, err := tx.Exec(t.Context(), "SELECT set_config('app.tenant_id', $1, true)", tenant.String()); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(t.Context(), q)
	return err //nolint:wrapcheck // test
}

// seedM1Ledger dựng tenant name có một vận đơn COD 530000 và hai bút toán expected, collected ghi bằng các cột của M1
// (chủ bảng, SQL thẳng; dòng sổ chưa có shipment_id).
func seedM1Ledger(t *testing.T, owner, app *db.DB, name string) (uuid.UUID, []m1Transfer) {
	t.Helper()
	var tenant uuid.UUID
	if err := owner.Pool.QueryRow(t.Context(), "INSERT INTO platform.tenants (name) VALUES ($1) RETURNING id", name).
		Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	var shop *shiptest.Shop
	var v uuid.UUID
	testkit.InTenant(t, owner, tenant, func(tx pgx.Tx) error {
		shop = shiptest.NewShop(t, tx, tenant)
		v = shop.Variant(t, tx, shop.Seller, 530000, 10)
		return nil
	})
	var ship uuid.UUID
	testkit.InTenant(t, app, tenant, func(tx pgx.Tx) error {
		o, err := shop.Order(t.Context(), tx, shop.Seller, uuid.Nil, 0, order.ItemInput{VariantID: v, Qty: 1})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		s, err := shop.Shipment(t.Context(), tx, shop.Seller, o.ID, uuid.Nil)
		ship = s.ID
		return err //nolint:wrapcheck // test
	})
	var out []m1Transfer
	testkit.InTenant(t, owner, tenant, func(tx pgx.Tx) error {
		for i, k := range []struct {
			kind     string
			from, to string
		}{{"expected", "to_collect", "sales_clearing"}, {"collected", "carrier_holding", "to_collect"}} {
			tr := m1Transfer{shipment: ship, kind: k.kind}
			if err := tx.QueryRow(t.Context(), `INSERT INTO cash.cod_transfers
				(tenant_id, shipment_id, kind, idempotency_key, actor_type, created_at)
				VALUES ($1, $2, $3, $4, 'carrier', $5) RETURNING id`, tenant, ship, k.kind, uuid.NewString(),
				time.Date(2026, 11, 1+i, 9, 0, 0, 0, time.UTC)).Scan(&tr.id); err != nil {
				return err //nolint:wrapcheck // test
			}
			if _, err := tx.Exec(t.Context(), `INSERT INTO cash.cod_ledger_entries (tenant_id, transfer_id, account, amount)
				VALUES ($1, $2, $3, 530000), ($1, $2, $4, -530000)`, tenant, tr.id, k.from, k.to); err != nil {
				return err //nolint:wrapcheck // test
			}
			out = append(out, tr)
		}
		return nil
	})
	return tenant, out
}

// Ràng buộc của các bảng mới (0303): CHECK, unique, khóa ngoại, quyền của crm_app.
func TestSchemaM3Cash_Constraints(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	var orderID uuid.UUID
	if err := e.app(func(tx pgx.Tx) error {
		o, err := e.shop.Order(t.Context(), tx, e.seller, e.user, 0, order.ItemInput{VariantID: e.variant, Qty: 1})
		orderID = o.ID
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	for _, c := range constraintCases(e, orderID) {
		err := e.execApp(c.q)
		got := pgCode(err)
		if err == nil {
			got = "ok"
		}
		if got != c.want {
			t.Errorf("%s: %s (%v), mong %s", c.name, got, err, c.want)
		}
	}
	for v, n := range map[string]int64{
		"cash.payments_v": 2, "cash.refund_obligations_v": 1, "cash.reconcile_runs_v": 1, "cash.cod_transfers_v": 0,
	} {
		if got := e.count("SELECT count(*) FROM " + v); got != n {
			t.Errorf("%s: %d dòng, mong %d", v, got, n)
		}
	}
	checkStatementLineFK(t, e)
}

// sqlCase là một câu ghi bằng crm_app và SQLSTATE mong đợi ("ok": không lỗi).
type sqlCase struct{ name, q, want string }

// constraintCases là các ca ràng buộc của bảng mới 0303 cho đơn orderID (chạy theo thứ tự: ca sau dựa vào dòng ca
// trước đã chèn).
func constraintCases(e *codEnv, orderID uuid.UUID) []sqlCase {
	sha := strings.Repeat("a", 64)
	stmt := func(sha, status string) string {
		return `INSERT INTO cash.cod_statements (tenant_id, carrier_key, carrier_account_id, file_id, file_sha256, format,
			status, imported_by) VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, 'fake', '` +
			e.shop.Account.String() + `', gen_random_uuid(), '` + sha + `', 'csv', '` + status + `', gen_random_uuid())`
	}
	pay := func(method string, amount string) string {
		return `INSERT INTO cash.payments (tenant_id, order_id, method, amount, received_at, actor_id)
			VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, '` + orderID.String() + `', '` + method +
			`', ` + amount + `, now(), gen_random_uuid())`
	}
	obl := func(source, amount, paid, ret string) string {
		return `INSERT INTO cash.refund_obligations (tenant_id, order_id, source, amount, paid_amount, return_id)
			VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, '` + orderID.String() + `', '` + source +
			`', ` + amount + `, ` + paid + `, ` + ret + `)`
	}
	return []sqlCase{
		{"bảng kê đúng", stmt(sha, "imported"), "ok"},
		{"bảng kê cùng tệp lần hai", stmt(sha, "imported"), "23505"},
		{"sha256 sai dạng", stmt(strings.Repeat("A", 64), "imported"), "23514"},
		{"trạng thái lạ", stmt(strings.Repeat("b", 64), "done"), "23514"},
		{"tài khoản hãng không có", strings.Replace(stmt(strings.Repeat("c", 64), "imported"),
			e.shop.Account.String(), uuid.NewString(), 1), "23503"},
		{
			"dòng bảng kê line_no 0", `INSERT INTO cash.cod_statement_lines (tenant_id, statement_id, line_no,
			tracking_code, cod_amount, net_amount) SELECT tenant_id, id, 0, 'T1', 1000, 1000 FROM cash.cod_statements`,
			"23514",
		},
		{"dòng bảng kê đúng", `INSERT INTO cash.cod_statement_lines (tenant_id, statement_id, line_no, tracking_code,
			cod_amount, net_amount) SELECT tenant_id, id, 1, 'T1', 1000, 1000 FROM cash.cod_statements`, "ok"},
		{
			"dòng bảng kê trùng số dòng", `INSERT INTO cash.cod_statement_lines (tenant_id, statement_id, line_no,
			tracking_code, cod_amount, net_amount) SELECT tenant_id, id, 1, 'T2', 1000, 1000 FROM cash.cod_statements`,
			"23505",
		},
		{"phiếu thu chuyển khoản", pay("bank_transfer", "200000"), "ok"},
		{"phiếu thu số âm không phải hoàn", pay("cash", "-1"), "23514"},
		{"phiếu hoàn số dương", pay("refund", "1"), "23514"},
		{"phiếu hoàn số âm", pay("refund", "-200000"), "ok"},
		{"phiếu thu 0 đồng", pay("deposit", "0"), "23514"},
		{"phương thức lạ", pay("card", "1"), "23514"},
		{"nghĩa vụ hoàn", obl("order_cancel", "200000", "0", "NULL"), "ok"},
		{"nghĩa vụ hoàn trùng (return_id rỗng)", obl("order_cancel", "100000", "0", "NULL"), "23505"},
		{"đã trả vượt số phải trả", obl("exchange", "100", "101", "NULL"), "23514"},
		{"phiếu trả không có", obl("customer_return", "100", "0", "'"+uuid.NewString()+"'"), "23503"},
		{"crm_app sửa phiếu thu", "UPDATE cash.payments SET amount = 1", "42501"},
		{"crm_app sửa file_sha256", "UPDATE cash.cod_statements SET file_sha256 = file_sha256", "42501"},
		{"crm_app đổi trạng thái bảng kê", "UPDATE cash.cod_statements SET status = 'parsing'", "ok"},
		{"lượt đối chiếu", `INSERT INTO cash.reconcile_runs (tenant_id, ran_at, ok, findings)
			VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, now(), false, 1)`, "ok"},
		{"phát hiện đối chiếu", `INSERT INTO cash.reconcile_findings (tenant_id, run_id, kind, detail)
			SELECT tenant_id, id, 'missing_line', '{"amount": 1}' FROM cash.reconcile_runs`, "ok"},
		{"phát hiện detail không phải object", `INSERT INTO cash.reconcile_findings (tenant_id, run_id, kind, detail)
			SELECT tenant_id, id, 'x', '[1]' FROM cash.reconcile_runs`, "23514"},
	}
}

// checkStatementLineFK: bút toán trỏ dòng bảng kê không có → 23503; trỏ dòng có thật → lưu statement_line_id.
func checkStatementLineFK(t *testing.T, e *codEnv) {
	t.Helper()
	// Bút toán trỏ dòng bảng kê không có → khóa ngoại.
	s := e.shipment(1000)
	tr := expectedTr(s, "k-line", 1000)
	missing := uuid.New()
	tr.StatementLineID = &missing
	if _, _, err := e.postID(tr); pgCode(err) != "23503" {
		t.Errorf("bút toán trỏ dòng bảng kê không có: mong 23503, có %v", err)
	}
	var line uuid.UUID
	if err := e.app(func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), "SELECT id FROM cash.cod_statement_lines").Scan(&line)
	}); err != nil {
		t.Fatal(err)
	}
	tr.StatementLineID = &line
	id := e.mustPost(tr)
	if n := e.count("SELECT count(*) FROM cash.cod_transfers_v WHERE id = $1 AND statement_line_id = $2", id, line); n != 1 {
		t.Errorf("statement_line_id không lưu")
	}
}
