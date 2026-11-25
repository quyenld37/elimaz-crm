package facebook_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
)

// recorder là handler vào giả của nguồn facebook (handler thật là của conversation, Task 153): ghi loại, tenant và
// khóa của mỗi sự kiện đã giao.
type recorder struct {
	mu   sync.Mutex
	seen []webhooks.Inbound
}

func (*recorder) Source() string { return facebook.Key }

func (r *recorder) Handle(_ context.Context, e webhooks.Inbound, _ pgx.Tx) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, e)
	return nil
}

func (r *recorder) types() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.seen))
	for _, e := range r.seen {
		out = append(out, e.Type)
	}
	slices.Sort(out)
	return out
}

// fbEnv: ingest thật với nguồn adapter facebook, worker có việc tách gói của M0 và handler vào giả; tenant A có
// tuyến fb_page cho hai Page của gói thử.
type fbEnv struct {
	staff  *testkit.Staff
	tenant uuid.UUID
	reg    *metrics.Registry
	srv    *httptest.Server
	worker *queue.Worker
	rec    *recorder
}

func newFBEnv(t *testing.T) *fbEnv {
	t.Helper()
	staff := testkit.OpenStaff(t)
	e := &fbEnv{staff: staff, tenant: staff.Tenant(t, "A"), reg: metrics.NewRegistry(), rec: &recorder{}}
	for _, p := range []string{page1, page2} {
		if err := tenancy.AddRoute(t.Context(), staff.Owner, facebook.RouteKind, p, e.tenant); err != nil {
			t.Fatal(err)
		}
	}
	src := []webhooks.Source{newAdapter()}
	e.srv = ingestServer(t, staff, src, e.reg)
	split, splitErr := webhooks.NewSplitHandler(webhooks.SplitOptions{Sources: src, Metrics: e.reg})
	inbound, inboundErr := webhooks.InboundJobHandlers(src, []webhooks.InboundHandler{e.rec})
	if err := errors.Join(splitErr, inboundErr); err != nil {
		t.Fatal(err)
	}
	e.worker = queue.NewWorker(staff.App, append([]queue.Handler{split}, inbound...), queue.WorkerOptions{})
	return e
}

// ingestServer dựng vai trò ingest thật (nguồn src, tệp tạm riêng, Lockout mặc định) sau một httptest.Server.
func ingestServer(t *testing.T, staff *testkit.Staff, src []webhooks.Source, reg *metrics.Registry) *httptest.Server {
	t.Helper()
	o := webhooks.IngestOptions{Role: "ingest", DB: staff.App, Sources: src, Metrics: reg}
	o.Spool, o.Lockout = &webhooks.Spool{Dir: t.TempDir()}, webhooks.NewLockout(webhooks.LockoutOptions{})
	o.Logger = slog.New(slog.DiscardHandler)
	in, err := webhooks.NewIngest(o)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	in.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func (e *fbEnv) post(t *testing.T, raw []byte, h http.Header) int {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, e.srv.URL+"/hooks/facebook", bytes.NewReader(raw))
	for k, v := range h {
		req.Header[k] = v
	}
	req.Header.Set("Content-Type", "application/json")
	status, _, _ := testkit.Do(t, req)
	return status
}

func (e *fbEnv) counter(name, label string) int64 {
	var n int64
	for _, r := range e.reg.Drain() {
		if r.Name == name && r.Kind == metrics.KindCounter && r.Label == label {
			n += r.Count
		}
	}
	return n
}

// Đường vào thật (02 mục 2, HT03 với nguồn thật): bước GET xác minh; POST không chữ ký 401, không lưu; có chữ ký 200,
// một bản nhận, một việc tách; worker tách ra đúng 7 việc inbound.facebook của tenant A; Meta giao lại đúng gói đó →
// thêm một bản nhận nhưng không thêm việc vào nào (chống trùng theo DedupeKey); Page không có tuyến → inbound_unrouted.
func TestIngestE2E(t *testing.T) {
	t.Parallel()
	e := newFBEnv(t)

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet,
		e.srv.URL+"/hooks/facebook?hub.mode=subscribe&hub.verify_token="+testVerifyToken+"&hub.challenge=abc", nil)
	if status, _, body := testkit.Do(t, req); status != http.StatusOK || string(body) != "abc" {
		t.Fatalf("GET xác minh: %d %q", status, body)
	}
	req, _ = http.NewRequestWithContext(t.Context(), http.MethodGet,
		e.srv.URL+"/hooks/facebook?hub.mode=subscribe&hub.verify_token=sai&hub.challenge=abc", nil)
	if status, _, _ := testkit.Do(t, req); status != http.StatusForbidden {
		t.Errorf("GET sai token: %d, mong 403", status)
	}

	raw := fixture(t, "batch_2pages_7events.json")
	if s := e.post(t, raw, nil); s != http.StatusUnauthorized {
		t.Errorf("không chữ ký: %d, mong 401", s)
	}
	if n := e.staff.Count(t, "SELECT count(*) FROM platform.webhook_deliveries"); n != 0 {
		t.Errorf("không chữ ký mà lưu %d bản nhận", n)
	}
	if s := e.post(t, raw, sign(raw, webhookAppSecret)); s != http.StatusOK {
		t.Fatalf("có chữ ký: %d, mong 200", s)
	}
	if d, j := e.staff.Count(t, "SELECT count(*) FROM platform.webhook_deliveries WHERE source = 'facebook'"),
		e.staff.Count(t, "SELECT count(*) FROM platform.jobs WHERE kind = 'webhook.split'"); d != 1 || j != 1 {
		t.Fatalf("bản nhận %d, việc tách %d, mong 1, 1", d, j)
	}
	testkit.DrainDue(t, e.staff.Owner, e.worker)
	wantTypes := []string{
		channel.EventCommentChanged, channel.EventLiveStatus, channel.EventMessageDelivered, channel.EventMessageEcho,
		channel.EventMessageRead, channel.EventMessageReceived, channel.EventMessageReceived,
	}
	if got := e.rec.types(); !slices.Equal(got, wantTypes) {
		t.Fatalf("sự kiện đã giao %v, mong %v", got, wantTypes)
	}
	if n := e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM platform.jobs WHERE kind = 'inbound.facebook'"); n != 7 {
		t.Errorf("việc inbound.facebook = %d, mong 7", n)
	}
	e.rec.mu.Lock()
	seen := slices.Clone(e.rec.seen)
	e.rec.mu.Unlock()
	for _, ev := range seen {
		if ev.TenantID != e.tenant || ev.Via != webhooks.ViaWebhook {
			t.Errorf("sự kiện %s: tenant %s, via %q", ev.Type, ev.TenantID, ev.Via)
		}
	}

	// Meta giao lại cùng gói (không nhận được 200 lần trước): lưu thêm bản nhận, không thêm việc vào.
	if s := e.post(t, raw, sign(raw, webhookAppSecret)); s != http.StatusOK {
		t.Fatalf("giao lại: %d", s)
	}
	testkit.DrainDue(t, e.staff.Owner, e.worker)
	if n := e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM platform.jobs WHERE kind = 'inbound.facebook'"); n != 7 {
		t.Errorf("giao lại: việc inbound.facebook = %d, mong vẫn 7", n)
	}
	if n := len(e.rec.types()); n != 7 {
		t.Errorf("giao lại: handler nhận %d sự kiện, mong vẫn 7", n)
	}

	// Page chưa kết nối (không có tuyến): không việc vào, đếm inbound_unrouted.
	e.reg.Drain()
	other := []byte(strings.ReplaceAll(string(fixture(t, "message_text.json")), page1, "104000000000999"))
	if s := e.post(t, other, sign(other, webhookAppSecret)); s != http.StatusOK {
		t.Fatalf("Page lạ: %d", s)
	}
	testkit.DrainDue(t, e.staff.Owner, e.worker)
	if n := e.counter("inbound_unrouted", facebook.Key); n != 1 {
		t.Errorf("inbound_unrouted = %d, mong 1", n)
	}
	if n := len(e.rec.types()); n != 7 {
		t.Errorf("Page lạ: handler nhận %d sự kiện, mong vẫn 7", n)
	}
}

// Gói có loại bị bỏ qua: đếm inbound_skipped theo nhãn "facebook:<loại>", không tạo việc vào.
func TestIngestE2E_Skipped(t *testing.T) {
	t.Parallel()
	e := newFBEnv(t)
	raw := fixture(t, "feed_status.json")
	if s := e.post(t, raw, sign(raw, webhookAppSecret)); s != http.StatusOK {
		t.Fatalf("POST: %d", s)
	}
	testkit.DrainDue(t, e.staff.Owner, e.worker)
	if n := e.counter("inbound_skipped", "facebook:feed:status"); n != 1 {
		t.Errorf("inbound_skipped = %d, mong 1", n)
	}
	if n := len(e.rec.types()); n != 0 {
		t.Errorf("handler nhận %d sự kiện, mong 0", n)
	}
}
