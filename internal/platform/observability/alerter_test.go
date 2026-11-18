package observability_test

import (
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/observability"
)

func tgConfig() config.Config {
	return config.Config{TelegramBotToken: "123:thu", TelegramChatID: "chat-thuong", TelegramUrgentChatID: "chat-gap"}
}

func quiet() *slog.Logger { return slog.New(slog.DiscardHandler) }

func rows(t *testing.T, e *env, key string) int {
	t.Helper()
	return e.count(t, "SELECT count(*) FROM platform.alert_events WHERE key = $1", key)
}

// normal: cùng key trong 10 phút chỉ gửi một lần; quá 10 phút gửi lại; mỗi lần gửi một dòng alert_events.
func TestNormalDedupTenMinutes(t *testing.T) {
	e := newEnv(t)
	tg, client := newTelegram(t)
	a := observability.NewTelegramAlerter(e.d, tgConfig(), client, quiet())
	for range 2 {
		if err := a.Notify(t.Context(), "dead:x", observability.SeverityNormal, "dead:x: 1 việc"); err != nil {
			t.Fatalf("Notify: %v", err)
		}
	}
	if n := len(tg.calls()); n != 1 {
		t.Fatalf("hai lần normal trong 10 phút phải gửi 1 lần, gửi %d", n)
	}
	if n := rows(t, e, "dead:x"); n != 1 {
		t.Fatalf("alert_events = %d dòng, muốn 1", n)
	}
	e.alertSent(t, "dead:y", 11*time.Minute)
	if err := a.Notify(t.Context(), "dead:y", observability.SeverityNormal, "dead:y"); err != nil {
		t.Fatal(err)
	}
	if n := len(tg.calls()); n != 2 {
		t.Fatalf("quá 10 phút phải gửi lại, tổng %d", n)
	}
	if n := rows(t, e, "dead:y"); n != 2 {
		t.Fatalf("alert_events dead:y = %d, muốn 2", n)
	}
}

// urgent: 29 phút sau lần gửi trước không gửi, 31 phút thì gửi lại.
func TestUrgentRepeatsEvery30Minutes(t *testing.T) {
	e := newEnv(t)
	tg, client := newTelegram(t)
	a := observability.NewTelegramAlerter(e.d, tgConfig(), client, quiet())
	e.alertSent(t, "queue-lag", 29*time.Minute)
	if err := a.Notify(t.Context(), "queue-lag", observability.SeverityUrgent, "queue-lag"); err != nil {
		t.Fatal(err)
	}
	if n := len(tg.calls()); n != 0 {
		t.Fatalf("29 phút không được gửi lại, gửi %d", n)
	}
	e.alertSent(t, "spool", 31*time.Minute)
	if err := a.Notify(t.Context(), "spool", observability.SeverityUrgent, "spool"); err != nil {
		t.Fatal(err)
	}
	if n := len(tg.calls()); n != 1 {
		t.Fatalf("31 phút phải gửi lại, gửi %d", n)
	}
	// urgent không chặn 10 phút như normal: 11 phút trước vẫn còn trong 30 phút → không gửi.
	e.alertSent(t, "listen-down", 11*time.Minute)
	if err := a.Notify(t.Context(), "listen-down", observability.SeverityUrgent, "x"); err != nil {
		t.Fatal(err)
	}
	if n := len(tg.calls()); n != 1 {
		t.Fatalf("urgent 11 phút trước không được gửi, tổng %d", n)
	}
}

// urgent tới chat gấp, normal tới chat thường; thiếu chat gấp thì urgent về chat thường. Đường dẫn mang token bot.
func TestChannelsPerSeverity(t *testing.T) {
	e := newEnv(t)
	tg, client := newTelegram(t)
	a := observability.NewTelegramAlerter(e.d, tgConfig(), client, quiet())
	if err := a.Notify(t.Context(), "k1", observability.SeverityUrgent, "gấp"); err != nil {
		t.Fatal(err)
	}
	if err := a.Notify(t.Context(), "k2", observability.SeverityNormal, "thường"); err != nil {
		t.Fatal(err)
	}
	cfg := tgConfig()
	cfg.TelegramUrgentChatID = ""
	if err := observability.NewTelegramAlerter(e.d, cfg, client, quiet()).
		Notify(t.Context(), "k3", observability.SeverityUrgent, "gấp 2"); err != nil {
		t.Fatal(err)
	}
	got := tg.calls()
	if len(got) != 3 || got[0].chatID != "chat-gap" || got[1].chatID != "chat-thuong" || got[2].chatID != "chat-thuong" {
		t.Fatalf("kênh: %+v", got)
	}
	for _, r := range got {
		if r.path != "/bot123:thu/sendMessage" {
			t.Fatalf("đường dẫn %q", r.path)
		}
	}
	var sev string
	if err := e.d.Pool.QueryRow(t.Context(), "SELECT severity FROM platform.alert_events WHERE key = 'k1'").Scan(&sev); err != nil || sev != "urgent" {
		t.Fatalf("severity = %q %v", sev, err)
	}
}

// Thiếu biến Telegram: không gọi mạng, không lỗi, vẫn ghi alert_events.
func TestNoTelegramOnlyRecords(t *testing.T) {
	e := newEnv(t)
	a := observability.NewTelegramAlerter(e.d, config.Config{}, &http.Client{Transport: failTransport{t}}, quiet())
	if err := a.Notify(t.Context(), "worker-heartbeat", observability.SeverityUrgent, "worker"); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if n := rows(t, e, "worker-heartbeat"); n != 1 {
		t.Fatalf("alert_events = %d, muốn 1", n)
	}
}

// Telegram lỗi: Notify lỗi, không ghi alert_events (lần sau gửi lại); lỗi không lộ token. Mức lạ bị từ chối.
func TestTelegramFailureNotRecorded(t *testing.T) {
	e := newEnv(t)
	tg, client := newTelegram(t)
	tg.set(http.StatusInternalServerError, `{"ok":false}`)
	a := observability.NewTelegramAlerter(e.d, tgConfig(), client, quiet())
	err := a.Notify(t.Context(), "k", observability.SeverityNormal, "x")
	if err == nil {
		t.Fatal("Telegram 500 phải lỗi")
	}
	if containsToken(err.Error()) {
		t.Fatalf("lỗi lộ token: %v", err)
	}
	if n := rows(t, e, "k"); n != 0 {
		t.Fatalf("gửi lỗi không được ghi alert_events: %d", n)
	}
	tg.set(http.StatusOK, `{"ok":true}`)
	if err := a.Notify(t.Context(), "k", observability.SeverityNormal, "x"); err != nil {
		t.Fatal(err)
	}
	if n := rows(t, e, "k"); n != 1 {
		t.Fatalf("gửi lại được phải ghi: %d", n)
	}
	if err := a.Notify(t.Context(), "k2", "critical", "x"); err == nil {
		t.Fatal("mức lạ phải lỗi")
	}
}

func containsToken(s string) bool { return strings.Contains(s, "123:thu") }
