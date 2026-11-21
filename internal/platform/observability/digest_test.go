package observability_test

import (
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/observability"
)

// Digest gọi hai lần trong một ngày chỉ một tin; tin có số cảnh báo 24 giờ theo key, số dead, in_doubt đang mở và
// tuổi bản sao lưu.
func TestDailyDigestOncePerDay(t *testing.T) {
	e := newEnv(t)
	tg, client := newTelegram(t)
	a := observability.NewTelegramAlerter(e.d, tgConfig(), client, quiet())
	e.alertSent(t, "queue-lag", time.Hour)
	e.alertSent(t, "queue-lag", 2*time.Hour)
	e.alertSent(t, "spool", 3*time.Hour)
	e.alertSent(t, "cu-qua", 25*time.Hour)
	now := time.Now()
	fin := now.Add(-time.Hour)
	e.job(t, "t.a", "dead", `{}`, now, &fin)
	e.job(t, "t.b", "dead", `{}`, now, &fin)
	e.job(t, "t.c", "in_doubt", `{}`, now, &fin)
	e.job(t, "t.d", "done", `{}`, now, &fin)
	e.exec(t, `INSERT INTO platform.backup_runs (kind, ok, finished_at) VALUES ('base', true, now() - interval '5 hours'),
		('base', false, now() - interval '1 hour')`)

	for range 2 {
		if err := observability.SendDailyDigest(t.Context(), e.d, a, now); err != nil {
			t.Fatalf("SendDailyDigest: %v", err)
		}
	}
	got := tg.calls()
	if len(got) != 1 {
		t.Fatalf("muốn 1 tin, có %d", len(got))
	}
	text := got[0].text
	for _, want := range []string{"queue-lag: 2", "spool: 1", "dead: 2", "in_doubt: 1", "sao lưu: 5 giờ"} {
		if !strings.Contains(text, want) {
			t.Fatalf("tin tổng kết thiếu %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "cu-qua") {
		t.Fatalf("cảnh báo quá 24 giờ không được tính:\n%s", text)
	}
	key := "digest:" + now.In(time.FixedZone("ICT", 7*3600)).Format("2006-01-02")
	if n := rows(t, e, key); n != 1 {
		t.Fatalf("alert_events %s = %d", key, n)
	}
	// Ngày hôm sau: tin mới.
	if err := observability.SendDailyDigest(t.Context(), e.d, a, now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n := len(tg.calls()); n != 2 {
		t.Fatalf("ngày mới phải có tin mới, tổng %d", n)
	}
}

// Chưa có bản sao lưu nào: tin ghi rõ, không lỗi.
func TestDailyDigestNoBackup(t *testing.T) {
	e := newEnv(t)
	f := &fakeAlerter{}
	if err := observability.SendDailyDigest(t.Context(), e.d, f, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 1 || !strings.Contains(f.sent[0].text, "sao lưu: chưa có") || f.sent[0].severity != "normal" {
		t.Fatalf("tin: %+v", f.sent)
	}
}

// Đồng hồ Go lệch ±5 phút so với PostgreSQL: tuổi bản sao lưu trong tin tổng kết tính theo đồng hồ PostgreSQL (bên ghi
// mốc). 4 giờ 33 phút (4,55 → 5) bắt đồng hồ Go chậm; 5 giờ 27 phút (5,45 → 5) bắt đồng hồ Go nhanh (Task 445).
func TestDailyDigestBackupClockSkew(t *testing.T) {
	for _, c := range []struct {
		skew time.Duration
		ago  string
	}{{-5 * time.Minute, "4 hours 33 minutes"}, {5 * time.Minute, "5 hours 27 minutes"}} {
		t.Run(c.skew.String(), func(t *testing.T) {
			e := newEnv(t)
			tg, client := newTelegram(t)
			a := observability.NewTelegramAlerter(e.d, tgConfig(), client, quiet())
			e.exec(t, "INSERT INTO platform.backup_runs (kind, ok, finished_at) VALUES ('base', true, now() - $1::interval)", c.ago)
			if err := observability.SendDailyDigest(t.Context(), e.d, a, time.Now().Add(c.skew)); err != nil {
				t.Fatalf("SendDailyDigest: %v", err)
			}
			got := tg.calls()
			if len(got) != 1 || !strings.Contains(got[0].text, "sao lưu: 5 giờ") {
				t.Fatalf("muốn một tin có 'sao lưu: 5 giờ', có %+v", got)
			}
		})
	}
}
