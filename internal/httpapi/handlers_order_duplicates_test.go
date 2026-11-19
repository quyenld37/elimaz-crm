package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// orderDupResolver có order.read, order.duplicates.resolve.
const orderDupResolver = "xu-ly-trung@elimaz.test"

// detectDuplicates giao sự kiện order.created của đơn id cho consumer order_duplicates (như worker, crm_app).
func (e *orderAPIEnv) detectDuplicates(id any) {
	e.t.Helper()
	oid := uuid.MustParse(fmt.Sprint(id))
	ev := events.StoredEvent{
		ID: uuid.New(), TenantID: e.shop.Tenant, AggregateType: "order", AggregateID: oid.String(), Seq: 1,
		Topic:   order.TopicOrderCreated,
		Payload: []byte(`{"orderId":"` + oid.String() + `","sellerId":"` + e.shop.Seller.String() + `"}`),
	}
	if err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.shop.Tenant, func(tx pgx.Tx) error {
		return order.DuplicatesConsumer{}.Handle(e.t.Context(), ev, tx)
	}); err != nil {
		e.t.Fatal(err)
	}
}

// Ba operation đơn trùng qua API: check (order.create) trả hai đơn cùng SĐT, cùng biến thể; list (order.read) một cặp
// matchedBy phone; resolve (order.duplicates.resolve) cancel_other → 200 resolution cancelled, đơn kia cancelled,
// audit; gửi lại cùng khóa → cùng kết quả; khóa khác → 422 invalid_transition; không quyền → 403; cặp không có → 404;
// con trỏ hỏng → 400.
func TestOrderAPI_Duplicates(t *testing.T) {
	e := newOrderAPIEnv(t)
	e.m2Prepare()
	r := e.staff.User(t, e.shop.Tenant, orderDupResolver, "active")
	e.staff.Grant(t, e.shop.Tenant, r, []string{"order.read", "order.duplicates.resolve"}, "all", "")
	v := e.variant(e.shop.Seller, 250000, 10)
	o1 := e.mustCreate("d1", m2Body("confirmed", fullAddr, 0, m2Line(v, 1, 0)))
	o2 := e.mustCreate("d2", m2Body("confirmed", fullAddr, 0, m2Line(v, 1, 0)))

	q := url.Values{"phone": {"0901234567"}, "variantIds": {v.String()}}
	rec := serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/order-duplicate-check?"+q.Encode(), orderClerk, "", "")
	got := decodeJSON(t, rec)
	if rec.Code != http.StatusOK || len(got["items"].([]any)) != 2 {
		t.Fatalf("check: %d %s", rec.Code, rec.Body.String())
	}
	expect(t, "check không quyền tạo", serveAs(e.srv, e.keys, http.MethodGet,
		"/api/v1/order-duplicate-check?"+q.Encode(), orderViewer, "", ""), http.StatusForbidden, "forbidden")

	e.detectDuplicates(o2["id"])
	rec = serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/order-duplicates", orderViewer, "", "")
	got = decodeJSON(t, rec)
	items, _ := got["items"].([]any)
	if rec.Code != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["matchedBy"] != "phone" {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	expect(t, "con trỏ hỏng", serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/order-duplicates?cursor=xx", orderViewer,
		"", ""), http.StatusBadRequest, "invalid_input")

	path := fmt.Sprintf("/api/v1/order-duplicates/%v/%v/resolve", o1["id"], o2["id"])
	body := `{"action":"cancel_other"}`
	expect(t, "resolve không quyền", serveAs(e.srv, e.keys, http.MethodPost, path, orderViewer, "r0", body),
		http.StatusForbidden, "forbidden")
	expect(t, "cặp không có", serveAs(e.srv, e.keys, http.MethodPost,
		fmt.Sprintf("/api/v1/order-duplicates/%v/%v/resolve", o1["id"], uuid.New()), orderDupResolver, "r1", body),
		http.StatusNotFound, "not_found")
	rec = serveAs(e.srv, e.keys, http.MethodPost, path, orderDupResolver, "r2", body)
	got = decodeJSON(t, rec)
	if rec.Code != http.StatusOK || got["resolution"] != "cancelled" ||
		got["otherOrder"].(map[string]any)["status"] != "cancelled" || got["order"].(map[string]any)["id"] != o1["id"] {
		t.Fatalf("resolve: %d %s", rec.Code, rec.Body.String())
	}
	if again := serveAs(e.srv, e.keys, http.MethodPost, path, orderDupResolver, "r2", body); again.Code != http.StatusOK ||
		again.Body.String() != rec.Body.String() {
		t.Errorf("gửi lại cùng khóa: %d %s", again.Code, again.Body.String())
	}
	expect(t, "xử lý lại", serveAs(e.srv, e.keys, http.MethodPost, path, orderDupResolver, "r3",
		`{"action":"not_duplicate"}`), http.StatusUnprocessableEntity, "invalid_transition")
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'order.duplicate.resolve'
		AND entity_id = $1`, fmt.Sprint(o1["id"])); n != 1 {
		t.Errorf("audit: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'order.duplicate.resolve'
		AND (before::text LIKE '%0901234567%' OR after::text LIKE '%0901234567%' OR after::text LIKE '%84901234567%')`); n != 0 {
		t.Errorf("SĐT lọt vào audit")
	}
}
