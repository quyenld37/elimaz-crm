package live_test

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/adapters/fake"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/inbound"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/modules/order"
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
	"elimaz.com.vn/crm/internal/testkit/livetest"
)

const (
	cAppID       = "1626301872631379"
	cAppSecret   = "0123456789abcdef0123456789abcdef"
	claimPageExt = "104000000000172"
	claimToken   = "EAAB-page-token-172"
	fakePageEx   = "fake-page-172"
	liveVideo    = "lv-172"
	// askBody là mẫu xin địa chỉ mặc định của Page Facebook thử.
	askBody = "Chào {ten_khach}, shop đã giữ hàng cho đơn {ma_don}. Bạn gửi giúp shop địa chỉ nhận hàng nhé"
)

// claimEnv: database thử, tenant A có người bán ELIMAZ, kho MAIN, Page Facebook (máy Meta giả, đã bật gửi thật, có
// token) và Page kênh giả; mỗi Page một phiên live đang chạy bắt đầu 12:15 giờ Việt Nam; Page Facebook có mẫu xin địa
// chỉ mặc định, khung giờ 11:00–14:00 → nguồn "Live ca trưa", nguồn comment "Comment"; nhân viên An, Bình có
// live.operate, conversation.read trên hai Page, order.create, order.update toàn shop; worker thật chạy lệnh gửi.
type claimEnv struct {
	t                   *testing.T
	staff               *testkit.Staff
	tenant              uuid.UUID
	shop                *invtest.Shop
	fg                  *fakegraph.Server
	chat                *fake.Chat
	fbPage, fakePage    uuid.UUID
	fbSession, fakeSess uuid.UUID
	liveStart           time.Time
	srcNoon, srcComment uuid.UUID
	tpl                 uuid.UUID
	bus                 *realtimetest.RecordingBus
	d                   deps.Deps
	an, binh            identity.AuthUser
	worker, inboundOnly *queue.Worker
	seq                 atomic.Int64
}

// liveComment là một comment live của khách trong env.
type liveComment struct {
	id, conv, customer uuid.UUID
	ext                string
}

func newClaimEnv(t *testing.T) *claimEnv {
	t.Helper()
	staff := testkit.OpenStaff(t)
	e := &claimEnv{
		t: t, staff: staff, chat: fake.New(cAppSecret), bus: &realtimetest.RecordingBus{},
	}
	e.tenant = staff.Tenant(t, "A")
	staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		e.shop = invtest.NewShop(t, tx, e.tenant)
		return order.EnsureDefaults(t.Context(), tx, e.tenant)
	})
	e.fg = fakegraph.New(t, fakegraph.Options{AppID: cAppID, AppSecret: cAppSecret})
	e.fg.AddPage(fakegraph.Page{ID: claimPageExt, Name: "Shop thử", Token: claimToken})
	opts := facebook.Options{AppID: cAppID, AppSecret: logging.Secret(cAppSecret), Version: "v26.0", HTTP: e.fg.Client()}
	opts.BaseURL, opts.StreamingBaseURL = e.fg.URL(), e.fg.URL()
	reg, err := channel.NewRegistry(facebook.New(opts), e.chat)
	if err != nil {
		t.Fatal(err)
	}
	kr := livetest.Keyring(t)
	e.fbPage = staff.ChannelAccount(t, e.tenant, facebook.Key, claimPageExt, "Shop thử")
	e.fakePage = staff.ChannelAccount(t, e.tenant, fake.Key, fakePageEx, "Page giả")
	e.d = deps.Deps{
		DB: staff.App, Queue: queue.NewQueue(), Events: events.NewRegistry(), Registry: reg, Keys: kr, Bus: e.bus,
		Metrics: metrics.NewRegistry(), Config: config.Config{Meta: config.MetaConfig{AppID: cAppID}},
	}
	e.seed(kr)
	e.an = e.user("an@elimaz.test", "An")
	e.binh = e.user("binh@elimaz.test", "Bình")
	e.worker = e.newWorker("w-send", true)
	e.inboundOnly = e.newWorker("w-in", false)
	return e
}

// seed: bật gửi thật, token, nguồn đơn, mẫu, cài đặt Page, hai phiên live (12:15 giờ Việt Nam hôm nay).
func (e *claimEnv) seed(kr *fieldcrypt.Keyring) {
	ctx := e.t.Context()
	now := time.Now().UTC()
	e.liveStart = time.Date(now.Year(), now.Month(), now.Day(), 5, 15, 0, 0, time.UTC) // 12:15 ICT
	if e.liveStart.After(now) {
		e.liveStart = e.liveStart.Add(-24 * time.Hour)
	}
	livetest.EnableSend(e.t, e.staff, e.tenant, e.fbPage, kr, claimToken)
	e.exec(`UPDATE channel.channel_accounts SET send_mode = 'enabled' WHERE id = $1`, e.fakePage)
	e.srcNoon = e.staff.TrafficSource(e.t, e.tenant, "Live ca trưa", true)
	e.srcComment = e.staff.TrafficSource(e.t, e.tenant, "Comment thử", true)
	e.fbSession = livetest.LiveSession(e.t, e.staff, e.tenant, e.fbPage, liveVideo, e.liveStart)
	e.fakeSess = livetest.LiveSession(e.t, e.staff, e.tenant, e.fakePage, liveVideo, e.liveStart)
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO conversation.reply_templates (tenant_id, shortcut, title, body)
			VALUES (current_setting('app.tenant_id')::uuid, 'xin-dia-chi', 'Xin địa chỉ', $1) RETURNING id`,
			askBody).Scan(&e.tpl); err != nil {
			return err //nolint:wrapcheck // test
		}
		slots, _ := json.Marshal([]map[string]string{{"from": "11:00", "to": "14:00", "trafficSourceId": e.srcNoon.String()}})
		if _, err := tx.Exec(ctx, `INSERT INTO conversation.page_settings (tenant_id, channel_account_id,
			ask_address_template_id, comment_traffic_source_id, live_slots)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3, $4)`, e.fbPage, e.tpl, e.srcComment, slots); err != nil {
			return err //nolint:wrapcheck // test
		}
		return nil
	})
}

// user tạo nhân viên tên hiển thị name có quyền live trên hai Page và quyền đơn toàn shop.
func (e *claimEnv) user(email, name string) identity.AuthUser {
	e.t.Helper()
	id := e.staff.User(e.t, e.tenant, email, "active")
	e.exec(`UPDATE identity.users SET display_name = $2 WHERE id = $1`, id, name)
	page := func(p identity.Permission, acc uuid.UUID) identity.Grant {
		return identity.Grant{Permission: p, Scope: identity.Scope{Type: identity.ScopePage, ID: acc.String()}}
	}
	all := identity.Scope{Type: identity.ScopeAll}
	return identity.AuthUser{UserID: id, TenantID: e.tenant, Grants: []identity.Grant{
		page("live.operate", e.fbPage), page("conversation.read", e.fbPage),
		page("live.operate", e.fakePage), page("conversation.read", e.fakePage),
		{Permission: "order.create", Scope: all},
		{Permission: "order.update", Scope: all},
	}}
}

// newWorker dựng worker id của đường vào kênh giả; withSend: chạy cả lệnh gửi.
func (e *claimEnv) newWorker(id string, withSend bool) *queue.Worker {
	handlers, err := webhooks.InboundJobHandlers([]webhooks.Source{e.chat}, []webhooks.InboundHandler{inbound.NewHandler(e.d, fake.Key)})
	if err != nil {
		e.t.Fatal(err)
	}
	opts := queue.WorkerOptions{WorkerID: id, Concurrency: 4, Metrics: e.d.Metrics, Logger: slog.New(slog.DiscardHandler)}
	if withSend {
		opts.Commands = commands.NewRunner(e.staff.App, send.Parts(e.d).Commands)
	}
	return queue.NewWorker(e.staff.App, handlers, opts)
}

// exec chạy câu sửa dữ liệu thử sql (chủ bảng) trong tenant A.
func (e *claimEnv) exec(sql string, args ...any) {
	e.t.Helper()
	livetest.Exec(e.t, e.staff, e.tenant, sql, args...)
}

func (e *claimEnv) count(q string, args ...any) int {
	e.t.Helper()
	return e.staff.CountIn(e.t, e.tenant, q, args...)
}

// variant tạo biến thể giá 150 000 của người bán ELIMAZ, nạp onHand ở kho MAIN.
func (e *claimEnv) variant(onHand int32) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		id = e.shop.Variant(e.t, tx, e.shop.Seller, 150_000, onHand)
		return nil
	})
	return id
}

// available đọc "có thể bán" của biến thể v ở kho MAIN.
func (e *claimEnv) available(v uuid.UUID) int {
	e.t.Helper()
	var n int
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT coalesce(sum(available), 0)::int FROM inventory.stock_available_v
			WHERE variant_id = $1`, v).Scan(&n)
	})
	return n
}

// customer tạo khách tên name.
func (e *claimEnv) customer(name string) uuid.UUID {
	e.t.Helper()
	return livetest.Customer(e.t, e.staff, e.tenant, name)
}

// comment chèn comment live của khách cust (mã người from) dưới bài live của Page page, lúc at; Page Facebook: comment
// cũng có trên máy Meta giả. Cùng (Page, from) dùng chung một hội thoại comment.
func (e *claimEnv) comment(page, cust uuid.UUID, from, body string, at time.Time) liveComment {
	e.t.Helper()
	ext := fmt.Sprintf("%s_c%04d", liveVideo, e.seq.Add(1))
	c := livetest.LiveComment(e.t, e.staff, e.tenant, page, cust, liveVideo, from, ext, body, at)
	if page == e.fbPage {
		e.fg.AddComment(fakegraph.Comment{ID: ext, ParentID: liveVideo, FromID: from, FromName: "Khách", Message: body, Created: at.UTC()})
	}
	return liveComment{id: c.ID, conv: c.Conversation, customer: cust, ext: ext}
}

// inbox tạo hội thoại hộp thư của khách cust (khóa luồng from) trên Page page, tin cuối của khách lúc lastIn.
func (e *claimEnv) inbox(page, cust uuid.UUID, from string, lastIn time.Time) uuid.UUID {
	e.t.Helper()
	id := e.staff.Conversation(e.t, e.tenant, page, cust, from)
	e.exec(`UPDATE conversation.conversations SET last_message_at = $2, last_customer_message_at = $2 WHERE id = $1`, id, lastIn.UTC())
	return id
}

func (e *claimEnv) sendEnv() conversation.SendEnv {
	return conversation.SendEnv{Registry: e.d.Registry, Queue: e.d.Queue, Bus: e.bus}
}

// claim chốt comment c bằng nhân viên u trong một transaction tenant (như WriteTx: chạy lại khi lỗi tạm).
func (e *claimEnv) claim(u identity.AuthUser, c uuid.UUID, in conversation.LiveClaimInput) (conversation.LiveClaimResult, error) {
	var out conversation.LiveClaimResult
	w := order.Writer{Events: e.d.Events, Queue: e.d.Queue, Realtime: e.bus}
	err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		var err error
		out, err = conversation.ClaimLiveComment(e.t.Context(), tx, e.sendEnv(), w,
			order.Actor{TenantID: u.TenantID, UserID: u.UserID}, u, c, in, time.Now().UTC())
		return err //nolint:wrapcheck // test
	})
	return out, err //nolint:wrapcheck // test
}

// newClaim là yêu cầu chốt mode new các dòng items (biến thể, số lượng), xin địa chỉ.
func newClaim(items ...any) conversation.LiveClaimInput {
	in := conversation.LiveClaimInput{Mode: conversation.LiveClaimNew, AskAddress: true}
	for i := 0; i+1 < len(items); i += 2 {
		in.Items = append(in.Items, conversation.LiveClaimItem{VariantID: items[i].(uuid.UUID), Qty: int32(items[i+1].(int))}) //nolint:forcetypeassert,gosec // test
	}
	return in
}

func (e *claimEnv) drain() {
	e.t.Helper()
	testkit.DrainDue(e.t, e.staff.Owner, e.worker)
}

// privateCalls đếm lần máy Meta giả nhận nhắn riêng comment ext (POST {page}/messages có recipient.comment_id).
func (e *claimEnv) privateCalls(ext string) int {
	n := 0
	for _, c := range e.fg.Calls(claimPageExt + "/messages") {
		var b struct {
			Recipient struct {
				CommentID string `json:"comment_id"`
			} `json:"recipient"`
		}
		if json.Unmarshal(c.Body, &b) == nil && b.Recipient.CommentID == ext {
			n++
		}
	}
	return n
}

// deadlocks đọc số deadlock của database thử.
func (e *claimEnv) deadlocks() int64 {
	e.t.Helper()
	var n int64
	if err := e.staff.Owner.Pool.QueryRow(e.t.Context(),
		`SELECT deadlocks FROM pg_stat_database WHERE datname = current_database()`).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}
