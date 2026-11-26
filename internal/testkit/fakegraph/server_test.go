package fakegraph_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

const (
	appID     = "1626301872631379"
	appSecret = "0123456789abcdef0123456789abcdef"
	pageID    = "1000111"
	pageToken = "EAAB-page-1"
	userToken = "EAAB-user-1"
)

type graphReply struct {
	Status int
	Header http.Header
	Body   map[string]any
}

func proof(token string) string {
	m := hmac.New(sha256.New, []byte(appSecret))
	m.Write([]byte(token))
	return hex.EncodeToString(m.Sum(nil))
}

func newServer(t *testing.T) *fakegraph.Server {
	t.Helper()
	fg := fakegraph.New(t, fakegraph.Options{AppID: appID, AppSecret: appSecret})
	fg.AddPage(fakegraph.Page{ID: pageID, Name: "Shop thử", Token: pageToken})
	fg.AddUser(fakegraph.User{Code: "code-1", Token: userToken, PageIDs: []string{pageID}})
	return fg
}

// call gửi một yêu cầu Graph có token (header Bearer) và appsecret_proof đúng.
func call(t *testing.T, fg *fakegraph.Server, method, path, token string, q url.Values, body any) graphReply {
	t.Helper()
	p := ""
	if token != "" {
		p = proof(token)
	}
	return send(t, fg, method, path, token, p, q, body)
}

// send gửi một yêu cầu Graph với appsecret_proof cho sẵn (rỗng: không gửi).
func send(t *testing.T, fg *fakegraph.Server, method, path, token, p string, q url.Values, body any) graphReply {
	t.Helper()
	if q == nil {
		q = url.Values{}
	}
	if p != "" {
		q.Set("appsecret_proof", p)
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequestWithContext(t.Context(), method, fg.URL()+"/v26.0/"+path+"?"+q.Encode(), rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := fg.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	out := graphReply{Status: resp.StatusCode, Header: resp.Header, Body: map[string]any{}}
	_ = json.NewDecoder(resp.Body).Decode(&out.Body)
	return out
}

func errCode(r graphReply) float64 {
	e, _ := r.Body["error"].(map[string]any)
	c, _ := e["code"].(float64)
	return c
}

func TestServer_ProofAndToken(t *testing.T) {
	t.Parallel()
	fg := newServer(t)
	if r := call(t, fg, http.MethodGet, pageID, pageToken, url.Values{"fields": {"name"}}, nil); r.Status != 200 || r.Body["name"] != "Shop thử" {
		t.Fatalf("đọc Page: %+v", r)
	}
	if code := errCode(send(t, fg, http.MethodGet, pageID, pageToken, "sai", nil, nil)); code != 100 {
		t.Fatalf("proof sai: mã %v, mong 100", code)
	}
	if r := call(t, fg, http.MethodGet, pageID, "", nil, nil); errCode(r) != 104 {
		t.Fatalf("thiếu token: %+v", r)
	}
	fg.RevokeToken(pageToken)
	if r := call(t, fg, http.MethodGet, pageID, pageToken, nil, nil); errCode(r) != 190 {
		t.Fatalf("token thu hồi: %+v, mong 190", r)
	}
}

func TestServer_SendCommentSubscribe(t *testing.T) {
	t.Parallel()
	fg := newServer(t)
	fg.AddPost(fakegraph.Post{ID: pageID + "_55", PageID: pageID, Message: "Áo mới", Created: time.Now()})
	fg.AddComment(fakegraph.Comment{ID: "55_9", ParentID: pageID + "_55", FromID: "7001", FromName: "Lan", Message: "giá?", Created: time.Now()})

	r := call(t, fg, http.MethodPost, pageID+"/messages", pageToken, nil, map[string]any{
		"recipient": map[string]any{"id": "7001"}, "message": map[string]any{"text": "chào chị", "metadata": "crm:1"},
	})
	if r.Status != 200 || r.Body["recipient_id"] != "7001" || r.Body["message_id"] == "" {
		t.Fatalf("gửi tin: %+v", r)
	}
	if ms := fg.Messages(); len(ms) != 1 || ms[0].Text != "chào chị" || ms[0].Metadata != "crm:1" {
		t.Fatalf("tin đã gửi: %+v", ms)
	}
	private := map[string]any{"recipient": map[string]any{"comment_id": "55_9"}, "message": map[string]any{"text": "inbox chị"}}
	if r := call(t, fg, http.MethodPost, pageID+"/messages", pageToken, nil, private); r.Body["recipient_id"] != "7001" {
		t.Fatalf("nhắn riêng: %+v", r)
	}
	if r := call(t, fg, http.MethodPost, pageID+"/messages", pageToken, nil, private); errCode(r) != 10900 {
		t.Fatalf("nhắn riêng lần hai: %+v, mong 10900", r)
	}
	r = call(t, fg, http.MethodPost, "55_9/comments", pageToken, nil, map[string]any{"message": "dạ 200k"})
	replyID, _ := r.Body["id"].(string)
	if c, ok := fg.CommentByID(replyID); !ok || c.Message != "dạ 200k" || c.FromID != pageID {
		t.Fatalf("trả lời comment: %+v %+v", r, c)
	}
	if r := call(t, fg, http.MethodPost, "55_9", pageToken, nil, map[string]any{"is_hidden": true}); r.Body["success"] != true {
		t.Fatalf("ẩn comment: %+v", r)
	}
	if c, _ := fg.CommentByID("55_9"); !c.Hidden {
		t.Fatal("comment chưa ẩn")
	}
	call(t, fg, http.MethodPost, pageID+"/subscribed_apps", pageToken, url.Values{"subscribed_fields": {"messages,feed"}}, nil)
	if got := fg.Subscribed(pageID); strings.Join(got, ",") != "messages,feed" {
		t.Fatalf("đăng ký: %v", got)
	}
	r = call(t, fg, http.MethodGet, pageID+"/subscribed_apps", pageToken, nil, nil)
	if data, _ := r.Body["data"].([]any); len(data) != 1 {
		t.Fatalf("GET subscribed_apps: %+v", r)
	}
	call(t, fg, http.MethodDelete, pageID+"/subscribed_apps", pageToken, nil, nil)
	if fg.Subscribed(pageID) != nil {
		t.Fatal("hủy đăng ký chưa xóa")
	}
}

func TestServer_OAuthAccountsDebugToken(t *testing.T) {
	t.Parallel()
	fg := newServer(t)
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, fg.URL()+"/v26.0/oauth/access_token?"+url.Values{
		"client_id": {appID}, "client_secret": {appSecret}, "code": {"code-1"}, "redirect_uri": {"https://crm/cb"},
	}.Encode(), nil)
	resp, err := fg.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var tok map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&tok)
	_ = resp.Body.Close()
	if tok["access_token"] != userToken {
		t.Fatalf("đổi mã: %+v", tok)
	}
	r := call(t, fg, http.MethodGet, "me/accounts", userToken, nil, nil)
	data, _ := r.Body["data"].([]any)
	if len(data) != 1 || data[0].(map[string]any)["access_token"] != pageToken {
		t.Fatalf("me/accounts: %+v", r)
	}
	app := appID + "|" + appSecret
	r = call(t, fg, http.MethodGet, "debug_token", app, url.Values{"input_token": {pageToken}}, nil)
	d, _ := r.Body["data"].(map[string]any)
	if d["is_valid"] != true || d["type"] != "PAGE" || d["profile_id"] != pageID {
		t.Fatalf("debug_token: %+v", r)
	}
	fg.RevokeToken(pageToken)
	r = call(t, fg, http.MethodGet, "debug_token", app, url.Values{"input_token": {pageToken}}, nil)
	if d, _ := r.Body["data"].(map[string]any); d["is_valid"] != false {
		t.Fatalf("debug_token sau thu hồi: %+v", r)
	}
}

func TestServer_ReadObjects(t *testing.T) {
	t.Parallel()
	fg := newServer(t)
	at := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
	fg.AddProfile(fakegraph.Profile{PSID: "7001", FirstName: "Lan", LastName: "Nguyễn"})
	fg.AddPost(fakegraph.Post{ID: pageID + "_55", PageID: pageID, Message: "Áo", PermalinkURL: "https://fb/p/55", Created: at})
	fg.AddInboundMessage(pageID, "7001", "m_in1", "còn size M?", at)
	fg.AddLiveVideo(fakegraph.LiveVideo{ID: "lv1", PageID: pageID, VideoID: "v1", Status: "LIVE", Title: "Live tối"})
	fg.PushLiveComment("lv1", fakegraph.Comment{ID: "lc1", FromID: "7002", Message: "chốt 1", Created: at})
	fg.PushLiveComment("lv1", fakegraph.Comment{ID: "lc2", FromID: "7003", Message: "chốt 2", Created: at.Add(time.Minute)})

	if r := call(t, fg, http.MethodGet, "7001", pageToken, nil, nil); r.Body["name"] != "Lan Nguyễn" {
		t.Fatalf("hồ sơ: %+v", r)
	}
	if r := call(t, fg, http.MethodGet, pageID+"_55", pageToken, nil, nil); r.Body["permalink_url"] != "https://fb/p/55" {
		t.Fatalf("bài: %+v", r)
	}
	r := call(t, fg, http.MethodGet, pageID+"/conversations", pageToken, url.Values{"user_id": {"7001"}}, nil)
	conv := r.Body["data"].([]any)[0].(map[string]any)
	msgs := conv["messages"].(map[string]any)["data"].([]any)
	if len(msgs) != 1 || msgs[0].(map[string]any)["message"] != "còn size M?" {
		t.Fatalf("hội thoại: %+v", r)
	}
	fg.AddComment(fakegraph.Comment{ID: "55_1", ParentID: pageID + "_55", FromID: "7001", Message: "giá?", Created: at})
	if r := call(t, fg, http.MethodGet, pageID+"_55/comments", pageToken, nil, nil); len(r.Body["data"].([]any)) != 1 {
		t.Fatalf("comment của bài: %+v", r)
	}
	if r := call(t, fg, http.MethodGet, pageID+"/feed", pageToken, nil, nil); len(r.Body["data"].([]any)) != 1 {
		t.Fatalf("feed: %+v", r)
	}
	if r := call(t, fg, http.MethodGet, pageID+"/live_videos", pageToken, nil, nil); len(r.Body["data"].([]any)) != 1 {
		t.Fatalf("live_videos: %+v", r)
	}
	fg.SetLiveStatus("lv1", "LIVE_STOPPED")
	if r := call(t, fg, http.MethodGet, "lv1", pageToken, nil, nil); r.Body["status"] != "LIVE_STOPPED" {
		t.Fatalf("trạng thái live: %+v", r)
	}
	q := url.Values{"filter": {"stream"}, "since": {strconv.FormatInt(at.Unix(), 10)}, "summary": {"total_count"}}
	r = call(t, fg, http.MethodGet, "lv1/comments", pageToken, q, nil)
	sum := r.Body["summary"].(map[string]any)
	if len(r.Body["data"].([]any)) != 1 || sum["total_count"] != float64(2) {
		t.Fatalf("comment live sau since: %+v", r)
	}
}

func TestServer_FaultsAndUsage(t *testing.T) {
	t.Parallel()
	fg := newServer(t)
	fg.SetUsagePct(85)
	fg.FailNext(pageID, fakegraph.GraphErrorSpec{Code: 613, Type: "OAuthException", Message: "Calls exceeded", RegainMinutes: 5})
	resp := call(t, fg, http.MethodGet, pageID, pageToken, nil, nil)
	if resp.Status != 400 || !strings.Contains(resp.Header.Get("X-Business-Use-Case-Usage"), `"estimated_time_to_regain_access":5`) ||
		!strings.Contains(resp.Header.Get("X-App-Usage"), `"call_count":85`) || errCode(resp) != 613 {
		t.Fatalf("lỗi 613: %+v", resp)
	}
	if r := call(t, fg, http.MethodGet, pageID, pageToken, nil, nil); r.Status != 200 {
		t.Fatalf("FailNext chỉ dùng một lần: %+v", r)
	}
	if n := len(fg.Calls(pageID)); n != 2 {
		t.Fatalf("Calls = %d, mong 2", n)
	}
}

func TestServer_LiveStream(t *testing.T) {
	t.Parallel()
	fg := newServer(t)
	fg.AddLiveVideo(fakegraph.LiveVideo{ID: "lv1", PageID: pageID, Status: "LIVE"})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	q := url.Values{"appsecret_proof": {proof(pageToken)}, "access_token": {pageToken}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fg.URL()+"/lv1/live_comments?"+q.Encode(), nil)
	resp, err := fg.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type %q", ct)
	}
	for fg.StreamCount("lv1") == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	fg.PushLiveComment("lv1", fakegraph.Comment{ID: "lc1", FromID: "7002", Message: "chốt áo", Created: time.Now()})
	sc := bufio.NewScanner(resp.Body)
	var line string
	for sc.Scan() {
		if line = sc.Text(); strings.HasPrefix(line, "data: ") {
			break
		}
	}
	if !strings.Contains(line, `"message":"chốt áo"`) {
		t.Fatalf("dòng SSE: %q", line)
	}
	fg.DropStream("lv1")
	_, _ = io.Copy(io.Discard, resp.Body) // đọc tới khi luồng đóng
	if fg.StreamCount("lv1") != 0 {
		t.Fatal("luồng chưa đóng")
	}
}

func TestServer_EmitWebhookSigned(t *testing.T) {
	t.Parallel()
	fg := newServer(t)
	var gotSig string
	var gotBody []byte
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Hub-Signature-256")
		gotBody, _ = io.ReadAll(r.Body)
	}))
	defer target.Close()
	payload := []byte(`{"object":"page","entry":[]}`)
	status, err := fg.EmitWebhook(t.Context(), target.URL+"/hooks/facebook", payload)
	if err != nil || status != 200 {
		t.Fatalf("gửi webhook: %d %v", status, err)
	}
	m := hmac.New(sha256.New, []byte(appSecret))
	m.Write(payload)
	if gotSig != "sha256="+hex.EncodeToString(m.Sum(nil)) || !bytes.Equal(gotBody, payload) {
		t.Fatalf("chữ ký %q thân %q", gotSig, gotBody)
	}
}

// Đọc một Page bằng token người dùng quản trị (đường "mã system user" của CRM, FetchPageToken) trả token Page và vai
// trò tasks như me/accounts; thiếu tasks thì CRM coi Page không đủ quyền nhắn, kiểm duyệt (Task 180).
func TestServer_PageTokenWithTasks(t *testing.T) {
	t.Parallel()
	fg := newServer(t)
	r := call(t, fg, http.MethodGet, pageID, userToken, url.Values{"fields": {"access_token,name,tasks"}}, nil)
	tasks, _ := r.Body["tasks"].([]any)
	if r.Body["access_token"] != pageToken || !slices.Contains(tasks, any("MESSAGING")) || !slices.Contains(tasks, any("MODERATE")) {
		t.Fatalf("Page đọc bằng token người dùng: %+v", r)
	}
}
