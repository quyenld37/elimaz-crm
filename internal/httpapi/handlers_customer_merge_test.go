package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
)

const customerMerger = "gop-khach@elimaz.test" // customer.read, customer.merge (quản lý)

// TestCustomerMergeAPI (Task M2-11): gộp, hoàn tác, gợi ý qua HTTP — 200 kèm movedIds và tên người làm; cùng gốc →
// 422 merge_same_customer; vai trò sale (customer.read, customer.write) → 403; gộp đè → 409 merge_not_latest kèm
// blockingMergeId; quá 30 ngày → 422 merge_window_expired; hoàn tác lần hai → 409 version_conflict; tenant khác → 404;
// gợi ý theo SĐT địa chỉ.
func TestCustomerMergeAPI(t *testing.T) {
	e := newCustomerAPIEnv(t)
	merger := e.staff.User(t, e.tenant, customerMerger, "active")
	e.staff.Grant(t, e.tenant, merger, []string{"customer.read", "customer.merge"}, "all", "")
	newCustomer := func(name, phone string) string {
		body := fmt.Sprintf(`{"fullName":%q}`, name)
		if phone != "" {
			body = fmt.Sprintf(`{"fullName":%q,"phone":%q}`, name, phone)
		}
		return fmt.Sprint(mustOK(t, "createCustomer", e.send(http.MethodPost, "/api/v1/customers", customerWriter, body),
			http.StatusCreated)["id"])
	}
	a, b, d := newCustomer("A", "0977000001"), newCustomer("B", "0977000002"), newCustomer("D", "")
	merge := func(email, survivor, merged string) *httptest.ResponseRecorder {
		return e.send(http.MethodPost, "/api/v1/customers/"+survivor+"/merge", email,
			fmt.Sprintf(`{"mergedId":%q,"matchedBy":"phone"}`, merged))
	}

	// Gợi ý: địa chỉ của A mang SĐT của B.
	mustOK(t, "addAddress", e.send(http.MethodPost, "/api/v1/customers/"+a+"/addresses", customerWriter,
		`{"address":{"name":"A","phone":"0977000002","line":"1 Đường Thử","provinceCode":"T01","wardCode":"T01001"}}`),
		http.StatusCreated)
	cands := mustOK(t, "listCustomerMergeCandidates", e.send(http.MethodGet, "/api/v1/customers/"+a+"/merge-candidates",
		customerReader, ""), http.StatusOK)
	items, _ := cands["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["rootId"] != b || //nolint:forcetypeassert // test
		fmt.Sprint(items[0].(map[string]any)["matchedPhones"]) != "[+84977000002]" { //nolint:forcetypeassert // test
		t.Fatalf("gợi ý: %v", cands)
	}

	expect(t, "sale không gộp được", merge(customerWriter, a, b), http.StatusForbidden, "forbidden")
	expect(t, "gộp A vào A", merge(customerMerger, a, a), http.StatusUnprocessableEntity, "merge_same_customer")
	ba := mustOK(t, "mergeCustomer", merge(customerMerger, a, b), http.StatusOK)
	if ba["survivorId"] != a || ba["mergedId"] != b || fmt.Sprint(ba["movedIds"]) != "["+b+"]" ||
		ba["matchedBy"] != "phone" || ba["actor"].(map[string]any)["id"] != merger.String() || //nolint:forcetypeassert // test
		ba["actor"].(map[string]any)["name"] == "" || ba["undoneAt"] != nil { //nolint:forcetypeassert // test
		t.Fatalf("lần gộp: %v", ba)
	}
	expect(t, "B đã thuộc A", merge(customerMerger, a, b), http.StatusUnprocessableEntity, "merge_same_customer")
	ad := mustOK(t, "mergeCustomer", merge(customerMerger, d, a), http.StatusOK)

	undo := func(email, id string) *httptest.ResponseRecorder {
		return e.send(http.MethodPost, "/api/v1/customer-merges/"+id+"/undo", email, "")
	}
	expect(t, "sale không hoàn tác được", undo(customerWriter, fmt.Sprint(ad["id"])), http.StatusForbidden, "forbidden")
	rec := undo(customerMerger, fmt.Sprint(ba["id"]))
	if body := decodeJSON(t, rec); rec.Code != http.StatusConflict || body["error"] != "merge_not_latest" ||
		body["blockingMergeId"] != ad["id"] {
		t.Fatalf("merge_not_latest: %d %s", rec.Code, rec.Body.String())
	}
	undone := mustOK(t, "undoCustomerMerge", undo(customerMerger, fmt.Sprint(ad["id"])), http.StatusOK)
	if undone["id"] != ad["id"] || undone["undoneAt"] == nil {
		t.Fatalf("hoàn tác: %v", undone)
	}
	expect(t, "hoàn tác lần hai", undo(customerMerger, fmt.Sprint(ad["id"])), http.StatusConflict, "version_conflict")

	// Quá 30 ngày: lùi created_at của lần B→A.
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE customer.customer_merges SET created_at = now() - interval '30 days 1 second'
			WHERE id = $1`, ba["id"])
		return err //nolint:wrapcheck // test
	})
	expect(t, "quá hạn", undo(customerMerger, fmt.Sprint(ba["id"])), http.StatusUnprocessableEntity, "merge_window_expired")

	// Tenant khác: khách và lần gộp không thấy → 404.
	var foreign string
	e.staff.Exec(t, e.other, func(tx pgx.Tx) error {
		id, err := customer.Create(t.Context(), tx, "Khách tenant B")
		foreign = id.String()
		return err //nolint:wrapcheck // test
	})
	expect(t, "gộp khách tenant khác", merge(customerMerger, a, foreign), http.StatusNotFound, "not_found")
	expect(t, "gợi ý khách tenant khác", e.send(http.MethodGet, "/api/v1/customers/"+foreign+"/merge-candidates",
		customerReader, ""), http.StatusNotFound, "not_found")
	if strings.Contains(e.logs.String(), "977000002") {
		t.Error("log chứa SĐT")
	}
}
