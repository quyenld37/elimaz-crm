package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/channel/channeltest"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

const (
	ccOwner  = "chu-cc@elimaz.test"
	ccOwner2 = "chu2-cc@elimaz.test"
	ccSale   = "sale-cc@elimaz.test"
)

var ccStateRe = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

type ccEnv struct {
	t       *testing.T
	staff   *testkit.Staff
	keys    *cfaccesstest.AccessKeys
	srv     http.Handler
	tenant  uuid.UUID
	adapter *channeltest.ConnectorAdapter
	kr      *fieldcrypt.Keyring
	now     atomic.Pointer[time.Time]
}

func newCCEnv(t *testing.T) *ccEnv {
	t.Helper()
	e := &ccEnv{t: t, staff: testkit.OpenStaff(t), keys: cfaccesstest.AccessKeyServer(t), adapter: channeltest.NewConnectorAdapter("facebook")}
	e.tenant = e.staff.Tenant(t, "A")
	// Người bán own của shop (testkit tạo cùng Page giả).
	e.staff.ChannelAccount(t, e.tenant, "fakechat", "seed", "Seed")
	owner := rolePerms(t, "owner")
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, ccOwner, "active"), owner, "all", "")
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, ccOwner2, "active"), owner, "all", "")
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, ccSale, "active"), []string{"conversation.read"}, "all", "")
	kr, err := fieldcrypt.ParseKeyring(logging.Secret("k1:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32))))
	if err != nil {
		t.Fatal(err)
	}
	e.kr = kr
	e.adapter.AddCode("code-1", "EAAB-user-1")
	e.adapter.AddPages("EAAB-user-1",
		channeltest.FakePage{PageID: "1001", Name: "Shop Một", Token: "EAAB-page-1", CanMessage: true, CanModerate: true},
		channeltest.FakePage{PageID: "1002", Name: "Shop Hai", Token: "EAAB-page-2", CanMessage: true})
	e.adapter.AddPages("EAAB-system-9",
		channeltest.FakePage{PageID: "1003", Name: "Shop Ba", Token: "EAAB-page-3", CanMessage: true, CanModerate: true})
	reg, err := channel.NewRegistry(e.adapter)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	e.now.Store(&start)
	d := testServerDeps(t, e.staff, e.keys, 0)
	d.Channels, d.ChannelKeys = reg, kr
	d.Now = func() time.Time { return *e.now.Load() }
	d.Config.Meta = config.MetaConfig{
		Enabled: true, AppID: "1626301872631379", LoginConfigID: "cfg-77", GraphVersion: "v26.0",
		OAuthRedirectURL: "https://crm.elimaz.test/channels/facebook/callback",
	}
	e.srv = NewServer(d)
	return e
}

func (e *ccEnv) advance(d time.Duration) {
	n := e.now.Load().Add(d)
	e.now.Store(&n)
}

func nextKey() string { return "cc-" + uuid.NewString() }

func (e *ccEnv) post(email, path, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	return serveAs(e.srv, e.keys, http.MethodPost, path, email, nextKey(), body)
}

// startState gọi start bằng email và trả state trong authorizeUrl.
func (e *ccEnv) startState(email string) string {
	e.t.Helper()
	rec := e.post(email, "/api/v1/channels/facebook/oauth/start", "")
	if rec.Code != http.StatusOK {
		e.t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	raw, _ := decodeJSON(e.t, rec)["authorizeUrl"].(string)
	u, err := url.Parse(raw)
	if err != nil {
		e.t.Fatal(err)
	}
	return u.Query().Get("state")
}

func (e *ccEnv) exchange(email, code, state string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.post(email, "/api/v1/channels/facebook/oauth/exchange", `{"code":"`+code+`","state":"`+state+`"}`)
}

func noSecrets(t *testing.T, what, body string) {
	t.Helper()
	for _, bad := range []string{"EAAB", "access_token", "accessToken", "credential"} {
		if strings.Contains(body, bad) {
			t.Errorf("%s: câu trả lời chứa %q: %s", what, bad, body)
		}
	}
}

// start: thiếu channel.manage → 403; owner → authorizeUrl đúng config_id, redirect_uri, state 43 ký tự base64url;
// bảng chỉ lưu băm; chưa bật kết nối (không adapter) → 503 retry_later.
func TestChannelConnect_Start(t *testing.T) {
	t.Parallel()
	e := newCCEnv(t)
	expect(t, "sale", e.post(ccSale, "/api/v1/channels/facebook/oauth/start", ""), http.StatusForbidden, "forbidden")
	rec := e.post(ccOwner, "/api/v1/channels/facebook/oauth/start", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("owner: %d %s", rec.Code, rec.Body.String())
	}
	raw, _ := decodeJSON(t, rec)["authorizeUrl"].(string)
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "www.facebook.com" || u.Path != "/v26.0/dialog/oauth" || q.Get("config_id") != "cfg-77" ||
		q.Get("redirect_uri") != "https://crm.elimaz.test/channels/facebook/callback" || q.Get("client_id") != "1626301872631379" ||
		!ccStateRe.MatchString(q.Get("state")) {
		t.Fatalf("authorizeUrl = %s", raw)
	}
	sum := sha256.Sum256([]byte(q.Get("state")))
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM channel.oauth_sessions WHERE state_hash = $1`, sum[:]); n != 1 {
		t.Fatalf("dòng theo băm = %d", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM channel.oauth_sessions WHERE state_hash = $1`, []byte(q.Get("state"))); n != 0 {
		t.Fatal("bảng lưu state thô")
	}
	off := NewServer(testServerDeps(t, e.staff, e.keys, 0))
	expect(t, "chưa bật kết nối", serveAs(off, e.keys, http.MethodPost, "/api/v1/channels/facebook/oauth/start", ccOwner, nextKey(), ""),
		http.StatusServiceUnavailable, "retry_later")
}

// exchange: state sai, của người khác, sau 16 phút, dùng lần hai → 400; Meta lỗi tạm → 503 không lưu (state vẫn dùng
// được); đúng → 2 Page, không lộ token.
func TestChannelConnect_Exchange(t *testing.T) {
	t.Parallel()
	e := newCCEnv(t)
	state := e.startState(ccOwner)
	expect(t, "state sai", e.exchange(ccOwner, "code-1", state+"x"), http.StatusBadRequest, "oauth_state_invalid")
	expect(t, "người khác", e.exchange(ccOwner2, "code-1", state), http.StatusBadRequest, "oauth_state_invalid")
	e.adapter.FailNext(channeltest.OpExchange, channeltest.ErrConnectNotSent)
	expect(t, "Meta lỗi tạm", e.exchange(ccOwner, "code-1", state), http.StatusServiceUnavailable, "retry_later")
	expect(t, "mã sai", e.exchange(ccOwner, "code-x", state), http.StatusBadRequest, "oauth_state_invalid")
	rec := e.exchange(ccOwner, "code-1", state)
	if rec.Code != http.StatusOK {
		t.Fatalf("đúng: %d %s", rec.Code, rec.Body.String())
	}
	noSecrets(t, "exchange", rec.Body.String())
	got := decodeJSON(t, rec)
	pages, _ := got["pages"].([]any)
	if _, err := uuid.Parse(got["sessionId"].(string)); err != nil || len(pages) != 2 {
		t.Fatalf("kết quả = %v", got)
	}
	p0, _ := pages[0].(map[string]any)
	if p0["pageId"] != "1001" || p0["name"] != "Shop Một" || p0["canMessage"] != true || p0["canModerate"] != true {
		t.Fatalf("Page 1 = %v", p0)
	}
	expect(t, "dùng lần hai", e.exchange(ccOwner, "code-1", state), http.StatusBadRequest, "oauth_state_invalid")
	late := e.startState(ccOwner)
	e.advance(16 * time.Minute)
	expect(t, "sau 16 phút", e.exchange(ccOwner, "code-1", late), http.StatusBadRequest, "oauth_state_invalid")
}

// Hai exchange song song cùng state, khác khóa chống trùng → đúng một 200, một 400.
func TestChannelConnect_ExchangeConcurrent(t *testing.T) {
	t.Parallel()
	e := newCCEnv(t)
	state := e.startState(ccOwner)
	codes := make([]int, 2)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Go(func() { codes[i] = e.exchange(ccOwner, "code-1", state).Code })
	}
	wg.Wait()
	if codes[0]+codes[1] != http.StatusOK+http.StatusBadRequest || (codes[0] != http.StatusOK && codes[1] != http.StatusOK) {
		t.Fatalf("kết quả song song = %v, muốn một 200 một 400", codes)
	}
}

// connect: từ phiên đăng nhập → Page read_only, active, một lệnh đăng ký, không lộ token; Page thiếu MODERATE, Page
// không có trong phiên → 422; system user → token_kind system_user; Page không thuộc system user → 422; thiếu/thừa
// nguồn token → 422 invalid_input; Page của shop khác → 409; người khác dùng phiên → 400.
func TestChannelConnect_Page(t *testing.T) {
	t.Parallel()
	e := newCCEnv(t)
	rec := e.exchange(ccOwner, "code-1", e.startState(ccOwner))
	session, _ := decodeJSON(t, rec)["sessionId"].(string)
	const path = "/api/v1/channels/facebook/pages"
	expect(t, "sale", e.post(ccSale, path, `{"pageId":"1001","sessionId":"`+session+`"}`), http.StatusForbidden, "forbidden")
	expect(t, "người khác dùng phiên", e.post(ccOwner2, path, `{"pageId":"1001","sessionId":"`+session+`"}`),
		http.StatusBadRequest, "oauth_state_invalid")
	expect(t, "thiếu MODERATE", e.post(ccOwner, path, `{"pageId":"1002","sessionId":"`+session+`"}`),
		http.StatusUnprocessableEntity, "page_not_accessible")
	expect(t, "Page không có trong phiên", e.post(ccOwner, path, `{"pageId":"1003","sessionId":"`+session+`"}`),
		http.StatusUnprocessableEntity, "page_not_accessible")
	expect(t, "không nguồn token", e.post(ccOwner, path, `{"pageId":"1001"}`), http.StatusUnprocessableEntity, "invalid_input")
	expect(t, "hai nguồn token", e.post(ccOwner, path, `{"pageId":"1001","sessionId":"`+session+`","systemUserToken":"EAAB-system-9"}`),
		http.StatusUnprocessableEntity, "invalid_input")
	e.adapter.FailNext(channeltest.OpPageToken, channeltest.ErrConnectUnknown)
	expect(t, "Meta lỗi tạm", e.post(ccOwner, path, `{"pageId":"1001","sessionId":"`+session+`"}`), http.StatusServiceUnavailable, "retry_later")
	rec = e.post(ccOwner, path, `{"pageId":"1001","sessionId":"`+session+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("nối Page: %d %s", rec.Code, rec.Body.String())
	}
	noSecrets(t, "connect", rec.Body.String())
	if got := decodeJSON(t, rec); got["sendMode"] != "read_only" || got["status"] != "active" || got["externalId"] != "1001" ||
		got["name"] != "Shop Một" || got["channel"] != "facebook" {
		t.Fatalf("tài khoản = %v", got)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.jobs WHERE kind = $1`, channel.SubscribePageKind); n != 1 {
		t.Fatalf("lệnh đăng ký = %d", n)
	}
	expect(t, "Page không thuộc system user", e.post(ccOwner, path, `{"pageId":"1001","systemUserToken":"EAAB-system-9"}`),
		http.StatusUnprocessableEntity, "page_not_accessible")
	rec = e.post(ccOwner, path, `{"pageId":"1003","systemUserToken":"EAAB-system-9"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("system user: %d %s", rec.Code, rec.Body.String())
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM channel.channel_accounts WHERE external_id = '1003'
		AND token_kind = 'system_user' AND send_mode = 'read_only'`); n != 1 {
		t.Fatal("Page system user không có token_kind system_user")
	}
	// Page 1004 đã là tuyến của shop B.
	b := e.staff.Tenant(t, "B")
	if err := tenancy.WithTenant(t.Context(), e.staff.App, b, func(tx pgx.Tx) error {
		return tenancy.PutRoute(t.Context(), tx, "fb_page", "1004")
	}); err != nil {
		t.Fatal(err)
	}
	e.adapter.AddPages("EAAB-system-9", channeltest.FakePage{PageID: "1004", Name: "Shop Bốn", Token: "EAAB-page-4", CanMessage: true, CanModerate: true})
	expect(t, "Page của shop khác", e.post(ccOwner, path, `{"pageId":"1004","systemUserToken":"EAAB-system-9"}`),
		http.StatusConflict, "page_connected_elsewhere")
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM channel.channel_accounts WHERE external_id = '1004'`); n != 0 {
		t.Fatal("Page của shop khác vẫn được ghi")
	}
}

// disconnect: sale → 403; owner → disconnected, một lệnh hủy đăng ký; Page không có → 404.
func TestChannelConnect_Disconnect(t *testing.T) {
	t.Parallel()
	e := newCCEnv(t)
	rec := e.post(ccOwner, "/api/v1/channels/facebook/pages", `{"pageId":"1003","systemUserToken":"EAAB-system-9"}`)
	id, _ := decodeJSON(t, rec)["id"].(string)
	path := "/api/v1/channels/" + id + "/disconnect"
	expect(t, "sale", e.post(ccSale, path, ""), http.StatusForbidden, "forbidden")
	rec = e.post(ccOwner, path, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("ngắt: %d %s", rec.Code, rec.Body.String())
	}
	if got := decodeJSON(t, rec); got["status"] != "disconnected" {
		t.Fatalf("sau ngắt = %v", got)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.jobs WHERE kind = $1`, channel.UnsubscribePageKind); n != 1 {
		t.Fatalf("lệnh hủy = %d", n)
	}
	expect(t, "Page không có", e.post(ccOwner, "/api/v1/channels/"+uuid.NewString()+"/disconnect", ""), http.StatusNotFound, "not_found")
}
