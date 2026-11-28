package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// nhanhFake trả trang trống cho mọi đường, riêng customer/list có 2 khách bịa; ghi lại đường đã gọi.
func nhanhFake(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, strings.TrimPrefix(r.URL.Path, "/"))
		mu.Unlock()
		if r.Header.Get("Authorization") != "tok-test-xyz" {
			_, _ = w.Write([]byte(`{"code":0,"errorCode":"ERR_INVALID_ACCESS_TOKEN"}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "customer/list") {
			_, _ = w.Write([]byte(`{"code":1,"data":[{"id":1,"name":"Khách Giả A","mobile":"0900000001"},{"id":2,"name":"Khách Giả B"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":1,"data":[]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), paths...) }
}

func nhanhEnv(srv *httptest.Server, dir string) func(string) string {
	m := map[string]string{
		envNhanhAppID: "1", envNhanhBusinessID: "2", envNhanhToken: "tok-test-xyz", envNhanhBaseURL: srv.URL, envNhanhDir: dir,
	}
	return func(k string) string { return m[k] }
}

// Chạy trọn: ghi tệp 0600, in chỉ số đếm (không tên, SĐT, token), xóa bản chụp cũ hơn 30 ngày.
func TestNhanhSnapshot_WritesPrivateFileAndPrintsCountsOnly(t *testing.T) {
	t.Parallel()
	srv, called := nhanhFake(t)
	dir := t.TempDir()
	old := filepath.Join(dir, "nhanh-snapshot-20200101T000000Z.json")
	if err := os.WriteFile(old, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := runNhanhSnapshot(t.Context(), nil, nhanhEnv(srv, dir), &out, &errb)
	if code != exitOK {
		t.Fatalf("mã thoát %d: %s", code, errb.String())
	}
	for _, bad := range []string{"Khách Giả", "0900000001", "tok-test-xyz"} {
		if strings.Contains(out.String()+errb.String(), bad) {
			t.Errorf("đầu ra chứa %q", bad)
		}
	}
	if !strings.Contains(out.String(), "khách: 2") || !strings.Contains(out.String(), "trường thiếu: 1") ||
		!strings.Contains(out.String(), "đã xóa: 1") {
		t.Errorf("tóm tắt sai: %s", out.String())
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("bản chụp cũ chưa bị xóa")
	}
	ents, _ := filepath.Glob(filepath.Join(dir, "nhanh-snapshot-*.json"))
	if len(ents) != 1 {
		t.Fatalf("tệp bản chụp = %v", ents)
	}
	if fi, _ := os.Stat(ents[0]); fi.Mode().Perm() != 0o600 {
		t.Errorf("quyền %v", fi.Mode().Perm())
	}
	b, _ := os.ReadFile(ents[0])
	var snap struct {
		Version   int               `json:"version"`
		Customers []json.RawMessage `json:"customers"`
	}
	if err := json.Unmarshal(b, &snap); err != nil || snap.Version != 1 || len(snap.Customers) != 2 {
		t.Errorf("tệp: %v %+v", err, snap)
	}
	for _, p := range called() {
		if p != "product/list" && p != "customer/list" {
			t.Errorf("gọi đường %q", p)
		}
	}
}

// Thiếu biến môi trường, tham số sai, quá 50: mã thoát cấu hình, không gọi mạng, không ghi tệp.
func TestNhanhSnapshot_BadUsageCallsNothing(t *testing.T) {
	t.Parallel()
	srv, called := nhanhFake(t)
	for name, tc := range map[string]struct {
		args []string
		env  func(string) string
	}{
		"không token":  {nil, func(k string) string { return map[string]string{envNhanhAppID: "1", envNhanhBusinessID: "2"}[k] }},
		"limit 51":     {[]string{"--limit", "51"}, nhanhEnv(srv, t.TempDir())},
		"mã không số":  {[]string{"--product-ids", "1,x"}, nhanhEnv(srv, t.TempDir())},
		"since sai":    {[]string{"--since", "hôm qua"}, nhanhEnv(srv, t.TempDir())},
		"tham số thừa": {[]string{"abc"}, nhanhEnv(srv, t.TempDir())},
	} {
		var out, errb bytes.Buffer
		if code := runNhanhSnapshot(t.Context(), tc.args, tc.env, &out, &errb); code != exitConfig {
			t.Errorf("%s: mã thoát %d (%s)", name, code, errb.String())
		}
	}
	if got := called(); len(got) != 0 {
		t.Errorf("đã gọi mạng: %v", got)
	}
}

// Token sai: lỗi API, mã thoát lỗi, không tệp bản chụp.
func TestNhanhSnapshot_APIErrorWritesNothing(t *testing.T) {
	t.Parallel()
	srv, _ := nhanhFake(t)
	dir := t.TempDir()
	env := nhanhEnv(srv, dir)
	bad := func(k string) string {
		if k == envNhanhToken {
			return "sai"
		}
		return env(k)
	}
	var out, errb bytes.Buffer
	if code := runNhanhSnapshot(t.Context(), nil, bad, &out, &errb); code != exitFail {
		t.Fatalf("mã thoát %d", code)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("có tệp: %v", ents)
	}
}

func TestParseSince(t *testing.T) {
	t.Parallel()
	a, _ := parseSince("1700000000")
	b, _ := parseSince("2023-11-14T22:13:20Z")
	if !a.Equal(b) || !a.Equal(time.Unix(1_700_000_000, 0)) {
		t.Errorf("%v %v", a, b)
	}
}
