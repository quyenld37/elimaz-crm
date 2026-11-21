package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/testkit"
)

const (
	settingsManager = "cai-dat@elimaz.test"
	settingsSale    = "ban-hang@elimaz.test"
)

type orderSettingsEnv struct {
	t      *testing.T
	staff  *testkit.Staff
	keys   *cfaccesstest.AccessKeys
	srv    http.Handler
	tenant uuid.UUID
}

// rolePerms: quyền của vai trò mẫu key (identity.DefaultRoles) dạng chuỗi.
func rolePerms(t *testing.T, key string) []string {
	t.Helper()
	for _, r := range identity.DefaultRoles {
		if r.Key == key {
			out := make([]string, 0, len(r.Permissions))
			for _, p := range r.Permissions {
				out = append(out, string(p))
			}
			return out
		}
	}
	t.Fatalf("không có vai trò mẫu %s", key)
	return nil
}

func newOrderSettingsEnv(t *testing.T) *orderSettingsEnv {
	t.Helper()
	e := &orderSettingsEnv{t: t, staff: testkit.OpenStaff(t), keys: cfaccesstest.AccessKeyServer(t)}
	e.tenant = e.staff.Tenant(t, "A")
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error { return order.EnsureDefaults(t.Context(), tx, e.tenant) })
	m := e.staff.User(t, e.tenant, settingsManager, "active")
	e.staff.Grant(t, e.tenant, m, rolePerms(t, "manager"), "all", "")
	s := e.staff.User(t, e.tenant, settingsSale, "active")
	e.staff.Grant(t, e.tenant, s, rolePerms(t, "sale"), "all", "")
	e.srv = testServer(t, e.staff, e.keys, 0)
	return e
}

func (e *orderSettingsEnv) do(method, path, email, key, body string) *httptest.ResponseRecorder {
	return serveAs(e.srv, e.keys, method, path, email, key, body)
}

func (e *orderSettingsEnv) ok(name string, rec *httptest.ResponseRecorder, status int) map[string]any {
	e.t.Helper()
	if rec.Code != status {
		e.t.Fatalf("%s: %d %s", name, rec.Code, rec.Body.String())
	}
	return decodeJSON(e.t, rec)
}

func TestOrderSettingsAPI(t *testing.T) {
	e := newOrderSettingsEnv(t)
	got := e.ok("sale đọc cài đặt", e.do(http.MethodGet, "/api/v1/order-settings", settingsSale, "", ""), http.StatusOK)
	if got["draftTtlMinutes"] != float64(1440) || got["duplicateWindowMinutes"] != float64(1440) ||
		got["discountApprovalThresholdBp"] != float64(1000) || got["returnWarningDays"] != float64(7) || got["version"] != float64(1) {
		t.Fatalf("mặc định: %v", got)
	}

	rec := e.do(http.MethodPatch, "/api/v1/order-settings", settingsManager, "s1", `{"version":1,"draftTtlMinutes":5}`)
	if b := decodeJSON(t, rec); rec.Code != http.StatusUnprocessableEntity || b["error"] != "setting_out_of_range" ||
		b["field"] != "draftTtlMinutes" || b["request_id"] == nil {
		t.Errorf("hạn nháp 5 phút: 422 setting_out_of_range phải nêu tên trường: %d %v", rec.Code, b)
	}
	expect(t, "sale sửa cài đặt", e.do(http.MethodPatch, "/api/v1/order-settings", settingsSale, "s2",
		`{"version":1,"discountApprovalThresholdBp":2000}`), http.StatusForbidden, "forbidden")

	got = e.ok("đổi ngưỡng", e.do(http.MethodPatch, "/api/v1/order-settings", settingsManager, "s3",
		`{"version":1,"discountApprovalThresholdBp":2000}`), http.StatusOK)
	if got["discountApprovalThresholdBp"] != float64(2000) || got["version"] != float64(2) || got["draftTtlMinutes"] != float64(1440) {
		t.Fatalf("sau khi đổi ngưỡng: %v", got)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'order.settings.update'
		AND entity_type = 'order.settings' AND (before->>'discountApprovalThresholdBp')::int = 1000
		AND (after->>'discountApprovalThresholdBp')::int = 2000 AND changed_fields @> ARRAY['discountApprovalThresholdBp']`); n != 1 {
		t.Errorf("audit 1000 → 2000: %d dòng", n)
	}
	expect(t, "version cũ", e.do(http.MethodPatch, "/api/v1/order-settings", settingsManager, "s4",
		`{"version":1,"returnWarningDays":3}`), http.StatusConflict, "version_conflict")
	// Gửi lại cùng khóa: trả lại đúng câu trả lời đã lưu, không sửa lần hai.
	again := e.ok("gửi lại s3", e.do(http.MethodPatch, "/api/v1/order-settings", settingsManager, "s3",
		`{"version":1,"discountApprovalThresholdBp":2000}`), http.StatusOK)
	if again["version"] != float64(2) {
		t.Errorf("gửi lại cùng khóa: %v", again)
	}
}

func TestTrafficSourcesAPI(t *testing.T) {
	e := newOrderSettingsEnv(t)
	list := e.ok("sale đọc nguồn", e.do(http.MethodGet, "/api/v1/traffic-sources", settingsSale, "", ""), http.StatusOK)
	items, _ := list["items"].([]any)
	if len(items) != 10 {
		t.Fatalf("10 nguồn mặc định: %v", list)
	}
	var zalo map[string]any
	for _, it := range items {
		if m, _ := it.(map[string]any); m["key"] == "zalo" {
			zalo = m
		}
	}
	if zalo == nil || zalo["name"] != "Zalo" || zalo["active"] != true {
		t.Fatalf("nguồn Zalo: %v", items)
	}

	expect(t, "sale tạo nguồn", e.do(http.MethodPost, "/api/v1/traffic-sources", settingsSale, "c1", `{"name":"Tiktok"}`),
		http.StatusForbidden, "forbidden")
	created := e.ok("tạo nguồn", e.do(http.MethodPost, "/api/v1/traffic-sources", settingsManager, "c2", `{"name":"Tiktok"}`),
		http.StatusCreated)
	if created["name"] != "Tiktok" || created["active"] != true || created["position"] != float64(11) {
		t.Fatalf("nguồn mới: %v", created)
	}
	if _, ok := created["key"]; ok {
		t.Errorf("nguồn nhân viên tạo không có key: %v", created)
	}
	expect(t, "tên trùng", e.do(http.MethodPost, "/api/v1/traffic-sources", settingsManager, "c3", `{"name":"Zalo"}`),
		http.StatusConflict, "already_exists")

	zaloPath := fmt.Sprintf("/api/v1/traffic-sources/%s", zalo["id"])
	expect(t, "sale tắt nguồn", e.do(http.MethodPatch, zaloPath, settingsSale, "u1", `{"version":1,"active":false}`),
		http.StatusForbidden, "forbidden")
	off := e.ok("tắt Zalo", e.do(http.MethodPatch, zaloPath, settingsManager, "u2", `{"version":1,"active":false}`), http.StatusOK)
	if off["active"] != false || off["version"] != float64(2) || off["key"] != "zalo" {
		t.Fatalf("Zalo sau khi tắt: %v", off)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'order.traffic_source.update'
		AND entity_id = $1 AND before->>'active' = 'true' AND after->>'active' = 'false'`, zalo["id"]); n != 1 {
		t.Errorf("audit tắt nguồn: %d dòng", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'order.traffic_source.create'
		AND entity_id = $1 AND after->>'name' = 'Tiktok'`, created["id"]); n != 1 {
		t.Errorf("audit tạo nguồn: %d dòng", n)
	}
	expect(t, "version cũ", e.do(http.MethodPatch, zaloPath, settingsManager, "u3", `{"version":1,"name":"Zalo OA"}`),
		http.StatusConflict, "version_conflict")
	expect(t, "đổi sang tên đã có", e.do(http.MethodPatch, zaloPath, settingsManager, "u4", `{"version":2,"name":"Tiktok"}`),
		http.StatusConflict, "already_exists")
	expect(t, "nguồn không có", e.do(http.MethodPatch, "/api/v1/traffic-sources/"+uuid.NewString(), settingsManager, "u5",
		`{"version":1,"active":false}`), http.StatusNotFound, "not_found")
}
