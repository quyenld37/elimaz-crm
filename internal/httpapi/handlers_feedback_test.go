package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

const feedbackPath = "/api/v1/feedback"

// seedFeedback gửi góp ý (người làm là khóa dịch vụ) trong tenant, trả mã.
func (e *couponEnv) seedFeedback(tenant uuid.UUID, in customer.FeedbackInput) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	w := customer.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}
	if err := tenancy.WithTenant(e.t.Context(), e.staff.App, tenant, func(tx pgx.Tx) error {
		var err error
		id, err = w.SubmitFeedback(e.t.Context(), tx, in, customer.Actor{Type: customer.ActorService}, e.now)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// [Task 455] Góp ý qua API nhân viên: bán hàng (không có customer.feedback.*) đọc, sửa → 403; quản lý đọc thấy nội dung,
// liên lạc, khách được gắn; lọc theo khách, trạng thái; con trỏ hỏng → 400; sửa trạng thái → 200 version 2 kèm tên
// người sửa; version cũ → 409 version_conflict; không trường nào → 400; góp ý lạ → 404; quản lý của tenant khác: danh
// sách rỗng, sửa → 404.
func TestFeedbackAPI_ListUpdate(t *testing.T) {
	t.Parallel()
	e := newCouponEnv(t)
	other := e.staff.Tenant(t, "B")
	otherManager := "quan-ly-b@elimaz.test"
	e.staff.Grant(t, other, e.staff.User(t, other, otherManager, "active"), rolePerms(t, "manager"), "all", "")
	var cust uuid.UUID
	if err := tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		c, err := customer.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}.CreateCustomer(t.Context(), tx,
			customer.CreateInput{FullName: "Chị Mơ", Phone: "0912888999"}, customer.Actor{Type: customer.ActorSystem}, e.now)
		cust = c.ID
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	linked := e.seedFeedback(e.tenant, customer.FeedbackInput{
		Topic: "product", Body: "<script>x</script>",
		Contact: "0912888999", Mood: new(5),
	})
	free := e.seedFeedback(e.tenant, customer.FeedbackInput{Topic: "website", Body: "Web chậm"})

	expect(t, "bán hàng đọc", e.do(http.MethodGet, feedbackPath, couponSale, "", ""), http.StatusForbidden, "forbidden")
	expect(t, "bán hàng sửa", e.do(http.MethodPatch, feedbackPath+"/"+free.String(), couponSale, "s1",
		`{"version":1,"state":"seen"}`), http.StatusForbidden, "forbidden")
	page := e.ok("đọc", e.do(http.MethodGet, feedbackPath, couponManager, "", ""), http.StatusOK)
	items, _ := page["items"].([]any)
	if len(items) != 2 || page["nextCursor"] != nil {
		t.Fatalf("danh sách: %v", page)
	}
	byCust := e.ok("lọc khách", e.do(http.MethodGet, feedbackPath+"?customerId="+cust.String(), couponManager, "", ""),
		http.StatusOK)
	got, _ := byCust["items"].([]any)
	if len(got) != 1 {
		t.Fatalf("lọc theo khách: %v", byCust)
	}
	if f, _ := got[0].(map[string]any); f["id"] != linked.String() || f["body"] != "<script>x</script>" ||
		f["contact"] != "0912888999" || f["customerId"] != cust.String() || f["customerName"] != "Chị Mơ" ||
		f["mood"] != float64(5) || f["state"] != "new" || f["version"] != float64(1) {
		t.Errorf("góp ý gắn khách: %v", f)
	}
	expect(t, "con trỏ hỏng", e.do(http.MethodGet, feedbackPath+"?cursor=abc", couponManager, "", ""),
		http.StatusBadRequest, "invalid_input")
	expect(t, "khách lạ", e.do(http.MethodGet, feedbackPath+"?customerId="+uuid.NewString(), couponManager, "", ""),
		http.StatusNotFound, "not_found")

	patch := func(email, key string, id uuid.UUID, body string) map[string]any {
		return e.ok("sửa "+key, e.do(http.MethodPatch, feedbackPath+"/"+id.String(), email, key, body), http.StatusOK)
	}
	f := patch(couponManager, "p1", free, `{"version":1,"state":"seen"}`)
	if f["state"] != "seen" || f["version"] != float64(2) || f["updatedByName"] == nil || f["body"] != "Web chậm" {
		t.Errorf("sửa trạng thái: %v", f)
	}
	f = patch(couponManager, "p2", free, `{"version":2,"note":"Đã gọi"}`)
	if f["note"] != "Đã gọi" || f["state"] != "seen" || f["version"] != float64(3) {
		t.Errorf("ghi chú: %v", f)
	}
	expect(t, "version cũ", e.do(http.MethodPatch, feedbackPath+"/"+free.String(), couponManager, "p3",
		`{"version":1,"state":"answered"}`), http.StatusConflict, "version_conflict")
	expect(t, "không trường nào", e.do(http.MethodPatch, feedbackPath+"/"+free.String(), couponManager, "p4",
		`{"version":3}`), http.StatusBadRequest, "invalid_input")
	expect(t, "góp ý lạ", e.do(http.MethodPatch, feedbackPath+"/"+uuid.NewString(), couponManager, "p5",
		`{"version":1,"state":"seen"}`), http.StatusNotFound, "not_found")
	seen := e.ok("lọc trạng thái", e.do(http.MethodGet, feedbackPath+"?state=seen", couponManager, "", ""), http.StatusOK)
	if s, _ := seen["items"].([]any); len(s) != 1 {
		t.Errorf("lọc trạng thái: %v", seen)
	}

	empty := e.ok("tenant khác đọc", e.do(http.MethodGet, feedbackPath, otherManager, "", ""), http.StatusOK)
	if s, _ := empty["items"].([]any); len(s) != 0 {
		t.Errorf("tenant khác thấy góp ý: %v", empty)
	}
	expect(t, "tenant khác sửa", e.do(http.MethodPatch, feedbackPath+"/"+linked.String(), otherManager, "b1",
		fmt.Sprintf(`{"version":%d,"state":"seen"}`, 1)), http.StatusNotFound, "not_found")
}
