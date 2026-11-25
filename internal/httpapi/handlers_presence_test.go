package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

const (
	presAn    = "an-pres@elimaz.test"
	presBinh  = "binh-pres@elimaz.test"
	presCuong = "cuong-pres@elimaz.test" // conversation.read trên Page khác
	presKho   = "kho-pres@elimaz.test"   // không có conversation.read
)

// presenceEnv: máy chủ thật trên database thật, đồng hồ cắm được, bus ghi báo hiệu, sổ chỉ số; tenant A có một hội
// thoại trên một Page; An, Bình có conversation.read trên Page đó.
type presenceEnv struct {
	t       *testing.T
	staff   *testkit.Staff
	keys    *cfaccesstest.AccessKeys
	srv     http.Handler
	bus     *realtimetest.RecordingBus
	reg     *metrics.Registry
	tenant  uuid.UUID
	page    uuid.UUID
	conv    uuid.UUID
	anID    uuid.UUID
	now     time.Time
	seq     int
	metricN int64
}

func newPresenceEnv(t *testing.T) *presenceEnv {
	t.Helper()
	e := &presenceEnv{
		t: t, staff: testkit.OpenStaff(t), keys: cfaccesstest.AccessKeyServer(t), bus: &realtimetest.RecordingBus{},
		reg: metrics.NewRegistry(), page: uuid.New(), now: time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC),
	}
	e.tenant = e.staff.Tenant(t, "A")
	e.anID = e.user(presAn, "An", "page", e.page.String())
	e.user(presBinh, "Bình", "page", e.page.String())
	e.user(presCuong, "Cường", "page", uuid.NewString())
	kho := e.staff.User(t, e.tenant, presKho, "active")
	e.staff.Grant(t, e.tenant, kho, []string{"order.read"}, "all", "")
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		var customer uuid.UUID
		if err := tx.QueryRow(t.Context(), `INSERT INTO customer.customers (tenant_id, full_name, name_search)
			VALUES (current_setting('app.tenant_id')::uuid, 'Chị Lan', 'chi lan') RETURNING id`).Scan(&customer); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(t.Context(), `INSERT INTO conversation.conversations
			(tenant_id, channel_account_id, customer_id, kind, external_thread_ref, last_message_at)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'inbox', 'psid-1', now()) RETURNING id`,
			e.page, customer).Scan(&e.conv)
	})
	d := testServerDeps(t, e.staff, e.keys, 0)
	d.Realtime, d.Metrics, d.Now = e.bus, e.reg, func() time.Time { return e.now }
	e.srv = NewServer(d)
	return e
}

func (e *presenceEnv) user(email, name, scopeType, scopeID string) uuid.UUID {
	e.t.Helper()
	id := e.staff.User(e.t, e.tenant, email, "active")
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), "UPDATE identity.users SET display_name = $2 WHERE id = $1", id, name)
		return err //nolint:wrapcheck // test
	})
	e.staff.Grant(e.t, e.tenant, id, []string{"conversation.read"}, scopeType, scopeID)
	return id
}

func (e *presenceEnv) path(conv uuid.UUID) string {
	return "/api/v1/conversations/" + conv.String() + "/presence"
}

// beat gửi nhịp tim state của email với khóa chống trùng mới (Q9).
func (e *presenceEnv) beat(email, state string) *httptest.ResponseRecorder {
	e.seq++
	return e.beatKey(email, state, fmt.Sprintf("hb-%d", e.seq))
}

func (e *presenceEnv) beatKey(email, state, key string) *httptest.ResponseRecorder {
	return serveAs(e.srv, e.keys, http.MethodPost, e.path(e.conv), email, key, `{"state":"`+state+`"}`)
}

func (e *presenceEnv) mustBeat(email, state string) {
	e.t.Helper()
	if rec := e.beat(email, state); rec.Code != http.StatusNoContent {
		e.t.Fatalf("nhịp tim %s %s: %d %s", email, state, rec.Code, rec.Body.String())
	}
}

// get trả danh sách có mặt email thấy, dạng "Tên:trạng thái".
func (e *presenceEnv) get(email string) []string {
	e.t.Helper()
	rec := serveAs(e.srv, e.keys, http.MethodGet, e.path(e.conv), email, "", "")
	if rec.Code != http.StatusOK {
		e.t.Fatalf("GET presence %s: %d %s", email, rec.Code, rec.Body.String())
	}
	var body struct {
		Items []struct {
			UserID      uuid.UUID `json:"userId"`
			DisplayName string    `json:"displayName"`
			State       string    `json:"state"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Items == nil {
		e.t.Fatalf("thân GET presence: %v %s", err, rec.Body.String())
	}
	out := []string{}
	for _, it := range body.Items {
		if it.DisplayName == "An" && it.UserID != e.anID {
			e.t.Errorf("userId của An = %s", it.UserID)
		}
		out = append(out, it.DisplayName+":"+it.State)
	}
	return out
}

func (e *presenceEnv) signals() int {
	n := 0
	for _, s := range e.bus.Signals() {
		if s.Topic == conversation.TopicPresence {
			n++
		}
	}
	return n
}

func (e *presenceEnv) keyRows() int {
	e.t.Helper()
	return e.staff.CountIn(e.t, e.tenant, "SELECT count(*) FROM platform.idempotency_keys")
}

// metric: tổng counter idempotency_keys_created nhãn setPresence đã gom (Drain cộng dồn vào metricN).
func (e *presenceEnv) metric() int64 {
	for _, r := range e.reg.Drain() {
		if r.Name == "idempotency_keys_created" && r.Label == "setPresence" {
			e.metricN += r.Count
		}
	}
	return e.metricN
}

func eq(a []string, b ...string) bool { return fmt.Sprint(a) == fmt.Sprint(b) }

// [CH10, LV07 phần hiển thị] Qua HTTP: An viewing → Bình thấy [An:viewing], 1 báo hiệu; An gửi lại viewing 5 lần →
// không thêm báo hiệu; An typing → 1 báo hiệu, Bình thấy typing; drafting_order → Bình thấy "đang lên đơn"; An không
// thấy chính mình. Mỗi nhịp tim sinh đúng 1 dòng idempotency_keys và chỉ số idempotency_keys_created đếm đúng số đó
// (theo ngưỡng của Q9); gửi lại cùng khóa trả 204 đã lưu, không thêm dòng, không báo hiệu.
func TestPresence_HeartbeatAndSignals(t *testing.T) {
	t.Parallel()
	e := newPresenceEnv(t)
	rows0 := e.keyRows()
	e.mustBeat(presAn, "viewing")
	if got := e.get(presBinh); !eq(got, "An:viewing") {
		t.Fatalf("Bình thấy %v", got)
	}
	if e.signals() != 1 {
		t.Fatalf("báo hiệu sau viewing đầu: %d", e.signals())
	}
	for range 5 {
		e.now = e.now.Add(10 * time.Second)
		e.mustBeat(presAn, "viewing")
	}
	if e.signals() != 1 {
		t.Fatalf("viewing lặp lại phát thêm báo hiệu: %d", e.signals())
	}
	perBeat := float64(e.keyRows()-rows0) / 6
	t.Logf("idempotency_keys mỗi nhịp tim: %.2f dòng (6 nhịp tim; Q9: ~300 nghìn dòng/ngày, xem lại khi > 5 triệu)", perBeat)
	if e.keyRows()-rows0 != 6 || e.metric() != 6 {
		t.Fatalf("6 nhịp tim: %d dòng idempotency_keys, chỉ số %d; mong 6, 6", e.keyRows()-rows0, e.metric())
	}
	rec := e.beatKey(presAn, "viewing", fmt.Sprintf("hb-%d", e.seq))
	if rec.Code != http.StatusNoContent || rec.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("gửi lại cùng khóa: %d %v", rec.Code, rec.Header())
	}
	if e.keyRows()-rows0 != 6 || e.metric() != 6 || e.signals() != 1 {
		t.Fatal("gửi lại cùng khóa không được thêm dòng, chỉ số, báo hiệu")
	}

	e.mustBeat(presAn, "typing")
	if e.signals() != 2 {
		t.Fatalf("typing: %d báo hiệu, mong 2", e.signals())
	}
	if got := e.get(presBinh); !eq(got, "An:typing") {
		t.Fatalf("Bình thấy %v, mong typing", got)
	}
	e.mustBeat(presAn, "drafting_order")
	if got := e.get(presBinh); !eq(got, "An:drafting_order") || e.signals() != 3 {
		t.Fatalf("Bình thấy %v (%d báo hiệu), mong drafting_order", got, e.signals())
	}
	if got := e.get(presAn); len(got) != 0 {
		t.Fatalf("An không thấy chính mình: %v", got)
	}
	e.mustBeat(presBinh, "viewing")
	if got := e.get(presAn); !eq(got, "Bình:viewing") {
		t.Fatalf("An thấy %v", got)
	}
}

// Không nhịp tim 31 s rồi lịch conversation.presence_cleanup chạy (việc nền của module, cùng đồng hồ) → Bình thấy [],
// có đúng 1 báo hiệu.
func TestPresence_ExpiresAfterCleanup(t *testing.T) {
	t.Parallel()
	e := newPresenceEnv(t)
	e.mustBeat(presAn, "viewing")
	e.now = e.now.Add(31 * time.Second)
	jobs := conversation.JobHandlers(conversation.Deps{Bus: e.bus, Now: func() time.Time { return e.now }})
	if len(jobs) != 1 {
		t.Fatalf("việc nền conversation: %d", len(jobs))
	}
	before := e.signals()
	if err := tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		_, err := jobs[0].Handle(t.Context(), queue.Job{TenantID: &e.tenant, Kind: jobs[0].Kind().Name}, tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if got := e.get(presBinh); len(got) != 0 {
		t.Fatalf("Bình thấy %v sau 31 s", got)
	}
	if e.signals()-before != 1 {
		t.Fatalf("dọn: %d báo hiệu, mong 1", e.signals()-before)
	}
}

// Người không có quyền trên Page của hội thoại, hội thoại không có → 404 not_found (không lộ sự tồn tại), không ghi,
// không báo hiệu; không có conversation.read → 403; trạng thái lạ → 400; thiếu khóa chống trùng → 400.
func TestPresence_Access(t *testing.T) {
	t.Parallel()
	e := newPresenceEnv(t)
	expect(t, "Page khác: POST", e.beat(presCuong, "viewing"), http.StatusNotFound, "not_found")
	expect(t, "Page khác: GET", serveAs(e.srv, e.keys, http.MethodGet, e.path(e.conv), presCuong, "", ""),
		http.StatusNotFound, "not_found")
	expect(t, "không có hội thoại", serveAs(e.srv, e.keys, http.MethodPost, e.path(uuid.New()), presAn, "x1",
		`{"state":"viewing"}`), http.StatusNotFound, "not_found")
	expect(t, "không có hội thoại: GET", serveAs(e.srv, e.keys, http.MethodGet, e.path(uuid.New()), presAn, "", ""),
		http.StatusNotFound, "not_found")
	expect(t, "không quyền", e.beat(presKho, "viewing"), http.StatusForbidden, "forbidden")
	expect(t, "trạng thái lạ", e.beat(presAn, "away"), http.StatusBadRequest, "bad_request")
	expect(t, "thiếu khóa", e.beatKey(presAn, "viewing", ""), http.StatusBadRequest, "idempotency_key_required")
	if n := e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM conversation.presence"); n != 0 || e.signals() != 0 {
		t.Fatalf("bị từ chối mà vẫn ghi: %d dòng, %d báo hiệu", n, e.signals())
	}
	if e.metric() != 0 {
		t.Fatalf("chỉ số chỉ đếm nhịp tim đã lưu: %d", e.metric())
	}
}
