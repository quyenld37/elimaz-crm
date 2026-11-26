package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
	"elimaz.com.vn/crm/internal/testkit/livetest"
)

const (
	liveFlowAppID = "1626301872631379"
	liveFlowPage  = "104000000000173"
	liveFlowToken = "EAAB-page-token-173"
	liveFlowVideo = "lv-e2e-173"
	liveFlowPSID  = "6100000000173999"
	liveFlowAUD   = "aud-live-flow"
	liveFlowStaff = "live-e2e@elimaz.test"
	liveFlowAddr  = `{"name":"Chị Lan","phone":"0901234567","line":"12 Lê Lợi","provinceCode":"T01","wardCode":"T01001"}`
)

// liveFlow là đường trọn của Task 173 (LV04) trong tiến trình test: api (httpapi thật, Page Facebook trỏ máy Meta giả),
// ingest (nguồn facebook thật, kiểm chữ ký), worker (tách gói, handler vào, lệnh gửi của conversation) trên PostgreSQL
// thật đã migrate.
type liveFlow struct {
	t       *testing.T
	staff   *testkit.Staff
	tenant  uuid.UUID
	shop    *invtest.Shop
	page    uuid.UUID
	session uuid.UUID
	fg      *fakegraph.Server
	api     *httptest.Server
	ingest  *httptest.Server
	token   string
	n       int
}

func newLiveFlow(t *testing.T) *liveFlow {
	t.Helper()
	_, ownerURL, appURL := testkit.StartPostgres(t)
	owner, app := openPool(t, ownerURL, false), openPool(t, appURL, false)
	f := &liveFlow{t: t, staff: &testkit.Staff{App: app, Owner: owner}}
	ctx := t.Context()
	tenant, err := tenancy.SeedTenant(ctx, owner, "LIVE")
	if err != nil {
		t.Fatal(err)
	}
	f.tenant = tenant
	f.staff.SeedAdminUnits(t)
	f.staff.Exec(t, tenant, func(tx pgx.Tx) error {
		f.shop = invtest.NewShop(t, tx, tenant)
		return order.EnsureDefaults(ctx, tx, tenant)
	})
	f.fg = fakegraph.New(t, fakegraph.Options{AppID: liveFlowAppID, AppSecret: secret32})
	f.fg.AddPage(fakegraph.Page{ID: liveFlowPage, Name: "Shop thử", Token: liveFlowToken})
	fbOpts := facebook.Options{
		AppID: liveFlowAppID, AppSecret: logging.Secret(secret32), VerifyToken: logging.Secret(secret32 + secret32),
		Version: "v26.0", HTTP: f.fg.Client(),
	}
	fbOpts.BaseURL, fbOpts.StreamingBaseURL = f.fg.URL(), f.fg.URL()
	fb := facebook.New(fbOpts)
	channels, err := channel.NewRegistry(fb)
	if err != nil {
		t.Fatal(err)
	}
	kr := livetest.Keyring(t)
	f.page = f.staff.ChannelAccount(t, tenant, facebook.Key, liveFlowPage, "Shop thử")
	if err := tenancy.AddRoute(ctx, owner, facebook.RouteKind, liveFlowPage, tenant); err != nil {
		t.Fatal(err)
	}
	livetest.EnableSend(t, f.staff, tenant, f.page, kr, liveFlowToken)
	f.seedPage()
	f.session = livetest.LiveSession(t, f.staff, tenant, f.page, liveFlowVideo, time.Now().Add(-time.Hour))

	keys := cfaccesstest.AccessKeyServer(t)
	u := f.staff.User(t, tenant, liveFlowStaff, "active")
	f.staff.Grant(t, tenant, u, []string{"live.operate", "conversation.read", "conversation.reply"}, identity.ScopePage, f.page.String())
	f.staff.Grant(t, tenant, u, []string{"order.create", "order.read", "order.update", "order.confirm"}, "all", "")
	f.token = keys.Sign(cfaccesstest.AccessClaims{Email: liveFlowStaff, AUD: liveFlowAUD, MFA: []string{"mfa"}})

	runCtx, logger, reg, evs, hub := startHub(t, appURL)
	f.api = httptest.NewServer(httpapi.NewServer(httpapi.Deps{
		Config: config.Config{ConsoleOrigins: []string{stackOrigin}}, DB: app, Logger: logger, Events: evs,
		Access: keys.Verifier(t, liveFlowAUD), Realtime: hub, Channels: channels, ChannelKeys: kr,
	}))
	sources := []webhooks.Source{fb}
	f.ingest = ingestServer(t, webhooks.IngestOptions{DB: app, Sources: sources, Spool: &webhooks.Spool{Dir: t.TempDir()}, Metrics: reg, Logger: logger})

	d := conversation.Deps{
		DB: app, Queue: queue.NewQueue(), Bus: hub, Events: evs, Registry: channels, Keys: kr, Metrics: reg,
		Config: config.Config{Meta: config.MetaConfig{AppID: liveFlowAppID}},
	}
	split, err := webhooks.NewSplitHandler(webhooks.SplitOptions{Sources: sources, Metrics: reg})
	if err != nil {
		t.Fatal(err)
	}
	inbound, err := webhooks.InboundJobHandlers(sources, conversation.InboundHandlers(d))
	if err != nil {
		t.Fatal(err)
	}
	handlers := append(append([]queue.Handler{split}, evs.JobHandlers()...), inbound...)
	w := queue.NewWorker(app, handlers, queue.WorkerOptions{
		WorkerID: "e2e-live", Metrics: reg, Logger: logger, Commands: commands.NewRunner(app, conversation.CommandHandlers(d)),
	})
	if err := w.Start(runCtx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = w.Stop(5 * time.Second)
		f.ingest.Close()
		f.api.Close()
	})
	return f
}

// startHub dựng logger bỏ log, sổ đo, sổ sự kiện và hub LISTEN thật trên appURL chạy tới khi test xong; ctx là ngữ
// cảnh của các vai trò chạy nền. Hub dừng sau mọi t.Cleanup đăng ký sau lời gọi này (worker, máy chủ dừng trước).
func startHub(t *testing.T, appURL string) (context.Context, *slog.Logger, *metrics.Registry, *events.Registry, *realtime.PGHub) {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	reg := metrics.NewRegistry()
	evs := events.NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	hub := realtime.NewPGHub(realtime.PGHubOptions{URL: appURL, Metrics: reg, Logger: logger})
	wg.Go(func() { hub.Run(ctx) })
	t.Cleanup(func() {
		cancel()
		wg.Wait()
	})
	return ctx, logger, reg, evs, hub
}

// seedPage: mẫu xin địa chỉ mặc định của Page.
func (f *liveFlow) seedPage() {
	f.staff.Exec(f.t, f.tenant, func(tx pgx.Tx) error {
		var tpl uuid.UUID
		if err := tx.QueryRow(f.t.Context(), `INSERT INTO conversation.reply_templates (tenant_id, shortcut, title, body)
			VALUES (current_setting('app.tenant_id')::uuid, 'xin-dia-chi', 'Xin địa chỉ',
			'Chào {ten_khach}, shop đã giữ hàng cho đơn {ma_don}. Bạn gửi giúp shop địa chỉ nhận hàng nhé') RETURNING id`).Scan(&tpl); err != nil {
			return err //nolint:wrapcheck // dữ liệu thử
		}
		_, err := tx.Exec(f.t.Context(), `INSERT INTO conversation.page_settings (tenant_id, channel_account_id, ask_address_template_id)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2)`, f.page, tpl)
		return err //nolint:wrapcheck // dữ liệu thử
	})
}

// call gửi yêu cầu tới api của nhân viên (POST, PATCH có Idempotency-Key mới); trả mã và thân JSON.
func (f *liveFlow) call(method, path, body string) (int, map[string]any) {
	f.t.Helper()
	req, err := http.NewRequestWithContext(f.t.Context(), method, f.api.URL+path, strings.NewReader(body))
	if err != nil {
		f.t.Fatal(err)
	}
	req.Header.Set("Cf-Access-Jwt-Assertion", f.token)
	req.Header.Set("Content-Type", "application/json")
	if method != http.MethodGet {
		f.n++
		req.Header.Set("Idempotency-Key", fmt.Sprintf("live-e2e-%d-%s", f.n, uuid.NewString()))
	}
	res, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	var out map[string]any
	_ = json.NewDecoder(bytes.NewReader(raw)).Decode(&out)
	return res.StatusCode, out
}

func (f *liveFlow) mustCall(method, path, body string, want int) map[string]any {
	f.t.Helper()
	code, out := f.call(method, path, body)
	if code != want {
		f.t.Fatalf("%s %s: %d %v, muốn %d", method, path, code, out, want)
	}
	return out
}

// TestLiveFlow_CommentToConfirmedOrder (LV04, đường trọn): comment live → chốt (đơn nháp giữ hàng) → tin riêng xin địa
// chỉ tới máy Meta giả → khách trả lời trong hộp thư bằng tin có địa chỉ (webhook có chữ ký qua ingest) → danh sách
// thiếu địa chỉ có customerRepliedAfterAsk = true → PATCH /orders/{id} thêm địa chỉ rồi POST /orders/{id}/confirm →
// đơn confirmed, "có thể bán" không đổi so với lúc nháp (hàng vẫn giữ), đơn biến khỏi danh sách, confirmedCount 1.
func TestLiveFlow_CommentToConfirmedOrder(t *testing.T) {
	t.Parallel()
	f := newLiveFlow(t)
	var v uuid.UUID
	f.staff.Exec(t, f.tenant, func(tx pgx.Tx) error {
		v = f.shop.Variant(t, tx, f.shop.Seller, 150_000, 5)
		return nil
	})
	cust := livetest.Customer(t, f.staff, f.tenant, "Lan")
	ext := liveFlowVideo + "_c0001"
	at := time.Now().Add(-time.Minute)
	cm := livetest.LiveComment(t, f.staff, f.tenant, f.page, cust, liveFlowVideo, liveFlowPSID, ext, "chốt 1 cái", at)
	f.fg.AddComment(fakegraph.Comment{ID: ext, ParentID: liveFlowVideo, FromID: liveFlowPSID, FromName: "Lan", Message: "chốt 1 cái", Created: at.UTC()})

	claim := f.mustCall(http.MethodPost, "/api/v1/live-comments/"+cm.ID.String()+"/claim",
		fmt.Sprintf(`{"items":[{"variantId":%q,"qty":1}],"mode":"new"}`, v), http.StatusCreated)
	if ask, _ := claim["ask"].(map[string]any); ask["status"] != "queued" {
		t.Fatalf("chốt: %v", claim)
	}
	orderID := fmt.Sprint(claim["orderId"])
	held := livetest.Available(t, f.staff, f.tenant, v)
	if held != 4 {
		t.Fatalf("có thể bán sau chốt = %d, muốn 4", held)
	}
	waitFor(t, 10*time.Second, "tin riêng xin địa chỉ tới máy Meta giả", func() bool { return livetest.PrivateReplies(f.fg, liveFlowPage, ext) == 1 })

	if status := postFacebookMessageText(t, f.ingest.URL, liveFlowPage, liveFlowPSID, "m_live_addr_1",
		"12 Lê Lợi, phường Bến Thành, TP.HCM"); status != http.StatusOK {
		t.Fatalf("POST /hooks/facebook = %d", status)
	}
	pending := "/api/v1/live-sessions/" + f.session.String() + "/pending-drafts"
	waitFor(t, 10*time.Second, "customerRepliedAfterAsk", func() bool {
		l := f.mustCall(http.MethodGet, pending, "", http.StatusOK)
		items, _ := l["items"].([]any)
		if len(items) != 1 {
			return false
		}
		row, _ := items[0].(map[string]any)
		return row["orderId"] == orderID && row["customerRepliedAfterAsk"] == true
	})

	o := f.mustCall(http.MethodGet, "/api/v1/orders/"+orderID, "", http.StatusOK)
	patched := f.mustCall(http.MethodPatch, "/api/v1/orders/"+orderID,
		fmt.Sprintf(`{"version":%v,"shipAddress":%s}`, o["version"], liveFlowAddr), http.StatusOK)
	done := f.mustCall(http.MethodPost, "/api/v1/orders/"+orderID+"/confirm",
		fmt.Sprintf(`{"version":%v}`, patched["version"]), http.StatusOK)
	if done["status"] != "confirmed" {
		t.Fatalf("sau confirm: %v", done)
	}
	if got := livetest.Available(t, f.staff, f.tenant, v); got != held {
		t.Errorf("có thể bán sau xác nhận = %d, muốn %d", got, held)
	}
	l := f.mustCall(http.MethodGet, pending, "", http.StatusOK)
	if items, _ := l["items"].([]any); len(items) != 0 || l["confirmedCount"] != float64(1) {
		t.Errorf("danh sách sau hoàn tất: %v", l)
	}
}

// postFacebookMessageText gửi tới ingest ở base một gói Facebook có chữ ký: tin text của PSID user tới Page page.
func postFacebookMessageText(t *testing.T, base, page, user, mid, text string) int {
	t.Helper()
	now := time.Now().UnixMilli()
	body, err := json.Marshal(map[string]any{
		"object": "page",
		"entry": []any{map[string]any{
			"id": page, "time": now,
			"messaging": []any{map[string]any{
				"sender": map[string]string{"id": user}, "recipient": map[string]string{"id": page}, "timestamp": now,
				"message": map[string]string{"mid": mid, "text": text},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return postSigned(t, base+"/hooks/facebook", "X-Hub-Signature-256", body)
}
