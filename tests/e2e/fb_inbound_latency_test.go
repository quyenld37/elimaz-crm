package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/httpapi"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
)

// fbLatencyBudget: từ lúc gửi webhook Facebook tới lúc màn hình nhận conversation.changed (CH01).
const fbLatencyBudget = 2000 * time.Millisecond

const (
	fbLatencyPage1 = "104000000000001"
	fbLatencyPage2 = "104000000000002"
	fbLatencyAUD   = "aud-fb-latency"
)

// fbKeyOnly là adapter chỉ có khóa facebook cho channel.Registry (conversation.InboundHandlers chỉ đọc khóa): adapter
// Facebook đầy đủ có ở Task 148 (facebook.Adapter còn trả nil). Phần vào thật là facebook.New ở ingest và worker.
type fbKeyOnly struct{ channel.MessagingAdapter }

func (fbKeyOnly) Key() string { return facebook.Key }

// TestFBInboundLatency (CH01, đường thật): ingest (nguồn facebook thật, kiểm chữ ký), worker (tách gói + handler vào
// của conversation), api (hub LISTEN thật, SSE). Nhân viên có conversation.read trên Page 1 mở SSE; POST /hooks/facebook
// có chữ ký một tin của khách mới → nhận changed đúng hội thoại dưới 2.000 ms; 20 lần, in lớn nhất và p95. Nhân viên
// chỉ có quyền Page 2 không nhận khung changed nào.
// fbLatency là ba vai trò thật của đường vào Facebook trong tiến trình test (xem TestFBInboundLatency).
type fbLatency struct {
	staff          *testkit.Staff
	tenant, page1  uuid.UUID
	token1, token2 string
	api, ingest    *httptest.Server
}

// newFBLatency dựng database thật đã migrate, tenant có Page 1, 2 (tuyến fb_page), hai nhân viên có conversation.read
// trên Page 1 và trên Page 2; api (hub LISTEN, SSE), ingest (nguồn facebook thật), worker (tách gói, handler vào của
// conversation); dừng khi test xong.
func newFBLatency(t *testing.T) *fbLatency {
	t.Helper()
	_, ownerURL, appURL := testkit.StartPostgres(t)
	owner, app := openPool(t, ownerURL, false), openPool(t, appURL, false)
	staff := &testkit.Staff{App: app, Owner: owner}
	ctx := t.Context()
	tenant, err := tenancy.SeedTenant(ctx, owner, "FB")
	if err != nil {
		t.Fatal(err)
	}
	page1 := staff.ChannelAccount(t, tenant, facebook.Key, fbLatencyPage1, "Page 1")
	page2 := staff.ChannelAccount(t, tenant, facebook.Key, fbLatencyPage2, "Page 2")
	for _, p := range []string{fbLatencyPage1, fbLatencyPage2} {
		if err := tenancy.AddRoute(ctx, owner, facebook.RouteKind, p, tenant); err != nil {
			t.Fatal(err)
		}
	}
	keys := cfaccesstest.AccessKeyServer(t)
	token := func(email string, page uuid.UUID) string {
		u := staff.User(t, tenant, email, "active")
		staff.Grant(t, tenant, u, []string{"conversation.read", "realtime.subscribe"}, identity.ScopePage, page.String())
		return keys.Sign(cfaccesstest.AccessClaims{Email: email, AUD: fbLatencyAUD, MFA: []string{"mfa"}})
	}
	token1, token2 := token("nv1@elimaz.test", page1), token("nv2@elimaz.test", page2)

	runCtx, logger, reg, evs, hub := startHub(t, appURL)
	streamStop := make(chan struct{})
	api := httptest.NewUnstartedServer(httpapi.NewServer(httpapi.Deps{
		Config: config.Config{ConsoleOrigins: []string{stackOrigin}}, DB: app, Logger: logger, Events: evs,
		Access: keys.Verifier(t, fbLatencyAUD), Realtime: hub, SSE: realtime.SSEOptions{Stop: streamStop},
	}))
	api.Config.RegisterOnShutdown(func() { close(streamStop) })
	api.Start()

	fb := facebook.New(facebook.Options{AppSecret: logging.Secret(secret32), VerifyToken: logging.Secret(secret32 + secret32)})
	sources := []webhooks.Source{fb}
	ingest := ingestServer(t, webhooks.IngestOptions{DB: app, Sources: sources, Spool: &webhooks.Spool{Dir: t.TempDir()}, Metrics: reg, Logger: logger})

	w := queue.NewWorker(app, fbWorkerHandlers(t, app, hub, evs, reg, sources),
		queue.WorkerOptions{WorkerID: "e2e-fb", Metrics: reg, Logger: logger})
	if err := w.Start(runCtx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = w.Stop(5 * time.Second)
		ingest.Close()
		api.Close()
	})
	waitFor(t, 10*time.Second, "hub LISTEN", func() bool { l, _ := hub.Status(); return l })

	return &fbLatency{staff: staff, tenant: tenant, page1: page1, token1: token1, token2: token2, api: api, ingest: ingest}
}

func TestFBInboundLatency(t *testing.T) {
	t.Parallel()
	f := newFBLatency(t)
	ctx := t.Context()
	staff, tenant, page1 := f.staff, f.tenant, f.page1
	open := func(tok string) *testkit.SSEClient {
		c := testkit.OpenSSE(t, f.api.URL, tok)
		if ev, _, err := c.Next(5 * time.Second); err != nil || ev != realtime.EventResync {
			t.Fatalf("khung đầu: %q %v, muốn resync", ev, err)
		}
		return c
	}
	c1, c2 := open(f.token1), open(f.token2)

	var took []time.Duration
	for i := range 20 {
		user := fmt.Sprintf("70000000000%05d", i)
		start := time.Now()
		if status := postFacebookMessage(t, f.ingest.URL, fbLatencyPage1, user, fmt.Sprintf("m_lat_%d", i)); status != http.StatusOK {
			t.Fatalf("lần %d: POST /hooks/facebook = %d", i+1, status)
		}
		id, err := nextConversationChanged(c1, fbLatencyBudget-time.Since(start))
		d := time.Since(start)
		if err != nil {
			t.Fatalf("lần %d: không nhận conversation.changed trong %v: %v", i+1, fbLatencyBudget, err)
		}
		var want string
		staff.Exec(t, tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT id::text FROM conversation.conversations WHERE channel_account_id = $1
				AND external_thread_ref = $2`, page1, user).Scan(&want)
		})
		if id != want {
			t.Fatalf("lần %d: báo hiệu hội thoại %s, mong %s", i+1, id, want)
		}
		took = append(took, d)
	}
	slices.Sort(took)
	t.Logf("CH01: lớn nhất %v, p95 %v (ngưỡng %v)", took[len(took)-1], took[len(took)*95/100-1], fbLatencyBudget)

	if id, err := nextConversationChanged(c2, time.Second); err == nil {
		t.Errorf("nhân viên chỉ có quyền Page 2 nhận báo hiệu hội thoại %s của Page 1", id)
	}
}

// postFacebookMessage gửi tới ingest ở base một gói Facebook có chữ ký: tin text mid của PSID user tới Page page.
func postFacebookMessage(t *testing.T, base, page, user, mid string) int {
	t.Helper()
	return postFacebookMessageText(t, base, page, user, mid, "còn hàng không shop")
}

// nextConversationChanged chờ khung changed conversation.changed kế tiếp trong timeout; trả mã hội thoại.
func nextConversationChanged(c *testkit.SSEClient, timeout time.Duration) (string, error) {
	return nextChanged(c, timeout, conversation.TopicConversationChanged, "conversation")
}

// fbWorkerHandlers là handler của worker như cmd/crm: tách gói của sources, consumer sự kiện nội bộ, handler vào của
// conversation cho nguồn facebook.
func fbWorkerHandlers(t *testing.T, app *db.DB, hub realtime.Bus, evs *events.Registry, reg *metrics.Registry,
	sources []webhooks.Source,
) []queue.Handler {
	t.Helper()
	channels, err := channel.NewRegistry(fbKeyOnly{})
	if err != nil {
		t.Fatal(err)
	}
	conv := conversation.InboundHandlers(conversation.Deps{
		DB: app, Queue: queue.NewQueue(), Bus: hub, Events: evs, Registry: channels, Metrics: reg,
		Config: config.Config{Meta: config.MetaConfig{AppID: "1626301872631379"}},
	})
	split, err := webhooks.NewSplitHandler(webhooks.SplitOptions{Sources: sources, Metrics: reg})
	if err != nil {
		t.Fatal(err)
	}
	inbound, err := webhooks.InboundJobHandlers(sources, conv)
	if err != nil {
		t.Fatal(err)
	}
	return append(append([]queue.Handler{split}, evs.JobHandlers()...), inbound...)
}
