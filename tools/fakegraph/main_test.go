package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
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
