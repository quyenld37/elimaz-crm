package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
)

// exchangeOrder biến đơn confirmed replacement thành đơn đổi của phiếu đổi (chủ bảng) có tiền hoàn refund trên đơn
// gốc orig, như CreateReturn kind exchange của M2 (luật đủ ca, đường thật ở cash/exchange_pending_test.go); trả mã
// phiếu.
func (e *shipAPIEnv) exchangeOrder(orig, replacement string, refund int64) string {
	e.t.Helper()
	var rid string
	e.staff.Exec(e.t, e.shop.Tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(e.t.Context(), `INSERT INTO ord.returns (tenant_id, code, order_id, kind, refund_amount,
			replacement_order_id) VALUES ($1, 'TH0000524', $2, 'exchange', $3, $4) RETURNING id::text`,
			e.shop.Tenant, orig, refund, replacement).Scan(&rid); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(e.t.Context(), `UPDATE ord.orders SET order_type = 'exchange', replaced_return_id = $2
			WHERE id = $1`, replacement, rid)
		return err //nolint:wrapcheck // test
	})
	return rid
}

// Task 524: đơn đổi chưa có phiếu cấn trừ → 409 exchange_credit_pending, không vận đơn, không lệnh gửi hãng, không lưu
// theo khóa (gửi lại cùng khóa sau khi có phiếu → 202); skipExchangeCredit → 202, audit ghi cờ; đơn thường không bị
// chặn.
func TestCreateShipmentAPI_ExchangeCreditPending(t *testing.T) {
	t.Parallel()
	e := newShipAPIEnv(t)
	orig, repl := e.confirmedOrder("o1"), e.confirmedOrder("o2")
	rid := e.exchangeOrder(orig, repl, 100000)
	path := "/api/v1/orders/" + repl + "/shipments"
	body := fmt.Sprintf(`{"carrierAccountId":"%s"}`, e.accounts[0])
	if code, b := e.post(path, "k1", body); code != http.StatusConflict || b["error"] != "exchange_credit_pending" {
		t.Fatalf("đơn đổi chưa cấn trừ = %d %v, muốn 409 exchange_credit_pending", code, b)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.shipments"); n != 0 {
		t.Fatalf("vận đơn = %d, muốn 0", n)
	}
	if n := e.count("SELECT count(*) FROM platform.jobs WHERE kind LIKE '%create_shipment%'"); n != 0 {
		t.Fatalf("lệnh gửi hãng = %d, muốn 0", n)
	}
	// Phiếu cấn trừ có (như consumer cash_refunds ghi) → gửi lại cùng khóa qua.
	e.staff.Exec(t, e.shop.Tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO cash.payments (tenant_id, order_id, method, amount, reference,
			received_at, actor_id) VALUES ($1, $2, 'exchange_credit', 100000, 'exchange:' || $3, now(), $4)`,
			e.shop.Tenant, repl, rid, "00000000-0000-0000-0000-000000000000")
		return err //nolint:wrapcheck // test
	})
	if code, b := e.post(path, "k1", body); code != http.StatusAccepted {
		t.Fatalf("sau khi có phiếu cấn trừ = %d %v, muốn 202", code, b)
	}
	if n := e.count("SELECT count(*) FROM platform.jobs WHERE kind LIKE '%create_shipment%'"); n != 1 {
		t.Fatalf("lệnh gửi hãng sau 202 = %d, muốn 1", n)
	}

	// Đơn đổi thứ hai: bỏ qua có chủ ý.
	orig2, repl2 := e.confirmedOrder("o3"), e.confirmedOrder("o4")
	e.staff.Exec(t, e.shop.Tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO ord.returns (tenant_id, code, order_id, kind, refund_amount,
			replacement_order_id) VALUES ($1, 'TH0000525', $2, 'exchange', 50000, $3)`, e.shop.Tenant, orig2, repl2)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err = tx.Exec(t.Context(), "UPDATE ord.orders SET order_type = 'exchange' WHERE id = $1", repl2)
		return err //nolint:wrapcheck // test
	})
	skip := fmt.Sprintf(`{"carrierAccountId":"%s","skipExchangeCredit":true}`, e.accounts[0])
	code, s := e.post("/api/v1/orders/"+repl2+"/shipments", "k2", skip)
	if code != http.StatusAccepted || s["codAmount"] != float64(280000) {
		t.Fatalf("bỏ qua cấn trừ = %d %v, muốn 202 COD đủ 280.000", code, s)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'fulfillment.shipment.create'
		AND (after->>'skipExchangeCredit')::boolean`); n != 1 {
		t.Fatalf("audit có cờ bỏ qua = %d, muốn 1", n)
	}
	// Đơn thường (đơn gốc) không bị chặn.
	if code, b := e.post("/api/v1/orders/"+orig+"/shipments", "k3", body); code != http.StatusAccepted {
		t.Fatalf("đơn thường = %d %v, muốn 202", code, b)
	}
}
