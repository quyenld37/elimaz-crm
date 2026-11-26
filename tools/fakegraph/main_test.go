package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadConfig(t *testing.T) {
	t.Parallel()
	if _, err := loadConfig(env(map[string]string{"CRM_ENV": "production"})); !errors.Is(err, errProduction) {
		t.Fatalf("production: %v", err)
	}
	if _, err := loadConfig(env(map[string]string{"FAKEGRAPH_APP_SECRET": "ngắn"})); !errors.Is(err, errBadSecret) {
		t.Fatalf("secret ngắn: %v", err)
	}
	if _, err := loadConfig(env(map[string]string{"PORT": "0"})); err == nil {
		t.Fatal("PORT 0 phải lỗi")
	}
	c, err := loadConfig(env(map[string]string{"CRM_ENV": "dev", "PORT": "4300", "FAKEGRAPH_PAGE_ID": "42"}))
	if err != nil || c.port != 4300 || c.pageID != "42" || c.secret != devAppSecret {
		t.Fatalf("cấu hình: %+v %v", c, err)
	}
	if d, _ := loadConfig(env(nil)); d.port != defaultPort {
		t.Fatalf("cổng mặc định %d", d.port)
	}
}

func get(t *testing.T, srv *httptest.Server, path, token string) (int, string) {
	t.Helper()
	m := hmac.New(sha256.New, []byte(devAppSecret))
	m.Write([]byte(token))
	q := url.Values{"appsecret_proof": {hex.EncodeToString(m.Sum(nil))}}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/v26.0/"+path+"?"+q.Encode(), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func postAdmin(t *testing.T, srv *httptest.Server, path, body string) int {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+path, strings.NewReader(body))
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func TestServer_DevSeedAndAdmin(t *testing.T) {
	t.Parallel()
	c, _ := loadConfig(env(nil))
	srv := httptest.NewServer(newServer(c))
	defer srv.Close()
	if st, body := get(t, srv, defaultPageID, devPageToken); st != 200 || !strings.Contains(body, "Shop dev") {
		t.Fatalf("Page dev: %d %s", st, body)
	}
	if st := postAdmin(t, srv, "/_fake/fail-next", `{"path":"`+defaultPageID+`","code":613,"regainMinutes":5}`); st != 200 {
		t.Fatalf("fail-next: %d", st)
	}
	if _, body := get(t, srv, defaultPageID, devPageToken); !strings.Contains(body, `"code":613`) {
		t.Fatalf("lỗi đã đặt: %s", body)
	}
	postAdmin(t, srv, "/_fake/revoke", `{"token":"`+devPageToken+`"}`)
	if _, body := get(t, srv, defaultPageID, devPageToken); !strings.Contains(body, `"code":190`) {
		t.Fatalf("sau thu hồi: %s", body)
	}
	if st := postAdmin(t, srv, "/_fake/usage", `{pct`); st != http.StatusBadRequest {
		t.Fatalf("JSON hỏng: %d", st)
	}
	resp, err := srv.Client().Get(srv.URL + "/_fake/calls?path=" + defaultPageID) //nolint:noctx // test ngắn
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if strings.Count(string(b), `"Method":"GET"`) != 3 {
		t.Fatalf("calls: %s", b)
	}
}

// Bài thử tải M4 (Task 180): nhiều Page, 2.000 PSID có hồ sơ, địa chỉ nghe đổi được; sai giới hạn → lỗi nêu tên biến.
func TestLoadConfig_LoadTest(t *testing.T) {
	t.Parallel()
	c, err := loadConfig(env(map[string]string{"FAKEGRAPH_PAGES": "2", "FAKEGRAPH_PSIDS": "2000", "FAKEGRAPH_LISTEN": "0.0.0.0"}))
	if err != nil || c.pages != 2 || c.psids != 2000 || c.listen != "0.0.0.0" {
		t.Fatalf("cấu hình: %+v %v", c, err)
	}
	if d, _ := loadConfig(env(nil)); d.pages != 1 || d.psids != 0 || d.listen != "127.0.0.1" {
		t.Fatalf("mặc định: %+v", d)
	}
	for k, v := range map[string]string{"FAKEGRAPH_PAGES": "0", "FAKEGRAPH_PSIDS": "100001"} {
		if _, err := loadConfig(env(map[string]string{k: v})); err == nil || !strings.Contains(err.Error(), k) {
			t.Errorf("%s=%s: %v", k, v, err)
		}
	}
	if _, err := loadConfig(env(map[string]string{"FAKEGRAPH_PAGES": "2", "FAKEGRAPH_PAGE_ID": "abc"})); err == nil {
		t.Error("nhiều Page cần FAKEGRAPH_PAGE_ID là số")
	}
}

func TestServer_PagesAndProfiles(t *testing.T) {
	t.Parallel()
	c, _ := loadConfig(env(map[string]string{"FAKEGRAPH_PAGES": "2", "FAKEGRAPH_PSIDS": "3"}))
	srv := httptest.NewServer(newServer(c))
	defer srv.Close()
	if st, body := get(t, srv, "100000000000002", pageToken(1)); st != 200 || !strings.Contains(body, "Shop dev 2") {
		t.Fatalf("Page thứ hai: %d %s", st, body)
	}
	if st, body := get(t, srv, "me/accounts", devUserToken); st != 200 || strings.Count(body, `"access_token"`) != 2 {
		t.Fatalf("người dùng dev quản trị cả hai Page: %d %s", st, body)
	}
	if st, body := get(t, srv, psid(3), pageToken(0)); st != 200 || !strings.Contains(body, "Khách") {
		t.Fatalf("hồ sơ PSID: %d %s", st, body)
	}
	if st, _ := get(t, srv, psid(4), pageToken(0)); st == 200 {
		t.Fatal("PSID ngoài số đã khai không có hồ sơ")
	}
}

// Sổ mở luồng: mỗi lần CRM mở /{video}/live_comments ghi lúc mở và lúc đóng (đo "luồng live nhận lại trong 15 giây").
func TestServer_StreamLog(t *testing.T) {
	t.Parallel()
	c, _ := loadConfig(env(nil))
	srv := httptest.NewServer(newServer(c))
	defer srv.Close()
	if st := postAdmin(t, srv, "/_fake/live-video", `{"videoId":"55","pageId":"`+defaultPageID+`","status":"LIVE"}`); st != 200 {
		t.Fatalf("live-video: %d", st)
	}
	m := hmac.New(sha256.New, []byte(devAppSecret))
	m.Write([]byte(devPageToken))
	ctx, cancel := context.WithCancel(t.Context())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/55/live_comments?appsecret_proof="+hex.EncodeToString(m.Sum(nil)), nil)
	req.Header.Set("Authorization", "Bearer "+devPageToken)
	resp, err := srv.Client().Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("mở luồng: %v", err)
	}
	logs := streams(t, srv)
	if len(logs) != 1 || logs[0].Video != "55" || logs[0].Opened.IsZero() || logs[0].Closed != nil {
		t.Fatalf("đang mở: %+v", logs)
	}
	cancel()
	_ = resp.Body.Close()
	deadline := time.Now().Add(5 * time.Second)
	for logs = streams(t, srv); logs[0].Closed == nil && time.Now().Before(deadline); logs = streams(t, srv) {
		time.Sleep(10 * time.Millisecond)
	}
	if logs[0].Closed == nil || logs[0].Closed.Before(logs[0].Opened) {
		t.Fatalf("đã đóng: %+v", logs)
	}
}

func streams(t *testing.T, srv *httptest.Server) []streamLog {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + "/_fake/streams") //nolint:noctx // test ngắn
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out []streamLog
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}
