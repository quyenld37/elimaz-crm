package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
)

// Người dùng của Task M2-24: sale (vai trò mẫu: đọc, sửa, xác nhận đơn), người không có order.read.
const (
	readSale   = "ban-hang-m224@elimaz.test"
	readNoPerm = "khong-xem-don@elimaz.test"
)

func newOrderReadEnv(t *testing.T) *orderAPIEnv {
	t.Helper()
	e := newOrderConfirmEnv(t)
	e.staff.Grant(t, e.shop.Tenant, e.staff.User(t, e.shop.Tenant, readSale, "active"), rolePerms(t, "sale"), "all", "")
	e.staff.Grant(t, e.shop.Tenant, e.staff.User(t, e.shop.Tenant, readNoPerm, "active"), []string{"customer.read"}, "all", "")
	return e
}

func (e *orderAPIEnv) get(email, path string) *httptest.ResponseRecorder {
	return serveAs(e.srv, e.keys, http.MethodGet, path, email, "", "")
}

func listCodes(t *testing.T, body map[string]any) []string {
	t.Helper()
	items, _ := body["items"].([]any)
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, fmt.Sprint(it.(map[string]any)["code"]))
	}
	return out
}

// listOrders qua HTTP: lọc nhãn, kênh, q (mã), phân trang bằng nextCursor; nhãn vận đơn → 422 invalid_input; q 2 chữ
// số → 422 query_too_short; con trỏ hỏng → 400 invalid_input; không có order.read → 403.
func TestOrderRead_List(t *testing.T) {
	e := newOrderReadEnv(t)
	v := e.variant(e.shop.Seller, 1000000, 50)
	o1 := e.mustCreate("l1", m2Body("confirmed", fullAddr, 0, m2Line(v, 1, 0)))
	o2 := e.mustCreate("l2", withFields(m2Body("confirmed", fullAddr, 0, m2Line(v, 2, 0)), `"orderDiscount":500000`))
	o3 := e.mustCreate("l3", m2Body("confirmed", fullAddr, 0, m2Line(v, 1, 0)))

	rec := e.get(orderViewer, "/api/v1/orders?label=awaiting_discount_approval")
	body := decodeJSON(t, rec)
	if rec.Code != http.StatusOK || strings.Join(listCodes(t, body), ",") != o2["code"] {
		t.Fatalf("lọc nhãn chờ duyệt: %d %s", rec.Code, rec.Body.String())
	}
	item := body["items"].([]any)[0].(map[string]any)
	if item["label"] != "awaiting_discount_approval" || item["customerName"] != "Chị Lan" || item["itemCount"] != float64(2) ||
		item["shipPhone"] != "+84901234567" || item["channel"] != "manual" || item["pendingReason"] != "discount_approval" {
		t.Errorf("dòng danh sách: %v", item)
	}
	rec = e.get(orderViewer, "/api/v1/orders?channel=manual&limit=2")
	page1 := decodeJSON(t, rec)
	next, _ := page1["nextCursor"].(string)
	if rec.Code != http.StatusOK || strings.Join(listCodes(t, page1), ",") != fmt.Sprint(o3["code"], ",", o2["code"]) || next == "" {
		t.Fatalf("trang 1: %d %s", rec.Code, rec.Body.String())
	}
	rec = e.get(orderViewer, "/api/v1/orders?channel=manual&limit=2&cursor="+url.QueryEscape(next))
	page2 := decodeJSON(t, rec)
	if rec.Code != http.StatusOK || strings.Join(listCodes(t, page2), ",") != o1["code"] || page2["nextCursor"] != nil {
		t.Fatalf("trang 2: %d %s", rec.Code, rec.Body.String())
	}
	rec = e.get(orderViewer, "/api/v1/orders?q="+fmt.Sprint(o1["code"]))
	if got := listCodes(t, decodeJSON(t, rec)); rec.Code != http.StatusOK || len(got) != 1 || got[0] != o1["code"] {
		t.Errorf("q mã đơn: %d %s", rec.Code, rec.Body.String())
	}
	expect(t, "nhãn vận đơn", e.get(orderViewer, "/api/v1/orders?label=shipping"), http.StatusUnprocessableEntity, "invalid_input")
	expect(t, "q 2 chữ số", e.get(orderViewer, "/api/v1/orders?q=56"), http.StatusUnprocessableEntity, "query_too_short")
	expect(t, "con trỏ hỏng", e.get(orderViewer, "/api/v1/orders?cursor=abc"), http.StatusBadRequest, "invalid_input")
	expect(t, "không order.read", e.get(readNoPerm, "/api/v1/orders"), http.StatusForbidden, "forbidden")
}

// getOrder chi tiết (Task M2-24): khách, người tạo (tên), phạm vi sửa, mảng cảnh báo, phiếu trả; không có order.read →
// 403.
func TestOrderRead_Detail(t *testing.T) {
	e := newOrderReadEnv(t)
	v := e.variant(e.shop.Seller, 250000, 5)
	o := e.mustCreate("d1", m2Body("confirmed", fullAddr, 0, m2Line(v, 1, 0)))
	rec := e.get(orderViewer, "/api/v1/orders/"+fmt.Sprint(o["id"]))
	d := decodeJSON(t, rec)
	if rec.Code != http.StatusOK {
		t.Fatalf("getOrder: %d %s", rec.Code, rec.Body.String())
	}
	c, _ := d["customer"].(map[string]any)
	created, _ := d["createdBy"].(map[string]any)
	scope, _ := d["editScope"].(map[string]any)
	if c == nil || c["fullName"] != "Chị Lan" || c["id"] != o["customerId"] || c["rootId"] != o["customerId"] || c["stats"] == nil {
		t.Errorf("khách: %v", d["customer"])
	}
	if created == nil || created["name"] != orderClerk {
		t.Errorf("người tạo: %v", d["createdBy"])
	}
	if scope == nil || scope["lines"] != true || scope["shipping"] != true || d["label"] != "confirmed" {
		t.Errorf("phạm vi sửa, nhãn: %v %v", scope, d["label"])
	}
	if w, ok := d["warnings"].([]any); !ok || len(w) != 0 {
		t.Errorf("cảnh báo: %v", d["warnings"])
	}
	if r, ok := d["returns"].([]any); !ok || len(r) != 0 {
		t.Errorf("phiếu trả: %v", d["returns"])
	}
	expect(t, "không order.read", e.get(readNoPerm, "/api/v1/orders/"+fmt.Sprint(o["id"])), http.StatusForbidden, "forbidden")
}

// Lịch sử thao tác và ghi chú qua HTTP: tạo (lên đơn) → xác nhận (sale) → ghi chú (sale) → timeline có 3 mục mới nhất
// trước, tên người làm; audit của ghi chú không thành mục sửa và không chép nội dung; ghi chú thân trắng → 400; không
// order.read → 403; người chỉ xem không ghi chú được (order.update) → 403.
func TestOrderRead_TimelineNotes(t *testing.T) {
	e := newOrderReadEnv(t)
	v := e.variant(e.shop.Seller, 250000, 5)
	o := e.mustCreate("t1", m2Body("new", fullAddr, 0, m2Line(v, 1, 0)))
	id := fmt.Sprint(o["id"])
	e.mustAction(readSale, "t2", id, "confirm", versionBody(o["version"]))
	rec := e.orderAction(readSale, "t3", id, "notes", `{"body":"Khách hẹn gọi 0901234567"}`)
	note := decodeJSON(t, rec)
	if rec.Code != http.StatusCreated || note["body"] != "Khách hẹn gọi 0901234567" ||
		note["author"].(map[string]any)["name"] != readSale {
		t.Fatalf("addOrderNote: %d %s", rec.Code, rec.Body.String())
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'order.add_note' AND entity_id = $1
		AND after::text NOT LIKE '%0901234567%'`, id); n != 1 {
		t.Errorf("audit order.add_note: %d", n)
	}
	expect(t, "thân trắng", e.orderAction(readSale, "t4", id, "notes", `{"body":"   "}`), http.StatusBadRequest, "invalid_input")
	expect(t, "chỉ xem không ghi chú", e.orderAction(orderViewer, "t5", id, "notes", `{"body":"x"}`), http.StatusForbidden, "")
	expect(t, "ghi chú đơn không có", e.orderAction(readSale, "t6", uuid.NewString(), "notes", `{"body":"x"}`),
		http.StatusNotFound, "not_found")

	rec = e.get(orderViewer, "/api/v1/orders/"+id+"/timeline")
	tl := decodeJSON(t, rec)
	items, _ := tl["items"].([]any)
	if rec.Code != http.StatusOK || len(items) != 3 {
		t.Fatalf("timeline: %d %s", rec.Code, rec.Body.String())
	}
	want := []struct{ kind, actor, to string }{{"note", readSale, ""}, {"status", readSale, "confirmed"}, {"status", orderClerk, "new"}}
	for i, w := range want {
		it := items[i].(map[string]any)
		actor, _ := it["actor"].(map[string]any)
		sum, _ := it["summary"].(map[string]any)
		if it["kind"] != w.kind || actor == nil || actor["name"] != w.actor || (w.to != "" && sum["to"] != w.to) {
			t.Errorf("mục %d: %v, muốn %+v", i, it, w)
		}
	}
	expect(t, "timeline không order.read", e.get(readNoPerm, "/api/v1/orders/"+id+"/timeline"), http.StatusForbidden, "forbidden")
	expect(t, "timeline đơn không có", e.get(orderViewer, "/api/v1/orders/"+uuid.NewString()+"/timeline"),
		http.StatusNotFound, "not_found")
}

// PQ08: đơn của tenant B → 404 ở getOrder, timeline, ghi chú (không lộ là có); danh sách của A không có đơn của B.
func TestOrderRead_TenantIsolation(t *testing.T) {
	e := newOrderReadEnv(t)
	vA := e.variant(e.shop.Seller, 250000, 5)
	oA := e.mustCreate("a1", orderBody(0, line(vA, 1)))
	tenantB := e.staff.Tenant(t, "B")
	var shopB *invtest.Shop
	e.staff.Exec(t, tenantB, func(tx pgx.Tx) error {
		shopB = invtest.NewShop(t, tx, tenantB)
		return nil
	})
	const clerkB = "len-don-b@elimaz.test"
	e.staff.Grant(t, tenantB, e.staff.User(t, tenantB, clerkB, "active"), []string{"order.create", "order.read"}, "all", "")
	var vB uuid.UUID
	e.staff.Exec(t, tenantB, func(tx pgx.Tx) error {
		vB = shopB.Variant(t, tx, shopB.Seller, 100000, 5)
		return nil
	})
	rec := serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/orders", clerkB, "b1", orderBody(0, line(vB, 1)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("đơn của B: %d %s", rec.Code, rec.Body.String())
	}
	oB := decodeJSON(t, rec)
	idB := fmt.Sprint(oB["id"])
	expect(t, "getOrder đơn B", e.get(orderViewer, "/api/v1/orders/"+idB), http.StatusNotFound, "not_found")
	expect(t, "timeline đơn B", e.get(orderViewer, "/api/v1/orders/"+idB+"/timeline"), http.StatusNotFound, "not_found")
	expect(t, "ghi chú đơn B", e.orderAction(readSale, "n1", idB, "notes", `{"body":"x"}`), http.StatusNotFound, "not_found")
	rec = e.get(orderViewer, "/api/v1/orders")
	if got := listCodes(t, decodeJSON(t, rec)); rec.Code != http.StatusOK || len(got) != 1 || got[0] != oA["code"] {
		t.Errorf("danh sách của A: %d %v", rec.Code, got)
	}
	if n := e.staff.CountIn(t, tenantB, "SELECT count(*) FROM ord.order_notes"); n != 0 {
		t.Errorf("ghi chú lọt vào đơn B: %d", n)
	}
}

// DH06 qua HTTP (Task 77, 78 thật): tạo (lên đơn) → PATCH địa chỉ và số lượng (sale) → hủy có lý do, ghi chú (người hủy)
// → timeline mới nhất trước: hủy (lý do), sửa (lines, shipAddress "changed", tên người sửa), tạo; không địa chỉ, SĐT cũ
// hay mới trong timeline; getOrder trả cancelReason, cancelNote của lần hủy.
func TestOrderRead_EditCancelTimeline(t *testing.T) {
	e := newOrderReadEnv(t)
	e.grantCanceller()
	v := e.variant(e.shop.Seller, 250000, 5)
	o := e.mustCreate("ec1", m2Body("new", fullAddr, 0, m2Line(v, 1, 0)))
	id := fmt.Sprint(o["id"])
	newAddr := `{"name":"Chị Lan","phone":"0987654321","line":"99 Hai Bà Trưng","provinceCode":"T01","wardCode":"T01001"}`
	rec := e.patchOrder(readSale, "ec2", id, fmt.Sprintf(`{"version":%v,"shipAddress":%s,"lines":[{"itemId":"%v",`+
		`"variantId":"%s","qty":2}]}`, o["version"], newAddr, firstItemID(t, o), v))
	edited := decodeJSON(t, rec)
	if rec.Code != http.StatusOK {
		t.Fatalf("sửa: %d %s", rec.Code, rec.Body.String())
	}
	if rec = e.cancelOrder(orderCanceller, "ec3", id, edited["version"], "wrong_info", "Khách nhầm địa chỉ"); rec.Code != http.StatusOK {
		t.Fatalf("hủy: %d %s", rec.Code, rec.Body.String())
	}

	rec = e.get(orderViewer, "/api/v1/orders/"+id+"/timeline")
	items, _ := decodeJSON(t, rec)["items"].([]any)
	if rec.Code != http.StatusOK || len(items) != 3 {
		t.Fatalf("timeline: %d %s", rec.Code, rec.Body.String())
	}
	checkTimelineItems(t, items, []timelineItemWant{
		{"status", orderCanceller, map[string]any{"to": "cancelled", "reason": "wrong_info"}},
		{"edit", readSale, map[string]any{"action": "order.update", "lines": "changed", "shipAddress": "changed"}},
		{"status", orderClerk, map[string]any{"to": "new"}},
	})
	for _, pii := range []string{"0901234567", "0987654321", "Lê Lợi", "Hai Bà Trưng", "Chị Lan", "nhầm địa chỉ"} {
		if strings.Contains(rec.Body.String(), pii) {
			t.Errorf("timeline lộ %q: %s", pii, rec.Body.String())
		}
	}

	rec = e.get(orderViewer, "/api/v1/orders/"+id)
	d := decodeJSON(t, rec)
	if rec.Code != http.StatusOK || d["status"] != "cancelled" || d["cancelReason"] != "wrong_info" ||
		d["cancelNote"] != "Khách nhầm địa chỉ" || d["cancelledAt"] == nil {
		t.Errorf("getOrder sau hủy: %d %s", rec.Code, rec.Body.String())
	}
}

// timelineItemWant: một mục TimelineItem mong đợi: loại, tên người làm, các khóa summary (mục edit: đúng bằng các khóa đó).
type timelineItemWant struct {
	kind, actor string
	sum         map[string]any
}

func checkTimelineItems(t *testing.T, items []any, want []timelineItemWant) {
	t.Helper()
	for i, w := range want {
		it := items[i].(map[string]any)
		actor, _ := it["actor"].(map[string]any)
		sum, _ := it["summary"].(map[string]any)
		if it["kind"] != w.kind || actor == nil || actor["name"] != w.actor {
			t.Errorf("mục %d: %v, muốn %s của %s", i, it, w.kind, w.actor)
		}
		for k, v := range w.sum {
			if sum[k] != v {
				t.Errorf("mục %d summary[%s] = %v, muốn %v (%v)", i, k, sum[k], v, sum)
			}
		}
		if w.kind == "edit" && len(sum) != len(w.sum) {
			t.Errorf("mục sửa có trường thừa: %v", sum)
		}
	}
}
