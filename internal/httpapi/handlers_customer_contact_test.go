package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// PUT /api/v1/customers/{id}/do-not-contact (customer.write, Task 152): chỉ xem → 403, không đổi; ghi → 204, cờ bật,
// audit chỉ tên trường (changed_fields = {do_not_contact}, không tên khách); tắt → 204, cờ tắt, gửi lại cùng khóa → 204
// không ghi thêm; khách không có hay của
// tenant khác → 404; thiếu khóa chống trùng → 400 (hợp đồng).
func TestSetCustomerDoNotContactAPI(t *testing.T) {
	t.Parallel()
	e := newCustomerAPIEnv(t)
	c := mustOK(t, "createCustomer", e.send(http.MethodPost, "/api/v1/customers", customerWriter,
		`{"fullName":"Trần Thị Mai Hoa"}`), http.StatusCreated)
	id := fmt.Sprint(c["id"])
	path := "/api/v1/customers/" + id + "/do-not-contact"
	flag := func() int {
		return e.count(`SELECT count(*) FROM customer.customers WHERE id = $1 AND do_not_contact`, id)
	}

	if rec := e.send(http.MethodPut, path, customerReader, `{"on":true}`); rec.Code != http.StatusForbidden || flag() != 0 {
		t.Fatalf("chỉ xem: %d %s, cờ %d", rec.Code, rec.Body.String(), flag())
	}
	if rec := e.send(http.MethodPut, path, customerWriter, `{"on":true}`); rec.Code != http.StatusNoContent || flag() != 1 {
		t.Fatalf("bật: %d %s, cờ %d", rec.Code, rec.Body.String(), flag())
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.do_not_contact' AND entity_id = $1
		AND changed_fields = ARRAY['do_not_contact'] AND actor_type = 'staff'`, id); n != 1 {
		t.Errorf("audit: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs a WHERE action = 'customer.do_not_contact'
		AND row_to_json(a)::text LIKE '%Mai Hoa%'`); n != 0 {
		t.Errorf("audit chứa tên khách: %d", n)
	}
	// Gửi lại cùng khóa chống trùng → 204 như lần đầu, không đổi gì thêm (một audit tắt).
	for range 2 {
		rec := serveAs(e.srv, e.keys, http.MethodPut, path, customerWriter, "kh-tat-"+id, `{"on":false}`)
		if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 || flag() != 0 {
			t.Fatalf("tắt: %d %s, cờ %d", rec.Code, rec.Body.String(), flag())
		}
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.do_not_contact' AND entity_id = $1`, id); n != 2 {
		t.Errorf("audit sau bật, tắt (tắt gửi hai lần): %d", n)
	}
	missing := "/api/v1/customers/" + uuid.NewString() + "/do-not-contact"
	if rec := e.send(http.MethodPut, missing, customerWriter, `{"on":true}`); rec.Code != http.StatusNotFound {
		t.Errorf("khách không có: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serveAs(e.srv, e.keys, http.MethodPut, path, customerWriter, "", `{"on":true}`); rec.Code != http.StatusBadRequest {
		t.Errorf("thiếu khóa chống trùng: %d %s", rec.Code, rec.Body.String())
	}
	if logs := e.logs.String(); strings.Contains(logs, "Mai Hoa") || strings.Contains(logs, "mai hoa") {
		t.Error("log chứa tên khách")
	}
}
