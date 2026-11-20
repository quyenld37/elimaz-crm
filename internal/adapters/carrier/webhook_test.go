package carrier_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/carrier"
	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

const hookSecret = "0123456789abcdef0123456789abcdef"

// hookEnv: tenant A (shiptest.Shop: tài khoản hãng giả fake-1, tuyến route_key đăng ký qua EnsureAccountRoute), ingest
// thật với nguồn chung carrier.WebhookSource của adapter fake, worker tách gói + handler vào của Module.
type hookEnv struct {
	t      *testing.T
	staff  *testkit.Staff
	shop   *shiptest.Shop
	tenant uuid.UUID
	user   uuid.UUID
	rk     string
	reg    *metrics.Registry
	srv    *httptest.Server
	worker *queue.Worker
}

func newHookEnv(t *testing.T) *hookEnv {
	t.Helper()
	staff := testkit.OpenStaff(t)
	tenant := staff.Tenant(t, "A")
	e := &hookEnv{t: t, staff: staff, tenant: tenant, reg: metrics.NewRegistry(), user: staff.User(t, tenant, "kho@elimaz.test", "active")}
	testkit.InTenant(t, staff.Owner, tenant, func(tx pgx.Tx) error {
		e.shop = shiptest.NewShop(t, tx, tenant)
		return nil
	})
	testkit.InTenant(t, staff.Owner, tenant, func(tx pgx.Tx) (err error) {
		e.rk, err = fulfillment.EnsureAccountRoute(t.Context(), tx, e.shop.Account)
		return err //nolint:wrapcheck // test
	})
	client := fake.NewClient("http://127.0.0.1:1", hookSecret, nil)
	in, err := webhooks.NewIngest(webhooks.IngestOptions{
		Role: "ingest", DB: e.staff.App, Sources: []webhooks.Source{carrier.WebhookSource(client, hookSecret)},
		Spool: &webhooks.Spool{Dir: t.TempDir()}, Lockout: webhooks.NewLockout(webhooks.LockoutOptions{}),
		Metrics: e.reg, Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	in.Register(mux)
	e.srv = httptest.NewServer(mux)
	t.Cleanup(e.srv.Close)

	reg, err := fulfillment.NewRegistry(client)
	if err != nil {
		t.Fatal(err)
	}
	sources := carrier.WebhookSources([]fulfillment.CarrierAdapter{client}, func(string) string { return "" })
	split, splitErr := webhooks.NewSplitHandler(webhooks.SplitOptions{Sources: sources, Metrics: e.reg})
	mod := fulfillment.New(fulfillment.Deps{DB: e.staff.App, Queue: queue.NewQueue(), Carriers: reg, Metrics: e.reg})
	inbound, inboundErr := webhooks.InboundJobHandlers(sources, mod.InboundHandlers())
	if err := errors.Join(splitErr, inboundErr); err != nil {
		t.Fatal(err)
	}
	e.worker = queue.NewWorker(e.staff.App, append([]queue.Handler{split}, inbound...), queue.WorkerOptions{})
	return e
}

// shipment tạo đơn 1 cái và vận đơn (chưa gửi hãng, chưa có mã hãng); tracking khác rỗng thì gắn mã hãng như lệnh tạo.
func (e *hookEnv) shipment(tracking string) fulfillment.Shipment {
	e.t.Helper()
	var v uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		v = e.shop.Variant(e.t, tx, e.shop.Seller, 250000, 5)
		return nil
	})
	var s fulfillment.Shipment
	e.app(func(tx pgx.Tx) error {
		o, err := e.shop.Order(e.t.Context(), tx, e.shop.Seller, e.user, 30000, order.ItemInput{VariantID: v, Qty: 1})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		s, err = e.shop.Shipment(e.t.Context(), tx, e.shop.Seller, o.ID, e.user)
		return err //nolint:wrapcheck // test
	})
	if tracking != "" {
		e.ownerExec("UPDATE fulfillment.shipments SET tracking_code = $2 WHERE id = $1", s.ID, tracking)
	}
	return s
}

func (e *hookEnv) app(fn func(tx pgx.Tx) error) {
	e.t.Helper()
	testkit.InTenant(e.t, e.staff.App, e.tenant, fn)
}

// ownerExec chạy một câu bằng chủ bảng trong ngữ cảnh tenant A (dựng tình huống như lệnh tạo, như kẻ có quyền database).
func (e *hookEnv) ownerExec(sql string, args ...any) {
	e.t.Helper()
	if err := e.staff.TryExec(e.t.Context(), e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), sql, args...)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("%s: %v", sql, err)
	}
}

func (e *hookEnv) status(id uuid.UUID) string {
	e.t.Helper()
	var st string
	e.app(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), "SELECT status FROM fulfillment.shipments WHERE id = $1", id).Scan(&st)
	})
	return st
}

// post gửi gói một sự kiện tới path (tương đối /hooks/fake), ký bằng key; trả mã HTTP.
func (e *hookEnv) post(path, key string, ev fake.Event) int {
	e.t.Helper()
	body, err := json.Marshal(fake.Packet{Events: []fake.Event{ev}})
	if err != nil {
		e.t.Fatal(err)
	}
	req, _ := http.NewRequestWithContext(e.t.Context(), http.MethodPost, e.srv.URL+"/hooks/fake"+path, bytes.NewReader(body))
	req.Header.Set(fake.SignatureHeader, fake.Sign(body, key))
	req.Header.Set("Content-Type", "application/json")
	status, _, _ := testkit.Do(e.t, req)
	return status
}

func (e *hookEnv) drain() {
	e.t.Helper()
	for range 20 {
		n, err := e.worker.RunOnce(e.t.Context(), 10)
		if err != nil {
			e.t.Fatal(err)
		}
		if n == 0 {
			return
		}
	}
	e.t.Fatal("worker còn việc sau 20 lượt")
}

func (e *hookEnv) countIn(q string, args ...any) int {
	e.t.Helper()
	return e.staff.CountIn(e.t, e.tenant, q, args...)
}

func (e *hookEnv) metric(name string) int64 {
	var n int64
	for _, r := range e.reg.Drain() {
		if r.Name == name && r.Label == "fake" {
			n += r.Count
		}
	}
	return n
}

func (e *hookEnv) sigRefresh(id uuid.UUID) int {
	e.t.Helper()
	return e.countIn(`SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.refresh_status'
		AND payload->>'shipmentId' = $1 AND dedupe_key LIKE 'sig:%'`, id.String())
}

func event(id, tracking, status string, at int) fake.Event {
	return fake.Event{EventID: id, AccountKey: "fake-1", Tracking: tracking, Status: status, SentAt: time.Unix(int64(1_795_510_800+at), 0)}
}

// VC17, M1 Task 31 trên đường có routeKey: chữ ký sai → 401, 0 bản nhận; chữ ký đúng, thân nói delivered, cùng
// eventId 3 lần → 200, bản nhận có route_key, một việc vào của tenant A, một lệnh tra cho V1, V1 không đổi trạng thái.
// routeKey không có tuyến → inbound_unrouted +1, không việc vào; đường /hooks/fake không routeKey → việc tách dead.
func TestWebhookRouted_SignalOnly(t *testing.T) {
	e := newHookEnv(t)
	v1 := e.shipment("FK0001")
	before := e.status(v1.ID)

	if s := e.post("/"+e.rk, "sai-khoa-sai-khoa-sai-khoa-sai-khoa", event("e-bad", "FK0001", "delivered", 0)); s != http.StatusUnauthorized {
		t.Fatalf("chữ ký sai = %d, muốn 401", s)
	}
	if n := e.staff.Count(t, "SELECT count(*) FROM platform.webhook_deliveries"); n != 0 {
		t.Fatalf("bản nhận = %d sau chữ ký sai", n)
	}
	for i := range 3 {
		if s := e.post("/"+e.rk, hookSecret, event("e1", "FK0001", "delivered", i)); s != http.StatusOK {
			t.Fatalf("POST %d = %d", i, s)
		}
	}
	if n := e.staff.Count(t, "SELECT count(*) FROM platform.webhook_deliveries WHERE source = 'fake' AND route_key = $1", e.rk); n != 3 {
		t.Fatalf("bản nhận có route_key = %d, muốn 3", n)
	}
	e.drain()
	if n := e.countIn("SELECT count(*) FROM platform.jobs WHERE kind = 'inbound.fake' AND status = 'done' AND tenant_id = $1", e.tenant); n != 1 {
		t.Fatalf("việc vào done của tenant A = %d, muốn 1", n)
	}
	if n := e.sigRefresh(v1.ID); n != 1 {
		t.Fatalf("lệnh tra V1 = %d, muốn 1", n)
	}
	if got := e.status(v1.ID); got != before {
		t.Fatalf("V1 = %s sau tín hiệu delivered, muốn giữ %s", got, before)
	}

	if s := e.post("/"+strings.Repeat("Q", 32), hookSecret, event("e-unrouted", "FK0001", "delivered", 9)); s != http.StatusOK {
		t.Fatalf("POST tuyến lạ = %d", s)
	}
	if s := e.post("", hookSecret, event("e-noroute", "FK0001", "delivered", 10)); s != http.StatusOK {
		t.Fatalf("POST không routeKey = %d", s)
	}
	e.drain()
	if n := e.metric("inbound_unrouted"); n != 1 {
		t.Fatalf("inbound_unrouted = %d, muốn 1", n)
	}
	if n := e.countIn("SELECT count(*) FROM platform.jobs WHERE kind = 'inbound.fake'"); n != 1 {
		t.Fatalf("việc vào = %d, muốn 1", n)
	}
	if n := e.staff.Count(t, "SELECT count(*) FROM platform.jobs WHERE kind = 'webhook.split' AND status = 'dead'"); n != 1 {
		t.Fatalf("việc tách dead = %d, muốn 1 (gói không routeKey)", n)
	}
}

// VC10: tín hiệu đến khi vận đơn chưa có mã hãng → việc vào pending, attempts 1; gắn mã hãng (như lệnh tạo) và chạy
// lại → một lệnh tra, việc vào done. Tài khoản revoked → carrier_signal_revoked_account +1, không lệnh tra.
func TestWebhookRouted_EarlyAndRevoked(t *testing.T) {
	e := newHookEnv(t)
	v2 := e.shipment("")
	if s := e.post("/"+e.rk, hookSecret, event("e-late", "FK0002", "picked_up", 0)); s != http.StatusOK {
		t.Fatalf("POST = %d", s)
	}
	e.drain()
	inboundJob := "SELECT count(*) FROM platform.jobs WHERE kind = 'inbound.fake' AND dedupe_key = $1 AND status = $2 AND attempts = $3"
	if n := e.countIn(inboundJob, e.rk+":e-late", "pending", 1); n != 1 {
		t.Fatalf("việc vào pending attempts 1 = %d", n)
	}
	e.ownerExec("UPDATE fulfillment.shipments SET tracking_code = 'FK0002' WHERE id = $1", v2.ID)
	e.ownerExec("UPDATE platform.jobs SET run_at = now() WHERE kind = 'inbound.fake' AND dedupe_key = $1", e.rk+":e-late")
	e.drain()
	if n := e.countIn(inboundJob, e.rk+":e-late", "done", 2); n != 1 {
		t.Fatalf("việc vào done sau khi gắn mã = %d", n)
	}
	if n := e.sigRefresh(v2.ID); n != 1 {
		t.Fatalf("lệnh tra V2 = %d, muốn 1", n)
	}

	e.ownerExec("UPDATE fulfillment.carrier_accounts SET status = 'revoked' WHERE id = $1", e.shop.Account)
	if s := e.post("/"+e.rk, hookSecret, event("e-rev", "FK0002", "delivered", 1)); s != http.StatusOK {
		t.Fatalf("POST = %d", s)
	}
	e.drain()
	if n := e.metric("carrier_signal_revoked_account"); n != 1 {
		t.Fatalf("carrier_signal_revoked_account = %d, muốn 1", n)
	}
	if n := e.sigRefresh(v2.ID); n != 1 {
		t.Fatalf("lệnh tra V2 = %d sau tài khoản revoked, muốn vẫn 1", n)
	}
}
