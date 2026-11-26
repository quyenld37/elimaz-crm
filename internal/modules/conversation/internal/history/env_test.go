package history_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/history"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/inbound"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

const (
	appID     = "1626301872631379"
	appSecret = "0123456789abcdef0123456789abcdef"
	pageExt   = "104000000000164"
	pageToken = "EAAB-page-token-164"
	psid      = "7000000000000164"
)

// env: database thử, tenant A có Page Facebook (máy Meta giả, token lưu mã hóa); handler vào thật của conversation;
// worker chạy cả lệnh của inbound và history; bus ghi lại báo hiệu.
type env struct {
	t      *testing.T
	staff  *testkit.Staff
	tenant uuid.UUID
	page   uuid.UUID
	fg     *fakegraph.Server
	fb     *facebook.Channel
	bus    *realtimetest.RecordingBus
	d      deps.Deps
	worker *queue.Worker
}

func newEnv(t *testing.T) *env {
	t.Helper()
	staff := testkit.OpenStaff(t)
	e := &env{t: t, staff: staff, tenant: staff.Tenant(t, "A"), bus: &realtimetest.RecordingBus{}}
	e.fg = fakegraph.New(t, fakegraph.Options{AppID: appID, AppSecret: appSecret})
	e.fg.AddPage(fakegraph.Page{ID: pageExt, Name: "Shop thử", Token: pageToken})
	e.fb = facebook.New(facebook.Options{
		BaseURL: e.fg.URL(), StreamingBaseURL: e.fg.URL(), Version: "v26.0", AppID: appID,
		AppSecret: logging.Secret(appSecret), VerifyToken: logging.Secret(appSecret + appSecret), HTTP: e.fg.Client(),
	})
	reg, err := channel.NewRegistry(e.fb)
	if err != nil {
		t.Fatal(err)
	}
	kr, err := fieldcrypt.ParseKeyring(logging.Secret("k1:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))))
	if err != nil {
		t.Fatal(err)
	}
	e.page = staff.ChannelAccount(t, e.tenant, facebook.Key, pageExt, "Shop thử")
	staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return channel.StoreCredentials(t.Context(), tx, kr, e.page, logging.Secret(pageToken), "page_user")
	})
	e.d = deps.Deps{
		DB: staff.App, Queue: queue.NewQueue(), Bus: e.bus, Events: events.NewRegistry(), Metrics: metrics.NewRegistry(),
		Registry: reg, Keys: kr, Config: config.Config{Meta: config.MetaConfig{AppID: appID}, ConversationRetentionMonths: 24},
	}
	hs, err := webhooks.InboundJobHandlers([]webhooks.Source{e.fb}, []webhooks.InboundHandler{inbound.NewHandler(e.d, facebook.Key)})
	if err != nil {
		t.Fatal(err)
	}
	runner := commands.NewRunner(staff.App, slices.Concat(inbound.Parts(e.d).Commands, history.Parts(e.d).Commands))
	e.worker = queue.NewWorker(staff.App, hs, queue.WorkerOptions{
		WorkerID: "w-164", Logger: slog.New(slog.DiscardHandler), Metrics: e.d.Metrics, Concurrency: 4, Commands: runner,
	})
	return e
}

// receive giao tin khách mid (webhook) lúc at rồi chạy worker tới hết việc tới hạn. Sự kiện ghi bằng chủ bảng trong
// ngữ cảnh tenant A, đúng khóa chống trùng, khóa thứ tự như adapter facebook tách từ webhook.
func (e *env) receive(mid, text string, at time.Time) {
	e.t.Helper()
	body, err := json.Marshal(channel.MessagePayload{PageID: pageExt, UserID: psid, MID: mid, Text: text, SentAt: at})
	if err != nil {
		e.t.Fatal(err)
	}
	ev := webhooks.InboundEvent{Type: channel.EventMessageReceived, DedupeKey: "m:" + mid, OccurredAt: at, Payload: body}
	ev.OrderKey = "fb:" + pageExt + ":" + psid
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		created, err := webhooks.EnqueueInbound(e.t.Context(), tx, e.d.Queue, e.tenant, facebook.Key, ev, at, webhooks.ViaWebhook)
		if err == nil && !created {
			e.t.Errorf("sự kiện %s đã có", mid)
		}
		return err //nolint:wrapcheck // test
	})
	testkit.DrainDue(e.t, e.staff.Owner, e.worker)
}

// conversationID trả mã hội thoại hộp thư của psid trên Page.
func (e *env) conversationID() uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT id FROM conversation.conversations
			WHERE channel_account_id = $1 AND kind = 'inbox' AND external_thread_ref = $2`, e.page, psid).Scan(&id)
	})
	return id
}

// count đếm bằng chủ bảng trong ngữ cảnh tenant A.
func (e *env) count(q string, args ...any) int {
	e.t.Helper()
	return e.staff.CountIn(e.t, e.tenant, q, args...)
}
