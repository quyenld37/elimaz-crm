package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

// receiveReturn theo số lượng (M3-23): đơn V1 ×2, V2 ×1 đã giao, phiếu khách trả đủ hai dòng. Thiếu một dòng → 422
// qty_mismatch; S+D+M lệch → 422 qty_mismatch; dòng có số cùng dòng chỉ có condition → dòng sau 0/0/0 → 422;
// đúng (V1 S1 D1, V2 S1) → 200 restocked, dòng V1 trộn không có condition, V2 sellable, tồn V1 +2 (hỏng 1); cùng khóa
// → cùng kết quả; khóa khác với version cũ → 409 version_conflict.
func TestReturnAPI_ReceiveQty(t *testing.T) {
	e := newReturnAPIEnv(t)
	v1, v2 := e.variant(e.shop.Seller, 250000, 3), e.variant(e.shop.Seller, 180000, 3)
	o := e.mustCreate("q1", m2Body("confirmed", fullAddr, 0, m2Line(v1, 2, 0), m2Line(v2, 1, 0)))
	id := uuid.MustParse(o["id"].(string))
	e.deliver(id)
	items := o["items"].([]any)
	i1, i2 := items[0].(map[string]any)["id"], items[1].(map[string]any)["id"]
	rec := e.returnPost(returnSale, "q2", fmt.Sprintf("/api/v1/orders/%s/returns", id), fmt.Sprintf(
		`{"kind":"customer_return","lines":[{"orderItemId":"%s","qty":2},{"orderItemId":"%s","qty":1}]}`, i1, i2))
	if rec.Code != http.StatusCreated {
		t.Fatalf("createReturn: %d %s", rec.Code, rec.Body.String())
	}
	r := decodeJSON(t, rec)
	if first := r["items"].([]any)[0].(map[string]any); first["sellable"] != nil || first["missing"] != nil {
		t.Errorf("phiếu chưa nhận không có ba số: %v", first)
	}
	path := fmt.Sprintf("/api/v1/returns/%s/receive", r["id"])
	line := func(item any, s, d, m int) string {
		return fmt.Sprintf(`{"orderItemId":"%s","condition":"sellable","sellable":%d,"damaged":%d,"missing":%d}`,
			item, s, d, m)
	}
	body := func(lines ...string) string {
		return fmt.Sprintf(`{"version":%v,"lines":[%s]}`, r["version"], strings.Join(lines, ","))
	}
	expect(t, "thiếu một dòng", e.returnPost(returnWarehouse, "k1", path, body(line(i1, 1, 1, 0))),
		http.StatusUnprocessableEntity, "qty_mismatch")
	expect(t, "tổng lệch", e.returnPost(returnWarehouse, "k2", path, body(line(i1, 1, 0, 0), line(i2, 1, 0, 0))),
		http.StatusUnprocessableEntity, "qty_mismatch")
	expect(t, "dòng chỉ có condition", e.returnPost(returnWarehouse, "k3", path,
		body(line(i1, 1, 1, 0), fmt.Sprintf(`{"orderItemId":"%s","condition":"sellable"}`, i2))),
		http.StatusUnprocessableEntity, "qty_mismatch")
	onHand := func() int { return e.count(`SELECT on_hand FROM inventory.stock_balances WHERE variant_id = $1`, v1) }
	before := onHand()
	ok := body(line(i1, 1, 1, 0), line(i2, 1, 0, 0))
	rec = e.returnPost(returnWarehouse, "k4", path, ok)
	got := decodeJSON(t, rec)
	if rec.Code != http.StatusOK || got["status"] != "restocked" {
		t.Fatalf("receiveReturn: %d %s", rec.Code, rec.Body.String())
	}
	for _, it := range got["items"].([]any) {
		m := it.(map[string]any)
		if want := map[any]any{i1: nil, i2: "sellable"}[m["orderItemId"]]; m["condition"] != want {
			t.Errorf("dòng %v: condition %v, mong %v", m["orderItemId"], m["condition"], want)
		}
		// Ba số của lúc nhận có trong thân trả về (Task 136).
		want := map[any][3]float64{i1: {1, 1, 0}, i2: {1, 0, 0}}[m["orderItemId"]]
		if m["sellable"] != want[0] || m["damaged"] != want[1] || m["missing"] != want[2] {
			t.Errorf("dòng %v: tốt/hỏng/thiếu %v/%v/%v, mong %v", m["orderItemId"], m["sellable"], m["damaged"],
				m["missing"], want)
		}
	}
	if after := onHand(); after != before+2 {
		t.Errorf("on_hand V1 %d → %d", before, after)
	}
	if d := e.count(`SELECT damaged FROM inventory.stock_balances WHERE variant_id = $1`, v1); d != 1 {
		t.Errorf("damaged V1 = %d", d)
	}
	again := e.returnPost(returnWarehouse, "k4", path, ok)
	if again.Code != http.StatusOK || again.Body.String() != rec.Body.String() {
		t.Errorf("cùng khóa: %d %s", again.Code, again.Body.String())
	}
	expect(t, "khóa khác, version cũ", e.returnPost(returnWarehouse, "k5", path, ok), http.StatusConflict,
		"version_conflict")
}
