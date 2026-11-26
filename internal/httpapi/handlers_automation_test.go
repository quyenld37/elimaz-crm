package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func autoHideBody(mode string, version int) string {
	return fmt.Sprintf(`{"enabled":true,"config":{"kind":"auto_hide","mode":%q,"exceptPostIds":["104_1"]},"expectedVersion":%d}`,
		mode, version)
}

// Quy tắc tự động: đọc lần đầu → đủ bốn loại, tắt, version 0; người chỉ đọc PUT → 403; Page khác → 404; chế độ lạ →
// 400 (hợp đồng); config.kind khác loại trên đường dẫn → 422 invalid_input; hợp lệ → 200 version 1, audit, view có một dòng; version cũ → 409;
// đọc lại thấy cấu hình đã lưu.
func TestAutomationRulesAPI(t *testing.T) {
	t.Parallel()
	e := newAssignAPIEnv(t)
	base := "/api/v1/channels/" + e.page1.String() + "/automation-rules"
	code, s := e.do(http.MethodGet, base, cvAn, "", "")
	items, _ := s["items"].([]any)
	if code != http.StatusOK || len(items) != 4 {
		t.Fatalf("mặc định: %d %v", code, s)
	}
	for i, kind := range []string{"auto_hide", "auto_reply_comment", "greeting", "follow_up"} {
		it, _ := items[i].(map[string]any)
		cfg, _ := it["config"].(map[string]any)
		if it["kind"] != kind || it["enabled"] != false || it["version"] != float64(0) || cfg["kind"] != kind {
			t.Fatalf("mặc định %s: %v", kind, it)
		}
	}
	path := base + "/auto_hide"
	expect(t, "người chỉ đọc", serveAs(e.srv, e.keys, http.MethodPut, path, cvAn, "a1", autoHideBody("all", 0)),
		http.StatusForbidden, "forbidden")
	expect(t, "Page khác", serveAs(e.srv, e.keys, http.MethodPut, "/api/v1/channels/"+e.page2.String()+"/automation-rules/auto_hide",
		cvPageOnly, "a2", autoHideBody("all", 0)), http.StatusNotFound, "not_found")
	expect(t, "chế độ lạ", serveAs(e.srv, e.keys, http.MethodPut, path, cvManager, "a3", autoHideBody("abc", 0)),
		http.StatusBadRequest, "")
	expect(t, "khác loại", serveAs(e.srv, e.keys, http.MethodPut, base+"/greeting", cvManager, "a4", autoHideBody("all", 0)),
		http.StatusUnprocessableEntity, "invalid_input")
	code, s = e.do(http.MethodPut, path, cvPageOnly, "a5", autoHideBody("phone_only", 0))
	cfg, _ := s["config"].(map[string]any)
	if code != http.StatusOK || s["version"] != float64(1) || s["enabled"] != true || cfg["mode"] != "phone_only" {
		t.Fatalf("lưu: %d %v", code, s)
	}
	expect(t, "version cũ", serveAs(e.srv, e.keys, http.MethodPut, path, cvManager, "a6", autoHideBody("all", 0)),
		http.StatusConflict, "version_conflict")
	code, s = e.do(http.MethodGet, base, cvAn, "", "")
	items, _ = s["items"].([]any)
	first, _ := items[0].(map[string]any)
	if code != http.StatusOK || first["version"] != float64(1) || first["updatedAt"] == nil {
		t.Fatalf("đọc lại: %d %v", code, first)
	}
	expect(t, "Page không có", serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/channels/"+uuid.NewString()+"/automation-rules",
		cvManager, "", ""), http.StatusNotFound, "not_found")
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'conversation.automation_rule_updated'
		AND entity_id = $1`, e.page1.String()); n != 1 {
		t.Fatalf("audit = %d", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.automation_rules_v WHERE channel_account_id = $1`,
		e.page1); n != 1 {
		t.Fatalf("view = %d dòng", n)
	}
}
