package httpapi

import (
	"fmt"
	"net/http"
	"testing"
)

// TestCustomerTagRuleAPI (Task M2-25): nhãn chưa có quy tắc → GET 404; PUT field "phone" → 422 invalid_tag_rule, phép
// lạ → 422; PUT hợp lệ → 200 version 1, GET trả đúng; PUT version cũ hay thiếu version → 409 version_conflict; sale
// không đặt được (403); nhãn lạ → 404.
func TestCustomerTagRuleAPI(t *testing.T) {
	e := newCustomerAPIEnv(t)
	mgr := e.staff.User(t, e.tenant, customerTagManager, "active")
	e.staff.Grant(t, e.tenant, mgr, []string{"customer.read", "customer.tags.manage"}, "all", "")
	tag := fmt.Sprint(mustOK(t, "createCustomerTag", e.send(http.MethodPost, "/api/v1/customer-tags", customerTagManager,
		`{"name":"Hay hoàn","color":"red"}`), http.StatusCreated)["id"])
	path := "/api/v1/customer-tags/" + tag + "/rule"

	expect(t, "chưa có quy tắc", e.send(http.MethodGet, path, customerReader, ""), http.StatusNotFound, "not_found")
	expect(t, "trường phone", e.send(http.MethodPut, path, customerTagManager,
		`{"enabled":true,"conditions":[{"field":"phone","op":"gte","value":1}]}`), http.StatusUnprocessableEntity,
		"invalid_tag_rule")
	expect(t, "phép lạ", e.send(http.MethodPut, path, customerTagManager,
		`{"enabled":true,"conditions":[{"field":"order_count","op":"eq","value":1}]}`), http.StatusUnprocessableEntity,
		"invalid_tag_rule")
	expect(t, "sale không đặt được", e.send(http.MethodPut, path, customerWriter,
		`{"enabled":true,"conditions":[{"field":"order_count","op":"gte","value":2}]}`), http.StatusForbidden, "forbidden")

	const body = `{"enabled":true,"conditions":[{"field":"return_rate_bp","op":"gte","value":3000},` +
		`{"field":"order_count","op":"gte","value":2}]}`
	got := mustOK(t, "putCustomerTagRule", e.send(http.MethodPut, path, customerTagManager, body), http.StatusOK)
	if got["tagId"] != tag || got["enabled"] != true || got["version"] != float64(1) ||
		fmt.Sprint(got["conditions"]) != "[map[field:return_rate_bp op:gte value:3000] map[field:order_count op:gte value:2]]" {
		t.Fatalf("đặt quy tắc: %v", got)
	}
	read := mustOK(t, "getCustomerTagRule", e.send(http.MethodGet, path, customerReader, ""), http.StatusOK)
	if fmt.Sprint(read) != fmt.Sprint(got) {
		t.Fatalf("đọc lại: %v, muốn %v", read, got)
	}
	expect(t, "thiếu version khi đã có quy tắc", e.send(http.MethodPut, path, customerTagManager, body),
		http.StatusConflict, "version_conflict")
	expect(t, "version cũ", e.send(http.MethodPut, path, customerTagManager,
		`{"version":7,"enabled":false,"conditions":[{"field":"order_count","op":"gte","value":2}]}`),
		http.StatusConflict, "version_conflict")
	upd := mustOK(t, "putCustomerTagRule", e.send(http.MethodPut, path, customerTagManager,
		`{"version":1,"enabled":false,"conditions":[{"field":"days_since_last_order","op":"gte","value":90}]}`),
		http.StatusOK)
	if upd["version"] != float64(2) || upd["enabled"] != false {
		t.Fatalf("sửa quy tắc: %v", upd)
	}
	expect(t, "nhãn lạ", e.send(http.MethodPut, "/api/v1/customer-tags/00000000-0000-7000-8000-000000000001/rule",
		customerTagManager, body), http.StatusNotFound, "not_found")
}
