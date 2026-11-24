package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// [Task 486] Yêu cầu về đơn theo mockup: danh sách có tên khách, số ảnh, lọc nhiều trạng thái (statuses; cùng status →
// 400 invalid_input); số theo trạng thái (đủ 8, số đang mở, tổng; không quyền → 403); chi tiết có lịch sử (nhân viên
// kèm tên, khách qua website) và biến thể muốn đổi sang (mã, số còn); website không nhận hai phần đó.
func TestOrderRequestAPI_MockupData(t *testing.T) {
	t.Parallel()
	e := newReqAPIEnv(t, nil)
	v, want := e.variant(e.shop.Seller, 250000, 5), e.variant(e.shop.Seller, 250000, 3)
	e.webOrder("W-1", v, 3, true)
	body := fmt.Sprintf(`{"kind":"exchange","reason":"SIZE","message":"Áo chật quá, em muốn đổi size L",
		"conditionConfirmed":true,"lines":[{"variantId":"%s","qty":1,"reason":"TOO_SMALL","wantVariantId":"%s"}],
		"fileIds":["%s"]}`, v, want, e.fileOf("ready"))
	ref := e.ok("tạo", e.createRequest("W-1", body), http.StatusCreated)["ref"].(string)
	id := e.requestID(ref)
	ref2 := e.ok("tạo 2", e.createRequest("W-1", exchangeBody(v, "FIT")), http.StatusCreated)["ref"].(string)
	e.ok("cần bổ sung", serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/order-requests/"+id+"/status", reqSale, "s1",
		`{"status":"needs_info","version":1,"message":"Chị gửi giúp em số đo nhé"}`), http.StatusOK)
	e.ok("khách trả lời", e.web(http.MethodPost, "/api/v1/web/orders/W-1/requests/"+ref+"/messages", e.writeKey, "w1",
		"application/json", strings.NewReader(`{"message":"Dạ em cao 1m60 nặng 50kg"}`)), http.StatusOK)
	e.ok("khách rút", e.web(http.MethodPost, "/api/v1/web/orders/W-1/requests/"+ref2+"/cancel", e.writeKey, "c1", "",
		nil), http.StatusOK)

	get := func(path, email string) map[string]any {
		return e.ok(path, serveAs(e.srv, e.keys, http.MethodGet, path, email, "", ""), http.StatusOK)
	}
	open := get("/api/v1/order-requests?statuses=received&statuses=in_review&statuses=needs_info", reqSale)
	items := open["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("đang mở: %v", open)
	}
	it := items[0].(map[string]any)
	if it["ref"] != ref || it["photoCount"] != float64(1) || it["customerName"] == nil || it["customerName"] == "" {
		t.Fatalf("dòng: %v", it)
	}
	expect(t, "status và statuses", serveAs(e.srv, e.keys, http.MethodGet,
		"/api/v1/order-requests?status=received&statuses=in_review", reqSale, "", ""), http.StatusBadRequest, "invalid_input")

	c := get("/api/v1/order-requests/counts", reqSale)
	sts := c["statuses"].([]any)
	byStatus := map[string]float64{}
	for _, s := range sts {
		m := s.(map[string]any)
		byStatus[m["status"].(string)] = m["count"].(float64)
	}
	if len(sts) != 8 || sts[0].(map[string]any)["status"] != "received" || c["open"] != float64(1) ||
		c["total"] != float64(2) || byStatus["in_review"] != 1 || byStatus["cancelled"] != 1 {
		t.Fatalf("số đếm: %v", c)
	}
	expect(t, "đếm không quyền", serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/order-requests/counts", reqNobody, "", ""),
		http.StatusForbidden, "forbidden")

	d := get("/api/v1/order-requests/"+id, reqSale)
	h := d["history"].([]any)
	h0, h1 := h[0].(map[string]any), h[1].(map[string]any)
	if len(h) != 2 || h0["fromStatus"] != "received" || h0["toStatus"] != "needs_info" || h0["actorType"] != "staff" ||
		h0["actorName"] == nil || h1["toStatus"] != "in_review" || h1["actorType"] != "customer" || h1["actorName"] != nil {
		t.Fatalf("lịch sử: %v", h)
	}
	w := d["lines"].([]any)[0].(map[string]any)["want"].(map[string]any)
	if w["sku"] == nil || w["available"] != float64(3) || w["warehouseName"] == nil || w["color"] != "Đỏ" {
		t.Fatalf("muốn đổi sang: %v", w)
	}
	wl := e.web(http.MethodGet, "/api/v1/web/orders/W-1/requests", e.readKy, "", "", nil)
	if s := wl.Body.String(); wl.Code != http.StatusOK || strings.Contains(s, `"want"`) || strings.Contains(s, "history") ||
		strings.Contains(s, "available") {
		t.Fatalf("website thấy phần của nhân viên: %d %s", wl.Code, s)
	}
}

// [Task 486] Số góp ý theo trạng thái (đủ 3, tổng; bán hàng → 403; tenant khác không tính) và mã KH của khách được gắn.
func TestFeedbackAPI_CountsAndCode(t *testing.T) {
	t.Parallel()
	e := newCouponEnv(t)
	other := e.staff.Tenant(t, "B")
	otherManager := "quan-ly-b2@elimaz.test"
	e.staff.Grant(t, other, e.staff.User(t, other, otherManager, "active"), rolePerms(t, "manager"), "all", "")
	var cust customer.Customer
	if err := tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		var err error
		cust, err = customer.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}.CreateCustomer(t.Context(), tx,
			customer.CreateInput{FullName: "Chị Mơ", Phone: "0912888777"}, customer.Actor{Type: customer.ActorSystem}, e.now)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	e.seedFeedback(e.tenant, customer.FeedbackInput{Topic: "website", Body: "Web chậm"})
	e.seedFeedback(e.tenant, customer.FeedbackInput{Topic: "product", Body: "Áo đẹp", Contact: "0912888777"})
	expect(t, "bán hàng đếm", e.do(http.MethodGet, feedbackPath+"/counts", couponSale, "", ""), http.StatusForbidden,
		"forbidden")
	c := e.ok("đếm", e.do(http.MethodGet, feedbackPath+"/counts", couponManager, "", ""), http.StatusOK)
	sts := c["states"].([]any)
	if len(sts) != 3 || c["total"] != float64(2) || sts[0].(map[string]any)["state"] != "new" ||
		sts[0].(map[string]any)["count"] != float64(2) || sts[2].(map[string]any)["count"] != float64(0) {
		t.Fatalf("số góp ý: %v", c)
	}
	page := e.ok("đọc", e.do(http.MethodGet, feedbackPath+"?topic=product", couponManager, "", ""), http.StatusOK)
	if f := page["items"].([]any)[0].(map[string]any); f["customerCode"] != cust.Code || cust.Code == "" {
		t.Fatalf("mã KH: %v, mong %q", f, cust.Code)
	}
	if b := e.ok("tenant khác", e.do(http.MethodGet, feedbackPath+"/counts", otherManager, "", ""), http.StatusOK); b["total"] != float64(0) {
		t.Fatalf("tenant khác: %v", b)
	}
}
