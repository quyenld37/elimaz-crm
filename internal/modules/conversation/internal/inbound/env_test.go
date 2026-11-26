package inbound_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/adapters/fake"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/inbound"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
)

const (
	// pageExt1 là mã Page của các fixture Facebook (internal/adapters/facebook/testdata).
	pageExt1 = "104000000000001"
	pageExt2 = "104000000000002"
	// ownAppID là META_APP_ID của CRM trong test (khớp fixture echo_crm.json).
	ownAppID = "1626301872631379"
	// businessSuiteAppID là app_id của Meta Business Suite (fixture echo_business_suite.json).
	businessSuiteAppID = "263902037430900"
	fakePage           = "fake-page-1"
	secret32           = "0123456789abcdef0123456789abcdef"
)

// syncBuffer là bộ đệm log dùng được từ nhiều goroutine.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p) //nolint:wrapcheck // bộ đệm trong bộ nhớ
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// env: database thử, tenant A có Page Facebook 1, 2 và Page kênh giả; handler vào thật của conversation cho hai
// nguồn facebook, fakechat; worker thật (log mức debug vào logs); bus ghi lại báo hiệu.
type env struct {
	t            *testing.T
	staff        *testkit.Staff
	tenant       uuid.UUID
	page1, page2 uuid.UUID
	fakePage     uuid.UUID
	bus          *realtimetest.RecordingBus
	reg          *metrics.Registry
	logs         *syncBuffer
	handlers     []queue.Handler
	worker       *queue.Worker
}

func newEnv(t *testing.T) *env {
	t.Helper()
	staff := testkit.OpenStaff(t)
	e := &env{t: t, staff: staff, tenant: staff.Tenant(t, "A"), bus: &realtimetest.RecordingBus{}, reg: metrics.NewRegistry(), logs: &syncBuffer{}}
	e.page1 = staff.ChannelAccount(t, e.tenant, facebook.Key, pageExt1, "Page 1")
	e.page2 = staff.ChannelAccount(t, e.tenant, facebook.Key, pageExt2, "Page 2")
	e.fakePage = staff.ChannelAccount(t, e.tenant, fake.Key, fakePage, "Page giả")
	d := deps.Deps{
		DB: staff.App, Queue: queue.NewQueue(), Bus: e.bus, Events: events.NewRegistry(), Metrics: e.reg,
		Config: config.Config{Meta: config.MetaConfig{AppID: ownAppID}},
	}
	sources := []webhooks.Source{facebook.New(facebook.Options{AppSecret: logging.Secret(secret32), VerifyToken: logging.Secret(secret32 + secret32)}), fake.New(secret32)}
	hs, err := webhooks.InboundJobHandlers(sources, []webhooks.InboundHandler{inbound.NewHandler(d, facebook.Key), inbound.NewHandler(d, fake.Key)})
	if err != nil {
		t.Fatal(err)
	}
	e.handlers = hs
	e.worker = e.newWorker("w-1")
	return e
}

// newWorker dựng một worker của các handler vào, log mức debug vào e.logs.
func (e *env) newWorker(id string) *queue.Worker {
	logger := slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return queue.NewWorker(e.staff.App, e.handlers, queue.WorkerOptions{WorkerID: id, Logger: logger, Metrics: e.reg, Concurrency: 4})
}

// at là mốc thử cố định (giờ Meta), cộng thêm sec giây.
func at(sec int) time.Time {
	return time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC).Add(time.Duration(sec) * time.Second)
}

// enqueue ghi một sự kiện vào của nguồn source như webhook đã tách (khóa chống trùng dedupe, thứ tự theo Page + người).
func (e *env) enqueue(source, typ, dedupe, page, user string, occurred time.Time, payload any) {
	e.t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		e.t.Fatal(err)
	}
	ev := webhooks.InboundEvent{
		Type: typ, DedupeKey: dedupe, OrderKey: source + ":" + page + ":" + user, OccurredAt: occurred, Payload: body,
	}
	if err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		_, err := webhooks.EnqueueInbound(e.t.Context(), tx, queue.NewQueue(), e.tenant, source, ev, time.Now().UTC(), webhooks.ViaWebhook)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
}

// deliver ghi sự kiện facebook rồi chạy worker tới hết việc tới hạn.
func (e *env) deliver(typ, dedupe, user string, occurred time.Time, payload any) {
	e.t.Helper()
	e.enqueue(facebook.Key, typ, dedupe, pageExt1, user, occurred, payload)
	e.drain()
}

func (e *env) drain() {
	e.t.Helper()
	testkit.DrainDue(e.t, e.staff.Owner, e.worker)
}

// message là tin khách user gửi Page 1 lúc sec.
func message(user, mid, text string, sec int) channel.MessagePayload {
	return channel.MessagePayload{PageID: pageExt1, UserID: user, MID: mid, Text: text, SentAt: at(sec)}
}

// receive giao tin khách (khóa chống trùng theo mid).
func (e *env) receive(user, mid, text string, sec int) {
	e.t.Helper()
	e.receiveAt(user, mid, text, at(sec))
}

// receiveAt giao tin khách lúc when.
func (e *env) receiveAt(user, mid, text string, when time.Time) {
	e.t.Helper()
	p := channel.MessagePayload{PageID: pageExt1, UserID: user, MID: mid, Text: text, SentAt: when}
	e.deliver(channel.EventMessageReceived, "m:"+mid, user, when, p)
}

// fixtureEvents tách fixture Facebook name bằng adapter facebook thật.
func fixtureEvents(t *testing.T, name string) []webhooks.InboundEvent {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "adapters", "facebook", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	res, err := facebook.New(facebook.Options{AppSecret: logging.Secret(secret32), VerifyToken: logging.Secret(secret32 + secret32)}).Split(raw)
	if err != nil {
		t.Fatal(err)
	}
	return res.Events
}

// enqueueEvent ghi một sự kiện đã tách của nguồn facebook.
func (e *env) enqueueEvent(ev webhooks.InboundEvent) {
	e.t.Helper()
	if err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		_, err := webhooks.EnqueueInbound(e.t.Context(), tx, queue.NewQueue(), e.tenant, facebook.Key, ev, time.Now().UTC(), webhooks.ViaWebhook)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
}

// conv là trạng thái một hội thoại đọc lại bằng chủ bảng.
type conv struct {
	ID                          uuid.UUID
	CustomerID                  uuid.UUID
	Kind, Status                string
	Unread                      int
	Snippet                     *string
	LastMessageAt               time.Time
	LastCustomerAt, LastStaffAt *time.Time
	Version                     int
}

// conversation đọc hội thoại inbox của PSID user trên Page account.
func (e *env) conversation(account uuid.UUID, user string) conv {
	e.t.Helper()
	var c conv
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT id, customer_id, kind, status, unread_count, last_snippet, last_message_at,
			last_customer_message_at, last_staff_message_at, version FROM conversation.conversations
			WHERE channel_account_id = $1 AND kind = 'inbox' AND external_thread_ref = $2`, account, user).
			Scan(&c.ID, &c.CustomerID, &c.Kind, &c.Status, &c.Unread, &c.Snippet, &c.LastMessageAt, &c.LastCustomerAt,
				&c.LastStaffAt, &c.Version)
	})
	return c
}

// msg là một tin đọc lại bằng chủ bảng.
type msg struct {
	ID                            uuid.UUID
	Direction, Kind, Origin       string
	Body, SendStatus, ExternalID  *string
	SentAt                        time.Time
	DeliveredAt, ReadAt, EditedAt *time.Time
	Reactions                     []byte
}

// messages đọc mọi tin của hội thoại id theo sent_at, id.
func (e *env) messages(id uuid.UUID) []msg {
	e.t.Helper()
	var out []msg
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), `SELECT id, direction, kind, origin, body, send_status, external_id, sent_at,
			delivered_at, read_at, edited_at, reactions FROM conversation.messages WHERE conversation_id = $1 ORDER BY sent_at, id`, id)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		defer rows.Close()
		for rows.Next() {
			var m msg
			if err := rows.Scan(&m.ID, &m.Direction, &m.Kind, &m.Origin, &m.Body, &m.SendStatus, &m.ExternalID, &m.SentAt,
				&m.DeliveredAt, &m.ReadAt, &m.EditedAt, &m.Reactions); err != nil {
				return err //nolint:wrapcheck // test
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	return out
}

// noDeadJobs đòi không việc nào dead.
func (e *env) noDeadJobs() {
	e.t.Helper()
	if n := e.staff.Count(e.t, "SELECT count(*) FROM platform.jobs WHERE status = 'dead'"); n != 0 {
		e.t.Fatalf("%d việc dead", n)
	}
}

// changedSignals trả báo hiệu conversation.changed đã phát cho hội thoại id.
func (e *env) changedSignals(id uuid.UUID) []realtime.Signal {
	var out []realtime.Signal
	for _, s := range e.bus.Signals() {
		if s.Topic == deps.TopicConversationChanged && s.Entity.ID == id.String() {
			out = append(out, s)
		}
	}
	return out
}

// deref trả chuỗi p trỏ tới, rỗng khi nil.
func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// texts chạy câu q (một cột chữ) bằng chủ bảng trong ngữ cảnh tenant A; trả mọi giá trị.
func (e *env) texts(q string, args ...any) []string {
	e.t.Helper()
	var out []string
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), q, args...)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		defer rows.Close()
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				return err //nolint:wrapcheck // test
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out
}
