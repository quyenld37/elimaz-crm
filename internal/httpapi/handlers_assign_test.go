package httpapi

import (
	"fmt"
	"net/http"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/adapters/fake"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/convtest"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/testkit"
)

const (
	cvAn       = "an-chat@elimaz.test"
	cvBinh     = "binh-chat@elimaz.test"
	cvManager  = "quanly-chat@elimaz.test"
	cvOutside  = "ngoai-chat@elimaz.test"
	cvPageOnly = "caidat-page@elimaz.test"
)

// assignAPIEnv: Page 1 (fakechat), Page 2; An, Bình đọc, trả lời Page 1; quản lý đọc, trả lời, giao, cài đặt toàn shop;
// người ngoài đọc, giao Page 2; cvPageOnly đọc, cài đặt chỉ Page 1.
type assignAPIEnv struct {
	t            *testing.T
	staff        *testkit.Staff
	keys         *cfaccesstest.AccessKeys
	srv          http.Handler
	bus          *realtimetest.RecordingBus
	tenant       uuid.UUID
	page1, page2 uuid.UUID
	ids          map[string]uuid.UUID
}

func newAssignAPIEnv(t *testing.T) *assignAPIEnv {
	t.Helper()
	s := testkit.OpenStaff(t)
	e := &assignAPIEnv{
		t: t, staff: s, keys: cfaccesstest.AccessKeyServer(t), bus: &realtimetest.RecordingBus{},
		ids: map[string]uuid.UUID{},
	}
	e.tenant = s.Tenant(t, "A")
	e.page1 = s.ChannelAccount(t, e.tenant, "fakechat", "page-1", "Page 1")
	e.page2 = s.ChannelAccount(t, e.tenant, "fakechat", "page-2", "Page 2")
	read := []string{"conversation.read", "conversation.reply"}
	e.ids[cvAn] = convtest.Staff(t, s, e.tenant, e.page1, cvAn, read...)
	e.ids[cvBinh] = convtest.Staff(t, s, e.tenant, e.page1, cvBinh, read...)
	e.ids[cvManager] = convtest.Staff(t, s, e.tenant, uuid.Nil, cvManager, "conversation.read", "conversation.reply",
		"conversation.assign", "conversation.settings")
	e.ids[cvOutside] = convtest.Staff(t, s, e.tenant, e.page2, cvOutside, "conversation.read", "conversation.assign")
	e.ids[cvPageOnly] = convtest.Staff(t, s, e.tenant, e.page1, cvPageOnly, "conversation.read", "conversation.settings")
	reg, err := channel.NewRegistry(fake.New("bi-mat"))
	if err != nil {
		t.Fatal(err)
	}
	d := testServerDeps(t, s, e.keys, 0)
	d.Realtime, d.Channels = e.bus, reg
	e.srv = NewServer(d)
	return e
}

func (e *assignAPIEnv) do(method, path, email, key, body string) (int, map[string]any) {
	e.t.Helper()
	rec := serveAs(e.srv, e.keys, method, path, email, key, body)
	if rec.Body.Len() == 0 || rec.Code >= 400 {
		return rec.Code, map[string]any{"raw": rec.Body.String()}
	}
	return rec.Code, decodeJSON(e.t, rec)
}

func assigneeOf(detail map[string]any) any {
	c, _ := detail["conversation"].(map[string]any)
	a, _ := c["assignee"].(map[string]any)
	return a["id"]
}

// CH06 qua API: An tự nhận → 200 chi tiết có người phụ trách An, chính sách gửi (Page chỉ xem → read_only); nhận lại
// cùng khóa → Idempotent-Replayed; quản lý giao cho Bình → 200; lịch sử 2 dòng; sale không có conversation.assign →
// 403; người ngoài Page → 404; giao người không có quyền trên Page → 422; version cũ → 409; Bình nhận khi đã giao cho
// Bình → 200 không đổi; danh sách "Giao cho…" của Page 1.
func TestAssignAPI_CH06(t *testing.T) {
	t.Parallel()
	e := newAssignAPIEnv(t)
	c := convtest.Conversation(t, e.staff, e.tenant, e.page1, "psid-api-1")
	base := "/api/v1/conversations/" + c.String()
	code, d := e.do(http.MethodPost, base+"/claim", cvAn, "c1", "")
	if code != http.StatusOK || assigneeOf(d) != e.ids[cvAn].String() {
		t.Fatalf("An nhận: %d %v", code, d)
	}
	pol, _ := d["sendPolicy"].(map[string]any)
	if pol["allowed"] != false || pol["reason"] != "read_only" || d["doNotContact"] != false {
		t.Fatalf("chi tiết = %v", d)
	}
	again := serveAs(e.srv, e.keys, http.MethodPost, base+"/claim", cvAn, "c1", "")
	if again.Code != http.StatusOK || again.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("gửi lại cùng khóa: %d", again.Code)
	}
	conv, _ := d["conversation"].(map[string]any)
	v := conv["version"].(float64) //nolint:forcetypeassert // test
	body := func(id uuid.UUID, ver float64) string {
		return fmt.Sprintf(`{"userId":%q,"expectedVersion":%d}`, id.String(), int(ver))
	}
	expect(t, "sale không có conversation.assign", serveAs(e.srv, e.keys, http.MethodPost, base+"/assign", cvAn, "a0",
		body(e.ids[cvBinh], v)), http.StatusForbidden, "forbidden")
	expect(t, "người ngoài Page", serveAs(e.srv, e.keys, http.MethodPost, base+"/assign", cvOutside, "a1",
		body(e.ids[cvBinh], v)), http.StatusNotFound, "not_found")
	expect(t, "người nhận không có quyền trên Page", serveAs(e.srv, e.keys, http.MethodPost, base+"/assign", cvManager, "a2",
		body(e.ids[cvOutside], v)), http.StatusUnprocessableEntity, "assignee_not_allowed")
	expect(t, "version cũ", serveAs(e.srv, e.keys, http.MethodPost, base+"/assign", cvManager, "a3",
		body(e.ids[cvBinh], v-1)), http.StatusConflict, "version_conflict")
	code, d = e.do(http.MethodPost, base+"/assign", cvManager, "a4", body(e.ids[cvBinh], v))
	if code != http.StatusOK || assigneeOf(d) != e.ids[cvBinh].String() {
		t.Fatalf("giao cho Bình: %d %v", code, d)
	}
	expect(t, "An nhận khi Bình đang nhận", serveAs(e.srv, e.keys, http.MethodPost, base+"/claim", cvAn, "c2", ""),
		http.StatusConflict, "already_assigned")
	if code, _ := e.do(http.MethodPost, base+"/claim", cvBinh, "c3", ""); code != http.StatusOK {
		t.Fatalf("Bình nhận lại của mình: %d", code)
	}
	code, d = e.do(http.MethodGet, base+"/assignments", cvAn, "", "")
	items, _ := d["items"].([]any)
	if code != http.StatusOK || len(items) != 2 {
		t.Fatalf("lịch sử: %d %v", code, d)
	}
	if r0, _ := items[0].(map[string]any); r0["reason"] != "self" {
		t.Fatalf("dòng 1 = %v", r0)
	}
	if r1, _ := items[1].(map[string]any); r1["reason"] != "manager" {
		t.Fatalf("dòng 2 = %v", r1)
	}
	expect(t, "lịch sử, người ngoài Page", serveAs(e.srv, e.keys, http.MethodGet, base+"/assignments", cvOutside, "", ""),
		http.StatusNotFound, "not_found")
	code, d = e.do(http.MethodGet, "/api/v1/channels/"+e.page1.String()+"/assignable-users", cvAn, "", "")
	users, _ := d["items"].([]any)
	if code != http.StatusOK || len(users) != 4 { // An, Bình, quản lý, cvPageOnly
		t.Fatalf("người được giao: %d %v", code, d)
	}
	expect(t, "người được giao Page không có", serveAs(e.srv, e.keys, http.MethodGet,
		"/api/v1/channels/"+uuid.NewString()+"/assignable-users", cvManager, "", ""), http.StatusNotFound, "not_found")
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.domain_events WHERE topic = 'conversation.assigned'
		AND aggregate_id = $1`, c.String()); n != 2 {
		t.Fatalf("sự kiện = %d", n)
	}
}

// Hai nhân viên cùng bấm nhận qua API: một 200, một 409 already_assigned.
func TestAssignAPI_ConcurrentClaim(t *testing.T) {
	t.Parallel()
	e := newAssignAPIEnv(t)
	c := convtest.Conversation(t, e.staff, e.tenant, e.page1, "psid-api-2")
	codes := make([]int, 2)
	var wg sync.WaitGroup
	for i, email := range []string{cvAn, cvBinh} {
		wg.Go(func() {
			codes[i] = serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/conversations/"+c.String()+"/claim", email, "k", "").Code
		})
	}
	wg.Wait()
	slices.Sort(codes)
	if codes[0] != http.StatusOK || codes[1] != http.StatusConflict {
		t.Fatalf("mã = %v", codes)
	}
}
