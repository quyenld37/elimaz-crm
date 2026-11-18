package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

const customerTagManager = "nhan-khach@elimaz.test" // customer.read, customer.tags.manage

// foreignCustomer tạo một khách của tenant B (chủ bảng) và trả mã.
func (e *customerAPIEnv) foreignCustomer() string {
	e.t.Helper()
	var id string
	e.staff.Exec(e.t, e.other, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `INSERT INTO customer.customers (tenant_id, full_name) VALUES ($1, 'Khách B')
			RETURNING id::text`, e.other).Scan(&id)
	})
	return id
}

// TestCustomerTagsAPI (Task M2-12): tạo nhãn 201, tên trùng → 409 already_exists, màu ngoài bảng màu → 400; sửa có
// version (lệch → 409); sale không tạo nhãn được (403); PUT nhãn hai lần → 200, một dòng, hồ sơ có nhãn staff; nhãn
// gắn ở hồ sơ con hiện trên gốc; gỡ nhãn rule → 409, dòng còn; gỡ nhãn staff → 200; người chỉ đọc không gắn được;
// tenant khác 404.
func TestCustomerTagsAPI(t *testing.T) {
	e := newCustomerAPIEnv(t)
	mgr := e.staff.User(t, e.tenant, customerTagManager, "active")
	e.staff.Grant(t, e.tenant, mgr, []string{"customer.read", "customer.tags.manage"}, "all", "")

	vip := mustOK(t, "createCustomerTag", e.send(http.MethodPost, "/api/v1/customer-tags", customerTagManager,
		`{"name":"VIP","color":"red"}`), http.StatusCreated)
	if vip["name"] != "VIP" || vip["color"] != "red" || vip["version"] != float64(1) {
		t.Fatalf("nhãn mới: %v", vip)
	}
	tagID := fmt.Sprint(vip["id"])
	expect(t, "tên trùng", e.send(http.MethodPost, "/api/v1/customer-tags", customerTagManager,
		`{"name":"VIP","color":"blue"}`), http.StatusConflict, "already_exists")
	expect(t, "màu ngoài bảng màu", e.send(http.MethodPost, "/api/v1/customer-tags", customerTagManager,
		`{"name":"Mới","color":"neon"}`), http.StatusBadRequest, "invalid_input")
	expect(t, "sale không tạo nhãn", e.send(http.MethodPost, "/api/v1/customer-tags", customerWriter,
		`{"name":"Sale","color":"red"}`), http.StatusForbidden, "forbidden")
	upd := mustOK(t, "updateCustomerTag", e.send(http.MethodPatch, "/api/v1/customer-tags/"+tagID, customerTagManager,
		`{"version":1,"color":"purple"}`), http.StatusOK)
	if upd["color"] != "purple" || upd["name"] != "VIP" || upd["version"] != float64(2) {
		t.Fatalf("sửa nhãn: %v", upd)
	}
	expect(t, "version cũ", e.send(http.MethodPatch, "/api/v1/customer-tags/"+tagID, customerTagManager,
		`{"version":1,"name":"X"}`), http.StatusConflict, "version_conflict")
	list := mustOK(t, "listCustomerTags", e.send(http.MethodGet, "/api/v1/customer-tags", customerReader, ""), http.StatusOK)
	if items, _ := list["items"].([]any); len(items) != 1 {
		t.Fatalf("danh sách nhãn: %v", list)
	}

	newCustomer := func(name string) string {
		return fmt.Sprint(mustOK(t, "createCustomer", e.send(http.MethodPost, "/api/v1/customers", customerWriter,
			fmt.Sprintf(`{"fullName":%q}`, name)), http.StatusCreated)["id"])
	}
	a, b := newCustomer("A"), newCustomer("B")
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE customer.customers SET root_id = $1 WHERE id = $2`, a, b)
		return err //nolint:wrapcheck // test
	})
	var got map[string]any
	for range 2 {
		got = mustOK(t, "addCustomerTag", e.send(http.MethodPut, "/api/v1/customers/"+b+"/tags/"+tagID, customerWriter, ""),
			http.StatusOK)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_tag_links WHERE tag_id = $1`, tagID); n != 1 {
		t.Fatalf("PUT hai lần: %d dòng", n)
	}
	if got["id"] != a || fmt.Sprint(got["tags"]) != "[map[source:staff tag:map[color:purple id:"+tagID+" name:VIP version:2]]]" {
		t.Fatalf("hồ sơ sau khi gắn: %v", got["tags"])
	}
	expect(t, "chỉ đọc không gắn", e.send(http.MethodPut, "/api/v1/customers/"+a+"/tags/"+tagID, customerReader, ""),
		http.StatusForbidden, "forbidden")

	auto := fmt.Sprint(mustOK(t, "createCustomerTag", e.send(http.MethodPost, "/api/v1/customer-tags", customerTagManager,
		`{"name":"Hay mua","color":"green"}`), http.StatusCreated)["id"])
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO customer.customer_tag_links (tenant_id, customer_id, tag_id, source)
			VALUES ($1, $2, $3, 'rule')`, e.tenant, b, auto)
		return err //nolint:wrapcheck // test
	})
	prof := mustOK(t, "getCustomer", e.send(http.MethodGet, "/api/v1/customers/"+a, customerReader, ""), http.StatusOK)
	if tags, _ := prof["tags"].([]any); len(tags) != 2 || !strings.Contains(fmt.Sprint(tags), "source:rule") {
		t.Fatalf("nhãn của nhóm trên gốc: %v", prof["tags"])
	}
	expect(t, "gỡ nhãn rule", e.send(http.MethodDelete, "/api/v1/customers/"+a+"/tags/"+auto, customerWriter, ""),
		http.StatusConflict, "version_conflict")
	if n := e.count(`SELECT count(*) FROM customer.customer_tag_links WHERE tag_id = $1`, auto); n != 1 {
		t.Fatal("dòng rule phải còn")
	}
	rm := mustOK(t, "removeCustomerTag", e.send(http.MethodDelete, "/api/v1/customers/"+a+"/tags/"+tagID, customerWriter, ""),
		http.StatusOK)
	if tags, _ := rm["tags"].([]any); len(tags) != 1 {
		t.Fatalf("sau khi gỡ: %v", rm["tags"])
	}

	foreign := e.foreignCustomer()
	expect(t, "khách tenant khác", e.send(http.MethodPut, "/api/v1/customers/"+foreign+"/tags/"+tagID, customerWriter, ""),
		http.StatusNotFound, "not_found")
}

// TestCustomerNotesTimelineAPI (Task M2-12): thêm ghi chú 201 (tên người viết), rỗng → 400, người chỉ đọc 403; dòng
// thời gian hai trang (limit 1, nextCursor) có mục note kèm nội dung và tên người viết; con trỏ hỏng → 400; tenant
// khác 404; log không có nội dung ghi chú.
func TestCustomerNotesTimelineAPI(t *testing.T) {
	e := newCustomerAPIEnv(t)
	id := fmt.Sprint(mustOK(t, "createCustomer", e.send(http.MethodPost, "/api/v1/customers", customerWriter,
		`{"fullName":"Chị Lan"}`), http.StatusCreated)["id"])
	const body = "Khách hẹn lấy hàng thứ bảy, số phụ 0909777888"
	note := mustOK(t, "addCustomerNote", e.send(http.MethodPost, "/api/v1/customers/"+id+"/notes", customerWriter,
		fmt.Sprintf(`{"body":%q}`, body)), http.StatusCreated)
	author, _ := note["author"].(map[string]any)
	if note["body"] != body || author["name"] == "" || author["id"] == nil || note["createdAt"] == nil {
		t.Fatalf("ghi chú: %v", note)
	}
	mustOK(t, "addCustomerNote", e.send(http.MethodPost, "/api/v1/customers/"+id+"/notes", customerWriter,
		`{"body":"Ghi chú hai"}`), http.StatusCreated)
	expect(t, "thân chỉ khoảng trắng", e.send(http.MethodPost, "/api/v1/customers/"+id+"/notes", customerWriter,
		`{"body":"   "}`), http.StatusBadRequest, "invalid_input")
	expect(t, "chỉ đọc không ghi chú", e.send(http.MethodPost, "/api/v1/customers/"+id+"/notes", customerReader,
		`{"body":"x"}`), http.StatusForbidden, "forbidden")

	p1 := mustOK(t, "getCustomerTimeline", e.send(http.MethodGet, "/api/v1/customers/"+id+"/timeline?limit=1",
		customerReader, ""), http.StatusOK)
	items, _ := p1["items"].([]any)
	cur, _ := p1["nextCursor"].(string)
	if len(items) != 1 || cur == "" {
		t.Fatalf("trang 1: %v", p1)
	}
	p2 := mustOK(t, "getCustomerTimeline", e.send(http.MethodGet, "/api/v1/customers/"+id+"/timeline?limit=1&cursor="+cur,
		customerReader, ""), http.StatusOK)
	items2, _ := p2["items"].([]any)
	if len(items2) != 1 || p2["nextCursor"] != nil {
		t.Fatalf("trang 2: %v", p2)
	}
	last, _ := items2[0].(map[string]any)
	actor, _ := last["actor"].(map[string]any)
	if last["kind"] != "note" || last["note"] != body || actor["name"] == "" || last["refId"] != note["id"] {
		t.Fatalf("mục ghi chú: %v", last)
	}
	expect(t, "con trỏ hỏng", e.send(http.MethodGet, "/api/v1/customers/"+id+"/timeline?cursor=xyz", customerReader, ""),
		http.StatusBadRequest, "invalid_input")

	foreign := e.foreignCustomer()
	expect(t, "dòng thời gian tenant khác", e.send(http.MethodGet, "/api/v1/customers/"+foreign+"/timeline", customerReader,
		""), http.StatusNotFound, "not_found")
	expect(t, "ghi chú tenant khác", e.send(http.MethodPost, "/api/v1/customers/"+foreign+"/notes", customerWriter,
		`{"body":"x"}`), http.StatusNotFound, "not_found")
	if strings.Contains(e.logs.String(), "0909777888") {
		t.Fatal("log có nội dung ghi chú")
	}
}
