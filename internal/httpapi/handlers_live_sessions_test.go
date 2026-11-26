package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/testkit"
)

// Phiên live qua HTTP (Task 170): theo dõi tay bằng đường dẫn video → 201 phiên manual, bấm lại → cùng phiên; chuỗi
// không có mã → 400 invalid_input; không có live.operate ở Page nào → 403; có ở Page khác hay Page lạ → 404; danh sách theo Page được đọc, lọc trạng thái; kết thúc
// tay → 200 ended, lần hai vẫn 200 cùng phiên; người không có Page nào thấy danh sách rỗng.
func TestLiveSessionsAPI(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	keys := cfaccesstest.AccessKeyServer(t)
	tenant := s.Tenant(t, "A")
	page := s.ChannelAccount(t, tenant, "fakechat", "page-1", "Shop thử")
	const op, viewer, other, opB = "live-op@elimaz.test", "live-xem@elimaz.test", "khac@elimaz.test", "live-op-b@elimaz.test"
	s.Grant(t, tenant, s.User(t, tenant, op, "active"), []string{"conversation.read", "live.operate"}, "page", page.String())
	s.Grant(t, tenant, s.User(t, tenant, viewer, "active"), []string{"conversation.read"}, "page", page.String())
	s.Grant(t, tenant, s.User(t, tenant, other, "active"), []string{"conversation.read"}, "page", uuid.NewString())
	s.Grant(t, tenant, s.User(t, tenant, opB, "active"), []string{"conversation.read", "live.operate"}, "page", uuid.NewString())
	now := time.Now().UTC().Truncate(time.Second)
	d := testServerDeps(t, s, keys, 0)
	d.Realtime, d.Now = &realtimetest.RecordingBus{}, func() time.Time { return now }
	srv := NewServer(d)
	n := 0
	send := func(method, path, email, body string) *httptest.ResponseRecorder {
		key := ""
		if method != http.MethodGet {
			n++
			key = fmt.Sprintf("live-%d-%s", n, uuid.NewString())
		}
		return serveAs(srv, keys, method, path, email, key, body)
	}
	start := "/api/v1/channels/" + page.String() + "/live-sessions"
	got := mustOK(t, "startLiveSessionManually", send(http.MethodPost, start, op,
		`{"videoIdOrUrl":"https://www.facebook.com/elimaz/videos/123456789/"}`), http.StatusCreated)
	if got["liveVideoId"] != "123456789" || got["status"] != "live" || got["detectedBy"] != "manual" ||
		got["pageId"] != page.String() || got["receivedCount"] != float64(0) {
		t.Fatalf("phiên tay: %v", got)
	}
	id := got["id"].(string) //nolint:forcetypeassert // test
	again := mustOK(t, "startLiveSessionManually lần 2", send(http.MethodPost, start, op, `{"videoIdOrUrl":"123456789"}`), http.StatusCreated)
	if again["id"] != id {
		t.Errorf("bấm lại tạo phiên mới: %v", again)
	}
	for _, c := range []struct {
		path, email, body, code string
		status                  int
	}{
		{start, op, `{"videoIdOrUrl":"https://www.facebook.com/elimaz/posts/1"}`, "invalid_input", http.StatusBadRequest},
		{start, viewer, `{"videoIdOrUrl":"1"}`, "forbidden", http.StatusForbidden},
		{start, opB, `{"videoIdOrUrl":"1"}`, "not_found", http.StatusNotFound},
		{"/api/v1/channels/" + uuid.NewString() + "/live-sessions", op, `{"videoIdOrUrl":"1"}`, "not_found", http.StatusNotFound},
		{"/api/v1/live-sessions/" + id + "/end", viewer, ``, "forbidden", http.StatusForbidden},
		{"/api/v1/live-sessions/" + id + "/end", opB, ``, "not_found", http.StatusNotFound},
		{"/api/v1/live-sessions/" + uuid.NewString() + "/end", op, ``, "not_found", http.StatusNotFound},
	} {
		w := send(http.MethodPost, c.path, c.email, c.body)
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != c.status || body["error"] != c.code {
			t.Errorf("%s %s: %d %s", c.email, c.path, w.Code, w.Body)
		}
	}
	checkLiveListAndEnd(t, send, id, now)
}

// checkLiveListAndEnd: danh sách theo Page được đọc, lọc trạng thái và since; kết thúc tay hai lần cùng kết quả.
func checkLiveListAndEnd(t *testing.T, send func(method, path, email, body string) *httptest.ResponseRecorder, id string,
	now time.Time,
) {
	t.Helper()
	const op, viewer, other = "live-op@elimaz.test", "live-xem@elimaz.test", "khac@elimaz.test"
	list := func(email, q string) []any {
		t.Helper()
		return mustOK(t, "listLiveSessions", send(http.MethodGet, "/api/v1/live-sessions"+q, email, ""), http.StatusOK)["items"].([]any) //nolint:forcetypeassert // test
	}
	if l := list(viewer, "?status=live"); len(l) != 1 {
		t.Errorf("danh sách live: %v", l)
	}
	if l := list(other, ""); len(l) != 0 {
		t.Errorf("Page khác thấy phiên: %v", l)
	}
	end := mustOK(t, "endLiveSessionManually", send(http.MethodPost, "/api/v1/live-sessions/"+id+"/end", op, ""), http.StatusOK)
	if end["status"] != "ended" || end["endedAt"] == nil || end["version"] != float64(2) {
		t.Errorf("kết thúc: %v", end)
	}
	end2 := mustOK(t, "endLiveSessionManually lần 2", send(http.MethodPost, "/api/v1/live-sessions/"+id+"/end", op, ""), http.StatusOK)
	if end2["version"] != float64(2) || end2["endedAt"] != end["endedAt"] {
		t.Errorf("kết thúc lần 2: %v", end2)
	}
	if l := list(viewer, "?status=live"); len(l) != 0 {
		t.Errorf("còn phiên live: %v", l)
	}
	if l := list(viewer, "?status=ended&since="+now.Add(-time.Minute).Format(time.RFC3339)); len(l) != 1 {
		t.Errorf("lọc ended từ since: %v", l)
	}
	if l := list(viewer, "?since="+now.Add(time.Minute).Format(time.RFC3339)); len(l) != 0 {
		t.Errorf("since sau giờ bắt đầu: %v", l)
	}
}
