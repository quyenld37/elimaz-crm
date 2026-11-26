package facebook_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

func TestFetchProfileAndPost(t *testing.T) {
	t.Parallel()
	e := newOutEnv(t)
	p, err := e.ch.FetchProfile(t.Context(), e.acc, e.cred, testPSID)
	if err != nil || p.Name != "Lan Nguyễn" || p.AvatarURL != "https://scontent.fbcdn.net/p.jpg" {
		t.Errorf("hồ sơ: %+v, %v", p, err)
	}
	if q := e.fg.Calls(testPSID)[0].Query.Get("fields"); q != "first_name,last_name,profile_pic" {
		t.Errorf("fields hồ sơ = %q", q)
	}
	post, err := e.ch.FetchPost(t.Context(), e.acc, e.cred, testPostID)
	if err != nil || post.ID != testPostID || post.Snippet != "Áo thun mới về" || post.Permalink != "https://www.facebook.com/"+testPostID ||
		!post.CreatedTime.Equal(time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("bài: %+v, %v", post, err)
	}
	if q := e.fg.Calls(testPostID)[0].Query.Get("fields"); q != "permalink_url,message,created_time,status_type" {
		t.Errorf("fields bài = %q", q)
	}
	if _, err := e.ch.FetchPost(t.Context(), e.acc, e.cred, "khong_co"); e.ch.ClassifyError(err) != channel.ErrorPermanent {
		t.Errorf("bài không có: %v", err)
	}
}

type evKey struct{ typ, dedupe, order, user string }

func eventKeys(t *testing.T, evs []webhooks.InboundEvent) []evKey {
	t.Helper()
	out := make([]evKey, 0, len(evs))
	for _, e := range evs {
		var p channel.MessagePayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatal(err)
		}
		if e.Route.Kind != facebook.RouteKind || e.Route.ExternalKey != testPageID || e.OccurredAt.IsZero() || p.PageID != testPageID {
			t.Errorf("sự kiện thiếu tuyến, giờ hay Page: %+v", e)
		}
		if p.IsEcho != (e.Type == channel.EventMessageEcho) {
			t.Errorf("IsEcho = %v với loại %s", p.IsEcho, e.Type)
		}
		out = append(out, evKey{e.Type, e.DedupeKey, e.OrderKey, p.UserID})
	}
	return out
}

// HistoryFetcher: hội thoại 25 tin → đúng 20 sự kiện mới nhất theo thứ tự thời gian; tin của Page là message.echo;
// khóa chống trùng m:<mid> như webhook (đọc lại tin đã vào không tạo việc thứ hai).
func TestFetchHistory(t *testing.T) {
	t.Parallel()
	e := newOutEnv(t)
	t0 := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	for i := range 20 {
		e.fg.AddInboundMessage(testPageID, testPSID, "m_in_"+strconv.Itoa(i), "tin "+strconv.Itoa(i), t0.Add(time.Duration(i)*time.Minute))
	}
	for i := range 5 {
		if out := e.send(t, textCmd("trả lời "+strconv.Itoa(i))); out.Status != channel.OutcomeSent {
			t.Fatal(out)
		}
	}
	evs, err := e.ch.FetchHistory(t.Context(), e.acc, e.cred, testPSID, 20)
	if err != nil || len(evs) != 20 {
		t.Fatalf("lịch sử: %d sự kiện, %v; muốn 20", len(evs), err)
	}
	keys := eventKeys(t, evs)
	order := "fb:" + testPageID + ":" + testPSID
	if keys[0] != (evKey{channel.EventMessageReceived, "m:m_in_5", order, testPSID}) {
		t.Errorf("sự kiện đầu = %+v, muốn tin m_in_5 (cũ nhất trong 20 tin mới nhất)", keys[0])
	}
	echoes := 0
	for i, k := range keys {
		if k.typ == channel.EventMessageEcho {
			echoes++
			if k.user != testPSID || k.order != order {
				t.Errorf("echo %d = %+v", i, k)
			}
		}
		if i > 0 && evs[i].OccurredAt.Before(evs[i-1].OccurredAt) {
			t.Errorf("sự kiện %d trước sự kiện %d", i, i-1)
		}
	}
	if echoes != 5 {
		t.Errorf("%d echo, muốn 5", echoes)
	}
	q := e.fg.Calls(testPageID + "/conversations")[0].Query
	if q.Get("user_id") != testPSID || !strings.Contains(q.Get("fields"), "messages.limit(20)") {
		t.Errorf("query lịch sử = %v", q)
	}
}

// cannedGraph là máy Graph nhỏ trả JSON cố định theo đường dẫn (không tiền tố phiên bản) cho các đường fakegraph chưa
// phục vụ (đổi token dài hạn, feed kèm comment); ghi lại query.
type cannedGraph struct {
	srv *httptest.Server
	mu  sync.Mutex
	got map[string][]map[string][]string
}

func newCannedGraph(t *testing.T, routes map[string]func(q map[string][]string) string) *cannedGraph {
	t.Helper()
	c := &cannedGraph{got: map[string][]map[string][]string{}}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/v26.0/")
		q := r.URL.Query()
		c.mu.Lock()
		c.got[path] = append(c.got[path], q)
		c.mu.Unlock()
		f, ok := routes[path]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":100,"message":"unknown path"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(f(q)))
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *cannedGraph) queries(path string) []map[string][]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.got[path]
}

func (c *cannedGraph) channel() *facebook.Channel {
	return facebook.New(facebook.Options{
		BaseURL: c.srv.URL, Version: "v26.0", AppID: testAppID, AppSecret: logging.Secret(testAppSecret),
		OAuthRedirectURL: "https://crm.example/oauth/facebook", HTTP: c.srv.Client(),
	})
}

// Backfill: tin cập nhật sau since từ Conversations API (cũ hơn since bỏ, dừng khi hội thoại cũ hơn since) và comment
// sau since từ feed; khóa chống trùng như webhook; giãn tối đa 2 lời gọi mỗi giây mỗi Page.
func TestBackfill(t *testing.T) {
	t.Parallel()
	since := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	ts := func(d time.Duration) string { return since.Add(d).Format("2006-01-02T15:04:05-0700") }
	msg := func(id, from, to string, d time.Duration) string {
		return `{"id":"` + id + `","message":"x","created_time":"` + ts(d) + `","from":{"id":"` + from + `"},"to":{"data":[{"id":"` + to + `"}]}}`
	}
	cg := newCannedGraph(t, map[string]func(map[string][]string) string{
		testPageID + "/conversations": func(q map[string][]string) string {
			if len(q["after"]) == 0 {
				return `{"data":[{"id":"t_1","updated_time":"` + ts(30*time.Minute) + `","messages":{"data":[` +
					msg("m_new_2", testPageID, testPSID, 30*time.Minute) + `,` + msg("m_new_1", testPSID, testPageID, 20*time.Minute) + `,` +
					msg("m_old", testPSID, testPageID, -time.Minute) + `]}}],"paging":{"cursors":{"after":"CUR2"},"next":"https://x"}}`
			}
			return `{"data":[{"id":"t_2","updated_time":"` + ts(-time.Hour) + `","messages":{"data":[` +
				msg("m_very_old", "7002", testPageID, -time.Hour) + `]}}],"paging":{"cursors":{"after":"CUR3"},"next":"https://x"}}`
		},
		testPageID + "/feed": func(map[string][]string) string {
			return `{"data":[{"id":"` + testPostID + `","comments":{"data":[{"id":"c_new","message":"Chốt","created_time":"` + ts(5*time.Minute) +
				`","from":{"id":"7003","name":"Mai"}},{"id":"c_reply","message":"Dạ","created_time":"` + ts(6*time.Minute) +
				`","from":{"id":"` + testPageID + `"},"parent":{"id":"c_new"}}]}}]}`
		},
	})
	ch := cg.channel()
	start := time.Now()
	var evs []webhooks.InboundEvent
	for ev, err := range ch.Backfill(t.Context(), outAccount(), outCred(), since) {
		if err != nil {
			t.Fatalf("Backfill: %v", err)
		}
		evs = append(evs, ev)
	}
	calls := len(cg.queries(testPageID+"/conversations")) + len(cg.queries(testPageID+"/feed"))
	if calls != 3 {
		t.Errorf("%d lời gọi, muốn 3 (2 trang hội thoại, trang 2 cũ hơn since thì dừng; 1 feed)", calls)
	}
	if took := time.Since(start); took < time.Second {
		t.Errorf("3 lời gọi xong trong %v, muốn ≥ 1 s (tối đa 2 lời gọi mỗi giây)", took)
	}
	var got []string
	for _, e := range evs {
		got = append(got, e.Type+" "+e.DedupeKey+" "+e.OrderKey)
	}
	want := []string{
		channel.EventMessageReceived + " m:m_new_1 fb:" + testPageID + ":" + testPSID,
		channel.EventMessageEcho + " m:m_new_2 fb:" + testPageID + ":" + testPSID,
		channel.EventCommentChanged + " c:c_new:add:" + textKey("Chốt") + " fbc:" + testPageID + ":" + testPostID + ":7003",
		channel.EventCommentChanged + " c:c_reply:add:" + textKey("Dạ") + " fbc:" + testPageID + ":" + testPostID + ":" + testPageID,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("sự kiện\n%s\nmuốn\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	var cp channel.CommentPayload
	_ = json.Unmarshal(evs[3].Payload, &cp)
	if cp.ParentID != "c_new" || !cp.FromPage || cp.Verb != "add" || cp.PostID != testPostID {
		t.Errorf("payload comment trả lời = %+v", cp)
	}
	if f := cg.queries(testPageID + "/feed")[0]["fields"]; len(f) != 1 || !strings.Contains(f[0], "since("+strconv.FormatInt(since.Unix(), 10)+")") {
		t.Errorf("fields feed = %v", f)
	}
}

// Đọc comment định kỳ (Q4): đúng một lời gọi feed (không gọi hội thoại), comment sau since thành comment.changed khóa
// như webhook, comment không sau since bị bỏ; lỗi Graph → trả lỗi.
func TestPollPageComments(t *testing.T) {
	t.Parallel()
	since := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	ts := func(d time.Duration) string { return since.Add(d).Format("2006-01-02T15:04:05-0700") }
	cg := newCannedGraph(t, map[string]func(map[string][]string) string{
		testPageID + "/feed": func(map[string][]string) string {
			return `{"data":[{"id":"` + testPostID + `","comments":{"data":[{"id":"c_new","message":"Chốt","created_time":"` +
				ts(time.Minute) + `","from":{"id":"7003","name":"Mai"}},{"id":"c_old","message":"cũ","created_time":"` +
				ts(0) + `","from":{"id":"7004"}}]}}]}`
		},
	})
	evs, err := cg.channel().PollPageComments(t.Context(), outAccount(), outCred(), since)
	if err != nil {
		t.Fatalf("PollPageComments: %v", err)
	}
	if len(cg.queries(testPageID+"/conversations")) != 0 || len(cg.queries(testPageID+"/feed")) != 1 {
		t.Errorf("lời gọi: hội thoại %d, feed %d; muốn 0, 1", len(cg.queries(testPageID+"/conversations")), len(cg.queries(testPageID+"/feed")))
	}
	if len(evs) != 1 || evs[0].Type != channel.EventCommentChanged || evs[0].DedupeKey != "c:c_new:add:"+textKey("Chốt") ||
		evs[0].OrderKey != "fbc:"+testPageID+":"+testPostID+":7003" {
		t.Fatalf("sự kiện = %+v", evs)
	}
	e := newOutEnv(t)
	e.fg.RevokeToken(testToken)
	if _, err := e.ch.PollPageComments(t.Context(), e.acc, e.cred, since); e.ch.ClassifyError(err) != channel.ErrorAuth {
		t.Errorf("token thu hồi: %v", err)
	}
}

// Backfill gặp lỗi Graph → trả lỗi một lần rồi dừng.
func TestBackfill_Error(t *testing.T) {
	t.Parallel()
	e := newOutEnv(t)
	e.fg.RevokeToken(testToken)
	n := 0
	for _, err := range e.ch.Backfill(t.Context(), e.acc, e.cred, time.Now().Add(-time.Hour)) {
		n++
		if e.ch.ClassifyError(err) != channel.ErrorAuth {
			t.Errorf("lỗi = %v", err)
		}
	}
	if n != 1 {
		t.Errorf("%d phần tử, muốn đúng 1 lỗi", n)
	}
}

func outAccount() channel.ChannelAccount {
	return channel.ChannelAccount{Channel: facebook.Key, ExternalID: testPageID}
}

func outCred() channel.Credentials { return channel.Credentials{AccessToken: testToken} }

// Connector: đổi mã lấy token người dùng rồi đổi token dài hạn; liệt kê Page kèm quyền nhắn, quản lý; lấy token Page.
func TestConnector(t *testing.T) {
	t.Parallel()
	cg := newCannedGraph(t, map[string]func(map[string][]string) string{
		"oauth/access_token": func(q map[string][]string) string {
			if len(q["code"]) > 0 {
				return `{"access_token":"EAAB-short-1","token_type":"bearer","expires_in":3600}`
			}
			return `{"access_token":"EAAB-long-1","token_type":"bearer","expires_in":5183944}`
		},
	})
	tok, err := cg.channel().ExchangeCode(t.Context(), "code-123")
	if err != nil || tok != "EAAB-long-1" {
		t.Fatalf("ExchangeCode: %q, %v", tok, err)
	}
	q := cg.queries("oauth/access_token")
	if len(q) != 2 || q[0]["code"][0] != "code-123" || q[0]["redirect_uri"][0] != "https://crm.example/oauth/facebook" ||
		q[0]["client_id"][0] != testAppID || q[1]["grant_type"][0] != "fb_exchange_token" || q[1]["fb_exchange_token"][0] != "EAAB-short-1" {
		t.Errorf("query đổi mã = %v", q)
	}

	e := newOutEnv(t)
	e.fg.AddPage(fakegraph.Page{ID: "1000222", Name: "Shop hai", Token: "EAAB-page-2"})
	e.fg.AddUser(fakegraph.User{Code: "c1", Token: "EAAB-user-1", PageIDs: []string{testPageID, "1000222"}})
	pages, err := e.ch.ListPages(t.Context(), "EAAB-user-1")
	if err != nil || len(pages) != 2 || pages[0].PageID != testPageID || pages[0].Name != "Shop thử" || !pages[0].CanMessage || !pages[0].CanModerate {
		t.Errorf("ListPages: %+v, %v", pages, err)
	}
	pt, info, err := e.ch.PageToken(t.Context(), "EAAB-user-1", "1000222")
	if err != nil || pt != "EAAB-page-2" || info.PageID != "1000222" || info.Name != "Shop hai" {
		t.Errorf("PageToken: %q, %+v, %v", pt, info, err)
	}
	if _, err := e.ch.ExchangeCode(t.Context(), "sai"); err == nil {
		t.Error("mã sai phải lỗi")
	}
}
