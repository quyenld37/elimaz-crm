package httpapi

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/testkit"
)

const customerNoRead = "khong-xem-khach@elimaz.test" // không có customer.read

// listItems gọi GET /api/v1/customers với tham số query và trả items, nextCursor.
func (e *customerAPIEnv) listItems(t *testing.T, query url.Values) ([]map[string]any, string) {
	t.Helper()
	body := mustOK(t, "listCustomers", e.send(http.MethodGet, "/api/v1/customers?"+query.Encode(), customerReader, ""), http.StatusOK)
	raw, _ := body["items"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, it := range raw {
		m, _ := it.(map[string]any)
		out = append(out, m)
	}
	next, _ := body["nextCursor"].(string)
	return out, next
}

// TestCustomerSearchAPI: tìm theo đuôi số, SĐT đủ, mã, tên; chuỗi quá ngắn → 422 query_too_short; danh sách khi không
// tìm, trang theo con trỏ, lọc nhãn; không có customer.read → 403; log và dấu vết không chứa chuỗi tìm, SĐT, tên.
func TestCustomerSearchAPI(t *testing.T) {
	exp := testkit.CaptureSpans(t)
	e := newCustomerAPIEnv(t)
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, customerNoRead, "active"), []string{"order.read"}, "all", "")
	ids, codes := map[string]string{}, map[string]string{}
	for _, c := range []struct{ key, body string }{
		{"A", `{"fullName":"Nguyễn Thị Hồng Nhung","phone":"0912345678"}`},
		{"B", `{"fullName":"Trần Văn Nam","phone":"0987655678"}`},
		{"C", `{"fullName":"Lê Hồng","phone":"0903000111"}`},
	} {
		got := mustOK(t, "createCustomer", e.send(http.MethodPost, "/api/v1/customers", customerWriter, c.body), http.StatusCreated)
		ids[c.key], codes[c.key] = got["id"].(string), got["code"].(string) //nolint:forcetypeassert // test
	}
	for _, c := range []struct {
		q, matched string
		want       []string
	}{
		{"5678", "phone_tail", []string{"A", "B"}},
		{"345678", "phone_tail", []string{"A"}},
		{"0912 345 678", "phone", []string{"A"}},
		{"+84912345678", "phone", []string{"A"}},
		{"Nhưng", "name", []string{"A"}},
		{"hong nhun", "name", []string{"A"}},
		{"hong", "name", []string{"A", "C"}},
		{codes["B"], "code", []string{"B"}},
		{"50%_", "name", nil},
	} {
		e.checkQuery(t, url.Values{"q": {c.q}}, c.matched, c.want, ids)
	}
	e.checkHitFields(t, codes["A"])
	expect(t, "chuỗi quá ngắn", e.send(http.MethodGet, "/api/v1/customers?q=12", customerReader, ""),
		http.StatusUnprocessableEntity, "query_too_short")
	expect(t, "không có customer.read", e.send(http.MethodGet, "/api/v1/customers?q=5678", customerNoRead, ""),
		http.StatusForbidden, "forbidden")
	expect(t, "con trỏ hỏng", e.send(http.MethodGet, "/api/v1/customers?cursor=%25%25", customerReader, ""),
		http.StatusBadRequest, "invalid_input")
	e.checkList(t, ids)

	text := e.logs.String() + testkit.SpanText(exp.GetSpans().Snapshots())
	for _, bad := range []string{"0912345678", "912345678", "345678", "5678", "Hồng Nhung", "hong nhun", "Nhưng"} {
		if strings.Contains(text, bad) {
			t.Errorf("log hoặc dấu vết chứa %q", bad)
		}
	}
	testkit.AssertNoPersonalData(t, "log và span", text)
}

// checkQuery: kết quả của query đúng tập khách want (khóa trong ids), mọi dòng có matchedBy = matched, không trang sau.
func (e *customerAPIEnv) checkQuery(t *testing.T, query url.Values, matched string, want []string, ids map[string]string) {
	t.Helper()
	items, next := e.listItems(t, query)
	got := map[any]bool{}
	for _, it := range items {
		got[it["id"]] = true
		if it["matchedBy"] != matched {
			t.Errorf("%v: matchedBy %v", query, it["matchedBy"])
		}
	}
	if len(got) != len(want) || next != "" {
		t.Errorf("%v: %v (next %q), muốn %v", query, items, next, want)
	}
	for _, k := range want {
		if !got[ids[k]] {
			t.Errorf("%v thiếu khách %s: %v", query, k, items)
		}
	}
}

// checkHitFields: dòng kết quả có mã, tên, SĐT, chỉ số của gốc.
func (e *customerAPIEnv) checkHitFields(t *testing.T, code string) {
	t.Helper()
	items, _ := e.listItems(t, url.Values{"q": {"345678"}})
	if len(items) != 1 {
		t.Fatalf("q 345678: %v", items)
	}
	it := items[0]
	phones, _ := it["phones"].([]any)
	if it["code"] != code || it["fullName"] != "Nguyễn Thị Hồng Nhung" || len(phones) != 1 || phones[0] != "+84912345678" ||
		it["stats"] == nil {
		t.Errorf("dòng kết quả: %v", it)
	}
}

// checkList: không tìm → mới nhất trước, trang theo con trỏ; tagId lọc nhóm có nhãn, kể cả khi kèm q.
func (e *customerAPIEnv) checkList(t *testing.T, ids map[string]string) {
	t.Helper()
	page1, next := e.listItems(t, url.Values{"limit": {"2"}})
	if len(page1) != 2 || page1[0]["id"] != ids["C"] || page1[1]["id"] != ids["B"] || next == "" {
		t.Fatalf("trang 1: %v %q", page1, next)
	}
	page2, next2 := e.listItems(t, url.Values{"limit": {"2"}, "cursor": {next}})
	if len(page2) != 1 || page2[0]["id"] != ids["A"] || next2 != "" || page2[0]["matchedBy"] != nil {
		t.Fatalf("trang 2: %v %q", page2, next2)
	}
	var tag string
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(t.Context(), `INSERT INTO customer.customer_tags (tenant_id, name, color)
			VALUES (current_setting('app.tenant_id')::uuid, 'VIP', 'red') RETURNING id::text`).Scan(&tag); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(t.Context(), `INSERT INTO customer.customer_tag_links (tenant_id, customer_id, tag_id, source)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'staff')`, ids["A"], tag)
		return err //nolint:wrapcheck // test
	})
	tagged, _ := e.listItems(t, url.Values{"tagId": {tag}})
	if len(tagged) != 1 || tagged[0]["id"] != ids["A"] {
		t.Errorf("lọc nhãn: %v", tagged)
	}
	e.checkQuery(t, url.Values{"tagId": {tag}, "q": {"5678"}}, "phone_tail", []string{"A"}, ids)
}
