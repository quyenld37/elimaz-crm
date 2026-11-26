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

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

// Sức khỏe kênh trọn đường (kế hoạch M4, Task 150) trên máy Meta giả và adapter Facebook thật: ingest /hooks/facebook
// (chữ ký thật) → webhook.split → việc inbound.facebook; worker chạy lệnh của channel (đăng ký, kiểm sức khỏe, hỏi bù).

const (
	silAppID     = "app-150"
	silAppSecret = "0123456789abcdef0123456789abcdef"
	silPage      = "1001"
	silToken     = "EAAB-e2e-150"
)

type silEnv struct {
	t      *testing.T
	staff  *testkit.Staff
	tenant uuid.UUID
	fg     *fakegraph.Server
	kr     *fieldcrypt.Keyring
	clock  time.Time
	jobs   []queue.Handler
	worker *queue.Worker
	ingest *httptest.Server
}

func newSilEnv(t *testing.T) *silEnv {
	t.Helper()
	e := &silEnv{t: t, staff: testkit.OpenStaff(t)}
	e.tenant = e.staff.Tenant(t, "A")
	e.fg = fakegraph.New(t, fakegraph.Options{AppID: silAppID, AppSecret: silAppSecret})
	e.fg.AddPage(fakegraph.Page{ID: silPage, Name: "Shop Một", Token: silToken})
	kr, err := fieldcrypt.ParseKeyring(logging.Secret("k1:MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="))
	if err != nil {
		t.Fatal(err)
	}
	e.kr = kr
	fb := facebook.New(facebook.Options{
		BaseURL: e.fg.URL(), StreamingBaseURL: e.fg.URL(), Version: "v26.0", AppID: silAppID,
		AppSecret: logging.Secret(silAppSecret), VerifyToken: logging.Secret("verify-token-verify-token-verify-tok"), HTTP: e.fg.Client(),
	})
	reg, err := channel.NewRegistry(fb)
	if err != nil {
		t.Fatal(err)
	}
	d := channel.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Bus: &realtimetest.RecordingBus{}, Registry: reg, Keys: kr,
		Now: func() time.Time { return e.clock },
	}
	e.jobs = channel.JobHandlers(d)
	sources := []webhooks.Source{fb}
	split, err := webhooks.NewSplitHandler(webhooks.SplitOptions{Sources: sources, Metrics: metrics.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	e.worker = queue.NewWorker(e.staff.App, []queue.Handler{split}, queue.WorkerOptions{
		WorkerID: "e2e-150/" + uuid.NewString(), Commands: commands.NewRunner(e.staff.App, channel.CommandHandlers(d)),
	})
	in, err := webhooks.NewIngest(webhooks.IngestOptions{
		DB: e.staff.App, Sources: sources, Spool: &webhooks.Spool{Dir: t.TempDir()},
		Lockout: webhooks.NewLockout(webhooks.LockoutOptions{}), Metrics: metrics.NewRegistry(),
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	in.Register(mux)
	e.ingest = httptest.NewServer(mux)
	t.Cleanup(e.ingest.Close)
	return e
}

func (e *silEnv) in(fn func(tx pgx.Tx) error) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, fn)
}

// inbound trả số việc inbound.facebook của shop theo đường tới (khóa "" là tổng).
func (e *silEnv) inbound() map[string]int {
	e.t.Helper()
	out := map[string]int{}
	e.in(func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), `SELECT payload->>'via', count(*)::int FROM platform.jobs
			WHERE kind = 'inbound.facebook' GROUP BY 1`)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		var via string
		var n int
		_, err = pgx.ForEachRow(rows, []any{&via, &n}, func() error {
			out[via] += n
			out[""] += n
			return nil
		})
		return err //nolint:wrapcheck // test
	})
	return out
}

func (e *silEnv) connect(token string) channel.ChannelAccount {
	e.t.Helper()
	actor := identity.AuthUser{TenantID: e.tenant, UserID: e.staff.User(e.t, e.tenant, uuid.NewString()+"@elimaz.test", "active")}
	var a channel.ChannelAccount
	e.in(func(tx pgx.Tx) error {
		var seller uuid.UUID
		if err := tx.QueryRow(e.t.Context(), `INSERT INTO catalog.sellers (tenant_id, code, name, kind)
			VALUES ($1, 'ELIMAZ', 'ELIMAZ', 'own') ON CONFLICT (tenant_id, code) DO UPDATE SET name = EXCLUDED.name
			RETURNING id`, e.tenant).Scan(&seller); err != nil {
			return err //nolint:wrapcheck // test
		}
		var err error
		a, err = channel.Writer{}.Connect(e.t.Context(), tx, e.kr, channel.ConnectInput{
			Channel: facebook.Key, PageID: silPage, Name: "Shop Một", SellerID: seller, Token: logging.Secret(token),
			TokenKind: channel.TokenPageUser,
		}, actor)
		return err //nolint:wrapcheck // test
	})
	return a
}

func (e *silEnv) schedule(name string) {
	e.t.Helper()
	for _, h := range e.jobs {
		if h.Kind().Name == "task."+name {
			e.in(func(tx pgx.Tx) error {
				_, err := h.Handle(e.t.Context(), queue.Job{ID: uuid.New(), TenantID: &e.tenant}, tx)
				return err //nolint:wrapcheck // test
			})
			return
		}
	}
	e.t.Fatalf("không có lịch %s", name)
}

func (e *silEnv) count(q string, args ...any) int {
	e.t.Helper()
	return e.staff.CountIn(e.t, e.tenant, q, args...)
}

func (e *silEnv) account(id uuid.UUID) channel.ChannelAccount {
	e.t.Helper()
	var a channel.ChannelAccount
	e.in(func(tx pgx.Tx) (err error) {
		a, err = channel.Account(e.t.Context(), tx, id)
		return err //nolint:wrapcheck // test
	})
	return a
}

// emitMessage gửi một webhook tin nhắn có chữ ký thật của máy Meta giả tới ingest.
func (e *silEnv) emitMessage(mid string) {
	e.t.Helper()
	ms := time.Now().UnixMilli()
	body := fmt.Sprintf(`{"object":"page","entry":[{"id":%q,"time":%d,"messaging":[{"sender":{"id":"7001"},`+
		`"recipient":{"id":%q},"timestamp":%d,"message":{"mid":%q,"text":"x"}}]}]}`, silPage, ms, silPage, ms, mid)
	code, err := e.fg.EmitWebhook(e.t.Context(), e.ingest.URL+"/hooks/facebook", []byte(body))
	if err != nil || code != http.StatusOK {
		e.t.Fatalf("webhook %s: %d, %v", mid, code, err)
	}
}

// HT15 trên Meta giả: token bị thu hồi → lịch sức khỏe → degraded, đúng một cảnh báo gấp channel-auth; trong lúc
// degraded 3 webhook có chữ ký → 3 việc inbound.facebook; nối lại → active; 31 phút sau không cảnh báo nhắc lại.
func TestE2E_ChannelTokenRevoked(t *testing.T) {
	t.Parallel()
	e := newSilEnv(t)
	a := e.connect(silToken)
	e.staff.DrainJobs(t, e.worker)
	if got := e.fg.Subscribed(silPage); len(got) != len(channel.WebhookFields()) {
		t.Fatalf("đăng ký ở Meta giả = %v", got)
	}
	e.clock = time.Date(2026, 11, 26, 14, 0, 0, 0, time.FixedZone("ICT", 7*3600))
	e.fg.RevokeToken(silToken)
	e.schedule(channel.HealthSchedule)
	e.staff.DrainJobs(t, e.worker)
	if got := e.account(a.ID); got.Status != channel.StatusDegraded {
		t.Fatalf("sau token thu hồi = %+v", got)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1 AND position('EAAB' IN payload::text) = 0`,
		channel.AuthAlertKind); n != 1 {
		t.Fatalf("cảnh báo mất quyền = %d, muốn 1", n)
	}
	for i := range 3 {
		e.emitMessage(fmt.Sprintf("m_deg_%d", i))
	}
	e.staff.DrainJobs(t, e.worker)
	if got := e.inbound(); got[""] != 3 {
		t.Fatalf("việc vào khi degraded = %v, muốn 3", got)
	}
	e.fg.AddPage(fakegraph.Page{ID: silPage, Name: "Shop Một", Token: silToken + "-2"})
	if r := e.connect(silToken + "-2"); r.Status != channel.StatusActive {
		t.Fatalf("nối lại = %+v", r)
	}
	e.staff.DrainJobs(t, e.worker)
	e.clock = e.clock.Add(31 * time.Minute)
	e.schedule(channel.HealthSchedule)
	e.staff.DrainJobs(t, e.worker)
	if got := e.account(a.ID); got.Status != channel.StatusActive {
		t.Fatalf("sau nối lại = %+v", got)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1`, channel.AuthAlertKind); n != 1 {
		t.Fatalf("cảnh báo nhắc lại: %d", n)
	}
}

// HT16 trên Meta giả: 20:00 giờ Việt Nam, webhook cuối cách 40 phút → một cảnh báo gấp im lặng, Meta giả nhận lại
// đăng ký đủ 11 trường, hỏi bù đọc hội thoại có 3 tin (1 tin đã vào qua webhook) → tổng 3 việc vào, 2 qua hỏi bù.
func TestE2E_ChannelSilence(t *testing.T) {
	t.Parallel()
	e := newSilEnv(t)
	a := e.connect(silToken)
	e.staff.DrainJobs(t, e.worker)
	e.emitMessage("m_1")
	e.staff.DrainJobs(t, e.worker)
	now := time.Date(2026, 11, 26, 20, 0, 0, 0, time.FixedZone("ICT", 7*3600))
	last := now.Add(-40 * time.Minute)
	for i, mid := range []string{"m_1", "m_2", "m_3"} {
		e.fg.AddInboundMessage(silPage, "7001", mid, "x", last.Add(time.Duration(i+1)*time.Minute))
	}
	e.in(func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE channel.channel_accounts SET last_webhook_at = $2 WHERE id = $1`, a.ID, last)
		return err //nolint:wrapcheck // test
	})
	subs := len(e.fg.Calls(silPage + "/subscribed_apps"))
	e.clock = now
	e.schedule(channel.SilenceSchedule)
	e.staff.DrainJobs(t, e.worker)
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1`, channel.SilenceAlertKind); n != 1 {
		t.Fatalf("cảnh báo im lặng = %d, muốn 1", n)
	}
	posts := slices.DeleteFunc(e.fg.Calls(silPage+"/subscribed_apps"), func(c fakegraph.Call) bool { return c.Method != http.MethodPost })
	if len(e.fg.Calls(silPage+"/subscribed_apps")) <= subs || len(posts) < 2 {
		t.Fatalf("không đăng ký lại: %d lời gọi POST", len(posts))
	}
	if got := e.inbound(); got[""] != 3 || got[webhooks.ViaBackfill] != 2 {
		t.Fatalf("việc vào theo đường tới = %v, muốn tổng 3, qua hỏi bù 2", got)
	}
}
