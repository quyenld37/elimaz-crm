package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

const (
	bdSettingsPath  = "/api/v1/birthday-settings"
	bdRunsPath      = "/api/v1/birthday-runs"
	bdTodoPath      = "/api/v1/birthday-gifts/todo"
	bdMessagedPath  = "/api/v1/birthday-gifts/messaged"
	bdCustomersPath = "/api/v1/birthday-customers"
	bdExportPath    = "/api/v1/birthday-customers/export"
	bdSettingsBody  = `{"enabled":true,"amount":50000,"minOrder":0,"daysBefore":7,"validDays":14,"minLeadDays":30,"version":%d}`
)

// seedBirthdays đăng ký sinh nhật 18/12 cho các SĐT (lúc e.now − 60 ngày); zalo[i]: khách đồng ý Zalo.
func (e *couponEnv) seedBirthdays(phones []string, zalo []bool) {
	e.t.Helper()
	w := customer.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}
	for i, p := range phones {
		if err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
			_, _, err := w.RegisterBirthday(e.t.Context(), tx, customer.BirthdayInput{
				Phone: p, FullName: "Khách " + p[len(p)-3:], Day: 18, Month: 12, Zalo: zalo[i],
			}, customer.Actor{Type: customer.ActorService}, e.now.Add(-60*24*time.Hour))
			return err //nolint:wrapcheck // test
		}); err != nil {
			e.t.Fatal(err)
		}
	}
}

// deliverAll đặt mọi khách có một đơn giao (đủ điều kiện nhận quà).
func (e *couponEnv) deliverAll() {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), `UPDATE customer.customers SET delivered_count = 1`)
		return err //nolint:wrapcheck // test
	})
}

// [WB05] Quà sinh nhật qua API nhân viên: bán hàng (promotion.read) đọc cài đặt, ghi → 403; mệnh giá 10.500 → 422
// invalid_gift_settings; lưu → version 2 kèm tên người sửa; "Tạo mã ngay" → 2 quà, audit promotion.birthday.run; lượt
// đang chạy → 409 another_run_in_progress; cài đặt có lượt cuối; Cần nhắn có 2 quà kèm SĐT; Đã nhắn 1 mã → rời Cần nhắn,
// Tất cả khách đếm given 1, manual 1; xuất CSV có BOM, tiêu đề, 2 dòng, audit promotion.birthday.export có số dòng; gọi
// lại cùng khóa → 409 already_processed (no-replay). Audit không chứa SĐT.
func TestBirthdayAPI_SettingsRunTodoExport(t *testing.T) {
	t.Parallel()
	e := newCouponEnv(t)
	e.checkBirthdaySettingsAPI()
	e.seedBirthdays([]string{"0912000001", "0912000002"}, []bool{true, false})
	e.deliverAll()
	expect(t, "sale chạy", e.do(http.MethodPost, bdRunsPath, couponSale, "r0", ""), http.StatusForbidden, "forbidden")
	run := e.ok("tạo mã ngay", e.do(http.MethodPost, bdRunsPath, couponManager, "r1", ""), http.StatusOK)
	if run["given"] != float64(2) || run["trigger"] != "manual" || run["startedBy"] == nil {
		t.Fatalf("lượt: %v", run)
	}
	// Lượt khác đang giữ khóa → 409.
	if err := tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		if _, err := (promotion.Writer{}).RunBirthdayGifts(t.Context(), tx, promotion.BirthdayTriggerSchedule,
			promotion.Actor{TenantID: e.tenant}, e.now); err != nil {
			return err //nolint:wrapcheck // test
		}
		expect(t, "lượt đang chạy", e.do(http.MethodPost, bdRunsPath, couponManager, "r2", ""), http.StatusConflict,
			"another_run_in_progress")
		return nil
	}); err != nil && !errors.Is(err, promotion.ErrRunInProgress) {
		t.Fatal(err)
	}
	if g := e.ok("đọc lại", e.do(http.MethodGet, bdSettingsPath, couponSale, "", ""), http.StatusOK); g["lastRun"] == nil {
		t.Fatalf("lượt cuối: %v", g)
	}
	todo := e.ok("cần nhắn", e.do(http.MethodGet, bdTodoPath, couponSale, "", ""), http.StatusOK)
	items, _ := todo["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("cần nhắn: %v", todo)
	}
	first, _ := items[0].(map[string]any)
	code, _ := first["code"].(string)
	if code == "" || !strings.HasPrefix(first["phone"].(string), "+8491200000") { //nolint:forcetypeassert // test
		t.Fatalf("mục cần nhắn: %v", first)
	}
	expect(t, "sale nhắn", e.do(http.MethodPost, bdMessagedPath, couponSale, "m0", `{"codes":["`+code+`"]}`),
		http.StatusForbidden, "forbidden")
	m := e.ok("đã nhắn", e.do(http.MethodPost, bdMessagedPath, couponManager, "m1", `{"codes":["`+code+`","KHONGCO-1"]}`),
		http.StatusOK)
	if marked, _ := m["marked"].([]any); len(marked) != 1 || marked[0] != code {
		t.Fatalf("đã nhắn: %v", m)
	}
	if todo := e.ok("cần nhắn sau", e.do(http.MethodGet, bdTodoPath, couponSale, "", ""), http.StatusOK); len(todo["items"].([]any)) != 1 { //nolint:forcetypeassert // test
		t.Fatalf("cần nhắn sau khi nhắn: %v", todo)
	}
	all := e.ok("tất cả khách", e.do(http.MethodGet, bdCustomersPath+"?state=all&limit=10", couponSale, "", ""), http.StatusOK)
	counts, _ := all["stateCounts"].(map[string]any)
	if all["total"] != float64(2) || counts["given"] != float64(1) || counts["manual"] != float64(1) {
		t.Fatalf("tất cả khách: %v", all)
	}
	expect(t, "trạng thái lạ", e.do(http.MethodGet, bdCustomersPath+"?state=xyz", couponSale, "", ""), http.StatusBadRequest, "")
	e.checkBirthdayExportAPI()
}

// checkBirthdaySettingsAPI: mặc định tắt version 1; bán hàng lưu → 403; 10.500 → 422; lưu → version 2 kèm tên người
// sửa; version cũ → 409.
func (e *couponEnv) checkBirthdaySettingsAPI() {
	t := e.t
	t.Helper()
	got := e.ok("đọc mặc định", e.do(http.MethodGet, bdSettingsPath, couponSale, "", ""), http.StatusOK)
	if got["enabled"] != false || got["version"] != float64(1) || got["amount"] != float64(50000) || got["lastRun"] != nil {
		t.Fatalf("mặc định: %v", got)
	}
	put := func(email, key, body string) map[string]any {
		return e.ok("lưu", e.do(http.MethodPut, bdSettingsPath, email, key, body), http.StatusOK)
	}
	expect(t, "sale lưu", e.do(http.MethodPut, bdSettingsPath, couponSale, "s0", fmt.Sprintf(bdSettingsBody, 1)),
		http.StatusForbidden, "forbidden")
	expect(t, "10.500", e.do(http.MethodPut, bdSettingsPath, couponManager, "s1",
		strings.Replace(fmt.Sprintf(bdSettingsBody, 1), "50000", "10500", 1)), http.StatusUnprocessableEntity, "invalid_gift_settings")
	saved := put(couponManager, "s2", fmt.Sprintf(bdSettingsBody, 1))
	if saved["enabled"] != true || saved["version"] != float64(2) || saved["updatedByName"] != couponManager {
		t.Fatalf("lưu: %v", saved)
	}
	expect(t, "version cũ", e.do(http.MethodPut, bdSettingsPath, couponManager, "s3", fmt.Sprintf(bdSettingsBody, 1)),
		http.StatusConflict, "version_conflict")
}

// checkBirthdayExportAPI: bán hàng xuất → 403; xuất → CSV có BOM, tiêu đề, 2 dòng; cùng khóa → 409; audit đủ, có số
// dòng, không SĐT.
func (e *couponEnv) checkBirthdayExportAPI() {
	t := e.t
	t.Helper()
	expect(t, "sale xuất", e.do(http.MethodPost, bdExportPath, couponSale, "x0", `{}`), http.StatusForbidden, "forbidden")
	x := e.ok("xuất", e.do(http.MethodPost, bdExportPath, couponManager, "x1", `{"state":"all"}`), http.StatusOK)
	csv, _ := x["csv"].(string)
	lines := strings.Split(strings.TrimRight(csv, "\r\n"), "\r\n")
	if x["rows"] != float64(2) || !strings.HasPrefix(csv, "\xef\xbb\xbf") || len(lines) != 3 ||
		!strings.Contains(csv, "+84912000001") || !strings.HasSuffix(x["filename"].(string), ".csv") { //nolint:forcetypeassert // test
		t.Fatalf("xuất: %v", x)
	}
	expect(t, "xuất lại cùng khóa", e.do(http.MethodPost, bdExportPath, couponManager, "x1", `{"state":"all"}`),
		http.StatusConflict, "already_processed")
	for action, n := range map[string]int{
		"promotion.birthday.settings": 1, "promotion.birthday.run": 1, "promotion.birthday.messaged": 1,
		"promotion.birthday.export": 1,
	} {
		if got := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = $1
			AND actor_type = 'staff'`, action); got != n {
			t.Errorf("audit %s: %d, muốn %d", action, got, n)
		}
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'promotion.birthday.export'
		AND (after->>'rows')::int = 2`); n != 1 {
		t.Errorf("audit xuất có số dòng: %d", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs
		WHERE action LIKE 'promotion.birthday.%' AND row_to_json(audit_logs)::text LIKE '%912000%'`); n != 0 {
		t.Errorf("audit chứa SĐT: %d", n)
	}
}
