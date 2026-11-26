package channel_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/channel/channeltest"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// connEnv là môi trường test kết nối Page: tenant A, người bán own, adapter giả khóa facebook, worker chạy hai lệnh
// đăng ký/hủy đăng ký của channel.
type connEnv struct {
	*chEnv
	actor   identity.AuthUser
	seller  uuid.UUID
	adapter *channeltest.ConnectorAdapter
	kr      *fieldcrypt.Keyring
	bus     *realtimetest.RecordingBus
	cmds    []commands.Handler
	jobs    []queue.Handler
	worker  *queue.Worker
	// clock là đồng hồ của Deps (lịch sức khỏe, im lặng); mặc định giờ thật.
	clock *testClock
}

// testClock là đồng hồ test đặt được; an toàn khi worker đọc từ goroutine khác.
type testClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.at.IsZero() {
		return time.Now()
	}
	return c.at
}

func (c *testClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = t
}

func newConnEnv(t *testing.T) *connEnv {
	t.Helper()
	e := &connEnv{
		chEnv: newChEnv(t), adapter: channeltest.NewConnectorAdapter("facebook"), kr: testKeyring(t),
		bus: &realtimetest.RecordingBus{}, clock: &testClock{},
	}
	e.actor = identity.AuthUser{TenantID: e.tenant, UserID: e.staff.User(t, e.tenant, "chu@elimaz.test", "active")}
	// Page giả khác kênh chỉ để testkit tạo người bán own; lấy seller_id của nó.
	e.seller = e.account(e.staff.ChannelAccount(t, e.tenant, "fakechat", "seed", "Seed")).SellerID
	reg, err := channel.NewRegistry(e.adapter)
	if err != nil {
		t.Fatal(err)
	}
	d := channel.Deps{DB: e.staff.App, Queue: queue.NewQueue(), Bus: e.bus, Registry: reg, Keys: e.kr, Now: e.clock.Now}
	e.cmds, e.jobs = channel.CommandHandlers(d), channel.JobHandlers(d)
	runner := commands.NewRunner(e.staff.App, e.cmds)
	e.worker = queue.NewWorker(e.staff.App, nil, queue.WorkerOptions{WorkerID: "channel-connect/" + uuid.NewString(), Commands: runner})
	return e
}

func (e *connEnv) connect(pageID, name, token, kind string) (channel.ChannelAccount, error) {
	w := channel.Writer{Realtime: e.bus, RequestID: "req-c"}
	var a channel.ChannelAccount
	err := e.in(e.t.Context(), func(tx pgx.Tx) (err error) {
		a, err = w.Connect(e.t.Context(), tx, e.kr, channel.ConnectInput{
			Channel: "facebook", PageID: pageID, Name: name, SellerID: e.seller, Token: logging.Secret(token), TokenKind: kind,
		}, e.actor)
		return err //nolint:wrapcheck // test
	})
	return a, err
}

func (e *connEnv) mustConnect(pageID, name, token, kind string) channel.ChannelAccount {
	e.t.Helper()
	a, err := e.connect(pageID, name, token, kind)
	if err != nil {
		e.t.Fatal(err)
	}
	return a
}

func (e *connEnv) disconnect(id uuid.UUID) channel.ChannelAccount {
	e.t.Helper()
	w := channel.Writer{Realtime: e.bus, RequestID: "req-d"}
	var a channel.ChannelAccount
	if err := e.in(e.t.Context(), func(tx pgx.Tx) (err error) {
		a, err = w.Disconnect(e.t.Context(), tx, id, e.actor)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return a
}

func (e *connEnv) token(id uuid.UUID) (logging.Secret, error) {
	var c channel.Credentials
	err := e.in(e.t.Context(), func(tx pgx.Tx) (err error) {
		c, err = channel.CredentialsFor(e.t.Context(), tx, e.kr, id)
		return err //nolint:wrapcheck // test
	})
	return c.AccessToken, err
}

func (e *connEnv) count(q string, args ...any) int {
	e.t.Helper()
	return e.staff.CountIn(e.t, e.tenant, q, args...)
}

func (e *connEnv) jobStatuses(kind string) []string {
	e.t.Helper()
	return e.jobColumn("status", kind)
}

// jobColumn trả cột col (biểu thức SQL cố định của test) của các việc kind thuộc tenant e, theo thứ tự ghi.
func (e *connEnv) jobColumn(col, kind string) []string {
	e.t.Helper()
	rows, err := e.staff.Owner.Pool.Query(e.t.Context(),
		`SELECT `+col+` FROM platform.jobs WHERE tenant_id = $1 AND kind = $2 ORDER BY created_at, id`, e.tenant, kind)
	if err != nil {
		e.t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}

// routeTenant trả tenant của tuyến fb_page:<pageID> (uuid.Nil khi không có; platform.tenant_routes không RLS).
func (e *connEnv) routeTenant(pageID string) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.staff.Owner.Pool.QueryRow(e.t.Context(), `SELECT tenant_id FROM platform.tenant_routes
		WHERE kind = 'fb_page' AND external_key = $1`, pageID).Scan(&id); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		e.t.Fatal(err)
	}
	return id
}

//nolint:ireturn // test lấy handler theo tên để gọi Lookup
func (e *connEnv) handler(kind string) commands.Handler {
	e.t.Helper()
	for _, h := range e.cmds {
		if h.Kind().Name == kind {
			return h
		}
	}
	e.t.Fatalf("không có lệnh %s", kind)
	return nil
}

// Kết nối Page mới: read_only, active, token page_user mã hóa (không lộ token), tuyến fb_page, một audit không token, một
// lệnh subscribe_page (payload không token); worker chạy → adapter nhận đúng 11 trường, subscribed_fields 11 phần tử.
func TestConnect_NewPage(t *testing.T) {
	t.Parallel()
	e := newConnEnv(t)
	a := e.mustConnect("1001", "Shop Một", "EAAB-page-1", channel.TokenPageUser)
	if a.SendMode != channel.SendModeReadOnly || a.Status != channel.StatusActive || a.TokenKind != channel.TokenPageUser ||
		a.Channel != "facebook" || a.ExternalID != "1001" || a.Name != "Shop Một" || a.SellerID != e.seller || a.Version != 1 {
		t.Fatalf("tài khoản = %+v", a)
	}
	if tok, err := e.token(a.ID); err != nil || tok != "EAAB-page-1" {
		t.Fatalf("token = %q, %v", tok, err)
	}
	if n := e.count(`SELECT count(*) FROM channel.channel_accounts WHERE id = $1 AND credential_enc IS NOT NULL
		AND position($2::bytea IN credential_enc) = 0`, a.ID, []byte("EAAB-page-1")); n != 1 {
		t.Fatal("credential_enc chứa token thô hoặc rỗng")
	}
	if got := e.routeTenant("1001"); got != e.tenant {
		t.Fatalf("tuyến fb_page:1001 thuộc %v, muốn %v", got, e.tenant)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'channel.connected' AND entity_id = $1
		AND request_id = 'req-c' AND position('EAAB' IN coalesce(before::text, '') || coalesce(after::text, '')) = 0`, a.ID.String()); n != 1 {
		t.Fatalf("audit channel.connected = %d, muốn 1", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1 AND position('EAAB' IN payload::text) = 0`,
		channel.SubscribePageKind); n != 1 {
		t.Fatalf("lệnh subscribe_page = %d, muốn 1", n)
	}
	if len(e.bus.Signals()) != 1 {
		t.Fatalf("báo hiệu = %d, muốn 1", len(e.bus.Signals()))
	}
	e.staff.DrainJobs(t, e.worker)
	if got := e.adapter.Subscribed("1001"); !slices.Equal(got, channel.WebhookFields()) || len(got) != 11 {
		t.Fatalf("trường đăng ký ở kênh = %v", got)
	}
	if calls := e.adapter.Calls(channeltest.OpSubscribe); len(calls) != 1 || calls[0].Token != "EAAB-page-1" {
		t.Fatalf("lần đăng ký = %+v", calls)
	}
	if got := e.account(a.ID); !slices.Equal(got.SubscribedFields, channel.WebhookFields()) || got.Status != channel.StatusActive {
		t.Fatalf("sau đăng ký = %+v", got)
	}
	if st := e.jobStatuses(channel.SubscribePageKind); !slices.Equal(st, []string{"done"}) {
		t.Fatalf("lệnh = %v", st)
	}
}

// Page đã nối với shop B → ErrPageConnectedElsewhere, B không có dòng nào, tuyến vẫn của A; crm_app không INSERT thẳng
// platform.tenant_routes được (đường duy nhất là put_tenant_route).
func TestConnect_PageConnectedElsewhere(t *testing.T) {
	t.Parallel()
	e := newConnEnv(t)
	e.mustConnect("1001", "Shop Một", "EAAB-page-1", channel.TokenPageUser)
	b := e.staff.Tenant(t, "B")
	sellerB := uuid.Nil
	if err := e.staff.TryExec(t.Context(), b, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `INSERT INTO catalog.sellers (tenant_id, code, name, kind) VALUES ($1, 'B', 'B', 'own')
			RETURNING id`, b).Scan(&sellerB)
	}); err != nil {
		t.Fatal(err)
	}
	actorB := identity.AuthUser{TenantID: b, UserID: e.staff.User(t, b, "chu-b@elimaz.test", "active")}
	err := tenancy.WithTenant(t.Context(), e.staff.App, b, func(tx pgx.Tx) error {
		_, err := channel.Writer{}.Connect(t.Context(), tx, e.kr, channel.ConnectInput{
			Channel: "facebook", PageID: "1001", Name: "Cướp", SellerID: sellerB, Token: "EAAB-x", TokenKind: channel.TokenPageUser,
		}, actorB)
		return err //nolint:wrapcheck // test
	})
	if !errors.Is(err, channel.ErrPageConnectedElsewhere) {
		t.Fatalf("shop B nối Page của A: %v", err)
	}
	if n := e.staff.CountIn(t, b, `SELECT count(*) FROM channel.channel_accounts`); n != 0 {
		t.Fatalf("shop B có %d tài khoản", n)
	}
	if n := e.staff.CountIn(t, b, `SELECT count(*) FROM platform.jobs WHERE tenant_id = $1`, b); n != 0 {
		t.Fatalf("shop B có %d việc", n)
	}
	if got := e.routeTenant("1001"); got != e.tenant {
		t.Fatalf("tuyến fb_page:1001 thuộc %v, muốn %v", got, e.tenant)
	}
	err = e.in(t.Context(), func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO platform.tenant_routes (kind, external_key, tenant_id) VALUES ('fb_page', '2002', $1)`, e.tenant)
		return err //nolint:wrapcheck // test
	})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("crm_app INSERT thẳng tenant_routes: %v, muốn lỗi quyền", err)
	}
	if _, err := e.connect("1001", "x", "EAAB-x", "lạ"); !errors.Is(err, channel.ErrInvalidTokenKind) {
		t.Fatalf("loại token lạ: %v", err)
	}
}

// Nối lại Page đang enabled, degraded bằng token mới (system user) → token mới, send_mode giữ enabled, status active, lý
// do xóa, version giữ, audit channel.reconnected, thêm một lệnh đăng ký.
func TestConnect_Reconnect(t *testing.T) {
	t.Parallel()
	e := newConnEnv(t)
	a := e.mustConnect("1001", "Shop Một", "EAAB-page-1", channel.TokenPageUser)
	if err := e.in(t.Context(), func(tx pgx.Tx) error {
		if _, err := (channel.Writer{}).SetSendMode(t.Context(), tx, a.ID, channel.SendModeEnabled, 1, e.actor); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := channel.MarkStatus(t.Context(), tx, a.ID, channel.StatusDegraded, "auth")
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	b := e.mustConnect("1001", "Shop Một Mới", "EAAB-system-9", channel.TokenSystemUser)
	if b.ID != a.ID || b.SendMode != channel.SendModeEnabled || b.Status != channel.StatusActive || b.StatusReason != "" ||
		b.TokenKind != channel.TokenSystemUser || b.Name != "Shop Một Mới" || b.Version != 2 {
		t.Fatalf("nối lại = %+v", b)
	}
	if tok, err := e.token(a.ID); err != nil || tok != "EAAB-system-9" {
		t.Fatalf("token sau nối lại = %q, %v", tok, err)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'channel.reconnected' AND entity_id = $1`, a.ID.String()); n != 1 {
		t.Fatalf("audit channel.reconnected = %d", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1`, channel.SubscribePageKind); n != 2 {
		t.Fatalf("lệnh subscribe = %d, muốn 2", n)
	}
}

// Ngắt kết nối → disconnected, một lệnh hủy đăng ký (payload không token); worker chạy → kênh nhận một lần hủy bằng
// token cũ, rồi token bị xóa; tuyến còn. Ngắt lần hai không thêm lệnh, audit.
func TestDisconnect(t *testing.T) {
	t.Parallel()
	e := newConnEnv(t)
	a := e.mustConnect("1001", "Shop Một", "EAAB-page-1", channel.TokenPageUser)
	e.staff.DrainJobs(t, e.worker)
	d := e.disconnect(a.ID)
	if d.Status != channel.StatusDisconnected {
		t.Fatalf("sau ngắt = %+v", d)
	}
	if tok, err := e.token(a.ID); err != nil || tok != "EAAB-page-1" {
		t.Fatalf("token phải còn tới khi hủy đăng ký xong: %q %v", tok, err)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1 AND position('EAAB' IN payload::text) = 0`,
		channel.UnsubscribePageKind); n != 1 {
		t.Fatalf("lệnh hủy = %d", n)
	}
	e.disconnect(a.ID)
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1`, channel.UnsubscribePageKind); n != 1 {
		t.Fatalf("ngắt lần hai thêm lệnh: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'channel.disconnected'`); n != 1 {
		t.Fatalf("audit ngắt = %d", n)
	}
	e.staff.DrainJobs(t, e.worker)
	if calls := e.adapter.Calls(channeltest.OpUnsubscribe); len(calls) != 1 || calls[0].Token != "EAAB-page-1" || calls[0].PageID != "1001" {
		t.Fatalf("lần hủy = %+v", calls)
	}
	if _, err := e.token(a.ID); !errors.Is(err, channel.ErrNoCredentials) {
		t.Fatalf("token sau khi hủy xong: %v", err)
	}
	if got := e.account(a.ID); got.Status != channel.StatusDisconnected || got.TokenKind != "" {
		t.Fatalf("sau hủy = %+v", got)
	}
	if got := e.routeTenant("1001"); got != e.tenant {
		t.Fatalf("tuyến sau ngắt thuộc %v", got)
	}
	// Nối lại sau khi ngắt hẳn: active, có token mới.
	if b := e.mustConnect("1001", "Shop Một", "EAAB-page-2", channel.TokenPageUser); b.Status != channel.StatusActive {
		t.Fatalf("nối lại = %+v", b)
	}
}

// Ngắt rồi nối lại trước khi lệnh hủy chạy: lệnh hủy không gọi kênh, không xóa token mới.
func TestDisconnect_ReconnectBeforeUnsubscribe(t *testing.T) {
	t.Parallel()
	e := newConnEnv(t)
	a := e.mustConnect("1001", "Shop Một", "EAAB-page-1", channel.TokenPageUser)
	e.disconnect(a.ID)
	e.mustConnect("1001", "Shop Một", "EAAB-page-2", channel.TokenPageUser)
	e.staff.DrainJobs(t, e.worker)
	if calls := e.adapter.Calls(channeltest.OpUnsubscribe); len(calls) != 0 {
		t.Fatalf("lệnh hủy vẫn gọi kênh: %+v", calls)
	}
	if tok, err := e.token(a.ID); err != nil || tok != "EAAB-page-2" {
		t.Fatalf("token mới = %q, %v", tok, err)
	}
	if got := e.account(a.ID); got.Status != channel.StatusActive || len(got.SubscribedFields) != 11 {
		t.Fatalf("sau nối lại = %+v", got)
	}
}

// Token đã bị thu hồi khi hủy đăng ký → lệnh dead nhưng token vẫn bị xóa.
func TestDisconnect_RevokedTokenStillCleared(t *testing.T) {
	t.Parallel()
	e := newConnEnv(t)
	a := e.mustConnect("1001", "Shop Một", "EAAB-page-1", channel.TokenPageUser)
	e.staff.DrainJobs(t, e.worker)
	e.adapter.RevokeToken("EAAB-page-1")
	e.disconnect(a.ID)
	e.staff.DrainJobs(t, e.worker)
	if st := e.jobStatuses(channel.UnsubscribePageKind); !slices.Equal(st, []string{"dead"}) {
		t.Fatalf("lệnh hủy = %v", st)
	}
	if _, err := e.token(a.ID); !errors.Is(err, channel.ErrNoCredentials) {
		t.Fatalf("token: %v", err)
	}
}

// Đăng ký gặp lỗi token (190) → dead, degraded, đúng một lệnh cảnh báo gấp channel-subscribe:<id> có tên Page, không
// token; lỗi chưa gửi → tự thử lại rồi xong.
func TestSubscribe_AuthDeadAlerts(t *testing.T) {
	t.Parallel()
	e := newConnEnv(t)
	e.adapter.FailNext(channeltest.OpSubscribe, channeltest.ErrConnectNotSent)
	ok := e.mustConnect("1002", "Shop Hai", "EAAB-page-2", channel.TokenPageUser)
	e.staff.DrainJobs(t, e.worker)
	if got := e.account(ok.ID); got.Status != channel.StatusActive || len(got.SubscribedFields) != 11 {
		t.Fatalf("thử lại sau chưa gửi = %+v", got)
	}
	e.adapter.FailNext(channeltest.OpSubscribe, channeltest.ErrConnectAuth)
	a := e.mustConnect("1001", "Shop Một", "EAAB-page-1", channel.TokenPageUser)
	e.staff.DrainJobs(t, e.worker)
	if got := e.account(a.ID); got.Status != channel.StatusDegraded || got.StatusReason == "" {
		t.Fatalf("sau đăng ký lỗi = %+v", got)
	}
	payloads := e.jobColumn("payload::text", channel.SubscribeAlertKind)
	if len(payloads) != 1 || strings.Contains(payloads[0], "EAAB") {
		t.Fatalf("lệnh cảnh báo = %v", payloads)
	}
	n := &recNotifier{}
	out, err := channel.NewSubscribeAlertCommand(n, nil).Execute(t.Context(), queue.Job{Payload: json.RawMessage(payloads[0])})
	if err != nil || out.Status != commands.StatusDone || n.key != "channel-subscribe:"+a.ID.String() || n.sev != "urgent" ||
		!strings.Contains(n.text, "Shop Một") || strings.Contains(n.text, "EAAB") {
		t.Fatalf("cảnh báo = %+v %v %+v", out, err, n)
	}
}

// Lookup của hai lệnh đọc trạng thái đăng ký ở kênh: subscribe đủ trường → done, thiếu → not_sent; unsubscribe (tài khoản
// đã ngắt) còn đăng ký → not_sent, hết → done; unsubscribe khi tài khoản đã nối lại → done (Execute cũng bỏ qua); tra
// lỗi → unknown.
func TestSubscribe_Lookup(t *testing.T) {
	t.Parallel()
	e := newConnEnv(t)
	a := e.mustConnect("1001", "Shop Một", "EAAB-page-1", channel.TokenPageUser)
	job := queue.Job{TenantID: &e.tenant, Payload: json.RawMessage(`{"accountId":"` + a.ID.String() + `"}`)}
	lookup := func(kind string) string {
		t.Helper()
		st, err := e.handler(kind).(commands.Looker).Lookup(t.Context(), job)
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	if got := lookup(channel.SubscribePageKind); got != commands.StatusNotSent {
		t.Fatalf("chưa đăng ký: subscribe = %s", got)
	}
	if got := lookup(channel.UnsubscribePageKind); got != commands.StatusDone {
		t.Fatalf("tài khoản active: unsubscribe = %s", got)
	}
	e.staff.DrainJobs(t, e.worker)
	if got := lookup(channel.SubscribePageKind); got != commands.StatusDone {
		t.Fatalf("đã đăng ký: subscribe = %s", got)
	}
	e.adapter.FailNext(channeltest.OpHealth, channeltest.ErrConnectUnknown)
	if got := lookup(channel.SubscribePageKind); got != commands.StatusUnknown {
		t.Fatalf("tra lỗi: subscribe = %s", got)
	}
	if err := e.in(t.Context(), func(tx pgx.Tx) error {
		_, err := channel.MarkStatus(t.Context(), tx, a.ID, channel.StatusDisconnected, "")
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if got := lookup(channel.UnsubscribePageKind); got != commands.StatusNotSent {
		t.Fatalf("đã ngắt, còn đăng ký: unsubscribe = %s", got)
	}
	if got := lookup(channel.SubscribePageKind); got != commands.StatusDone {
		t.Fatalf("đã ngắt: subscribe = %s (bỏ qua)", got)
	}
	if err := e.adapter.Unsubscribe(t.Context(), a, channel.Credentials{AccessToken: "EAAB-page-1"}); err != nil {
		t.Fatal(err)
	}
	if got := lookup(channel.UnsubscribePageKind); got != commands.StatusDone {
		t.Fatalf("đã hủy ở kênh: unsubscribe = %s", got)
	}
	if err := e.in(t.Context(), func(tx pgx.Tx) error { return channel.ClearCredentials(t.Context(), tx, a.ID) }); err != nil {
		t.Fatal(err)
	}
	if got := lookup(channel.UnsubscribePageKind); got != commands.StatusDone {
		t.Fatalf("đã ngắt, không còn token: unsubscribe = %s", got)
	}
}
