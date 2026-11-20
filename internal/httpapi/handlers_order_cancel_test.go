package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

// orderCanceller có order.read, order.cancel (không order.create).
const orderCanceller = "huy-don@elimaz.test"

func (e *orderAPIEnv) grantCanceller() {
	e.t.Helper()
	u := e.staff.User(e.t, e.shop.Tenant, orderCanceller, "active")
	e.staff.Grant(e.t, e.shop.Tenant, u, []string{"order.read", "order.cancel"}, "all", "")
}

// newCancelAPIEnv: orderAPIEnv có danh mục hành chính, cài đặt đơn, người hủy đơn, mã SALE10 còn hạn và biến thể giá
// 250.000 tồn onHand.
func newCancelAPIEnv(t *testing.T, onHand int32) (*orderAPIEnv, uuid.UUID) {
	t.Helper()
	e := newOrderAPIEnv(t)
	e.m2Prepare()
	e.grantCanceller()
	e.couponNow("SALE10", 0, -24, 24)
	return e, e.variant(e.shop.Seller, 250000, onHand)
}

func (e *orderAPIEnv) cancelOrder(email, key string, id any, version any, reason, note string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"version":%v,"reason":"%s"`, version, reason)
	if note != "" {
		body += `,"note":"` + note + `"`
	}
	return serveAs(e.srv, e.keys, http.MethodPost, fmt.Sprintf("/api/v1/orders/%v/cancel", id), email, key, body+"}")
}

// deliver gắn vận đơn mới vào đơn id rồi báo giao xong (đường của M3, gọi thẳng module).
func (e *orderAPIEnv) deliver(id uuid.UUID) {
	e.t.Helper()
	w := order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}
	s := uuid.New()
	e.staff.Exec(e.t, e.shop.Tenant, func(tx pgx.Tx) error {
		testkit.StubShipment(e.t, tx, id, s)
		a := order.Actor{TenantID: e.shop.Tenant, Type: "carrier"}
		if err := w.AttachShipmentBy(e.t.Context(), tx, e.shop.Seller, id, s, a, time.Now().UTC()); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := w.OnShipmentEvent(e.t.Context(), tx, e.shop.Seller, id, s, order.StepDelivered, a, time.Now().UTC())
		return err //nolint:wrapcheck // test
	})
}

// cancelOrder qua API: 200 cancelled (nhãn cancelled), hàng nhả, lượt mã trả, audit order.cancel không có nội dung ghi
// chú; version cũ → 409 version_conflict; đơn delivered → 422 invalid_transition; lý do hệ thống (draft_expired) → 400;
// không quyền order.cancel → 403; đơn không có → 404.
func TestOrderAPI_Cancel(t *testing.T) {
	e, v := newCancelAPIEnv(t, 3)
	o := e.mustCreate("c1", withFields(m2Body("confirmed", fullAddr, 0, m2Line(v, 2, 0)), `"couponCode":"SALE10"`))
	expect(t, "version cũ", e.cancelOrder(orderCanceller, "x0", o["id"], 1, "other", ""), http.StatusConflict,
		"version_conflict")
	expect(t, "lý do hệ thống", e.cancelOrder(orderCanceller, "x1", o["id"], o["version"], "draft_expired", ""),
		http.StatusBadRequest, "bad_request")
	expect(t, "không quyền", e.cancelOrder(orderViewer, "x2", o["id"], o["version"], "other", ""), http.StatusForbidden,
		"forbidden")
	expect(t, "đơn không có", e.cancelOrder(orderCanceller, "x3", uuid.New(), 1, "other", ""), http.StatusNotFound,
		"not_found")
	rec := e.cancelOrder(orderCanceller, "x4", o["id"], o["version"], "wrong_info", "sai số nhà 12")
	got := decodeJSON(t, rec)
	if rec.Code != http.StatusOK || got["status"] != "cancelled" || got["label"] != "cancelled" ||
		got["cancelledAt"] == nil || got["version"] != o["version"].(float64)+1 {
		t.Fatalf("hủy: %d %s", rec.Code, rec.Body.String())
	}
	if n := e.count(`SELECT held FROM inventory.stock_available_v WHERE variant_id = $1`, v); n != 0 {
		t.Errorf("held sau hủy = %d", n)
	}
	if n := e.count(`SELECT used_count FROM promotion.coupons WHERE code = 'SALE10'`); n != 0 {
		t.Errorf("used_count sau hủy = %d", n)
	}
	if n := e.count(`SELECT count(*) FROM ord.orders WHERE id = $1 AND cancel_reason = 'wrong_info'
		AND cancel_note = 'sai số nhà 12'`, o["id"]); n != 1 {
		t.Errorf("lý do, ghi chú hủy chưa ghi")
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'order.cancel' AND entity_id = $1
		AND before->>'status' = 'confirmed' AND after->>'status' = 'cancelled' AND after->>'cancelReason' = 'wrong_info'
		AND after::text NOT LIKE '%số nhà%'`, o["id"]); n != 1 {
		t.Errorf("audit order.cancel: %d dòng", n)
	}

	d := e.mustCreate("d1", m2Body("confirmed", fullAddr, 0, m2Line(e.variant(e.shop.Seller, 1000, 5), 1, 0)))
	e.deliver(uuid.MustParse(d["id"].(string)))
	expect(t, "đơn delivered", e.cancelOrder(orderCanceller, "x5", d["id"], d["version"].(float64)+2, "other", ""),
		http.StatusUnprocessableEntity, "invalid_transition")
}

// 50 lần hủy song song cùng Idempotency-Key → một lần hủy thật: 1 dòng lịch sử sang cancelled, used_count giảm đúng 1;
// mọi câu trả lời 200 cùng thân hoặc 409 request_in_progress.
func TestOrderAPI_CancelSameKey50(t *testing.T) {
	e, v := newCancelAPIEnv(t, 100)
	body := withFields(m2Body("confirmed", fullAddr, 0, m2Line(v, 1, 0)), `"couponCode":"SALE10"`)
	o := e.mustCreate("c1", body)
	e.mustCreate("c2", body) // lượt thứ hai của mã: không bị đụng
	recs := make([]*httptest.ResponseRecorder, 50)
	var wg sync.WaitGroup
	for i := range recs {
		wg.Go(func() { recs[i] = e.cancelOrder(orderCanceller, "k-cancel", o["id"], o["version"], "other", "") })
	}
	wg.Wait()
	bodies := map[string]int{}
	for _, r := range recs {
		if r.Code == http.StatusConflict && decodeErr(t, r).Error == "request_in_progress" {
			continue
		}
		if r.Code != http.StatusOK {
			t.Errorf("mã lạ %d %s", r.Code, r.Body.String())
		}
		bodies[r.Body.String()]++
	}
	if len(bodies) != 1 {
		t.Fatalf("mong đúng một thân 200 (mọi lần lặp lại cùng thân), có %d: %v", len(bodies), bodies)
	}
	if n := e.count(`SELECT count(*) FROM ord.order_status_history WHERE order_id = $1 AND to_status = 'cancelled'`,
		o["id"]); n != 1 {
		t.Errorf("%d dòng lịch sử sang cancelled, mong 1", n)
	}
	if n := e.count(`SELECT used_count FROM promotion.coupons WHERE code = 'SALE10'`); n != 1 {
		t.Errorf("used_count = %d, mong 1", n)
	}
}
