package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const webIntakePath = "/api/v1/web-intake"

// Tạm dừng, mở lại nhận đơn web qua API nhân viên: sale (chỉ order.read) đọc được, ghi → 403; tạm dừng thiếu lý do →
// 422 invalid_input; lý do 301 ký tự → 400; tạm dừng → 200 kèm lý do, người (mã, tên), giờ của máy chủ, version 2;
// version cũ → 409; mở lại không lý do → 200; mỗi lần đổi một audit order.web_intake.set có lý do.
func TestWebIntakeAPI_PauseReopen(t *testing.T) {
	t.Parallel()
	e := newCouponEnv(t)
	got := e.ok("đọc mặc định", e.do(http.MethodGet, webIntakePath, couponSale, "", ""), http.StatusOK)
	if got["paused"] != false || got["version"] != float64(1) || got["reason"] != nil || got["changedBy"] != nil {
		t.Fatalf("mặc định: %v", got)
	}
	put := func(email, key, body string) *httptest.ResponseRecorder {
		return e.do(http.MethodPut, webIntakePath, email, key, body)
	}
	expect(t, "sale tạm dừng", put(couponSale, "i0", `{"paused":true,"reason":"x","version":1}`), http.StatusForbidden, "forbidden")
	expect(t, "thiếu lý do", put(couponManager, "i1", `{"paused":true,"reason":"   ","version":1}`),
		http.StatusUnprocessableEntity, "invalid_input")
	expect(t, "không gửi lý do", put(couponManager, "i2", `{"paused":true,"version":1}`),
		http.StatusUnprocessableEntity, "invalid_input")
	expect(t, "301 ký tự", put(couponManager, "i3", `{"paused":true,"reason":"`+strings.Repeat("ạ", 301)+`","version":1}`),
		http.StatusBadRequest, "")
	p := e.ok("tạm dừng", put(couponManager, "i4", `{"paused":true,"reason":"Nghỉ Tết","version":1}`), http.StatusOK)
	if p["paused"] != true || p["reason"] != "Nghỉ Tết" || p["version"] != float64(2) || p["changedBy"] == nil ||
		p["changedByName"] != couponManager || p["changedAt"] != e.now.UTC().Format(time.RFC3339) {
		t.Fatalf("sau tạm dừng: %v", p)
	}
	if g := e.ok("đọc", e.do(http.MethodGet, webIntakePath, couponSale, "", ""), http.StatusOK); g["paused"] != true ||
		g["reason"] != "Nghỉ Tết" || g["changedByName"] != couponManager {
		t.Fatalf("sale đọc lại: %v", g)
	}
	expect(t, "version cũ", put(couponManager, "i5", `{"paused":false,"version":1}`), http.StatusConflict, "version_conflict")
	r := e.ok("mở lại", put(couponManager, "i6", `{"paused":false,"version":2}`), http.StatusOK)
	if r["paused"] != false || r["reason"] != nil || r["version"] != float64(3) {
		t.Fatalf("sau mở lại: %v", r)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'order.web_intake.set'`); n != 2 {
		t.Fatalf("audit: %d dòng, mong 2", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'order.web_intake.set'
		AND after->>'reason' = 'Nghỉ Tết' AND (after->>'paused')::boolean AND NOT (before->>'paused')::boolean`); n != 1 {
		t.Fatalf("audit tạm dừng có lý do: %d dòng, mong 1", n)
	}
}

// 50 lần setWebIntake song song cùng khóa chống trùng → mọi câu trả lời 200 (hay 409 request_in_progress), trạng thái
// đổi một lần (version 2), một audit, một sự kiện.
func TestWebIntakeAPI_ConcurrentSameKey(t *testing.T) {
	t.Parallel()
	e := newCouponEnv(t)
	const body = `{"paused":true,"reason":"Kiểm kho","version":1}`
	var mu sync.Mutex
	okBodies := map[string]int{}
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			rec := e.do(http.MethodPut, webIntakePath, couponManager, "same-key", body)
			mu.Lock()
			defer mu.Unlock()
			switch rec.Code {
			case http.StatusOK:
				okBodies[rec.Body.String()]++
			case http.StatusConflict:
				expect(t, "song song", rec, http.StatusConflict, "request_in_progress")
			default:
				t.Errorf("mã %d %s", rec.Code, rec.Body.String())
			}
		})
	}
	wg.Wait()
	if len(okBodies) != 1 {
		t.Fatalf("câu trả lời 200 khác nhau hoặc không có: %d loại", len(okBodies))
	}
	if g := e.ok("đọc", e.do(http.MethodGet, webIntakePath, couponSale, "", ""), http.StatusOK); g["version"] != float64(2) ||
		g["paused"] != true {
		t.Fatalf("sau 50 lần: %v", g)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'order.web_intake.set'`); n != 1 {
		t.Fatalf("audit: %d dòng, mong 1", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.domain_events WHERE topic = 'order.web_intake.changed'`); n != 1 {
		t.Fatalf("sự kiện: %d, mong 1", n)
	}
}

// Tenant khác: trạng thái riêng từng shop — quản lý shop B thấy mặc định, tạm dừng của B không đổi shop A.
func TestWebIntakeAPI_OtherTenant(t *testing.T) {
	t.Parallel()
	e := newCouponEnv(t)
	e.ok("A tạm dừng", e.do(http.MethodPut, webIntakePath, couponManager, "a1", `{"paused":true,"reason":"A nghỉ","version":1}`),
		http.StatusOK)
	tb := e.staff.Tenant(t, "B")
	mb := e.staff.User(t, tb, "quan-ly-b@elimaz.test", "active")
	e.staff.Grant(t, tb, mb, rolePerms(t, "manager"), "all", "")
	if g := e.ok("B đọc", e.do(http.MethodGet, webIntakePath, "quan-ly-b@elimaz.test", "", ""), http.StatusOK); g["paused"] != false ||
		g["version"] != float64(1) || g["reason"] != nil {
		t.Fatalf("B thấy: %v", g)
	}
	e.ok("B tạm dừng", e.do(http.MethodPut, webIntakePath, "quan-ly-b@elimaz.test", "b1", `{"paused":true,"reason":"B nghỉ","version":1}`),
		http.StatusOK)
	if g := e.ok("A đọc", e.do(http.MethodGet, webIntakePath, couponSale, "", ""), http.StatusOK); g["reason"] != "A nghỉ" ||
		g["version"] != float64(2) {
		t.Fatalf("A sau khi B đổi: %v", g)
	}
}
