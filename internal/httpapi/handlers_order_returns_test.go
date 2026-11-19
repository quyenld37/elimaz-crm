package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// Nhân viên của test phiếu trả: bán hàng (quyền như vai trò sale: đọc, tạo phiếu, không nhận hàng) và kho (đọc, nhận).
const (
	returnSale      = "ban-hang-tra@elimaz.test"
	returnWarehouse = "kho-tra@elimaz.test"
)

func newReturnAPIEnv(t *testing.T) *orderAPIEnv {
	t.Helper()
	e := newOrderAPIEnv(t)
	e.m2Prepare()
	s := e.staff.User(t, e.shop.Tenant, returnSale, "active")
	e.staff.Grant(t, e.shop.Tenant, s, []string{"order.read", "order.create", "order.return"}, "all", "")
	w := e.staff.User(t, e.shop.Tenant, returnWarehouse, "active")
	e.staff.Grant(t, e.shop.Tenant, w, []string{"order.read", "order.return.receive"}, "all", "")
	return e
}

func (e *orderAPIEnv) returnPost(email, key, path, body string) *httptest.ResponseRecorder {
	return serveAs(e.srv, e.keys, http.MethodPost, path, email, key, body)
}

// deliveredAPIOrder tạo đơn confirmed qua API (V ×1 giá 250.000) rồi giao (đường của M3); trả thân đơn đọc lại.
func (e *orderAPIEnv) deliveredAPIOrder(key string, v uuid.UUID) map[string]any {
	e.t.Helper()
	o := e.mustCreate(key, m2Body("confirmed", fullAddr, 0, m2Line(v, 1, 0)))
	id := uuid.MustParse(o["id"].(string))
	e.deliver(id)
	rec := serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/orders/"+id.String(), orderViewer, "", "")
	if rec.Code != http.StatusOK {
		e.t.Fatalf("getOrder: %d %s", rec.Code, rec.Body.String())
	}
	return decodeJSON(e.t, rec)
}

func firstItem(o map[string]any) string {
	return o["items"].([]any)[0].(map[string]any)["id"].(string)
}

// Luồng phiếu trả qua API: tạo 201 requested (refundAmount 250.000, chưa có condition); lần hai cùng dòng → 422
// return_qty_exceeds; đơn chưa giao → 422 return_not_allowed; bán hàng nhận → 403; kho nhận → 200 restocked (condition
// sellable), tồn +1; đóng → closed; get, list; phiếu không có → 404; audit không có lý do trả.
func TestReturnAPI_Flow(t *testing.T) {
	e := newReturnAPIEnv(t)
	v := e.variant(e.shop.Seller, 250000, 3)
	o := e.deliveredAPIOrder("d1", v)
	path := fmt.Sprintf("/api/v1/orders/%s/returns", o["id"])
	body := fmt.Sprintf(`{"kind":"customer_return","reason":"khách đổi ý","lines":[{"orderItemId":"%s","qty":1}]}`, firstItem(o))
	rec := e.returnPost(returnSale, "r1", path, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("createReturn: %d %s", rec.Code, rec.Body.String())
	}
	r := decodeJSON(t, rec)
	item := r["items"].([]any)[0].(map[string]any)
	if r["status"] != "requested" || r["refundAmount"] != float64(250000) || r["kind"] != "customer_return" ||
		item["condition"] != nil || r["reason"] != "khách đổi ý" {
		t.Fatalf("phiếu: %v", r)
	}
	expect(t, "lần hai cùng dòng", e.returnPost(returnSale, "r2", path, body), http.StatusUnprocessableEntity,
		"return_qty_exceeds")
	expect(t, "kho không có order.return", e.returnPost(returnWarehouse, "r3", path, body), http.StatusForbidden,
		"forbidden")
	conf := e.mustCreate("c1", m2Body("confirmed", fullAddr, 0, m2Line(v, 1, 0)))
	expect(t, "đơn chưa giao", e.returnPost(returnSale, "r4", fmt.Sprintf("/api/v1/orders/%s/returns", conf["id"]),
		fmt.Sprintf(`{"kind":"customer_return","lines":[{"orderItemId":"%s","qty":1}]}`, firstItem(conf))),
		http.StatusUnprocessableEntity, "return_not_allowed")

	receive := fmt.Sprintf(`{"version":%v,"lines":[{"orderItemId":"%s","condition":"sellable"}]}`, r["version"], firstItem(o))
	rpath := fmt.Sprintf("/api/v1/returns/%s", r["id"])
	expect(t, "bán hàng nhận hàng", e.returnPost(returnSale, "v1", rpath+"/receive", receive), http.StatusForbidden,
		"forbidden")
	before := e.count(`SELECT on_hand FROM inventory.stock_balances WHERE variant_id = $1`, v)
	rec = e.returnPost(returnWarehouse, "v2", rpath+"/receive", receive)
	got := decodeJSON(t, rec)
	if rec.Code != http.StatusOK || got["status"] != "restocked" || got["receivedAt"] == nil ||
		got["items"].([]any)[0].(map[string]any)["condition"] != "sellable" {
		t.Fatalf("receiveReturn: %d %s", rec.Code, rec.Body.String())
	}
	if after := e.count(`SELECT on_hand FROM inventory.stock_balances WHERE variant_id = $1`, v); after != before+1 {
		t.Errorf("on_hand %d → %d", before, after)
	}
	expect(t, "nhận lần hai", e.returnPost(returnWarehouse, "v3", rpath+"/receive",
		fmt.Sprintf(`{"version":%v,"lines":[{"orderItemId":"%s","condition":"sellable"}]}`, got["version"], firstItem(o))),
		http.StatusUnprocessableEntity, "return_not_allowed")
	expect(t, "đóng version cũ", e.returnPost(returnSale, "x1", rpath+"/close", `{"version":1}`), http.StatusConflict,
		"version_conflict")
	rec = e.returnPost(returnSale, "x2", rpath+"/close", fmt.Sprintf(`{"version":%v}`, got["version"]))
	if g := decodeJSON(t, rec); rec.Code != http.StatusOK || g["status"] != "closed" {
		t.Fatalf("closeReturn: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serveAs(e.srv, e.keys, http.MethodGet, rpath, orderViewer, "", ""); rec.Code != http.StatusOK ||
		decodeJSON(t, rec)["status"] != "closed" {
		t.Errorf("getReturn: %d %s", rec.Code, rec.Body.String())
	}
	rec = serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/returns?status=closed&limit=10", orderViewer, "", "")
	if page := decodeJSON(t, rec); rec.Code != http.StatusOK || len(page["items"].([]any)) != 1 {
		t.Errorf("listReturns: %d %s", rec.Code, rec.Body.String())
	}
	expect(t, "con trỏ hỏng", serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/returns?cursor=!!", orderViewer, "", ""),
		http.StatusBadRequest, "invalid_input")
	expect(t, "phiếu không có", serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/returns/"+uuid.NewString(), orderViewer,
		"", ""), http.StatusNotFound, "not_found")
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action LIKE 'order.return.%'
		AND (after::text LIKE '%khách đổi ý%' OR before::text LIKE '%khách đổi ý%')`); n != 0 {
		t.Errorf("audit chứa lý do trả: %d dòng", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action IN ('order.return.create',
		'order.return.receive', 'order.return.close')`); n != 3 {
		t.Errorf("số dòng audit phiếu trả = %d, mong 3", n)
	}
}

// Đổi hàng qua API: 201 phiếu exchange có replacementOrderId; đơn thay thế orderType exchange; hủy phiếu → cancelled,
// đơn thay thế cancelled.
func TestReturnAPI_Exchange(t *testing.T) {
	e := newReturnAPIEnv(t)
	v, l := e.variant(e.shop.Seller, 250000, 3), e.variant(e.shop.Seller, 250000, 3)
	o := e.deliveredAPIOrder("d1", v)
	body := fmt.Sprintf(`{"kind":"exchange","lines":[{"orderItemId":"%s","qty":1}],"replacement":{"items":[%s]}}`,
		firstItem(o), m2Line(l, 1, 0))
	rec := e.returnPost(returnSale, "e1", fmt.Sprintf("/api/v1/orders/%s/returns", o["id"]), body)
	r := decodeJSON(t, rec)
	if rec.Code != http.StatusCreated || r["kind"] != "exchange" || r["replacementOrderId"] == nil {
		t.Fatalf("createReturn exchange: %d %s", rec.Code, rec.Body.String())
	}
	rec = serveAs(e.srv, e.keys, http.MethodGet, fmt.Sprintf("/api/v1/orders/%s", r["replacementOrderId"]), orderViewer, "", "")
	if n := decodeJSON(t, rec); rec.Code != http.StatusOK || n["orderType"] != "exchange" || n["status"] != "confirmed" {
		t.Fatalf("đơn thay thế: %d %s", rec.Code, rec.Body.String())
	}
	rec = e.returnPost(returnSale, "e2", fmt.Sprintf("/api/v1/returns/%s/cancel", r["id"]), fmt.Sprintf(`{"version":%v}`, r["version"]))
	if c := decodeJSON(t, rec); rec.Code != http.StatusOK || c["status"] != "cancelled" {
		t.Fatalf("cancelReturn: %d %s", rec.Code, rec.Body.String())
	}
	rec = serveAs(e.srv, e.keys, http.MethodGet, fmt.Sprintf("/api/v1/orders/%s", r["replacementOrderId"]), orderViewer, "", "")
	if n := decodeJSON(t, rec); n["status"] != "cancelled" {
		t.Errorf("đơn thay thế sau hủy phiếu: %v", n["status"])
	}
	expect(t, "đổi thiếu replacement", e.returnPost(returnSale, "e3", fmt.Sprintf("/api/v1/orders/%s/returns", o["id"]),
		fmt.Sprintf(`{"kind":"exchange","lines":[{"orderItemId":"%s","qty":1}]}`, firstItem(o))),
		http.StatusBadRequest, "invalid_input")
}
