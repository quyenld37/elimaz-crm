package channel_test

import (
	"crypto/sha256"
	"errors"
	"net/url"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/channel/channeltest"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/logging"
)

var stateRe = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func testPages() []channel.PageInfo {
	return []channel.PageInfo{
		{PageID: "1001", Name: "Shop Một", CanMessage: true, CanModerate: true},
		{PageID: "1002", Name: "Shop Hai", CanMessage: true},
	}
}

// newOAuthEnv dựng shop A, một nhân viên và mốc giờ cố định của test đăng nhập.
func newOAuthEnv(t *testing.T) (*chEnv, uuid.UUID, time.Time) {
	t.Helper()
	e := newChEnv(t)
	return e, e.staff.User(t, e.tenant, "chu@elimaz.test", "active"), time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
}

func (e *chEnv) start(user uuid.UUID, now time.Time) string {
	e.t.Helper()
	var state string
	if err := e.in(e.t.Context(), func(tx pgx.Tx) (err error) {
		state, err = channel.StartOAuth(e.t.Context(), tx, user, now)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return state
}

func (e *chEnv) checkState(user uuid.UUID, state string, now time.Time) (uuid.UUID, error) {
	var id uuid.UUID
	err := e.in(e.t.Context(), func(tx pgx.Tx) (err error) {
		id, err = channel.CheckOAuthState(e.t.Context(), tx, user, state, now)
		return err //nolint:wrapcheck // test
	})
	return id, err
}

func (e *chEnv) savePages(startID, user uuid.UUID, tok string, now time.Time) (uuid.UUID, error) {
	kr := testKeyring(e.t)
	var id uuid.UUID
	err := e.in(e.t.Context(), func(tx pgx.Tx) (err error) {
		id, err = channel.SaveOAuthPages(e.t.Context(), tx, kr, startID, user,
			channel.OAuthLogin{Token: logging.Secret(tok), Pages: testPages(), AppUserID: "app-" + tok}, now)
		return err //nolint:wrapcheck // test
	})
	return id, err
}

func (e *chEnv) userToken(session, user uuid.UUID, pageID string, now time.Time) (logging.Secret, error) {
	kr := testKeyring(e.t)
	var tok logging.Secret
	err := e.in(e.t.Context(), func(tx pgx.Tx) (err error) {
		tok, err = channel.OAuthUserToken(e.t.Context(), tx, kr, session, user, pageID, now)
		return err //nolint:wrapcheck // test
	})
	return tok, err
}

// start: state 43 ký tự base64url (32 byte), bảng chỉ lưu băm SHA-256; địa chỉ đăng nhập có đúng client_id, config_id,
// redirect_uri, phiên bản, state; cấu hình thiếu → ErrNoConnector.
func TestOAuth_StartAndAuthorizeURL(t *testing.T) {
	t.Parallel()
	e, user, now := newOAuthEnv(t)
	state := e.start(user, now)
	if !stateRe.MatchString(state) {
		t.Fatalf("state = %q", state)
	}
	sum := sha256.Sum256([]byte(state))
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM channel.oauth_sessions WHERE state_hash = $1 AND user_id = $2
		AND expires_at = $3 AND used_at IS NULL AND user_token_enc IS NULL`, sum[:], user, now.Add(15*time.Minute)); n != 1 {
		t.Fatalf("dòng phiên theo băm = %d, muốn 1", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM channel.oauth_sessions WHERE state_hash = $1`, []byte(state)); n != 0 {
		t.Fatal("bảng lưu state thô")
	}
	m := config.MetaConfig{
		AppID: "1626301872631379", LoginConfigID: "cfg-77", OAuthRedirectURL: "https://crm.elimaz.test/channels/facebook/callback",
		GraphVersion: "v26.0",
	}
	raw, err := channel.AuthorizeURL(m, state)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme != "https" || u.Host != "www.facebook.com" || u.Path != "/v26.0/dialog/oauth" || q.Get("client_id") != m.AppID ||
		q.Get("config_id") != "cfg-77" || q.Get("redirect_uri") != m.OAuthRedirectURL || q.Get("response_type") != "code" ||
		q.Get("state") != state {
		t.Fatalf("authorizeUrl = %s", raw)
	}
	m.LoginConfigID = ""
	if _, err := channel.AuthorizeURL(m, state); !errors.Is(err, channel.ErrNoConnector) {
		t.Fatalf("thiếu config_id: %v", err)
	}
}

// state sai, của người khác, quá 15 phút, đã dùng → ErrOAuthStateInvalid; đúng → lưu phiên Page (token mã hóa, hạn 30
// phút); phiên Page: người khác, quá hạn → ErrOAuthStateInvalid; Page không có trong phiên → ErrPageNotAccessible.
func TestOAuth_StateAndPagesSession(t *testing.T) {
	t.Parallel()
	e, user, now := newOAuthEnv(t)
	other := e.staff.User(t, e.tenant, "khac@elimaz.test", "active")
	state := e.start(user, now)
	for name, c := range map[string]struct {
		user  uuid.UUID
		state string
		at    time.Time
	}{
		"state sai":     {user, state + "x", now},
		"người khác":    {other, state, now},
		"sau 16 phút":   {user, state, now.Add(16 * time.Minute)},
		"state rỗng":    {user, "", now},
		"state lạ ngắn": {user, "abc", now},
	} {
		if _, err := e.checkState(c.user, c.state, c.at); !errors.Is(err, channel.ErrOAuthStateInvalid) {
			t.Errorf("%s: %v, muốn ErrOAuthStateInvalid", name, err)
		}
	}
	startID, err := e.checkState(user, state, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.savePages(startID, other, "EAAB-user-1", now.Add(time.Minute)); !errors.Is(err, channel.ErrOAuthStateInvalid) {
		t.Fatalf("lưu bằng người khác: %v", err)
	}
	session, err := e.savePages(startID, user, "EAAB-user-1", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.savePages(startID, user, "EAAB-user-1", now.Add(time.Minute)); !errors.Is(err, channel.ErrOAuthStateInvalid) {
		t.Fatalf("dùng lần hai: %v", err)
	}
	if _, err := e.checkState(user, state, now.Add(2*time.Minute)); !errors.Is(err, channel.ErrOAuthStateInvalid) {
		t.Fatalf("state đã dùng: %v", err)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM channel.oauth_sessions WHERE id = $1 AND expires_at = $2
		AND position($3::bytea IN user_token_enc) = 0 AND jsonb_array_length(pages) = 2`,
		session, now.Add(31*time.Minute), []byte("EAAB-user-1")); n != 1 {
		t.Fatalf("phiên Page = %d dòng đúng, muốn 1", n)
	}
	tok, err := e.userToken(session, user, "1001", now.Add(10*time.Minute))
	if err != nil || tok != "EAAB-user-1" {
		t.Fatalf("token người dùng = %q, %v", tok, err)
	}
	for name, c := range map[string]struct {
		id, user uuid.UUID
		page     string
		at       time.Time
		want     error
	}{
		"người khác":         {session, other, "1001", now, channel.ErrOAuthStateInvalid},
		"quá 30 phút":        {session, user, "1001", now.Add(32 * time.Minute), channel.ErrOAuthStateInvalid},
		"phiên start":        {startID, user, "1001", now, channel.ErrOAuthStateInvalid},
		"phiên không có":     {uuid.New(), user, "1001", now, channel.ErrOAuthStateInvalid},
		"Page không có":      {session, user, "9999", now, channel.ErrPageNotAccessible},
		"Page có, đúng giờ ": {session, user, "1002", now.Add(30 * time.Minute), nil},
	} {
		if _, err := e.userToken(c.id, c.user, c.page, c.at); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, muốn %v", name, err, c.want)
		}
	}
	// start lúc sau dọn phiên đã hết hạn của shop.
	e.start(user, now.Add(time.Hour))
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM channel.oauth_sessions`); n != 1 {
		t.Fatalf("còn %d phiên sau khi dọn, muốn 1", n)
	}
}

// Hai lần lưu cùng một state chạy song song: đúng một thành công, một ErrOAuthStateInvalid.
func TestOAuth_ConcurrentSave(t *testing.T) {
	t.Parallel()
	e, user, now := newOAuthEnv(t)
	startID, err := e.checkState(user, e.start(user, now), now)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() { _, errs[i] = e.savePages(startID, user, "EAAB-user-1", now) })
	}
	wg.Wait()
	ok, invalid := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, channel.ErrOAuthStateInvalid):
			invalid++
		}
	}
	if ok != 1 || invalid != 1 {
		t.Fatalf("kết quả song song = %v", errs)
	}
}

// ExchangeCode, FetchPageToken: lớp lỗi của adapter đổi sang sentinel của channel; Page thiếu quyền → không kết nối.
func TestConnector_Classify(t *testing.T) {
	t.Parallel()
	a := channeltest.NewConnectorAdapter("facebook")
	a.AddCode("code-1", "EAAB-user-1")
	a.AddPages("EAAB-user-1", channeltest.FakePage{PageID: "1001", Name: "Shop Một", Token: "EAAB-page-1", CanMessage: true, CanModerate: true},
		channeltest.FakePage{PageID: "1002", Name: "Shop Hai", Token: "EAAB-page-2", CanMessage: true})
	reg, err := channel.NewRegistry(a)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	a.SetAppUserID("EAAB-user-1", "218471")
	login, err := channel.ExchangeCode(ctx, reg, "facebook", "code-1")
	pages := login.Pages
	if err != nil || login.Token != "EAAB-user-1" || login.AppUserID != "218471" || len(pages) != 2 || pages[0].PageID != "1001" ||
		!pages[0].CanModerate || pages[1].CanModerate {
		t.Fatalf("ExchangeCode = %+v %v", login, err)
	}
	for name, c := range map[string]struct {
		op   string
		fail error
		code string
		want error
	}{
		"mã sai":                 {"", nil, "code-x", channel.ErrOAuthStateInvalid},
		"đổi mã chưa gửi":        {channeltest.OpExchange, channeltest.ErrConnectNotSent, "code-1", channel.ErrConnectUnavailable},
		"đổi mã không rõ":        {channeltest.OpExchange, channeltest.ErrConnectUnknown, "code-1", channel.ErrConnectUnavailable},
		"liệt kê lỗi auth":       {channeltest.OpListPages, channeltest.ErrConnectAuth, "code-1", channel.ErrOAuthStateInvalid},
		"liệt kê không rõ":       {channeltest.OpListPages, channeltest.ErrConnectUnknown, "code-1", channel.ErrConnectUnavailable},
		"mã người dùng lỗi auth": {channeltest.OpAppUserID, channeltest.ErrConnectAuth, "code-1", channel.ErrOAuthStateInvalid},
		"mã người dùng chưa gửi": {channeltest.OpAppUserID, channeltest.ErrConnectNotSent, "code-1", channel.ErrConnectUnavailable},
	} {
		if c.op != "" {
			a.FailNext(c.op, c.fail)
		}
		if _, err := channel.ExchangeCode(ctx, reg, "facebook", c.code); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, muốn %v", name, err, c.want)
		}
	}
	if _, err := channel.ExchangeCode(ctx, reg, "khac", "code-1"); !errors.Is(err, channel.ErrNoConnector) {
		t.Errorf("không có adapter: %v", err)
	}
	pt, info, err := channel.FetchPageToken(ctx, reg, "facebook", "EAAB-user-1", "1001")
	if err != nil || pt != "EAAB-page-1" || info.Name != "Shop Một" {
		t.Fatalf("FetchPageToken = %q %+v %v", pt, info, err)
	}
	for name, c := range map[string]struct {
		op   string
		fail error
		page string
		want error
	}{
		"thiếu MODERATE":   {"", nil, "1002", channel.ErrPageNotAccessible},
		"không thấy Page":  {"", nil, "1003", channel.ErrPageNotAccessible},
		"token hỏng":       {channeltest.OpPageToken, channeltest.ErrConnectAuth, "1001", channel.ErrPageNotAccessible},
		"yêu cầu sai":      {channeltest.OpPageToken, channeltest.ErrConnectPermanent, "1001", channel.ErrPageNotAccessible},
		"mạng chưa gửi":    {channeltest.OpPageToken, channeltest.ErrConnectNotSent, "1001", channel.ErrConnectUnavailable},
		"mạng không rõ":    {channeltest.OpPageToken, channeltest.ErrConnectUnknown, "1001", channel.ErrConnectUnavailable},
		"lỗi bọc vẫn nhận": {channeltest.OpPageToken, errors.Join(errors.New("x"), channeltest.ErrConnectAuth), "1001", channel.ErrPageNotAccessible},
	} {
		if c.op != "" {
			a.FailNext(c.op, c.fail)
		}
		if _, _, err := channel.FetchPageToken(ctx, reg, "facebook", "EAAB-user-1", c.page); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, muốn %v", name, err, c.want)
		}
	}
}
