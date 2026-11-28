package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// Người bán ở các đường tiền (ADR-29, Task 531): bảng kê, lệch đối soát, quá hạn, phát hiện đối chiếu, nghĩa vụ hoàn,
// phiếu của đơn của người bán PARTNER1 không đọc, không ghi được qua API (chạy với người bán mặc định) — cùng 404 như mã
// lạ — và không có trong danh sách.

// sellerCash là tiền của một người bán: bảng kê đã chốt (một dòng ngoài hệ thống), bảng kê đã khớp chờ chốt, nghĩa vụ
// hoàn mở, phiếu thu của đơn, phát hiện đối chiếu gắn vận đơn; vận đơn đã giao 30 ngày, hãng còn giữ tiền (quá hạn,
// phần còn mở).
type sellerCash struct {
	sellerShip
	account, confirmed, matched, line, obligation, payment, finding uuid.UUID
	tracking                                                        string
}

// newCashSellerEnv: newSellerEnv có kho tệp (nhập bảng kê) cho server và module tiền.
func newCashSellerEnv(t *testing.T) *sellerEnv {
	t.Helper()
	cash.RegisterFilePurposes()
	e := newSellerEnv(t)
	store := filestest.MemFileStore(t)
	d := shipDeps(t, e.orderAPIEnv)
	d.Files = store
	d.Cash = cash.New(cash.Deps{DB: e.staff.App, Queue: queue.NewQueue(), Events: events.NewRegistry(), Files: store}).Service
	e.srv, e.files = NewServer(d), store
	return e
}

// cashOf nạp tiền của người bán seller (chủ bảng, trong tenant) vào run đối chiếu run.
func (e *sellerEnv) cashOf(seller, run uuid.UUID, n int) sellerCash {
	e.t.Helper()
	c := sellerCash{sellerShip: e.shipmentOf(seller), account: e.ship.AccountOf(seller), tracking: fmt.Sprintf("NGOAI%d", n)}
	e.staff.Exec(e.t, e.shop.Tenant, func(tx pgx.Tx) error {
		ctx, tenant := e.t.Context(), e.shop.Tenant
		for _, st := range []struct {
			dst    *uuid.UUID
			status string
		}{{&c.confirmed, "confirmed"}, {&c.matched, "matched"}} {
			if err := tx.QueryRow(ctx, `INSERT INTO cash.cod_statements (tenant_id, carrier_key, carrier_account_id, file_id,
				file_sha256, format, status, imported_by, line_count)
				VALUES ($1, $2, $3, gen_random_uuid(), md5(random()::text) || md5(random()::text), 'elimaz_v1', $4, $5, 1)
				RETURNING id`, tenant, shiptest.CarrierKey, c.account, st.status, e.owner).Scan(st.dst); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		if err := tx.QueryRow(ctx, `INSERT INTO cash.cod_statement_lines (tenant_id, statement_id, line_no, tracking_code,
			cod_amount, net_amount, match_status) VALUES ($1, $2, 1, $3, 450000, 420000, 'external') RETURNING id`,
			tenant, c.confirmed, c.tracking).Scan(&c.line); err != nil {
			return err //nolint:wrapcheck // test
		}
		for _, q := range []string{
			`UPDATE fulfillment.shipments SET status = 'delivered', delivered_at = now() - interval '30 days',
				tracking_code = 'GH' || right($2::uuid::text, 12), create_state = 'created'
				WHERE tenant_id = $1 AND id = $2::uuid`,
			// Hãng thu 150.000 rồi trả 50.000: còn giữ 100.000 (cod_open_holdings do trigger của sổ cộng).
			`WITH t AS (INSERT INTO cash.cod_transfers (tenant_id, shipment_id, kind, idempotency_key, actor_type, occurred_at)
				VALUES ($1, $2, 'collected', 'thu-' || $2::uuid::text, 'carrier', now()) RETURNING id)
				INSERT INTO cash.cod_ledger_entries (tenant_id, transfer_id, shipment_id, account, amount)
				SELECT $1, t.id, $2, a, v FROM t, (VALUES ('to_collect', -150000), ('carrier_holding', 150000)) x (a, v)`,
			`WITH t AS (INSERT INTO cash.cod_transfers (tenant_id, shipment_id, kind, idempotency_key, actor_type, occurred_at)
				VALUES ($1, $2, 'remitted', 'tra-' || $2::uuid::text, 'carrier', now()) RETURNING id)
				INSERT INTO cash.cod_ledger_entries (tenant_id, transfer_id, shipment_id, account, amount)
				SELECT $1, t.id, $2, a, v FROM t, (VALUES ('carrier_holding', -50000), ('shop_received', 50000)) x (a, v)`,
		} {
			if _, err := tx.Exec(ctx, q, tenant, c.shipment); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		if err := tx.QueryRow(ctx, `INSERT INTO cash.reconcile_findings (tenant_id, run_id, kind, shipment_id)
			VALUES ($1, $2, 'balance_mismatch', $3) RETURNING id`, tenant, run, c.shipment).Scan(&c.finding); err != nil {
			return err //nolint:wrapcheck // test
		}
		if err := tx.QueryRow(ctx, `INSERT INTO cash.refund_obligations (tenant_id, order_id, source, amount)
			VALUES ($1, $2, 'order_cancel', 2000) RETURNING id`, tenant, c.order).Scan(&c.obligation); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(ctx, `INSERT INTO cash.payments (tenant_id, order_id, method, amount, received_at, actor_id)
			VALUES ($1, $2, 'cash', 1000, now(), $3) RETURNING id`, tenant, c.order, e.owner).Scan(&c.payment)
	})
	return c
}

// reconcileRun chèn lần đối chiếu gần nhất cùng một phát hiện cấp tenant (không vận đơn) và trả mã lần chạy, mã phát hiện.
func (e *sellerEnv) reconcileRun() (run, tenantFinding uuid.UUID) {
	e.t.Helper()
	e.staff.Exec(e.t, e.shop.Tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(e.t.Context(), `INSERT INTO cash.reconcile_runs (tenant_id, ran_at, ok, findings)
			VALUES ($1, now(), false, 3) RETURNING id`, e.shop.Tenant).Scan(&run); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(e.t.Context(), `INSERT INTO cash.reconcile_findings (tenant_id, run_id, kind)
			VALUES ($1, $2, 'ledger_nonzero') RETURNING id`, e.shop.Tenant, run).Scan(&tenantFinding)
	})
	return run, tenantFinding
}

// cashSnapshot: trạng thái, version của hai bảng kê, nghĩa vụ hoàn, số phiếu của đơn, số bảng kê của tài khoản, audit.
func (e *sellerEnv) cashSnapshot(c sellerCash) string {
	e.t.Helper()
	return e.scalar(`SELECT concat_ws('|',
			(SELECT status || ':' || version FROM cash.cod_statements WHERE id = $1),
			(SELECT status || ':' || version FROM cash.cod_statements WHERE id = $2),
			(SELECT status || ':' || paid_amount || ':' || version FROM cash.refund_obligations WHERE id = $3),
			(SELECT count(*) FROM cash.payments WHERE order_id = $4),
			(SELECT count(*) FROM cash.cod_statements WHERE carrier_account_id = $5),
			(SELECT count(*) FROM identity.audit_logs))`,
		c.confirmed, c.matched, c.obligation, c.order, c.account)
}

// TestCashRoutes_OtherSellerNotFound: chủ shop gọi từng route tiền theo mã của người bán PARTNER1 → 404 not_found, tiền
// không đổi; đối chứng: đọc cùng route trên tiền của người bán mặc định → 200.
func TestCashRoutes_OtherSellerNotFound(t *testing.T) {
	t.Parallel()
	e := newCashSellerEnv(t)
	run, _ := e.reconcileRun()
	own, other := e.cashOf(e.shop.Seller, run, 1), e.cashOf(e.shop.Partner, run, 2)
	file := uploadCSV(t, e.staff.App, e.files, e.shop.Tenant, "bk.csv", []byte(statementCSV))
	ob := "/api/v1/refund-obligations/" + other.obligation.String()
	cases := []sellerCall{
		{http.MethodGet, "/api/v1/cod-statements/" + other.confirmed.String(), ""},
		{http.MethodPost, "/api/v1/cod-statements/" + other.matched.String() + "/confirm", `{"version":1}`},
		{http.MethodPost, "/api/v1/cod-statements", importBody(other.account, file, "")},
		{http.MethodGet, "/api/v1/orders/" + other.order.String() + "/payments", ""},
		{http.MethodPost, "/api/v1/orders/" + other.order.String() + "/payments", `{"method":"cash","amount":1000}`},
		{http.MethodPost, ob + "/payments", `{"amount":500}`},
		{http.MethodPost, ob + "/waive", `{"reason":"Khách không nhận"}`},
	}
	e.expectNotFound(100, cases, func() string { return e.cashSnapshot(other) })
	for _, p := range []string{
		"/api/v1/cod-statements/" + own.confirmed.String(), "/api/v1/orders/" + own.order.String() + "/payments",
	} {
		if code, body := e.send(0, http.MethodGet, p, ""); code != http.StatusOK {
			t.Errorf("đối chứng GET %s của người bán mặc định = %d %s, muốn 200", p, code, body)
		}
	}
}

// TestCashLists_OtherSellerHidden: danh sách bảng kê, lệch đối soát (dòng bảng kê, phần còn mở), quá hạn, phát hiện đối
// chiếu, nghĩa vụ hoàn chỉ có tiền của người bán mặc định; phát hiện cấp tenant (không vận đơn) vẫn hiện (QĐ-531-2).
func TestCashLists_OtherSellerHidden(t *testing.T) {
	t.Parallel()
	e := newCashSellerEnv(t)
	run, tenantFinding := e.reconcileRun()
	own, other := e.cashOf(e.shop.Seller, run, 1), e.cashOf(e.shop.Partner, run, 2)
	hidden := []string{
		other.confirmed.String(), other.matched.String(), other.shipment.String(), other.order.String(),
		other.finding.String(), other.obligation.String(), other.line.String(), other.tracking,
	}
	for _, c := range []struct {
		path string
		want []string
	}{
		{"/api/v1/cod-statements", []string{own.confirmed.String(), own.matched.String()}},
		{"/api/v1/cod/discrepancies", []string{`"trackingCode":"` + own.tracking + `"`, `"kind":"open_remainder"`, own.shipment.String()}},
		{"/api/v1/cod/overdue", []string{own.shipment.String()}},
		{"/api/v1/cod/reconcile-findings", []string{own.finding.String(), tenantFinding.String()}},
		{"/api/v1/refund-obligations", []string{own.obligation.String()}},
	} {
		code, body := e.send(0, http.MethodGet, c.path, "")
		if code != http.StatusOK {
			t.Errorf("%s = %d %s", c.path, code, body)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(body, w) {
				t.Errorf("%s thiếu %s của người bán mặc định: %s", c.path, w, body)
			}
		}
		for _, h := range hidden {
			if strings.Contains(body, h) {
				t.Errorf("%s có %s của người bán khác: %s", c.path, h, body)
			}
		}
	}
}
