package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Người dùng của Task M2-19: vai trò mẫu sale (order.confirm, không order.discount.approve) và manager.
const (
	confirmSale    = "ban-hang-m219@elimaz.test"
	confirmManager = "quan-ly-m219@elimaz.test"
)

// newOrderConfirmEnv là orderAPIEnv có danh mục, cài đặt (m2Prepare) và hai người dùng sale, manager.
func newOrderConfirmEnv(t *testing.T) *orderAPIEnv {
	t.Helper()
	e := newOrderAPIEnv(t)
	e.m2Prepare()
	for email, role := range map[string]string{confirmSale: "sale", confirmManager: "manager"} {
		e.staff.Grant(t, e.shop.Tenant, e.staff.User(t, e.shop.Tenant, email, "active"), rolePerms(t, role), "all", "")
	}
	return e
}

// orderAction gửi POST /api/v1/orders/{id}/{action} của người email, khóa key, thân body.
func (e *orderAPIEnv) orderAction(email, key string, id any, action, body string) *httptest.ResponseRecorder {
	return serveAs(e.srv, e.keys, http.MethodPost, fmt.Sprintf("/api/v1/orders/%v/%s", id, action), email, key, body)
}

func versionBody(v any) string { return fmt.Sprintf(`{"version":%v}`, v) }

// mustAction đòi 200 và trả thân đơn.
func (e *orderAPIEnv) mustAction(email, key string, id any, action, body string) map[string]any {
	e.t.Helper()
	rec := e.orderAction(email, key, id, action, body)
	if rec.Code != http.StatusOK {
		e.t.Fatalf("%s: %d %s", action, rec.Code, rec.Body.String())
	}
	return decodeJSON(e.t, rec)
}

// new → confirm → 200, lịch sử new → confirmed, sự kiện order.confirmed seq = version, audit order.confirm; version cũ
// → 409 version_conflict; đơn không có → 404.
func TestOrderConfirm_New(t *testing.T) {
	e := newOrderConfirmEnv(t)
	v := e.variant(e.shop.Seller, 250000, 5)
	o := e.mustCreate("c-new", m2Body("new", fullAddr, 0, m2Line(v, 1, 0)))
	expect(t, "version cũ", e.orderAction(confirmSale, "k0", o["id"], "confirm", versionBody(0)),
		http.StatusConflict, "version_conflict")
	got := e.mustAction(confirmSale, "k1", o["id"], "confirm", versionBody(o["version"]))
	if got["status"] != "confirmed" || got["label"] != "confirmed" || got["version"] != float64(2) ||
		got["confirmedAt"] == nil {
		t.Errorf("sau confirm: %v", got)
	}
	if n := e.count(`SELECT count(*) FROM ord.order_status_history WHERE order_id = $1 AND from_status = 'new'
		AND to_status = 'confirmed'`, o["id"]); n != 1 {
		t.Errorf("lịch sử new → confirmed: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE aggregate_id = $1::text AND topic = 'order.confirmed'
		AND seq = 2`, o["id"]); n != 1 {
		t.Errorf("order.confirmed seq 2: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'order.confirm' AND entity_id = $1::text
		AND after->>'status' = 'confirmed' AND after::text NOT LIKE '%901234567%' AND after::text NOT LIKE '%Lê Lợi%'`,
		o["id"]); n != 1 {
		t.Errorf("audit order.confirm: %d", n)
	}
	expect(t, "đơn không có", e.orderAction(confirmSale, "k2", "0193a000-0000-7000-8000-000000000000", "confirm",
		versionBody(1)), http.StatusNotFound, "not_found")
	expect(t, "chỉ xem", e.orderAction(orderViewer, "k3", o["id"], "confirm", versionBody(2)), http.StatusForbidden, "")
}

// DH16: đơn chờ duyệt; sale confirm → 422 needs_approval; sale approve-discount → 403; manager approve-discount →
// confirmed, lịch sử có lý do, audit có discountApprovedBy.
func TestOrderConfirm_DiscountApproval(t *testing.T) {
	e := newOrderConfirmEnv(t)
	v := e.variant(e.shop.Seller, 1000000, 5)
	o := e.mustCreate("c-dh16", withFields(m2Body("confirmed", fullAddr, 0, m2Line(v, 1, 0)), `"orderDiscount":100001`))
	if o["status"] != "new" || o["pendingReason"] != "discount_approval" {
		t.Fatalf("đơn chờ duyệt: %v", o)
	}
	expect(t, "sale confirm", e.orderAction(confirmSale, "a1", o["id"], "confirm", versionBody(1)),
		http.StatusUnprocessableEntity, "needs_approval")
	expect(t, "sale approve", e.orderAction(confirmSale, "a2", o["id"], "approve-discount", versionBody(1)),
		http.StatusForbidden, "")
	expect(t, "sale đặt chờ khách", e.orderAction(confirmSale, "a3", o["id"], "pending",
		`{"version":1,"reason":"awaiting_customer"}`), http.StatusUnprocessableEntity, "needs_approval")
	got := e.mustAction(confirmManager, "a4", o["id"], "approve-discount", versionBody(1))
	if got["status"] != "confirmed" || got["pendingReason"] != nil || got["version"] != float64(3) {
		t.Errorf("sau duyệt: %v", got)
	}
	if n := e.count(`SELECT count(*) FROM ord.order_status_history WHERE order_id = $1 AND reason = 'discount_approved'
		AND from_pending = 'discount_approval'`, o["id"]); n != 1 {
		t.Errorf("lịch sử duyệt: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs a JOIN ord.orders o ON o.id::text = a.entity_id
		WHERE a.action = 'order.approve_discount' AND a.entity_id = $1::text
		AND a.after->>'discountApprovedBy' = o.discount_approved_by::text AND a.after->>'status' = 'confirmed'`,
		o["id"]); n != 1 {
		t.Errorf("audit order.approve_discount có discountApprovedBy: %d", n)
	}
}

// Chốt nháp: thiếu phường → 422 address_incomplete; nháp khác có địa chỉ đủ → confirmed, draftExpiresAt null.
func TestOrderConfirm_Draft(t *testing.T) {
	e := newOrderConfirmEnv(t)
	v := e.variant(e.shop.Seller, 250000, 5)
	noWard := strings.Replace(fullAddr, `,"wardCode":"T01001"`, "", 1)
	d1 := e.mustCreate("d1", m2Body("draft", noWard, 0, m2Line(v, 1, 0)))
	expect(t, "nháp thiếu phường", e.orderAction(confirmSale, "d1c", d1["id"], "confirm", versionBody(d1["version"])),
		http.StatusUnprocessableEntity, "address_incomplete")
	d2 := e.mustCreate("d2", m2Body("draft", fullAddr, 0, m2Line(v, 1, 0)))
	if d2["draftExpiresAt"] == nil {
		t.Fatalf("nháp không có hạn: %v", d2)
	}
	got := e.mustAction(confirmSale, "d2c", d2["id"], "confirm", versionBody(d2["version"]))
	if got["status"] != "confirmed" || got["draftExpiresAt"] != nil {
		t.Errorf("sau chốt nháp: %v", got)
	}
}

// setOrderPending awaiting_customer → label awaiting_customer; confirm → confirmed; pending trên đơn confirmed → 422
// invalid_transition; confirm đơn cancelled → 422 invalid_transition.
func TestOrderConfirm_Pending(t *testing.T) {
	e := newOrderConfirmEnv(t)
	v := e.variant(e.shop.Seller, 250000, 5)
	o := e.mustCreate("p1", m2Body("new", fullAddr, 0, m2Line(v, 1, 0)))
	p := e.mustAction(confirmSale, "p1a", o["id"], "pending", `{"version":1,"reason":"awaiting_customer"}`)
	if p["status"] != "new" || p["label"] != "awaiting_customer" || p["pendingReason"] != "awaiting_customer" {
		t.Errorf("chờ khách: %v", p)
	}
	c := e.mustAction(confirmSale, "p1b", o["id"], "confirm", versionBody(p["version"]))
	if c["status"] != "confirmed" {
		t.Errorf("confirm sau chờ khách: %v", c)
	}
	expect(t, "pending trên confirmed", e.orderAction(confirmSale, "p1c", o["id"], "pending",
		fmt.Sprintf(`{"version":%v,"reason":"out_of_stock"}`, c["version"])), http.StatusUnprocessableEntity,
		"invalid_transition")

	// Hủy đơn qua API có ở Task M2-21: test đặt thẳng trạng thái bằng chủ bảng.
	x := e.mustCreate("p2", m2Body("new", fullAddr, 0, m2Line(v, 1, 0)))
	e.staff.Exec(t, e.shop.Tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE ord.orders SET status = 'cancelled', version = version + 1 WHERE id = $1`, x["id"])
		return err //nolint:wrapcheck // test
	})
	expect(t, "confirm đơn hủy", e.orderAction(confirmSale, "p2c", x["id"], "confirm", versionBody(2)),
		http.StatusUnprocessableEntity, "invalid_transition")
}

// 50 confirm song song cùng khóa → 1 dòng lịch sử new → confirmed (200 hoặc 409 request_in_progress); hai khóa khác,
// cùng version → một 200, một 409 version_conflict.
func TestOrderConfirm_Parallel(t *testing.T) {
	e := newOrderConfirmEnv(t)
	v := e.variant(e.shop.Seller, 250000, 10)
	o := e.mustCreate("r1", m2Body("new", fullAddr, 0, m2Line(v, 1, 0)))
	recs := make([]*httptest.ResponseRecorder, 50)
	var wg sync.WaitGroup
	for i := range recs {
		wg.Go(func() { recs[i] = e.orderAction(confirmSale, "same", o["id"], "confirm", versionBody(1)) })
	}
	wg.Wait()
	ok := 0
	for _, rec := range recs {
		if rec.Code == http.StatusOK {
			ok++
		} else if rec.Code != http.StatusConflict || errCode(t, rec) != "request_in_progress" {
			t.Errorf("cùng khóa chỉ được 200 hoặc 409 request_in_progress: %d %s", rec.Code, rec.Body.String())
		}
	}
	if n := e.count(`SELECT count(*) FROM ord.order_status_history WHERE order_id = $1 AND to_status = 'confirmed'`,
		o["id"]); ok == 0 || n != 1 {
		t.Errorf("50 cùng khóa: %d lần 200, %d dòng lịch sử, mong ≥ 1 và 1", ok, n)
	}

	o2 := e.mustCreate("r2", m2Body("new", fullAddr, 0, m2Line(v, 1, 0)))
	var a, b *httptest.ResponseRecorder
	wg.Go(func() { a = e.orderAction(confirmSale, "ka", o2["id"], "confirm", versionBody(1)) })
	wg.Go(func() { b = e.orderAction(confirmManager, "kb", o2["id"], "confirm", versionBody(1)) })
	wg.Wait()
	codes := map[int]int{a.Code: 1}
	codes[b.Code]++
	if codes[http.StatusOK] != 1 || codes[http.StatusConflict] != 1 {
		t.Fatalf("hai khóa cùng version: %d %s / %d %s", a.Code, a.Body.String(), b.Code, b.Body.String())
	}
	for _, rec := range []*httptest.ResponseRecorder{a, b} {
		if rec.Code == http.StatusConflict {
			if c := errCode(t, rec); c != "version_conflict" {
				t.Errorf("409 phải version_conflict, có %q", c)
			}
		}
	}
}
