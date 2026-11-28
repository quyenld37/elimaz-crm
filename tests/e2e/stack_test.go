package e2e

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"elimaz.com.vn/crm/internal/adapters/carrier"
	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/httpapi"
	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/selftest"
	"elimaz.com.vn/crm/internal/platform/telemetry"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

const (
	stackAUD       = "aud-e2e"
	stackOrigin    = "http://127.0.0.1:4192"
	stackOwner     = "owner@elimaz.test"
	stackTenantKey = "shop-e2e"
)

// stack là ba vai trò thật của CRM chạy trong tiến trình test (để exporter dấu vết trong bộ nhớ thấy span của cả ba):
// ingest (webhooks.Ingest + Drainer, pool riêng có thể đi qua proxy có công tắc), api (httpapi.NewServer với hub
// LISTEN thật, thẻ Access thử) và worker (queue.Worker với việc tách gói, selftest.Inbound phát qua hub, lệnh
// command.selftest gọi FakeExternal; sự kiện nội bộ vào sổ COD cash). Cùng một database thật đã migrate; tenant có chủ
// shop (identity.SeedOwner), định tuyến ("selftest", stackTenantKey), người bán ELIMAZ, kho MAIN và tài khoản hãng giả
// fake-1 (shiptest.Shop, tuyến carrier_account:<route_key>). Có stackOptions.fakeCarrierURL thì nối hãng giả như cmd/crm
// (nguồn /hooks/fake/<route_key> ở ingest và worker, handler tín hiệu của fulfillment, hai lệnh vận chuyển).
type stack struct {
	t      *testing.T
	owner  *db.DB // chủ bảng: dựng dữ liệu, đếm dòng
	app    *db.DB // crm_app trực tiếp (worker, api)
	tenant uuid.UUID
	shop   *shiptest.Shop
	token  string // thẻ Access của chủ shop
	keys   *cfaccesstest.AccessKeys
	spans  *tracetest.InMemoryExporter
	spool  *webhooks.Spool
	ext    *testkit.FakeExternal
	ingest *httptest.Server
	api    *httptest.Server
	// m3: hai module M3 đủ phần (stackOptions.m3); nil khi stack chỉ nối phần M1.
	m3 *m3Wiring
	// cmds là lệnh của worker (test tra lại lệnh in_doubt bằng commands.ResolveInDoubt).
	cmds []commands.Handler
}

type stackOptions struct {
	// appURL đổi chuỗi kết nối crm_app của api, worker và module (proxy có công tắc: database ngừng giữa lúc worker
	// chạy lệnh); nil: nối thẳng. Hub LISTEN, chủ bảng của test luôn nối thẳng.
	appURL func(appURL string) string
	// ingestURL đổi chuỗi kết nối của pool ingest (proxy có công tắc); nil: như worker.
	ingestURL func(appURL string) string
	// drainEvery: nhịp Drainer của ingest (mặc định 100 ms).
	drainEvery time.Duration
	// fakeCarrierURL: địa chỉ tools/fakecarrier (bí mật secret32); rỗng → không nối hãng giả.
	fakeCarrierURL string
	// m3: nối đủ hai module M3 (fulfillment, cash) và phần chạy nền của M2 như cmd/crm (đường trọn M3, Task M3-48);
	// cần fakeCarrierURL. false: chỉ lệnh tạo, tra vận đơn và consumer sổ COD của M1.
	m3 bool
	// m6: nối thêm phần chạy nền của báo cáo M6 (lệnh command.report_run, việc chụp tồn và dọn) vào worker, trên kho
	// tệp trong bộ nhớ của m3 như cmd/crm (Task 242); cần m3. api luôn có bộ chạy báo cáo tại chỗ mặc định.
	m6 bool
}

func newStack(t *testing.T, o stackOptions) *stack {
	t.Helper()
	s := &stack{
		t: t, spans: tracetest.NewInMemoryExporter(), keys: cfaccesstest.AccessKeyServer(t),
		spool: &webhooks.Spool{Dir: t.TempDir()}, ext: &testkit.FakeExternal{Mode: testkit.FakeOK},
	}
	// Dấu vết bật, mọi span được giữ, xuất ngay khi kết thúc: đặt trước khi mở pool và dựng handler (otelpgx,
	// otelhttp, queue lấy tracer lúc dựng).
	shutdown, err := telemetry.Setup(t.Context(), config.Config{OTelEnabled: true, TracesSampler: "always_on", Role: "e2e"},
		telemetry.WithSyncExporter(s.spans))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	_, ownerURL, appURL := testkit.StartPostgres(t)
	s.owner = openPool(t, ownerURL, false)
	if o.appURL != nil {
		s.app = openPool(t, o.appURL(appURL), true)
	} else {
		s.app = openPool(t, appURL, true)
	}
	ingestURL := appURL
	if o.ingestURL != nil {
		ingestURL = o.ingestURL(appURL)
	}
	ingestDB := openPool(t, ingestURL, true)
	s.seed()
	if o.fakeCarrierURL != "" {
		s.seedFakeLocations()
	}

	logger := slog.New(slog.DiscardHandler)
	reg := metrics.NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	// Sổ consumer dùng chung cho api, lệnh vận chuyển và worker, như newModules của cmd/crm.
	evs := events.NewRegistry()

	// api: hub LISTEN thật + máy chủ HTTP thật (fulfillment.Service như cmd/crm, cho gửi hãng của M3).
	hub := realtime.NewPGHub(realtime.PGHubOptions{URL: appURL, Metrics: reg, Logger: logger})
	wg.Go(func() { hub.Run(ctx) })
	s.startAPI(o, hub, evs, reg, logger)

	// ingest: /hooks/* + Drainer.
	ingestSources := []webhooks.Source{selftest.NewSource(secret32)}
	if o.fakeCarrierURL != "" {
		hook := fake.NewClient(o.fakeCarrierURL, secret32, nil)
		ingestSources = append(ingestSources, carrier.WebhookSource(hook, secret32))
	}
	s.ingest = ingestServer(t, webhooks.IngestOptions{DB: ingestDB, Sources: ingestSources, Spool: s.spool, Metrics: reg, Logger: logger})
	drainEvery := o.drainEvery
	if drainEvery <= 0 {
		drainEvery = 100 * time.Millisecond
	}
	drainer := &webhooks.Drainer{Spool: s.spool, DB: ingestDB, Interval: drainEvery, Metrics: reg, Logger: logger}
	wg.Go(func() { drainer.Run(ctx) })

	// worker: cùng các handler, lệnh như cmd/crm.
	handlers, cmds := s.workerHandlers(o, hub, evs, reg)
	s.cmds = cmds
	w := queue.NewWorker(s.app, handlers, queue.WorkerOptions{
		WorkerID: "e2e-worker", Metrics: reg, Logger: logger, Commands: commands.NewRunner(s.app, cmds),
	})
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = w.Stop(5 * time.Second)
		s.ingest.Close()
		s.api.Close()
		cancel()
		wg.Wait()
	})
	waitFor(t, 10*time.Second, "hub LISTEN", func() bool { l, _ := hub.Status(); return l })
	return s
}

// startAPI đăng ký consumer (đủ M3 khi o.m3, không thì consumer sổ COD của M1) vào evs và chạy máy chủ HTTP thật của
// api (fulfillment.Service như cmd/crm, cho gửi hãng của M3; đủ hai module M3 khi o.m3).
func (s *stack) startAPI(o stackOptions, hub *realtime.PGHub, evs *events.Registry, reg *metrics.Registry, logger *slog.Logger) {
	s.t.Helper()
	if o.m3 {
		s.m3 = s.wireM3(o.fakeCarrierURL, hub, evs, reg)
	} else if err := evs.Register(cash.Consumer{}); err != nil {
		s.t.Fatal(err)
	}
	if o.m6 {
		reporting.RegisterFilePurposes()
	}
	streamStop := make(chan struct{})
	apiDeps := httpapi.Deps{
		Config: config.Config{ConsoleOrigins: []string{stackOrigin}}, DB: s.app, Logger: logger, Events: evs,
		Access: s.keys.Verifier(s.t, stackAUD), Realtime: hub, SSE: realtime.SSEOptions{Stop: streamStop},
		Fulfillment: fulfillment.New(fulfillment.Deps{DB: s.app, Queue: queue.NewQueue(), Bus: hub, Events: evs}).Service,
	}
	if s.m3 != nil {
		apiDeps = s.m3.apiDeps(apiDeps)
	}
	s.api = httptest.NewUnstartedServer(httpapi.NewServer(apiDeps))
	s.api.Config.RegisterOnShutdown(func() { close(streamStop) })
	s.api.Start()
}

// workerHandlers trả handler và lệnh của worker, như cmd/crm: tách gói, sự kiện vào selftest (và tín hiệu hãng giả khi
// có o.fakeCarrierURL), consumer sự kiện nội bộ của evs; lệnh selftest (và hai lệnh vận chuyển với hãng giả).
func (s *stack) workerHandlers(o stackOptions, hub *realtime.PGHub, evs *events.Registry, reg *metrics.Registry) ([]queue.Handler, []commands.Handler) {
	s.t.Helper()
	sources := []webhooks.Source{selftest.NewSource("")}
	inboundHandlers := []webhooks.InboundHandler{selftest.NewInbound(hub)}
	cmds := []commands.Handler{selftest.NewCommand(s.ext)}
	var jobs []queue.Handler
	switch {
	case s.m3 != nil:
		src, in, c, j, err := s.m3.worker()
		if err != nil {
			s.t.Fatal(err)
		}
		sources, inboundHandlers, cmds, jobs = append(sources, src...), append(inboundHandlers, in...), append(cmds, c...), j
	case o.fakeCarrierURL != "":
		client := fake.NewClient(o.fakeCarrierURL, secret32, nil)
		sources = append(sources, carrier.WebhookSource(client, ""))
		co := fulfillment.CommandOptions{
			DB: s.app, Adapters: []fulfillment.CarrierAdapter{client}, Events: evs, Queue: queue.NewQueue(), Realtime: hub,
		}
		reg, err := fulfillment.NewRegistry(client)
		if err != nil {
			s.t.Fatal(err)
		}
		mod := fulfillment.New(fulfillment.Deps{
			DB: s.app, Queue: queue.NewQueue(), Bus: hub, Events: evs, Carriers: reg,
		})
		inboundHandlers = append(inboundHandlers, mod.InboundHandlers()...)
		cmds = append(cmds, fulfillment.NewCreateCommand(co), fulfillment.NewRefreshCommand(mod.Service))
	}
	if o.m6 {
		if s.m3 == nil {
			s.t.Fatal("stackOptions.m6 cần m3")
		}
		d := reporting.Deps{
			DB: s.app, Files: s.m3.files, Bus: hub, Queue: queue.NewQueue(), Metrics: reg, Now: time.Now,
			Result: httpapi.ReportResultJSON, // như cmd/crm: kết quả lần chạy nền = JSON của GET báo cáo
		}
		cmds = append(cmds, reporting.JobHandlers(d)...)
		jobs = append(jobs, reporting.TxHandlers(d)...)
	}
	split, err := webhooks.NewSplitHandler(webhooks.SplitOptions{Sources: sources, Metrics: reg})
	if err != nil {
		s.t.Fatal(err)
	}
	inbound, err := webhooks.InboundJobHandlers(sources, inboundHandlers)
	if err != nil {
		s.t.Fatal(err)
	}
	return append(append(append([]queue.Handler{split}, evs.JobHandlers()...), inbound...), jobs...), cmds
}

func openPool(t *testing.T, u string, tracing bool) *db.DB {
	t.Helper()
	d, err := db.Open(t.Context(), u, db.PoolOptions{MaxConns: 10, Tracing: tracing})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Pool.Close)
	return d
}

// seed: tenant, chủ shop có mọi quyền (vai trò owner, phạm vi all), cửa hàng có tài khoản hãng giả (shiptest.Shop) và
// tuyến webhook carrier_account:<route_key> của fake-1 (như seed-dev), thẻ Access của chủ shop.
func (s *stack) seed() {
	s.t.Helper()
	ctx := s.t.Context()
	id, err := seedSelftestTenant(ctx, s.owner)
	if err != nil {
		s.t.Fatal(err)
	}
	s.tenant = id
	if err := tenancy.WithTenant(ctx, s.owner, id, func(tx pgx.Tx) error {
		s.shop = shiptest.NewShop(s.t, tx, id)
		return nil
	}); err != nil {
		s.t.Fatal(err)
	}
	if err := tenancy.WithTenant(ctx, s.owner, id, func(tx pgx.Tx) error {
		_, err := fulfillment.EnsureAccountRoute(ctx, tx, s.shop.Account)
		return err //nolint:wrapcheck // test
	}); err != nil {
		s.t.Fatal(err)
	}
	s.token = s.keys.Sign(cfaccesstest.AccessClaims{Email: stackOwner, AUD: stackAUD, MFA: []string{"mfa"}})
}

// fakeProvince, fakeWard là mã tỉnh, phường của địa chỉ giao thử: hãng giả (tools/fakecarrier /v1/locations) có cùng
// mã; seedFakeLocations khớp sẵn hai mã cho hãng giả.
const (
	fakeProvince = "79"
	fakeWard     = "26734"
)

// seedFakeLocations: danh mục hành chính có tỉnh fakeProvince, phường fakeWard (bảng chung, chủ bảng) và hai mã đã khớp
// với hãng giả trong carrier_locations của tenant (như sau khi syncCarrierLocations chạy): hãng giả khai Locations nên
// báo phí, tạo vận đơn đòi địa chỉ đã khớp (address_unmapped).
func (s *stack) seedFakeLocations() {
	s.t.Helper()
	ctx := s.t.Context()
	if _, err := s.owner.Pool.Exec(ctx, `INSERT INTO customer.admin_units
		(code, level, parent_code, name, name_search, effective_from, source) VALUES
		($1, 'province', NULL, 'Thành phố Hồ Chí Minh', 'thanh pho ho chi minh', DATE '2025-07-01', 'test'),
		($2, 'ward', $1, 'Phường Sài Gòn', 'phuong sai gon', DATE '2025-07-01', 'test')
		ON CONFLICT DO NOTHING`, fakeProvince, fakeWard); err != nil {
		s.t.Fatal(err)
	}
	if err := tenancy.WithTenant(ctx, s.owner, s.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO fulfillment.carrier_locations
			(tenant_id, carrier_key, level, our_code, carrier_code, carrier_name, synced_at) VALUES
			($1, 'fake', 'province', $2, $2, 'TP Hồ Chí Minh', now()), ($1, 'fake', 'ward', $3, $3, 'Phường Sài Gòn', now())
			ON CONFLICT DO NOTHING`, s.tenant, fakeProvince, fakeWard)
		return err //nolint:wrapcheck // test
	}); err != nil {
		s.t.Fatal(err)
	}
}

// seedSelftestTenant dựng tenant "E2E", chủ shop stackOwner (identity.SeedOwner), định tuyến email và định tuyến
// ("selftest", stackTenantKey) bằng chủ bảng.
func seedSelftestTenant(ctx context.Context, owner *db.DB) (uuid.UUID, error) {
	id, err := tenancy.SeedTenant(ctx, owner, "E2E")
	if err != nil {
		return uuid.Nil, err //nolint:wrapcheck // test
	}
	if err := identity.SeedOwner(ctx, owner, id, stackOwner); err != nil {
		return uuid.Nil, err //nolint:wrapcheck // test
	}
	for _, r := range [][2]string{{identity.RouteKindStaffEmail, identity.NormalizeEmail(stackOwner)}, {selftest.Key, stackTenantKey}} {
		if err := tenancy.AddRoute(ctx, owner, r[0], r[1], id); err != nil {
			return uuid.Nil, err //nolint:wrapcheck // test
		}
	}
	return id, nil
}

// postPing gửi một gói selftest có chữ ký với các ping eventIDs tới ingest của stack; trả mã HTTP.
func (s *stack) postPing(eventIDs ...string) int {
	s.t.Helper()
	return postPingTo(s.t, s.ingest.URL, eventIDs...)
}

// postPingTo gửi tới ingest ở base.
func postPingTo(t *testing.T, base string, eventIDs ...string) int {
	t.Helper()
	type ev struct {
		Kind      string    `json:"kind"`
		EventID   string    `json:"eventId"`
		TenantKey string    `json:"tenantKey"`
		SentAt    time.Time `json:"sentAt"`
	}
	p := struct {
		Events []ev `json:"events"`
	}{}
	for _, id := range eventIDs {
		p.Events = append(p.Events, ev{Kind: "ping", EventID: id, TenantKey: stackTenantKey, SentAt: time.Now().UTC()})
	}
	body, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return postSigned(t, base+"/hooks/selftest", "X-Selftest-Signature", body)
}

// postSigned POST body tới url với header ký HMAC-SHA256 "sha256=<hex>" bằng secret32; trả mã HTTP.
func postSigned(t *testing.T, url, header string, body []byte) int {
	t.Helper()
	m := hmac.New(sha256.New, []byte(secret32))
	m.Write(body)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(header, "sha256="+hex.EncodeToString(m.Sum(nil)))
	status, _, _ := testkit.Do(t, req)
	return status
}

// ingestServer dựng vai trò ingest (o với Lockout mặc định) sau một httptest.Server; bên gọi đóng.
func ingestServer(t *testing.T, o webhooks.IngestOptions) *httptest.Server {
	t.Helper()
	o.Lockout = webhooks.NewLockout(webhooks.LockoutOptions{})
	in, err := webhooks.NewIngest(o)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	in.Register(mux)
	return httptest.NewServer(mux)
}

// openSSE mở luồng SSE bằng thẻ chủ shop và đòi khung đầu là resync.
func (s *stack) openSSE() *testkit.SSEClient {
	s.t.Helper()
	c := testkit.OpenSSE(s.t, s.api.URL, s.token)
	if ev, _, err := c.Next(5 * time.Second); err != nil || ev != realtime.EventResync {
		s.t.Fatalf("khung đầu: %q %v, muốn resync", ev, err)
	}
	return c
}

// pong là data của một khung changed.
type pong struct {
	Topic  string          `json:"topic"`
	Entity realtime.Entity `json:"entity"`
}

// nextPong chờ khung changed topic selftest.pong kế tiếp (bỏ qua ping, resync) trong timeout; trả eventId.
func nextPong(c *testkit.SSEClient, timeout time.Duration) (string, error) {
	return nextChanged(c, timeout, selftest.PongTopic, selftest.Key)
}

// nextChanged chờ khung changed kế tiếp có chủ đề topic và loại thực thể entity (bỏ qua khung khác) trong timeout;
// trả mã thực thể.
func nextChanged(c *testkit.SSEClient, timeout time.Duration, topic, entity string) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return "", testkit.ErrSSETimeout
		}
		ev, data, err := c.Next(left)
		if err != nil {
			return "", fmt.Errorf("chờ khung: %w", err)
		}
		if ev != realtime.EventChanged {
			continue
		}
		var p pong
		if err := json.Unmarshal(data, &p); err != nil {
			return "", fmt.Errorf("đọc khung %s: %w", data, err)
		}
		if p.Topic == topic && p.Entity.Type == entity {
			return p.Entity.ID, nil
		}
	}
}

// count chạy câu đếm bằng chủ bảng.
func (s *stack) count(q string, args ...any) int {
	s.t.Helper()
	return countRows(s.t, s.owner, q, args...)
}

func countRows(t *testing.T, owner *db.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := owner.Pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// spansSnapshot trả các span đã kết thúc.
func (s *stack) spansSnapshot() []sdktrace.ReadOnlySpan {
	return s.spans.GetSpans().Snapshots()
}

// waitFor chờ cond đúng trong timeout, không thì test đỏ với what.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("quá %v chờ %s", timeout, what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
