package history_test

import (
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
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
	"elimaz.com.vn/crm/internal/testkit/fbtest"
)

const (
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
	fbe := fbtest.New(t, fbtest.Options{PageID: pageExt, PageToken: pageToken})
	e := &env{
		t: t, staff: fbe.Staff, tenant: fbe.Tenant, page: fbe.Page, fg: fbe.Graph, fb: fbe.Channel,
		bus: &realtimetest.RecordingBus{},
	}
	// Hồ sơ của khách (Task 155: khách mới → lệnh command.fb.fetch_profile đọc tên từ máy Meta giả).
	e.fg.AddProfile(fakegraph.Profile{PSID: psid, FirstName: "Khách", LastName: "Thử"})
	e.d = deps.Deps{
		DB: fbe.Staff.App, Queue: queue.NewQueue(), Bus: e.bus, Events: events.NewRegistry(), Metrics: metrics.NewRegistry(),
		Registry: fbe.Registry, Keys: fbe.Keys,
		Config: config.Config{Meta: config.MetaConfig{AppID: fbtest.AppID}, ConversationRetentionMonths: 24},
	}
	hs, err := webhooks.InboundJobHandlers([]webhooks.Source{e.fb}, []webhooks.InboundHandler{inbound.NewHandler(e.d, facebook.Key)})
	if err != nil {
		t.Fatal(err)
	}
	runner := commands.NewRunner(e.staff.App, slices.Concat(inbound.Parts(e.d).Commands, history.Parts(e.d).Commands))
	e.worker = queue.NewWorker(e.staff.App, hs, queue.WorkerOptions{
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
