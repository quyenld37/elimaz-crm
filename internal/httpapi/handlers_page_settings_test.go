package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func pageSettingsBody(src uuid.UUID, slot2From string, version int) string {
	return fmt.Sprintf(`{"roundRobinEnabled":true,"roundRobinUserIds":[],"inboxTrafficSourceId":%q,
		"liveSlots":[{"from":"11:00","to":"14:00","trafficSourceId":%q},{"from":%q,"to":"16:00","trafficSourceId":%q}],
		"commentPollMode":"auto","automationMaxAgeMinutes":30,"expectedVersion":%d}`, src, src, slot2From, src, version)
}

// Cài đặt Page: đọc lần đầu → mặc định version 0; hai khung giờ chồng nhau → 422 invalid_input; nguồn đơn lạ → 422
// traffic_source_inactive; hợp lệ → 200 version 1, audit; version cũ → 409; người chỉ đọc → 403; cài đặt Page khác →
// 404; Page không có → 404.
func TestPageSettingsAPI(t *testing.T) {
	t.Parallel()
	e := newAssignAPIEnv(t)
	path := "/api/v1/channels/" + e.page1.String() + "/page-settings"
	code, s := e.do(http.MethodGet, path, cvAn, "", "")
	if code != http.StatusOK || s["version"] != float64(0) || s["roundRobinEnabled"] != false || s["commentPollMode"] != "auto" {
		t.Fatalf("mặc định: %d %v", code, s)
	}
	expect(t, "đọc Page khác", serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/channels/"+e.page2.String()+"/page-settings",
		cvAn, "", ""), http.StatusNotFound, "not_found")
	src := e.staff.TrafficSource(t, e.tenant, "Live ca trưa", true)
	expect(t, "hai khung chồng nhau", serveAs(e.srv, e.keys, http.MethodPut, path, cvManager, "p1", pageSettingsBody(src, "13:00", 0)),
		http.StatusUnprocessableEntity, "invalid_input")
	expect(t, "nguồn đơn lạ", serveAs(e.srv, e.keys, http.MethodPut, path, cvManager, "p2", pageSettingsBody(uuid.New(), "14:00", 0)),
		http.StatusUnprocessableEntity, "traffic_source_inactive")
	expect(t, "người chỉ đọc", serveAs(e.srv, e.keys, http.MethodPut, path, cvAn, "p3", pageSettingsBody(src, "14:00", 0)),
		http.StatusForbidden, "forbidden")
	expect(t, "cài đặt Page khác", serveAs(e.srv, e.keys, http.MethodPut, "/api/v1/channels/"+e.page2.String()+"/page-settings",
		cvPageOnly, "p4", pageSettingsBody(src, "14:00", 0)), http.StatusNotFound, "not_found")
	code, s = e.do(http.MethodPut, path, cvPageOnly, "p5", pageSettingsBody(src, "14:00", 0))
	slots, _ := s["liveSlots"].([]any)
	if code != http.StatusOK || s["version"] != float64(1) || len(slots) != 2 || s["inboxTrafficSourceId"] != src.String() {
		t.Fatalf("lưu: %d %v", code, s)
	}
	expect(t, "version cũ", serveAs(e.srv, e.keys, http.MethodPut, path, cvManager, "p6", pageSettingsBody(src, "14:00", 0)),
		http.StatusConflict, "version_conflict")
	if code, s = e.do(http.MethodGet, path, cvAn, "", ""); code != http.StatusOK || s["version"] != float64(1) {
		t.Fatalf("đọc lại: %d %v", code, s)
	}
	expect(t, "Page không có", serveAs(e.srv, e.keys, http.MethodPut, "/api/v1/channels/"+uuid.NewString()+"/page-settings",
		cvManager, "p7", pageSettingsBody(src, "14:00", 0)), http.StatusNotFound, "not_found")
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'conversation.page_settings_updated'
		AND entity_id = $1`, e.page1.String()); n != 1 {
		t.Fatalf("audit = %d", n)
	}
}
