package automation_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/automation"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/inbound"
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
	appID     = "1626301872631379"
	appSecret = "0123456789abcdef0123456789abcdef"
	pageExt   = "104000000000165"
	pageToken = "EAAB-page-token-165"
	postA     = pageExt + "_900000000000001"
	postB     = pageExt + "_900000000000002"
	userA     = "7000000000000165"
)

// env: database thử, tenant A có Page Facebook (máy Meta giả) đã bật gửi thật và có token; handler vào thật của
// conversation; worker thật chạy đường vào, lệnh đọc bài và lệnh ẩn tự động.
type env struct {
	t      *testing.T
	staff  *testkit.Staff
	tenant uuid.UUID
	fg     *fakegraph.Server
	page   uuid.UUID
	user   uuid.UUID
	d      deps.Deps
	worker *queue.Worker
	seq    int
	mu     sync.Mutex
}

func newEnv(t *testing.T) *env {
	t.Helper()
	staff := testkit.OpenStaff(t)
	e := &env{t: t, staff: staff, tenant: staff.Tenant(t, "A")}
	e.fg = fakegraph.New(t, fakegraph.Options{AppID: appID, AppSecret: appSecret})
	e.fg.AddPage(fakegraph.Page{ID: pageExt, Name: "Shop thử", Token: pageToken})
	for _, p := range []string{postA, postB} {
		e.fg.AddPost(fakegraph.Post{
			ID: p, PageID: pageExt, Message: "Bài bán hàng", PermalinkURL: "https://www.facebook.com/" + p,
			Created: time.Now().Add(-time.Hour).UTC(),
		})
	}
	fb := facebook.New(facebook.Options{
		BaseURL: e.fg.URL(), StreamingBaseURL: e.fg.URL(), Version: "v26.0", AppID: appID,
		AppSecret: logging.Secret(appSecret), VerifyToken: logging.Secret(appSecret + appSecret), HTTP: e.fg.Client(),
	})
	reg, err := channel.NewRegistry(fb)
	if err != nil {
		t.Fatal(err)
	}
	kr, err := fieldcrypt.ParseKeyring(logging.Secret("k1:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))))
	if err != nil {
		t.Fatal(err)
	}
	e.page = staff.ChannelAccount(t, e.tenant, facebook.Key, pageExt, "Shop thử")
	e.user = staff.User(t, e.tenant, "quanly@elimaz.test", "active")
	staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(t.Context(), `UPDATE channel.channel_accounts SET send_mode = 'enabled'`); err != nil {
			return err //nolint:wrapcheck // test
		}
		return channel.StoreCredentials(t.Context(), tx, kr, e.page, pageToken, "page_user")
	})
	e.d = deps.Deps{
		DB: staff.App, Queue: queue.NewQueue(), Bus: &realtimetest.RecordingBus{}, Events: events.NewRegistry(),
		Metrics: metrics.NewRegistry(), Registry: reg, Keys: kr, Config: config.Config{Meta: config.MetaConfig{AppID: appID}},
	}
	e.worker = e.newWorker("w-1")
	return e
}

// newWorker dựng một worker của handler vào facebook kèm lệnh của inbound (đọc bài) và automation (ẩn tự động).
func (e *env) newWorker(id string) *queue.Worker {
	fb, _ := e.d.Registry.Get(facebook.Key)
	hs, err := webhooks.InboundJobHandlers([]webhooks.Source{fb}, []webhooks.InboundHandler{inbound.NewHandler(e.d, facebook.Key)})
	if err != nil {
		e.t.Fatal(err)
	}
	cmds := slices.Concat(inbound.Parts(e.d).Commands, automation.Parts(e.d).Commands)
	return queue.NewWorker(e.staff.App, hs, queue.WorkerOptions{
		WorkerID: id, Logger: slog.New(slog.DiscardHandler), Concurrency: 4,
		Commands: commands.NewRunner(e.staff.App, cmds),
	})
}

// setRule lưu quy tắc auto_hide của Page với chế độ mode, bài ngoại lệ except, bật/tắt enabled (đọc version hiện tại).
func (e *env) setRule(enabled bool, mode string, except ...string) {
	e.t.Helper()
	if except == nil {
		except = []string{}
	}
	cfg, err := json.Marshal(automation.HideConfig{Kind: automation.KindAutoHide, Mode: mode, ExceptPostIDs: except})
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.storeRule(automation.KindAutoHide, enabled, cfg); err != nil {
		e.t.Fatal(err)
	}
}

// storeRule lưu quy tắc loại kind của Page với cấu hình cfg (đọc version hiện tại); trả lỗi của SaveRule.
func (e *env) storeRule(kind string, enabled bool, cfg []byte) error {
	e.t.Helper()
	var saveErr error
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		cur, err := automation.LoadRule(e.t.Context(), tx, e.page, kind)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		_, _, saveErr = automation.SaveRule(e.t.Context(), tx, e.page, kind, enabled, cfg, cur.Version, e.user)
		return nil
	})
	return saveErr //nolint:wrapcheck // test
}

// setSendMode đổi chế độ gửi của Page (enabled, read_only).
func (e *env) setSendMode(mode string) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), `UPDATE channel.channel_accounts SET send_mode = $1`, mode)
		return err //nolint:wrapcheck // test
	})
}

// comment là một comment khách mới của from trên bài post, tạo lúc at; mã comment mới mỗi lần, có trên máy Meta giả.
func (e *env) comment(post, from, text string, at time.Time) channel.CommentPayload {
	e.mu.Lock()
	e.seq++
	n := e.seq
	e.mu.Unlock()
	id := post + "_" + strconv.Itoa(100000+n)
	e.fg.AddComment(fakegraph.Comment{ID: id, ParentID: post, FromID: from, FromName: "Chị Lan", Message: text, Created: at})
	return channel.CommentPayload{
		PageID: pageExt, PostID: post, CommentID: id, ParentID: post, FromID: from, FromName: "Chị Lan", Text: text,
		Verb: "add", FromPage: from == pageExt, CreatedAt: at,
	}
}

// enqueue ghi sự kiện comment p tới qua đường via (khóa chống trùng theo đường để mô phỏng ba việc khác nhau).
func (e *env) enqueue(p channel.CommentPayload, via string) {
	e.t.Helper()
	body, err := json.Marshal(p)
	if err != nil {
		e.t.Fatal(err)
	}
	ev := webhooks.InboundEvent{
		Type: channel.EventCommentChanged, DedupeKey: via + ":c:" + p.CommentID + ":" + p.Verb,
		OrderKey: facebook.Key + ":" + pageExt + ":" + p.PostID + ":" + p.FromID, OccurredAt: p.CreatedAt, Payload: body,
	}
	if err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		_, err := webhooks.EnqueueInbound(e.t.Context(), tx, queue.NewQueue(), e.tenant, facebook.Key, ev, time.Now().UTC(), via)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
}

// deliver giao comment p qua webhook rồi chạy worker tới hết việc tới hạn.
func (e *env) deliver(p channel.CommentPayload) {
	e.t.Helper()
	e.enqueue(p, webhooks.ViaWebhook)
	e.drain()
}

func (e *env) drain() {
	e.t.Helper()
	testkit.DrainDue(e.t, e.staff.Owner, e.worker)
}

// hideCalls là số lần máy Meta giả nhận POST ẩn comment id.
func (e *env) hideCalls(id string) int {
	n := 0
	for _, c := range e.fg.Calls(id) {
		if c.Method == "POST" {
			n++
		}
	}
	return n
}

// hiddenOnPage hỏi máy Meta giả (qua adapter thật) comment id đang ẩn không.
func (e *env) hiddenOnPage(id string) bool {
	e.t.Helper()
	fb, _ := e.d.Registry.Get(facebook.Key)
	want := true
	res, err := fb.Lookup(e.t.Context(), channel.ChannelAccount{ExternalID: pageExt}, channel.Credentials{AccessToken: pageToken},
		channel.LookupQuery{Purpose: channel.PurposeHide, CommentID: id, WantHidden: &want})
	if err != nil {
		e.t.Fatal(err)
	}
	return res.Status == channel.OutcomeSent
}

// run là lượt chạy (outcome) và số lệnh ẩn của comment id; không có lượt → outcome rỗng.
func (e *env) run(id string) (outcome string, rows, jobs int) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT coalesce(max(outcome), ''), count(*) FROM conversation.automation_runs
			WHERE external_ref = $1`, "hide:"+id).Scan(&outcome, &rows)
	})
	jobs = e.staff.CountIn(e.t, e.tenant, `SELECT count(*) FROM platform.jobs j JOIN conversation.messages m
		ON j.payload->>'messageId' = m.id::text WHERE j.kind = $1 AND m.external_id = $2`, automation.KindHideCommand, id)
	return outcome, rows, jobs
}

// hiddenInCRM: cờ is_hidden của comment id và số dòng lịch sử nguồn automation.
func (e *env) hiddenInCRM(id string) (hidden bool, autoRevisions int) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT m.is_hidden, (SELECT count(*) FROM conversation.message_revisions r
			WHERE r.message_id = m.id AND r.source = 'automation') FROM conversation.messages m WHERE m.external_id = $1`, id).
			Scan(&hidden, &autoRevisions)
	})
	return hidden, autoRevisions
}
