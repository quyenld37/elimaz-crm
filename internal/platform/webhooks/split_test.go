package webhooks_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/selftest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
)

// splitEnv: database thử, tenant A (route selftest:ka), tenant B (route selftest:kb), worker có việc tách gói và
// handler sự kiện vào selftest ghi lại các lần gọi.
type splitEnv struct {
	*testkit.Staff
	a, b  uuid.UUID
	reg   *metrics.Registry
	split *webhooks.SplitHandler
	rec   *recorder
	w     *queue.Worker
}

func newSplitEnv(t *testing.T, concurrency int) *splitEnv {
	t.Helper()
	s := testkit.OpenStaff(t)
	e := &splitEnv{Staff: s, a: s.Tenant(t, "a"), b: s.Tenant(t, "b"), reg: metrics.NewRegistry(), rec: &recorder{}}
	e.route(t, "ka", e.a)
	e.route(t, "kb", e.b)
	sources := []webhooks.Source{selftest.NewSource(secret)}
	split, err := webhooks.NewSplitHandler(webhooks.SplitOptions{Sources: sources, Metrics: e.reg})
	if err != nil {
		t.Fatal(err)
	}
	e.split = split
	inbound, err := webhooks.InboundJobHandlers(sources, []webhooks.InboundHandler{e.rec})
	if err != nil {
		t.Fatal(err)
	}
	e.w = queue.NewWorker(s.App, append([]queue.Handler{split}, inbound...), queue.WorkerOptions{Concurrency: concurrency})
	return e
}

func (e *splitEnv) route(t *testing.T, key string, tenant uuid.UUID) {
	t.Helper()
	if err := tenancy.AddRoute(t.Context(), e.Owner, selftest.Key, key, tenant); err != nil {
		t.Fatal(err)
	}
}

// deliver lưu một gói selftest như ingest (bản nhận + việc tách gói).
func (e *splitEnv) deliver(t *testing.T, body []byte) {
	t.Helper()
	created, err := webhooks.Save(t.Context(), e.App, webhooks.Entry{
		Source: selftest.Key, Headers: json.RawMessage(`{}`), Body: body, ReceivedAt: time.Now(),
	})
	if err != nil || !created {
		t.Fatalf("lưu gói: created=%v err=%v", created, err)
	}
}

// drain chạy worker tới khi không còn việc tới hạn.
func (e *splitEnv) drain(t *testing.T) {
	t.Helper()
	for range 50 {
		n, err := e.w.RunOnce(t.Context(), 100)
		if err != nil {
			t.Logf("RunOnce: %v", err) // lỗi của từng việc (test cố ý làm lỗi) không dừng vòng
		}
		if n == 0 {
			return
		}
	}
	t.Fatal("worker còn việc sau 50 lượt")
}

type jobRow struct {
	id       uuid.UUID
	tenant   *uuid.UUID
	status   string
	attempts int
	payload  json.RawMessage
}

func (e *splitEnv) jobs(t *testing.T, kind string) []jobRow {
	t.Helper()
	rows, err := e.Owner.Pool.Query(t.Context(),
		"SELECT id, tenant_id, status, attempts, payload FROM platform.jobs WHERE kind = $1 ORDER BY id", kind)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (jobRow, error) {
		var j jobRow
		err := r.Scan(&j.id, &j.tenant, &j.status, &j.attempts, &j.payload)
		return j, err //nolint:wrapcheck // test
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// recorder là InboundHandler của nguồn selftest: ghi mọi lần gọi; fail quyết định lỗi.
type recorder struct {
	mu   sync.Mutex
	got  []webhooks.Inbound
	fail func(e webhooks.Inbound) error
	// tenantInTx: app.tenant_id thấy được trong tx của mỗi lần gọi.
	tenantInTx []string
}

func (*recorder) Source() string { return selftest.Key }

func (r *recorder) Handle(ctx context.Context, e webhooks.Inbound, tx pgx.Tx) error {
	var cur string
	if err := tx.QueryRow(ctx, "SELECT current_setting('app.tenant_id', true)").Scan(&cur); err != nil {
		return fmt.Errorf("đọc tenant: %w", err)
	}
	if r.fail != nil {
		if err := r.fail(e); err != nil {
			return err
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, e)
	r.tenantInTx = append(r.tenantInTx, cur)
	return nil
}

func (r *recorder) calls() []webhooks.Inbound {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.got)
}

type ev struct {
	Kind      string    `json:"kind"`
	EventID   string    `json:"eventId"`
	TenantKey string    `json:"tenantKey"`
	SentAt    time.Time `json:"sentAt"`
}

func packet(t *testing.T, evs ...ev) []byte {
	t.Helper()
	b, err := json.Marshal(map[string][]ev{"events": evs})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// t0Unix là mốc giờ của sự kiện thử.
const t0Unix = 1_795_510_800 // 2026-11-24 09:00 UTC

func at(d time.Duration) time.Time { return time.Unix(t0Unix, 0).UTC().Add(d) }

func ping(id, key string, at time.Time) ev {
	return ev{Kind: "ping", EventID: id, TenantKey: key, SentAt: at}
}

// Gói 5 sự kiện: 2 của ka, 1 của kb, 1 loại lạ, 1 không định tuyến → 3 việc đúng tenant, đếm bỏ qua và chưa định
// tuyến, việc tách done; handler chạy trong ngữ cảnh tenant của sự kiện; payload việc không mang nội dung.
func TestSplitRoutesEvents(t *testing.T) {
	t.Parallel()
	e := newSplitEnv(t, 0)
	e.deliver(t, packet(t,
		ping("e1", "ka", at(0)), ping("e2", "ka", at(time.Second)), ping("e3", "kb", at(0)),
		ev{Kind: "mystery", EventID: "e4", TenantKey: "ka", SentAt: at(0)}, ping("e5", "none", at(0)),
	))
	e.drain(t)

	splits := e.jobs(t, webhooks.SplitKind.Name)
	if len(splits) != 1 || splits[0].status != "done" {
		t.Fatalf("việc tách: %+v", splits)
	}
	checkRoutedJobs(t, e)
	checkRoutedCalls(t, e)

	rows := e.reg.Drain()
	if n := counter(rows, "inbound_skipped", "selftest:mystery"); n != 1 {
		t.Errorf("inbound_skipped selftest:mystery = %d, muốn 1", n)
	}
	if n := counter(rows, "inbound_unrouted", "selftest"); n != 1 {
		t.Errorf("inbound_unrouted selftest = %d, muốn 1", n)
	}

	// Chạy lại việc tách (giữ chỗ hết hạn, bấm chạy lại): không thêm việc nào.
	j := queue.Job{ID: splits[0].id, Kind: webhooks.SplitKind.Name, Payload: splits[0].payload, Attempts: 2}
	if err := e.App.RetryTx(t.Context(), dbTxOptions(), func(tx pgx.Tx) error {
		_, err := e.split.Handle(t.Context(), j, tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if n := len(e.jobs(t, webhooks.InboundKind(selftest.Key).Name)); n != 3 {
		t.Fatalf("sau khi chạy lại việc tách có %d việc inbound, muốn 3", n)
	}
}

// checkRoutedJobs: 3 việc inbound done, 2 của A, 1 của B; payload chỉ mang mã.
func checkRoutedJobs(t *testing.T, e *splitEnv) {
	t.Helper()
	inbound := e.jobs(t, webhooks.InboundKind(selftest.Key).Name)
	tenants := map[uuid.UUID]int{}
	for _, j := range inbound {
		if j.tenant == nil || j.status != "done" {
			t.Fatalf("việc inbound: %+v", j)
		}
		tenants[*j.tenant]++
		var p map[string]json.RawMessage
		if err := json.Unmarshal(j.payload, &p); err != nil {
			t.Fatal(err)
		}
		for k := range p {
			if !slices.Contains([]string{"type", "deliveryId", "index", "dedupeKey", "occurredAt"}, k) {
				t.Errorf("payload việc inbound có khóa %q (chỉ được mang mã): %s", k, j.payload)
			}
		}
	}
	if len(inbound) != 3 || tenants[e.a] != 2 || tenants[e.b] != 1 {
		t.Fatalf("việc inbound: %d, theo tenant %v", len(inbound), tenants)
	}
}

// checkRoutedCalls: handler gọi 3 lần, đúng tenant, trong ngữ cảnh tenant, Data là sự kiện gốc.
func checkRoutedCalls(t *testing.T, e *splitEnv) {
	t.Helper()
	calls := e.rec.calls()
	if len(calls) != 3 {
		t.Fatalf("handler gọi %d lần, muốn 3", len(calls))
	}
	for i, c := range calls {
		if c.Type != "selftest.ping" || c.JobID == uuid.Nil || c.OccurredAt.IsZero() {
			t.Errorf("Inbound %d: %+v", i, c)
		}
		if e.rec.tenantInTx[i] != c.TenantID.String() {
			t.Errorf("handler chạy với app.tenant_id %q, muốn %s", e.rec.tenantInTx[i], c.TenantID)
		}
		var d ev
		if err := json.Unmarshal(c.Data, &d); err != nil || d.Kind != "ping" || d.EventID == "" {
			t.Errorf("Data của Inbound %d: %s (%v)", i, c.Data, err)
		}
		want := e.a
		if d.TenantKey == "kb" {
			want = e.b
		}
		if c.TenantID != want {
			t.Errorf("sự kiện %s giao cho tenant %s, muốn %s", d.EventID, c.TenantID, want)
		}
	}
}

// Hai gói khác thân cùng eventId X → một việc cho X.
func TestSplitDedupesAcrossPackets(t *testing.T) {
	t.Parallel()
	e := newSplitEnv(t, 0)
	e.deliver(t, packet(t, ping("X", "ka", at(0))))
	e.deliver(t, packet(t, ping("X", "ka", at(0)), ping("Y", "ka", at(time.Second))))
	e.drain(t)
	if n := len(e.jobs(t, webhooks.SplitKind.Name)); n != 2 {
		t.Fatalf("việc tách: %d, muốn 2", n)
	}
	got := map[string]int{}
	for _, j := range e.jobs(t, webhooks.InboundKind(selftest.Key).Name) {
		var p struct {
			DedupeKey string `json:"dedupeKey"`
		}
		if err := json.Unmarshal(j.payload, &p); err != nil {
			t.Fatal(err)
		}
		got[p.DedupeKey]++
	}
	if got["X"] != 1 || got["Y"] != 1 || len(got) != 2 {
		t.Fatalf("việc theo eventId: %v", got)
	}
	if n := len(e.rec.calls()); n != 2 {
		t.Fatalf("handler gọi %d lần, muốn 2", n)
	}
}

var errSeventh = errors.New("lỗi thử ở sự kiện thứ 7")

// Gói 50 sự kiện (50 hội thoại), handler lỗi đúng ở sự kiện thứ 7 → 49 việc done, 1 việc pending attempts = 1.
func TestSplitOneFailureDoesNotBlockPacket(t *testing.T) {
	t.Parallel()
	e := newSplitEnv(t, 0)
	evs := make([]ev, 0, 50)
	for i := 1; i <= 50; i++ {
		key := fmt.Sprintf("k%02d", i)
		e.route(t, key, e.a)
		evs = append(evs, ping(fmt.Sprintf("e%02d", i), key, at(time.Duration(i)*time.Millisecond)))
	}
	e.rec.fail = func(in webhooks.Inbound) error {
		var d ev
		_ = json.Unmarshal(in.Data, &d)
		if d.EventID == "e07" {
			return errSeventh
		}
		return nil
	}
	e.deliver(t, packet(t, evs...))
	e.drain(t)

	status := map[string]int{}
	for _, j := range e.jobs(t, webhooks.InboundKind(selftest.Key).Name) {
		status[j.status]++
		if j.status == "pending" && j.attempts != 1 {
			t.Errorf("việc pending có attempts = %d, muốn 1", j.attempts)
		}
	}
	if status["done"] != 49 || status["pending"] != 1 || len(status) != 2 {
		t.Fatalf("trạng thái việc inbound: %v", status)
	}
}

// Hai sự kiện cùng OrderKey cách 1 ms, ghi ngược trong gói → Handle theo thời gian xảy ra (Concurrency 4).
func TestSplitOrdersByOccurredAt(t *testing.T) {
	t.Parallel()
	e := newSplitEnv(t, 4)
	e.deliver(t, packet(t, ping("late", "ka", at(time.Millisecond)), ping("early", "ka", at(0))))
	e.drain(t)
	calls := e.rec.calls()
	if len(calls) != 2 {
		t.Fatalf("handler gọi %d lần, muốn 2", len(calls))
	}
	if !calls[0].OccurredAt.Before(calls[1].OccurredAt) {
		t.Fatalf("thứ tự Handle: %v rồi %v, muốn sớm trước", calls[0].OccurredAt, calls[1].OccurredAt)
	}
}

// Thân không phải JSON → việc tách dead, không việc inbound nào.
func TestSplitUnreadableBodyIsDead(t *testing.T) {
	t.Parallel()
	e := newSplitEnv(t, 0)
	e.deliver(t, []byte("không phải json"))
	e.drain(t)
	splits := e.jobs(t, webhooks.SplitKind.Name)
	if len(splits) != 1 || splits[0].status != "dead" || splits[0].attempts != 1 {
		t.Fatalf("việc tách: %+v", splits)
	}
	if n := len(e.jobs(t, webhooks.InboundKind(selftest.Key).Name)); n != 0 {
		t.Fatalf("việc inbound: %d, muốn 0", n)
	}
}

// Bản nhận của nguồn worker không biết → việc tách dead.
func TestSplitUnknownSourceIsDead(t *testing.T) {
	t.Parallel()
	e := newSplitEnv(t, 0)
	if _, err := webhooks.Save(t.Context(), e.App, webhooks.Entry{
		Source: "lạ", Headers: json.RawMessage(`{}`), Body: []byte(`{"events":[]}`), ReceivedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	e.drain(t)
	if splits := e.jobs(t, webhooks.SplitKind.Name); len(splits) != 1 || splits[0].status != "dead" {
		t.Fatalf("việc tách: %+v", splits)
	}
}

type otherHandler struct{ src string }

func (h otherHandler) Source() string { return h.src }
func (otherHandler) Handle(context.Context, webhooks.Inbound, pgx.Tx) error {
	return nil
}

func TestInboundKindAndOptions(t *testing.T) {
	t.Parallel()
	k := webhooks.InboundKind("selftest")
	if k.Name != "inbound.selftest" || k.Policy != queue.DefaultPolicy {
		t.Fatalf("InboundKind: %+v", k)
	}
	sources := []webhooks.Source{newSelftest()}
	if _, err := webhooks.InboundJobHandlers(sources, []webhooks.InboundHandler{otherHandler{"lạ"}}); !errors.Is(err, webhooks.ErrInvalidOptions) {
		t.Errorf("handler của nguồn không có: %v", err)
	}
	dup := []webhooks.InboundHandler{otherHandler{"selftest"}, otherHandler{"selftest"}}
	if _, err := webhooks.InboundJobHandlers(sources, dup); !errors.Is(err, webhooks.ErrInvalidOptions) {
		t.Errorf("hai handler cùng nguồn: %v", err)
	}
	if _, err := webhooks.NewSplitHandler(webhooks.SplitOptions{Sources: append(sources, newSelftest())}); !errors.Is(err, webhooks.ErrInvalidOptions) {
		t.Errorf("hai nguồn cùng khóa: %v", err)
	}
	hs, err := webhooks.InboundJobHandlers(sources, []webhooks.InboundHandler{otherHandler{"selftest"}})
	if err != nil || len(hs) != 1 || hs[0].Kind() != k {
		t.Fatalf("InboundJobHandlers: %v, %v", hs, err)
	}
}
