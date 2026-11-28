package httpapi

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// payOrder tạo đơn 2 × 300.000 + phí ship 30.000 = 630.000 (confirmed) của người bán ELIMAZ trong shop.
func (e *m3Env) payOrder(shop *shiptest.Shop, user uuid.UUID) order.Order {
	e.t.Helper()
	var o order.Order
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		v := shop.Variant(e.t, tx, shop.Seller, 300000, 10)
		var err error
		o, err = shop.Order(e.t.Context(), tx, shop.Seller, user, 30000, order.ItemInput{VariantID: v, Qty: 2})
		return err //nolint:wrapcheck // test
	})
	return o
}

// cancelAndRefund hủy đơn o (order.Writer.Cancel) rồi giao order.cancelled cho consumer cash_refunds.
func (e *m3Env) cancelAndRefund(shop *shiptest.Shop, user uuid.UUID, o order.Order) {
	e.t.Helper()
	ctx := e.t.Context()
	w := order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &realtimetest.RecordingBus{}}
	var ev events.StoredEvent
	if err := tenancy.WithTenant(ctx, e.staff.App, e.tenant, func(tx pgx.Tx) error {
		cur, err := order.Get(ctx, tx, shop.Seller, o.ID)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		if _, err := w.Cancel(ctx, tx, shop.Seller, o.ID, cur.Version, order.CancelCustomerChangedMind, "",
			order.Actor{TenantID: e.tenant, UserID: user}, time.Now().UTC()); err != nil {
			return err //nolint:wrapcheck // test
		}
		ev = events.StoredEvent{TenantID: e.tenant, AggregateType: "order", AggregateID: o.ID.String(), Topic: "order.cancelled"}
		return tx.QueryRow(ctx, `SELECT id, seq, payload FROM platform.domain_events WHERE aggregate_type = 'order'
			AND aggregate_id = $1 AND topic = 'order.cancelled'`, o.ID.String()).Scan(&ev.ID, &ev.Seq, &ev.Payload)
	}); err != nil {
		e.t.Fatalf("hủy đơn: %v", err)
	}
	for _, c := range e.cash.Consumers() {
		if c.Name() != cash.RefundsConsumerName {
			continue
		}
		if err := tenancy.WithTenant(ctx, e.staff.App, e.tenant, func(tx pgx.Tx) error {
			return c.Handle(ctx, ev, tx)
		}); err != nil {
			e.t.Fatalf("giao order.cancelled: %v", err)
		}
	}
}

// auditAfter đọc after->>key của dòng audit action của thực thể id (chủ bảng); không có → "".
func (e *m3Env) auditAfter(action, id, key string) (n int, val string) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT count(*), coalesce(max(after->>$3), '') FROM identity.audit_logs
			WHERE action = $1 AND entity_id = $2`, action, id, key).Scan(&n, &val)
	})
	return n, val
}

// Phiếu thu qua HTTP (Task M3-30, Q-17): kế toán ghi cọc 200.000 → 201 kèm người ghi, audit cash.payment.record;
// danh sách phiếu của đơn có phiếu; vượt tổng đơn → 409 payment_exceeds_total; đơn lạ → 404; con trỏ sai → 400; Kho
// (không có cash.payments.record) → 403; đơn đã gửi hãng → 409 cod_locked_by_shipment.
func TestPaymentsAPI_Record(t *testing.T) {
	t.Parallel()
	e := newM3Env(t)
	shop, user := e.newCodShop()
	o := e.payOrder(shop, user)
	path := "/api/v1/orders/" + o.ID.String() + "/payments"
	code, got := e.send(http.MethodPost, path, m3Accounter, `{"method":"deposit","amount":200000,"reference":"CK1"}`)
	actor, _ := got["actor"].(map[string]any)
	if code != http.StatusCreated || got["amount"] != float64(200000) || got["method"] != "deposit" ||
		got["reference"] != "CK1" || actor["id"] == nil {
		t.Fatalf("ghi cọc: %d %v", code, got)
	}
	id, _ := got["id"].(string)
	if n, method := e.auditAfter("cash.payment.record", id, "method"); n != 1 || method != "deposit" {
		t.Errorf("audit: %d dòng, method %q", n, method)
	}
	code, page := e.send(http.MethodGet, path+"?limit=10", m3Accounter, "")
	items, _ := page["items"].([]any)
	if code != http.StatusOK || len(items) != 1 {
		t.Errorf("danh sách: %d %v", code, page)
	}
	for _, c := range []struct {
		name, method, path, email, body, code string
		status                                int
	}{
		{"vượt tổng", http.MethodPost, path, m3Accounter, `{"method":"cash","amount":430001}`, "payment_exceeds_total", http.StatusConflict},
		{"đơn lạ", http.MethodPost, "/api/v1/orders/" + uuid.NewString() + "/payments", m3Accounter, `{"method":"cash","amount":1}`, "not_found", http.StatusNotFound},
		{"đơn lạ (đọc)", http.MethodGet, "/api/v1/orders/" + uuid.NewString() + "/payments", m3Accounter, "", "not_found", http.StatusNotFound},
		{"con trỏ sai", http.MethodGet, path + "?cursor=x", m3Accounter, "", "invalid_input", http.StatusBadRequest},
		{"Kho", http.MethodPost, path, m3Warehouse, `{"method":"cash","amount":1}`, "forbidden", http.StatusForbidden},
	} {
		if code, b := e.send(c.method, c.path, c.email, c.body); code != c.status || b["error"] != c.code {
			t.Errorf("%s: %d %v, mong %d %s", c.name, code, b, c.status, c.code)
		}
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := shop.Shipment(t.Context(), tx, shop.Seller, o.ID, user)
		return err //nolint:wrapcheck // test
	})
	if code, b := e.send(http.MethodPost, path, m3Accounter, `{"method":"bank_transfer","amount":1000}`); code != http.StatusConflict ||
		b["error"] != "cod_locked_by_shipment" {
		t.Errorf("sau khi gửi hãng: %d %v", code, b)
	}
}

// Nghĩa vụ hoàn qua HTTP (TI09, Q-2): đơn có cọc 200.000 bị hủy → danh sách mở có nghĩa vụ kèm mã đơn; chi 250.000 →
// 409 refund_exceeds_remaining; chi 150.000 → 200 paidAmount 150.000, audit cash.refund.pay; Kế toán miễn → 403; Chủ
// miễn lý do khoảng trắng → 422 invalid_input, lý do rỗng → 400; có lý do → 200 waived, audit có lý do; chi sau khi
// miễn → 409 invalid_transition; nghĩa vụ lạ → 404; trạng thái lọc lạ → 400.
func TestPaymentsAPI_RefundObligations(t *testing.T) {
	t.Parallel()
	e := newM3Env(t)
	shop, user := e.newCodShop()
	o := e.payOrder(shop, user)
	if code, b := e.send(http.MethodPost, "/api/v1/orders/"+o.ID.String()+"/payments", m3Accounter,
		`{"method":"deposit","amount":200000}`); code != http.StatusCreated {
		t.Fatalf("ghi cọc: %d %v", code, b)
	}
	e.cancelAndRefund(shop, user, o)
	code, page := e.send(http.MethodGet, "/api/v1/refund-obligations?status=open", m3Accounter, "")
	items, _ := page["items"].([]any)
	if code != http.StatusOK || len(items) != 1 {
		t.Fatalf("danh sách mở: %d %v", code, page)
	}
	ob, _ := items[0].(map[string]any)
	if ob["orderCode"] != o.Code || ob["source"] != "order_cancel" || ob["amount"] != float64(200000) || ob["status"] != "open" {
		t.Errorf("nghĩa vụ: %v", ob)
	}
	id, _ := ob["id"].(string)
	// Lọc theo đơn (Task 524): đúng đơn → nghĩa vụ đó; đơn khác → rỗng; orderId sai dạng → 400.
	if code, b := e.send(http.MethodGet, "/api/v1/refund-obligations?orderId="+o.ID.String(), m3Accounter, ""); code != http.StatusOK ||
		len(b["items"].([]any)) != 1 || b["items"].([]any)[0].(map[string]any)["id"] != id { //nolint:forcetypeassert // test
		t.Errorf("lọc theo đơn: %d %v", code, b)
	}
	if code, b := e.send(http.MethodGet, "/api/v1/refund-obligations?orderId="+uuid.NewString(), m3Accounter, ""); code != http.StatusOK ||
		len(b["items"].([]any)) != 0 { //nolint:forcetypeassert // test
		t.Errorf("lọc đơn khác: %d %v", code, b)
	}
	if code, b := e.send(http.MethodGet, "/api/v1/refund-obligations?orderId=x", m3Accounter, ""); code != http.StatusBadRequest {
		t.Errorf("orderId sai dạng: %d %v", code, b)
	}
	pay := "/api/v1/refund-obligations/" + id + "/payments"
	if code, b := e.send(http.MethodPost, pay, m3Accounter, `{"amount":250000}`); code != http.StatusConflict ||
		b["error"] != "refund_exceeds_remaining" {
		t.Errorf("chi vượt: %d %v", code, b)
	}
	code, got := e.send(http.MethodPost, pay, m3Accounter, `{"amount":150000,"reference":"CK-HOAN"}`)
	if code != http.StatusOK || got["paidAmount"] != float64(150000) || got["status"] != "open" {
		t.Errorf("chi 150.000: %d %v", code, got)
	}
	if n, amount := e.auditAfter("cash.refund.pay", id, "amount"); n != 1 || amount != "150000" {
		t.Errorf("audit chi: %d dòng, amount %q", n, amount)
	}
	checkWaiveAPI(t, e, id, pay)
}

// checkWaiveAPI: Kế toán miễn → 403; lý do khoảng trắng → 422, rỗng → 400; Chủ miễn có lý do → waived, audit có lý
// do; chi sau khi miễn → 409; nghĩa vụ lạ → 404; lọc lạ → 400; danh sách đã miễn có nghĩa vụ.
func checkWaiveAPI(t *testing.T, e *m3Env, id, pay string) {
	t.Helper()
	waive := "/api/v1/refund-obligations/" + id + "/waive"
	for _, c := range []struct {
		name, email, body, code string
		status                  int
	}{
		{"Kế toán miễn", m3Accounter, `{"reason":"x"}`, "forbidden", http.StatusForbidden},
		{"lý do khoảng trắng", m3Owner, `{"reason":"   "}`, "invalid_input", http.StatusUnprocessableEntity},
		{"lý do rỗng", m3Owner, `{"reason":""}`, "bad_request", http.StatusBadRequest},
	} {
		if code, b := e.send(http.MethodPost, waive, c.email, c.body); code != c.status || b["error"] != c.code {
			t.Errorf("%s: %d %v, mong %d %s", c.name, code, b, c.status, c.code)
		}
	}
	code, got := e.send(http.MethodPost, waive, m3Owner, `{"reason":"Khách đồng ý mất phần cọc còn lại"}`)
	if code != http.StatusOK || got["status"] != "waived" || got["waiveReason"] != "Khách đồng ý mất phần cọc còn lại" {
		t.Errorf("miễn: %d %v", code, got)
	}
	if n, reason := e.auditAfter("cash.refund.waive", id, "reason"); n != 1 || reason != "Khách đồng ý mất phần cọc còn lại" {
		t.Errorf("audit miễn: %d dòng, lý do %q", n, reason)
	}
	for _, c := range []struct {
		name, method, path, body, code string
		status                         int
	}{
		{"chi sau khi miễn", http.MethodPost, pay, `{"amount":1}`, "invalid_transition", http.StatusConflict},
		{"nghĩa vụ lạ", http.MethodPost, "/api/v1/refund-obligations/" + uuid.NewString() + "/payments", `{"amount":1}`, "not_found", http.StatusNotFound},
		{"lọc lạ", http.MethodGet, "/api/v1/refund-obligations?status=x", "", "bad_request", http.StatusBadRequest},
	} {
		if code, b := e.send(c.method, c.path, m3Owner, c.body); code != c.status || b["error"] != c.code {
			t.Errorf("%s: %d %v, mong %d %s", c.name, code, b, c.status, c.code)
		}
	}
	if code, b := e.send(http.MethodGet, fmt.Sprintf("/api/v1/refund-obligations?status=waived&limit=%d", 5), m3Accounter, ""); code != http.StatusOK ||
		len(b["items"].([]any)) != 1 { //nolint:forcetypeassert // test
		t.Errorf("danh sách đã miễn: %d %v", code, b)
	}
}
