package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// patchOrder gửi PATCH /api/v1/orders/{id} của người email, khóa key, thân body.
func (e *orderAPIEnv) patchOrder(email, key string, id any, body string) *httptest.ResponseRecorder {
	return serveAs(e.srv, e.keys, http.MethodPatch, fmt.Sprintf("/api/v1/orders/%v", id), email, key, body)
}

// firstItemID là mã dòng đầu của thân đơn o.
func firstItemID(t *testing.T, o map[string]any) any {
	t.Helper()
	items, ok := o["items"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("đơn không có dòng: %v", o)
	}
	return items[0].(map[string]any)["id"] //nolint:forcetypeassert // test
}

// DH10/AT07 qua API (V1 tồn 5, đơn V1 ×2 confirmed): sửa thành 6 → 409 out_of_stock, không lưu theo khóa (gửi lại cùng
// khóa thân khác được xử lý lại); thành 5 → 200, version + 1, đơn giá giữ; gửi lại cùng khóa cùng thân → cùng câu trả
// lời; audit order.update có tên trường, không SĐT, địa chỉ; người chỉ có order.create, order.read → 403.
func TestOrderEdit_Quantity(t *testing.T) {
	t.Parallel()
	e := newOrderConfirmEnv(t)
	v := e.variant(e.shop.Seller, 250000, 5)
	o := e.mustCreate("e-dh10", m2Body("confirmed", fullAddr, 0, m2Line(v, 2, 0)))
	item := firstItemID(t, o)
	body := func(q int) string {
		return fmt.Sprintf(`{"version":%v,"lines":[{"itemId":"%v","variantId":"%s","qty":%d}]}`, o["version"], item, v, q)
	}
	oos := e.patchOrder(confirmSale, "k1", o["id"], body(6))
	if b := decodeJSON(t, oos); oos.Code != http.StatusConflict || b["error"] != "out_of_stock" || b["available"] != float64(3) {
		t.Fatalf("sửa thành 6: %d %v", oos.Code, b)
	}
	rec := e.patchOrder(confirmSale, "k1", o["id"], body(5))
	if rec.Code != http.StatusOK {
		t.Fatalf("sửa thành 5: %d %s", rec.Code, rec.Body.String())
	}
	got := decodeJSON(t, rec)
	line := got["items"].([]any)[0].(map[string]any) //nolint:forcetypeassert // test
	if got["version"] != o["version"].(float64)+1 || line["qty"] != float64(5) || line["unitPrice"] != float64(250000) ||
		got["grandTotal"] != float64(1250000) {
		t.Errorf("sau sửa: %v", got)
	}
	again := e.patchOrder(confirmSale, "k1", o["id"], body(5))
	if again.Code != http.StatusOK || again.Body.String() != rec.Body.String() {
		t.Errorf("gửi lại cùng khóa: %d %s", again.Code, again.Body.String())
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'order.update' AND entity_id = $1::text
		AND after->'fields' = '["lines"]'::jsonb AND after::text NOT LIKE '%901234567%' AND after::text NOT LIKE '%Lê Lợi%'`,
		o["id"]); n != 1 {
		t.Errorf("audit order.update: %d", n)
	}
	expect(t, "không có order.update", e.patchOrder(orderClerk, "k2", o["id"], body(4)), http.StatusForbidden, "")
}

// DH11/AT28 qua API: hai người cùng đọc một version; người A lưu → 200; người B lưu → 409 version_conflict, dữ liệu là
// của A. Sale tăng giảm cấp đơn của đơn confirmed vượt ngưỡng → 403 discount_needs_approval; manager → 200.
func TestOrderEdit_ConflictAndApproval(t *testing.T) {
	t.Parallel()
	e := newOrderConfirmEnv(t)
	v := e.variant(e.shop.Seller, 1000000, 5)
	o := e.mustCreate("e-dh11", m2Body("confirmed", fullAddr, 0, m2Line(v, 1, 0)))
	ver := o["version"]
	a := e.patchOrder(confirmSale, "a", o["id"], fmt.Sprintf(`{"version":%v,"carrierNote":"của A"}`, ver))
	if a.Code != http.StatusOK || decodeJSON(t, a)["version"] != ver.(float64)+1 {
		t.Fatalf("người A: %d %s", a.Code, a.Body.String())
	}
	expect(t, "người B", e.patchOrder(confirmManager, "b", o["id"], fmt.Sprintf(`{"version":%v,"carrierNote":"của B"}`, ver)),
		http.StatusConflict, "version_conflict")
	if n := e.count(`SELECT count(*) FROM ord.orders WHERE id = $1 AND carrier_note = 'của A'`, o["id"]); n != 1 {
		t.Errorf("dữ liệu không phải của A")
	}
	next := ver.(float64) + 1
	expect(t, "sale vượt ngưỡng", e.patchOrder(confirmSale, "c", o["id"],
		fmt.Sprintf(`{"version":%v,"orderDiscount":100001}`, next)), http.StatusForbidden, "discount_needs_approval")
	m := e.patchOrder(confirmManager, "d", o["id"], fmt.Sprintf(`{"version":%v,"orderDiscount":100001}`, next))
	if m.Code != http.StatusOK || decodeJSON(t, m)["grandTotal"] != float64(899999) {
		t.Errorf("manager vượt ngưỡng: %d %s", m.Code, m.Body.String())
	}
}
