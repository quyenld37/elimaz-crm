package webhooks_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
)

// routedKey là tên nguồn thử có route key (như hãng vận chuyển của M3: /hooks/<hãng>/<routeKey>).
const routedKey = "routed"

// Khóa tuyến thử: đúng 32 ký tự chữ, số.
const (
	rkA = "AbCdEfGh0123456789AbCdEfGh012345"
	rkB = "ZyXwVuTs9876543210ZyXwVuTs987654"
)

// routedSrc là RoutedSource thử: chữ ký là header X-Routed-Sig = "ok"; thân {"events":[{"id","at"}]}; SplitRouted
// định tuyến mọi sự kiện theo (routed, routeKey); Split (đường không có routeKey) định tuyến theo (routed, "plain").
type routedSrc struct {
	mu     sync.Mutex
	routed []string // routeKey của mỗi lần SplitRouted
	plain  int      // số lần Split
}

type routedEv struct {
	ID string    `json:"id"`
	At time.Time `json:"at"`
}

func (*routedSrc) Key() string                                  { return routedKey }
func (*routedSrc) Challenge(url.Values) (string, bool)          { return "", false }
func (*routedSrc) VerifySignature(_ []byte, h http.Header) bool { return h.Get("X-Routed-Sig") == "ok" }

func (s *routedSrc) Split(raw []byte) (webhooks.SplitResult, error) {
	s.mu.Lock()
	s.plain++
	s.mu.Unlock()
	return routedSplit(raw, "plain")
}

func (s *routedSrc) SplitRouted(raw []byte, routeKey string) (webhooks.SplitResult, error) {
	s.mu.Lock()
	s.routed = append(s.routed, routeKey)
	s.mu.Unlock()
	return routedSplit(raw, routeKey)
}

func (s *routedSrc) calls() (routed []string, plain int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.routed...), s.plain
}

func routedSplit(raw []byte, key string) (webhooks.SplitResult, error) {
	var b struct {
		Events []routedEv `json:"events"`
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return webhooks.SplitResult{}, errors.New("thân không đọc được")
	}
	res := webhooks.SplitResult{}
	for _, e := range b.Events {
		p, _ := json.Marshal(e) //nolint:errchkjson // struct đơn giản
		res.Events = append(res.Events, webhooks.InboundEvent{
			Type: "signal", DedupeKey: e.ID, Route: webhooks.Route{Kind: routedKey, ExternalKey: key},
			OccurredAt: e.At, Payload: p,
		})
	}
	return res, nil
}

func routedBody(t *testing.T, ids ...string) []byte {
	t.Helper()
	evs := make([]routedEv, 0, len(ids))
	for _, id := range ids {
		evs = append(evs, routedEv{ID: id, At: at(0)})
	}
	b, err := json.Marshal(map[string][]routedEv{"events": evs})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// postPath gửi POST tới path (đã có /hooks/...) với chữ ký của nguồn routed.
func (r *rig) postPath(t *testing.T, path string, body []byte) reply {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, r.srv.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Routed-Sig", "ok")
	req.Header.Set("X-Selftest-Signature", sign(body))
	return do(t, req)
}

// routedRig: rig có cả nguồn selftest (không RoutedSource) và nguồn routed.
func routedRig(t *testing.T, src *routedSrc, dead bool) *rig {
	t.Helper()
	d := openDeadDB(t)
	if !dead {
		d = openDB(t)
	}
	return newRig(t, d, func(o *webhooks.IngestOptions) { o.Sources = append(o.Sources, src) })
}

func deliveryRouteKeys(t *testing.T, r *rig) []string {
	t.Helper()
	rows, err := r.in.DBForTest().Pool.Query(t.Context(),
		"SELECT coalesce(route_key, '') FROM platform.webhook_deliveries ORDER BY received_at, id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		out = append(out, k)
	}
	return out
}

// POST /hooks/routed/<32 ký tự> → 200, một bản nhận có route_key; cùng thân gửi lại cùng khóa → không thêm dòng;
// cùng thân tới khóa khác → dòng riêng (chống trùng theo từng tuyến). Đường không routeKey của nguồn routed vẫn nhận,
// route_key rỗng.
func TestRoutedPostStoresRouteKey(t *testing.T) {
	src := &routedSrc{}
	r := routedRig(t, src, false)
	body := routedBody(t, "s1")
	for _, p := range []string{"/hooks/routed/" + rkA, "/hooks/routed/" + rkA, "/hooks/routed/" + rkB, "/hooks/routed"} {
		if got := r.postPath(t, p, body); got.status != http.StatusOK {
			t.Fatalf("POST %s = %d %s", p, got.status, got.body)
		}
	}
	keys := deliveryRouteKeys(t, r)
	if strings.Join(keys, ",") != rkA+","+rkB+"," {
		t.Fatalf("route_key của các bản nhận = %q, muốn [A B rỗng]", keys)
	}
	if _, jobs := counts(t, r.in.DBForTest()); jobs != 3 {
		t.Fatalf("việc tách gói = %d, muốn 3", jobs)
	}
}

// routeKey sai mẫu (31, 33 ký tự, ký tự lạ) hay nguồn không phải RoutedSource → 404 mà không chạm database (database
// chết vẫn 404, không 503, không ghi tệp tạm); nguồn lạ → 404.
func TestRoutedBadKeyIs404WithoutDatabase(t *testing.T) {
	src := &routedSrc{}
	r := routedRig(t, src, true)
	body := routedBody(t, "s1")
	for _, p := range []string{
		"/hooks/routed/" + rkA[:31], "/hooks/routed/" + rkA + "x", "/hooks/routed/" + rkA[:31] + "-",
		"/hooks/routed/" + rkA[:31] + "%C3%A1", "/hooks/selftest/" + rkA, "/hooks/nope/" + rkA,
	} {
		if got := r.postPath(t, p, body); got.status != http.StatusNotFound || !strings.Contains(got.body, `"not_found"`) {
			t.Fatalf("POST %s = %d %s, muốn 404 not_found", p, got.status, got.body)
		}
	}
	if names, err := r.spool.List(); err != nil || len(names) != 0 {
		t.Fatalf("tệp tạm = %v %v, muốn không có", names, err)
	}
	if routed, plain := src.calls(); len(routed) != 0 || plain != 0 {
		t.Fatalf("nguồn bị gọi tách: %v %d", routed, plain)
	}
}

// Database chết: tệp tạm mang routeKey; nạp lại → bản nhận có đúng route_key.
func TestRoutedSpoolKeepsRouteKey(t *testing.T) {
	src := &routedSrc{}
	dead := routedRig(t, src, true)
	if got := dead.postPath(t, "/hooks/routed/"+rkA, routedBody(t, "s1")); got.status != http.StatusOK {
		t.Fatalf("POST khi database chết = %d", got.status)
	}
	names, err := dead.spool.List()
	if err != nil || len(names) != 1 {
		t.Fatalf("tệp tạm = %v %v", names, err)
	}
	raw, err := os.ReadFile(filepath.Join(dead.spool.Dir, names[0]))
	if err != nil || !strings.Contains(string(raw), `"routeKey":"`+rkA+`"`) {
		t.Fatalf("tệp tạm thiếu routeKey: %s %v", raw, err)
	}
	live := routedRig(t, src, false)
	if n, err := dead.spool.Drain(t.Context(), live.in.DBForTest()); err != nil || n != 1 {
		t.Fatalf("nạp tệp tạm: %d %v", n, err)
	}
	if keys := deliveryRouteKeys(t, live); len(keys) != 1 || keys[0] != rkA {
		t.Fatalf("route_key sau khi nạp = %q", keys)
	}
}

// Tệp tạm có routeKey sai mẫu là tệp hỏng (không chặn hàng nạp, không ghi dòng sai vào database).
func TestRoutedSpoolBadKeyIsCorrupt(t *testing.T) {
	live := routedRig(t, &routedSrc{}, false)
	e := map[string]any{
		"source": routedKey, "headers": map[string]any{}, "bodyBase64": []byte("{}"),
		"receivedAt": time.Now().UTC(), "routeKey": "short",
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	name := "1795510800000-0195a000-0000-7000-8000-000000000001.json"
	if err := os.WriteFile(filepath.Join(live.spool.Dir, name), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if n, err := live.spool.Drain(t.Context(), live.in.DBForTest()); n != 0 || !errors.Is(err, webhooks.ErrCorruptSpoolFile) {
		t.Fatalf("nạp tệp có routeKey sai: %d %v, muốn ErrCorruptSpoolFile", n, err)
	}
	if keys := deliveryRouteKeys(t, live); len(keys) != 0 {
		t.Fatalf("có dòng: %v", keys)
	}
}

// Dấu vết của POST /hooks/{source}/{routeKey} không chứa routeKey (khóa tuyến là bí mật của đường dẫn).
func TestRoutedSpanHidesRouteKey(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	r := routedRig(t, &routedSrc{}, false)
	if got := r.postPath(t, "/hooks/routed/"+rkA, routedBody(t, "s1")); got.status != http.StatusOK {
		t.Fatalf("POST = %d", got.status)
	}
	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("không có span")
	}
	for _, s := range spans {
		for _, a := range s.Attributes {
			if strings.Contains(a.Value.String(), rkA) {
				t.Fatalf("span %s có routeKey ở %s", s.Name, a.Key)
			}
		}
		if strings.Contains(s.Name, rkA) {
			t.Fatalf("tên span có routeKey: %s", s.Name)
		}
	}
}

// Worker: bản nhận có route_key của nguồn RoutedSource → SplitRouted với đúng khóa, sự kiện định tuyến theo khóa đó
// tới tenant A; bản nhận không route_key → Split như M0. Handler nhận ReceivedAt (giờ nhận của bản nhận) và Via webhook.
func TestRoutedSplitUsesRouteKey(t *testing.T) {
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "a")
	for _, k := range []string{rkA, "plain"} {
		if err := tenancy.AddRoute(t.Context(), s.Owner, routedKey, k, a); err != nil {
			t.Fatal(err)
		}
	}
	src := &routedSrc{}
	sources := []webhooks.Source{src}
	split, err := webhooks.NewSplitHandler(webhooks.SplitOptions{Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	rec := &routedRecorder{}
	inbound, err := webhooks.InboundJobHandlers(sources, []webhooks.InboundHandler{rec})
	if err != nil {
		t.Fatal(err)
	}
	w := queue.NewWorker(s.App, append([]queue.Handler{split}, inbound...), queue.WorkerOptions{})
	got := time.Date(2026, 11, 24, 9, 0, 1, 0, time.UTC)
	for _, e := range []webhooks.Entry{
		{Source: routedKey, Body: routedBody(t, "r1"), ReceivedAt: got, RouteKey: rkA},
		{Source: routedKey, Body: routedBody(t, "p1"), ReceivedAt: got},
	} {
		if created, err := webhooks.Save(t.Context(), s.App, e); err != nil || !created {
			t.Fatalf("Save: %v %v", created, err)
		}
	}
	for range 10 {
		if n, err := w.RunOnce(t.Context(), 100); err != nil || n == 0 {
			break
		}
	}
	routed, plain := src.calls()
	// Việc tách gọi SplitRouted/Split một lần; mỗi việc vào đọc lại bản nhận gọi thêm một lần.
	if len(routed) != 2 || routed[0] != rkA || routed[1] != rkA || plain != 2 {
		t.Fatalf("SplitRouted %v, Split %d; muốn [A A], 2", routed, plain)
	}
	calls := rec.calls()
	if len(calls) != 2 {
		t.Fatalf("handler nhận %d sự kiện, muốn 2", len(calls))
	}
	for _, c := range calls {
		if c.TenantID != a || !c.ReceivedAt.Equal(got) || c.Via != webhooks.ViaWebhook {
			t.Fatalf("sự kiện vào: tenant %v receivedAt %v via %q", c.TenantID, c.ReceivedAt, c.Via)
		}
	}
}

// routedRecorder là InboundHandler của nguồn routed.
type routedRecorder struct {
	mu  sync.Mutex
	got []webhooks.Inbound
}

func (*routedRecorder) Source() string { return routedKey }

func (r *routedRecorder) Handle(_ context.Context, e webhooks.Inbound, _ pgx.Tx) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, e)
	return nil
}

func (r *routedRecorder) calls() []webhooks.Inbound {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]webhooks.Inbound(nil), r.got...)
}
