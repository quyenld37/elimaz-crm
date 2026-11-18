package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEvaluate(t *testing.T) {
	ok := func(n string) Result { return Result{Target: n, OK: true} }
	bad := func(n string) Result { return Result{Target: n, OK: false} }
	if alert, f := Evaluate([][]Result{{ok("hooks"), bad("deep")}}); alert || len(f) != 0 {
		t.Fatalf("hỏng một lượt không báo: %v %v", alert, f)
	}
	if alert, f := Evaluate([][]Result{{ok("hooks"), bad("deep")}, {ok("hooks"), ok("deep")}}); alert || len(f) != 0 {
		t.Fatalf("lượt hai đạt không báo: %v %v", alert, f)
	}
	alert, f := Evaluate([][]Result{{bad("hooks"), bad("deep")}, {ok("hooks"), bad("deep")}})
	if !alert || !slices.Equal(f, []string{"deep"}) {
		t.Fatalf("deep hỏng hai lượt phải báo và nêu tên: %v %v", alert, f)
	}
	alert, f = Evaluate([][]Result{{bad("hooks"), bad("deep")}, {bad("hooks"), bad("deep")}})
	if !alert || !slices.Equal(f, []string{"deep", "hooks"}) {
		t.Fatalf("hai đích: %v %v", alert, f)
	}
	if alert, _ := Evaluate(nil); alert {
		t.Fatal("không lượt nào không báo")
	}
}

type fakeTargets struct {
	deepStatus atomic.Int32
	deepAuth   atomic.Value
	beats      atomic.Int32
	mu         sync.Mutex
	messages   []string
}

func newTargets(t *testing.T) (*fakeTargets, Config) {
	t.Helper()
	f := &fakeTargets{}
	f.deepStatus.Store(http.StatusOK)
	f.deepAuth.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/health":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/health/deep":
			f.deepAuth.Store(r.Header.Get("Authorization"))
			w.WriteHeader(int(f.deepStatus.Load()))
		case r.URL.Path == "/beat":
			f.beats.Add(1)
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			_ = r.ParseForm()
			f.mu.Lock()
			f.messages = append(f.messages, r.PostForm.Get("text"))
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return f, Config{
		HooksHealthURL: srv.URL + "/health", DeepHealthURL: srv.URL + "/health/deep", DeepHealthToken: "the-bi-mat",
		TelegramAPI: srv.URL, TelegramBotToken: "123:x", TelegramChatID: "chat", HeartbeatURL: srv.URL + "/beat",
		StateFile: filepath.Join(t.TempDir(), "state.json"), Gap: time.Millisecond, Timeout: 2 * time.Second,
	}
}

func (f *fakeTargets) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.messages)
}

// Hỏng hai lượt → một tin nêu tên đích; chạy lại trong 10 phút với cùng nội dung → không nhắn lại; mỗi lần chạy gửi
// nhịp sống; kiểm sâu mang thẻ Bearer.
func TestRunAlertsOnceAndBeats(t *testing.T) {
	f, cfg := newTargets(t)
	f.deepStatus.Store(http.StatusServiceUnavailable)
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	cfg.Now = func() time.Time { return now }
	for range 2 {
		if err := Run(context.Background(), cfg); err != nil {
			t.Fatalf("Run: %v", err)
		}
	}
	msgs := f.sent()
	if len(msgs) != 1 || !strings.Contains(msgs[0], "deep") || strings.Contains(msgs[0], "hooks") {
		t.Fatalf("tin: %q", msgs)
	}
	if got := f.deepAuth.Load(); got != "Bearer the-bi-mat" {
		t.Fatalf("Authorization = %q", got)
	}
	if n := f.beats.Load(); n != 2 {
		t.Fatalf("nhịp sống %d lần, muốn 2", n)
	}
	now = now.Add(11 * time.Minute)
	if err := Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if n := len(f.sent()); n != 2 {
		t.Fatalf("sau 10 phút phải nhắn lại, tổng %d", n)
	}
}

// Hỏng một lượt rồi lành → không nhắn.
func TestRunFlakyNoAlert(t *testing.T) {
	f, cfg := newTargets(t)
	var calls atomic.Int32
	f.deepStatus.Store(http.StatusServiceUnavailable)
	cfg.Sleep = func(context.Context, time.Duration) error {
		calls.Add(1)
		f.deepStatus.Store(http.StatusOK)
		return nil
	}
	if err := Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if n := len(f.sent()); n != 0 || calls.Load() != 1 {
		t.Fatalf("hỏng một lượt không nhắn: %d tin, %d lần chờ", n, calls.Load())
	}
	// Mọi thứ đạt: không chờ lượt hai.
	if err := Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("đạt ngay lượt đầu thì không chờ")
	}
}

func TestConfigFromEnv(t *testing.T) {
	m := map[string]string{
		"HOOKS_HEALTH_URL": "https://hooks.x/health", "DEEP_HEALTH_URL": "https://crm.x/health/deep",
		"DEEP_HEALTH_TOKEN": "t", "TELEGRAM_BOT_TOKEN": "b", "TELEGRAM_CHAT_ID": "c",
	}
	cfg, err := ConfigFromEnv(func(k string) string { return m[k] })
	if err != nil || cfg.Gap != 20*time.Second || cfg.TelegramAPI != "https://api.telegram.org" {
		t.Fatalf("cfg %+v %v", cfg, err)
	}
	if _, err := ConfigFromEnv(func(string) string { return "" }); err == nil ||
		!strings.Contains(err.Error(), "HOOKS_HEALTH_URL") || !strings.Contains(err.Error(), "TELEGRAM_CHAT_ID") {
		t.Fatalf("thiếu biến phải nêu tên: %v", err)
	}
}
