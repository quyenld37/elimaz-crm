package inbound_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/fake"
	"elimaz.com.vn/crm/internal/imager"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/inbound"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
)

// truncatedMarker mở đầu nội dung "JPEG cụt": imager giả trả decode_failed như imager thật với truncated.jpg.
const truncatedMarker = "\xFF\xD8\xFF\xE0TRUNC"

// countingImager là imager giả của M1 (testkit.StubImager) có đếm lời gọi; nguồn mở đầu truncatedMarker → rejected
// decode_failed.
type countingImager struct {
	mu    sync.Mutex
	calls int
}

func (c *countingImager) Process(ctx context.Context, req imager.Request) (imager.Result, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	if c.truncated(ctx, req.SourceURL) {
		return imager.Result{Status: imager.StatusRejected, Reason: imager.ReasonDecodeFailed}, nil
	}
	return testkit.StubImager{}.Process(ctx, req) //nolint:wrapcheck // imager giả
}

// truncated đọc vài byte đầu của nguồn.
func (*countingImager) truncated(ctx context.Context, u string) bool {
	body, err := httpGet(ctx, u)
	return err == nil && bytes.HasPrefix(body, []byte(truncatedMarker))
}

func (c *countingImager) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// fileEnv: hai tenant A, B, mỗi tenant một Page kênh giả có token; kênh giả làm CDN và hồ sơ; kho tệp trong bộ nhớ;
// worker thật chạy đường vào, lệnh tải tệp, đọc hồ sơ (conversation) và file.process_private (M1, imager giả).
type fileEnv struct {
	t              *testing.T
	staff          *testkit.Staff
	tenant, tenB   uuid.UUID
	page, pageB    uuid.UUID
	chat           *fake.Chat
	store          *files.MemStore
	img            *countingImager
	bus            *realtimetest.RecordingBus
	worker         *queue.Worker
	fetchOnly      *queue.Worker // chỉ đường vào và lệnh của conversation (ảnh nằm chờ file.process_private)
	reader, outsdr identity.AuthUser
}

// testKeyring là FIELD_ENC_KEYS của test (khóa k1 = 32 byte 0x07).
const testKeyring logging.Secret = "k1:BwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwc="

const (
	fakePageA = "fake-page-155a"
	fakePageB = "fake-page-155b"
)

func newFileEnv(t *testing.T) *fileEnv {
	t.Helper()
	conversation.RegisterFilePurposes()
	s := testkit.OpenStaff(t)
	e := &fileEnv{
		t: t, staff: s, tenant: s.Tenant(t, "A"), tenB: s.Tenant(t, "B"), chat: fake.New(secret32),
		store: filestest.MemFileStore(t), img: &countingImager{}, bus: &realtimetest.RecordingBus{},
	}
	e.page = s.ChannelAccount(t, e.tenant, fake.Key, fakePageA, "Page A")
	e.pageB = s.ChannelAccount(t, e.tenB, fake.Key, fakePageB, "Page B")
	other := s.ChannelAccount(t, e.tenant, fake.Key, "fake-page-155c", "Page khác")
	kr, err := fieldcrypt.ParseKeyring(testKeyring)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct{ tenant, page uuid.UUID }{{e.tenant, e.page}, {e.tenB, e.pageB}} {
		s.Exec(t, p.tenant, func(tx pgx.Tx) error {
			return channel.StoreCredentials(t.Context(), tx, kr, p.page, "tok", "page_user")
		})
	}
	reg, err := channel.NewRegistry(e.chat)
	if err != nil {
		t.Fatal(err)
	}
	d := deps.Deps{
		DB: s.App, Queue: queue.NewQueue(), Bus: e.bus, Events: events.NewRegistry(), Registry: reg, Keys: kr,
		Files: e.store, Metrics: metrics.NewRegistry(),
	}
	in, err := webhooks.InboundJobHandlers([]webhooks.Source{e.chat}, []webhooks.InboundHandler{inbound.NewHandler(d, fake.Key)})
	if err != nil {
		t.Fatal(err)
	}
	cmds := inbound.Parts(d).Commands
	cmds = append(cmds, files.NewProcessPrivateHandler(files.ProcessorOptions{
		DB: s.App, Store: e.store, Imager: e.img,
	}), files.NewProcessHandler(files.ProcessorOptions{DB: s.App, Store: e.store, Imager: e.img}))
	e.worker = queue.NewWorker(s.App, in, queue.WorkerOptions{
		WorkerID: "w-155", Logger: slog.New(slog.DiscardHandler), Concurrency: 4,
		Commands: commands.NewRunner(s.App, cmds),
	})
	e.fetchOnly = queue.NewWorker(s.App, in, queue.WorkerOptions{
		WorkerID: "w-155-fetch", Logger: slog.New(slog.DiscardHandler), Concurrency: 4,
		Commands: commands.NewRunner(s.App, inbound.Parts(d).Commands),
	})
	read := func(page uuid.UUID) identity.AuthUser {
		return identity.AuthUser{UserID: uuid.New(), TenantID: e.tenant, Grants: []identity.Grant{{
			Permission: "conversation.read", Scope: identity.Scope{Type: identity.ScopePage, ID: page.String()},
		}}}
	}
	e.reader, e.outsdr = read(e.page), read(other)
	return e
}

// receive giao tin khách user (Page của tenant) có các tệp atts rồi chạy worker tới hết việc.
func (e *fileEnv) receive(tenant uuid.UUID, user, mid string, atts ...channel.AttachmentRef) {
	e.t.Helper()
	e.enqueue(tenant, user, mid, atts...)
	e.drain()
}

// receiveNoProcess như receive ở tenant A nhưng không chạy file.process_private (ảnh còn uploaded).
func (e *fileEnv) receiveNoProcess(user, mid string, atts ...channel.AttachmentRef) {
	e.t.Helper()
	e.enqueue(e.tenant, user, mid, atts...)
	testkit.DrainDue(e.t, e.staff.Owner, e.fetchOnly)
}

func (e *fileEnv) enqueue(tenant uuid.UUID, user, mid string, atts ...channel.AttachmentRef) {
	e.t.Helper()
	page := fakePageA
	if tenant == e.tenB {
		page = fakePageB
	}
	p := channel.MessagePayload{PageID: page, UserID: user, MID: mid, SentAt: time.Now().UTC(), Attachments: atts}
	body, err := json.Marshal(p)
	if err != nil {
		e.t.Fatal(err)
	}
	ev := webhooks.InboundEvent{
		Type: channel.EventMessageReceived, DedupeKey: "m:" + mid, OrderKey: fake.Key + ":" + page + ":" + user,
		OccurredAt: p.SentAt, Payload: body,
	}
	if err := tenancy.WithTenant(e.t.Context(), e.staff.App, tenant, func(tx pgx.Tx) error {
		_, err := webhooks.EnqueueInbound(e.t.Context(), tx, queue.NewQueue(), tenant, fake.Key, ev, time.Now().UTC(), webhooks.ViaWebhook)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *fileEnv) drain() {
	e.t.Helper()
	testkit.DrainDue(e.t, e.staff.Owner, e.worker)
}

// item là một mục đính kèm đọc lại.
type item struct {
	Index        int        `json:"index"`
	Type         string     `json:"type"`
	Status       string     `json:"status"`
	FileID       *uuid.UUID `json:"fileId"`
	DownloadOnly bool       `json:"downloadOnly"`
	URL          string     `json:"url"`
}

// items đọc danh sách đính kèm và hội thoại của tin mid (tenant).
func (e *fileEnv) items(tenant uuid.UUID, mid string) ([]item, uuid.UUID) {
	e.t.Helper()
	var raw []byte
	var conv uuid.UUID
	e.staff.Exec(e.t, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT attachments, conversation_id FROM conversation.messages
			WHERE external_id = $1`, mid).Scan(&raw, &conv)
	})
	var out []item
	if err := json.Unmarshal(raw, &out); err != nil {
		e.t.Fatal(err)
	}
	return out, conv
}

// one đọc mục duy nhất của tin mid ở tenant A.
func (e *fileEnv) one(mid string) (item, uuid.UUID) {
	e.t.Helper()
	its, conv := e.items(e.tenant, mid)
	if len(its) != 1 {
		e.t.Fatalf("tin %s: %d mục", mid, len(its))
	}
	return its[0], conv
}

// link gọi conversation.AttachmentURL bằng u.
func (e *fileEnv) link(u identity.AuthUser, conv, file uuid.UUID) (conversation.AttachmentLink, error) {
	var l conversation.AttachmentLink
	err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		var err error
		l, err = conversation.AttachmentURL(e.t.Context(), tx, e.store, u, conv, file, time.Now())
		return err //nolint:wrapcheck // test
	})
	return l, err //nolint:wrapcheck // test
}

// jobs đếm việc loại kind ở trạng thái status (rỗng: mọi trạng thái).
func (e *fileEnv) jobs(kind, status string) int {
	e.t.Helper()
	return e.staff.Count(e.t, `SELECT count(*) FROM platform.jobs WHERE kind = $1 AND ($2 = '' OR status = $2)`, kind, status)
}

// storeBase là gốc đường dẫn của kho tệp trong bộ nhớ (FILES_PUBLIC_BASE_URL của test).
func (e *fileEnv) storeBase() string {
	e.t.Helper()
	u, err := e.store.PresignGet(e.t.Context(), files.BucketTmp, "x", time.Minute, "")
	if err != nil {
		e.t.Fatal(err)
	}
	base, _, _ := strings.Cut(u, "/tmp/x")
	return base
}

// texts chạy câu q (một cột chữ) bằng chủ bảng trong ngữ cảnh tenant.
func (e *fileEnv) texts(tenant uuid.UUID, q string, args ...any) []string {
	e.t.Helper()
	return queryTexts(e.t, e.staff, tenant, q, args...)
}
