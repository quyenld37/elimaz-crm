package send_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"log/slog"
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
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

const (
	appID      = "1626301872631379"
	appSecret  = "0123456789abcdef0123456789abcdef"
	fbPageExt  = "104000000000009"
	fbToken    = "EAAB-page-token-156"
	fakePageEx = "fake-page-156"
	fakeToken  = "fake-token-156"
	psid       = "7000000000000001"
	filesBase  = "https://files.test"
	textHello  = "Dạ shop chào chị, mẫu này còn size M ạ"
)

// clock là đồng hồ của lệnh gửi và API trong test: mặc định giờ thật; Freeze giữ đứng một mốc (thùng lượt gửi).
type clock struct {
	mu     sync.Mutex
	frozen *time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.frozen != nil {
		return *c.frozen
	}
	return time.Now().UTC()
}

func (c *clock) Freeze(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.frozen = &t
}

// env: database thử, tenant A có Page Facebook (máy Meta giả) và Page kênh giả, cả hai đã bật gửi thật và có token;
// mỗi Page một hội thoại hộp thư với khách psid (tin cuối của khách 1 giờ trước); nhân viên có conversation.reply trên
// hai Page; worker thật chạy lệnh gửi và đường vào kênh giả (echo).
type env struct {
	t                *testing.T
	staff            *testkit.Staff
	tenant           uuid.UUID
	fg               *fakegraph.Server
	chat             *fake.Chat
	fbPage, fakePage uuid.UUID
	fbConv, fakeConv uuid.UUID
	bus              *realtimetest.RecordingBus
	reg              *metrics.Registry
	clock            *clock
	d                deps.Deps
	user             identity.AuthUser
	worker           *queue.Worker
	// inboundOnly chạy riêng đường vào (echo) mà không chạy lệnh gửi: echo tới trước khi lệnh chạy.
	inboundOnly *queue.Worker
}

func newEnv(t *testing.T) *env {
	t.Helper()
	staff := testkit.OpenStaff(t)
	e := &env{t: t, staff: staff, clock: &clock{}, chat: fake.New(appSecret)}
	e.tenant, e.reg, e.bus = staff.Tenant(t, "A"), metrics.NewRegistry(), &realtimetest.RecordingBus{}
	e.fg = fakegraph.New(t, fakegraph.Options{AppID: appID, AppSecret: appSecret})
	e.fg.AddPage(fakegraph.Page{ID: fbPageExt, Name: "Shop thử", Token: fbToken})
	fb := facebook.New(facebook.Options{
		BaseURL: e.fg.URL(), StreamingBaseURL: e.fg.URL(), Version: "v26.0", AppID: appID,
		AppSecret: logging.Secret(appSecret), HTTP: e.fg.Client(),
	})
	reg, err := channel.NewRegistry(fb, e.chat)
	if err != nil {
		t.Fatal(err)
	}
	kr, err := fieldcrypt.ParseKeyring(logging.Secret("k1:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))))
	if err != nil {
		t.Fatal(err)
	}
	e.fbPage = staff.ChannelAccount(t, e.tenant, facebook.Key, fbPageExt, "Shop thử")
	e.fakePage = staff.ChannelAccount(t, e.tenant, fake.Key, fakePageEx, "Page giả")
	ctx := t.Context()
	staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE channel.channel_accounts SET send_mode = 'enabled'`); err != nil {
			return err //nolint:wrapcheck // test
		}
		if err := channel.StoreCredentials(ctx, tx, kr, e.fbPage, fbToken, "page_user"); err != nil {
			return err //nolint:wrapcheck // test
		}
		return channel.StoreCredentials(ctx, tx, kr, e.fakePage, fakeToken, "page_user")
	})
	e.fbConv = e.conversation(e.fbPage, psid, time.Now().Add(-time.Hour))
	e.fakeConv = e.conversation(e.fakePage, psid, time.Now().Add(-time.Hour))
	uid := staff.User(t, e.tenant, "sale@elimaz.test", "active")
	e.user = identity.AuthUser{UserID: uid, TenantID: e.tenant, Grants: []identity.Grant{
		{Permission: "conversation.reply", Scope: identity.Scope{Type: identity.ScopePage, ID: e.fbPage.String()}},
		{Permission: "conversation.reply", Scope: identity.Scope{Type: identity.ScopePage, ID: e.fakePage.String()}},
	}}
	cfg := config.Config{Meta: config.MetaConfig{AppID: appID}, FilesPublicBaseURL: filesBase}
	e.d = deps.Deps{Registry: reg, Keys: kr, Now: e.clock.Now, Config: cfg, DB: staff.App, Bus: e.bus, Metrics: e.reg}
	e.d.Queue, e.d.Events = queue.NewQueue(), events.NewRegistry()
	e.worker = e.newWorker(true)
	e.inboundOnly = e.newWorker(false)
	return e
}

// newWorker dựng worker của đường vào kênh giả, kèm lệnh gửi khi withSend.
func (e *env) newWorker(withSend bool) *queue.Worker {
	in, err := webhooks.InboundJobHandlers([]webhooks.Source{e.chat}, []webhooks.InboundHandler{inbound.NewHandler(e.d, fake.Key)})
	if err != nil {
		e.t.Fatal(err)
	}
	o := queue.WorkerOptions{WorkerID: "w-in", Logger: slog.New(slog.DiscardHandler), Metrics: e.reg, Concurrency: 4}
	if withSend {
		o.WorkerID, o.Commands = "w-send", commands.NewRunner(e.staff.App, send.Parts(e.d).Commands)
	}
	return queue.NewWorker(e.staff.App, in, o)
}

// conversation chèn (chủ bảng) khách và hội thoại hộp thư với khách user trên Page account, tin cuối của khách lúc
// lastIn.
func (e *env) conversation(account uuid.UUID, user string, lastIn time.Time) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	ctx := e.t.Context()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		var cust uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO customer.customers (tenant_id, full_name, name_search)
			VALUES (current_setting('app.tenant_id')::uuid, 'Chị Lan', 'chi lan') RETURNING id`).Scan(&cust); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(ctx, `INSERT INTO conversation.conversations (tenant_id, channel_account_id, customer_id, kind,
			external_thread_ref, last_message_at, last_customer_message_at)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'inbox', $3, $4, $4) RETURNING id`,
			account, cust, user, lastIn.UTC()).Scan(&id)
	})
	return id
}

// exec sửa dữ liệu thử (chủ bảng) bằng câu sql trong tenant A.
func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	run := func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), sql, args...)
		return err //nolint:wrapcheck // test
	}
	e.staff.Exec(e.t, e.tenant, run)
}

func (e *env) env() send.Env { return send.Env{Registry: e.d.Registry, Queue: e.d.Queue, Bus: e.bus} }

// inTx chạy fn trong một transaction tenant A của crm_app và trả kết quả của nó.
func inTx[T any](e *env, fn func(tx pgx.Tx) (T, error)) (T, error) { //nolint:ireturn // T là kiểu cụ thể của bên gọi
	e.t.Helper()
	var out T
	err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		var err error
		out, err = fn(tx)
		return err
	})
	return out, err //nolint:wrapcheck // test
}

// send gửi in vào hội thoại conv bằng nhân viên của env, trong một transaction tenant.
func (e *env) send(conv uuid.UUID, in send.Input) (send.Message, error) {
	e.t.Helper()
	return inTx(e, func(tx pgx.Tx) (send.Message, error) {
		return send.Send(e.t.Context(), tx, e.env(), e.user, conv, in, e.clock.Now())
	})
}

// resolve tra lại mọi lệnh in_doubt của send, đòi đúng want lệnh ra done.
func (e *env) resolve(want int) {
	e.t.Helper()
	done, _, _, err := commands.ResolveInDoubt(e.t.Context(), e.staff.App, send.Parts(e.d).Commands, 10)
	if err != nil || done != want {
		e.t.Fatalf("ResolveInDoubt = %d, %v; muốn %d", done, err, want)
	}
}

// mustSend như send, lỗi thì dừng.
func (e *env) mustSend(conv uuid.UUID, in send.Input) send.Message {
	e.t.Helper()
	m, err := e.send(conv, in)
	if err != nil {
		e.t.Fatalf("Send: %v", err)
	}
	return m
}

func (e *env) drain() {
	e.t.Helper()
	testkit.DrainDue(e.t, e.staff.Owner, e.worker)
}

// msg là một dòng tin đọc lại bằng chủ bảng.
type msg struct {
	Status, Origin, FailReason, ExternalID string
	CheckingSince                          *time.Time
	DeliveredAt                            *time.Time
}

func (e *env) message(id uuid.UUID) msg {
	e.t.Helper()
	var m msg
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT coalesce(send_status, ''), origin, coalesce(fail_reason, ''),
			coalesce(external_id, ''), send_checking_since, delivered_at FROM conversation.messages WHERE id = $1`, id).
			Scan(&m.Status, &m.Origin, &m.FailReason, &m.ExternalID, &m.CheckingSince, &m.DeliveredAt)
	})
	return m
}

// job đọc trạng thái, payload, lỗi cuối, run_at − now() (giây) của lệnh gửi tin của tin id.
func (e *env) job(id uuid.UUID) (status string, payload map[string]any, lastError string, inSec float64) {
	e.t.Helper()
	var raw []byte
	if err := e.staff.Owner.Pool.QueryRow(e.t.Context(), `SELECT status, payload, coalesce(last_error, ''),
		extract(epoch FROM run_at - now())::float8 FROM platform.jobs WHERE kind = $1 AND payload->>'messageId' = $2`,
		send.KindSendMessage, id.String()).Scan(&status, &raw, &lastError, &inSec); err != nil {
		e.t.Fatalf("đọc lệnh gửi: %v", err)
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		e.t.Fatal(err)
	}
	return status, payload, lastError, inSec
}

// fbCalls là các lần máy Meta giả nhận POST {page}/messages.
func (e *env) fbCalls() []fakegraph.Call { return e.fg.Calls(fbPageExt + "/messages") }

// body là thân JSON lần gửi i tới máy Meta giả.
func (e *env) body(i int) map[string]any {
	e.t.Helper()
	calls := e.fbCalls()
	if len(calls) <= i {
		e.t.Fatalf("máy Meta giả nhận %d lần gửi, muốn lần %d", len(calls), i)
	}
	var b map[string]any
	if err := json.Unmarshal(calls[i].Body, &b); err != nil {
		e.t.Fatal(err)
	}
	return b
}

// signals đếm báo hiệu conversation.changed của hội thoại conv.
func (e *env) signals(conv uuid.UUID) int {
	n := 0
	for _, s := range e.bus.Signals() {
		if s.Topic == "conversation.changed" && s.Entity.ID == conv.String() {
			n++
		}
	}
	return n
}

// echo giao một echo của kênh giả (Page gửi khách psid) với mã mid, app id echoApp, metadata meta; chạy riêng đường
// vào (lệnh gửi đang chờ không chạy).
func (e *env) echo(mid, echoApp, meta string) {
	e.t.Helper()
	p := channel.MessagePayload{
		PageID: fakePageEx, UserID: psid, MID: mid, Text: "echo", IsEcho: true, EchoAppID: echoApp, Metadata: meta,
		SentAt: time.Now().UTC(),
	}
	body, err := json.Marshal(p)
	if err != nil {
		e.t.Fatal(err)
	}
	ev := webhooks.InboundEvent{
		Type: channel.EventMessageEcho, DedupeKey: "echo:" + mid, OrderKey: fake.Key + ":" + fakePageEx + ":" + psid,
		OccurredAt: p.SentAt, Payload: body,
	}
	if err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		_, err := webhooks.EnqueueInbound(e.t.Context(), tx, queue.NewQueue(), e.tenant, fake.Key, ev, time.Now().UTC(), webhooks.ViaWebhook)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	testkit.DrainDue(e.t, e.staff.Owner, e.inboundOnly)
}

// rowsWithMID đếm dòng tin của Page account mang mã ngoài mid.
func (e *env) rowsWithMID(account uuid.UUID, mid string) int {
	return e.staff.CountIn(e.t, e.tenant, `SELECT count(*) FROM conversation.messages WHERE channel_account_id = $1 AND external_id = $2`, account, mid)
}
