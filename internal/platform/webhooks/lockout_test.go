package webhooks_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/webhooks"
)

func TestLockoutCountsFailuresInWindow(t *testing.T) {
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	l := webhooks.NewLockout(webhooks.LockoutOptions{
		Threshold: 3, Window: time.Minute, Block: 10 * time.Minute, Now: func() time.Time { return now },
	})
	l.NoteFailure("a")
	l.NoteFailure("a")
	if l.Blocked("a") {
		t.Fatal("2 lần sai đã bị chặn")
	}
	now = now.Add(61 * time.Second) // hai lần đầu ra khỏi cửa sổ
	l.NoteFailure("a")
	if l.Blocked("a") {
		t.Fatal("lần sai ngoài cửa sổ vẫn được đếm")
	}
	l.NoteFailure("a")
	l.NoteFailure("a")
	if !l.Blocked("a") || l.Blocked("b") {
		t.Fatal("3 lần sai trong cửa sổ phải chặn a, không chặn b")
	}
	now = now.Add(10*time.Minute - time.Second)
	if !l.Blocked("a") {
		t.Fatal("hết chặn sớm")
	}
	now = now.Add(2 * time.Second)
	if l.Blocked("a") {
		t.Fatal("hết 10 phút vẫn chặn")
	}
	l.NoteFailure("a")
	if l.Blocked("a") {
		t.Fatal("sau khi hết chặn, một lần sai đã chặn lại (đếm cũ chưa xóa)")
	}
}

func TestLockoutDefaults(t *testing.T) {
	now := time.Now()
	l := webhooks.NewLockout(webhooks.LockoutOptions{Now: func() time.Time { return now }})
	for range 19 {
		l.NoteFailure("a")
	}
	if l.Blocked("a") {
		t.Fatal("19 lần sai đã chặn; mặc định 20")
	}
	l.NoteFailure("a")
	if !l.Blocked("a") {
		t.Fatal("20 lần sai chưa chặn")
	}
	now = now.Add(599 * time.Second)
	if !l.Blocked("a") {
		t.Fatal("mặc định chặn 600 s")
	}
}

func TestLockoutForgetsManyAddresses(t *testing.T) {
	now := time.Now()
	l := webhooks.NewLockout(webhooks.LockoutOptions{Threshold: 2, Now: func() time.Time { return now }})
	for i := range 50_000 {
		l.NoteFailure(fmt.Sprint(i))
	}
	now = now.Add(2 * time.Minute)
	l.NoteFailure("x")
	if got := webhooks.Tracked(l); got > 10_001 {
		t.Fatalf("còn giữ %d địa chỉ sau khi cửa sổ đã qua", got)
	}
}

func TestLockoutOverHTTP(t *testing.T) {
	d := openDB(t)
	r := newRig(t, d, func(o *webhooks.IngestOptions) {
		o.Lockout = webhooks.NewLockout(webhooks.LockoutOptions{Threshold: 3, Window: time.Minute, Block: 200 * time.Millisecond})
	})
	const bad, other = "198.51.100.7", "198.51.100.8"
	for i := range 3 {
		if got := r.post(t, "selftest", pingBody(fmt.Sprint("bad", i)), bad, "sha256=00", nil); got.status != http.StatusUnauthorized {
			t.Fatalf("lần sai %d = %d", i, got.status)
		}
	}
	if got := r.post(t, "selftest", pingBody("good"), bad, "", nil); got.status != http.StatusTooManyRequests {
		t.Fatalf("lần 4 (chữ ký đúng) từ địa chỉ bị chặn = %d, muốn 429", got.status)
	}
	if got := r.post(t, "selftest", pingBody("o"), other, "", nil); got.status != http.StatusOK {
		t.Fatalf("địa chỉ khác = %d", got.status)
	}
	for i := range 100 {
		if got := r.post(t, "selftest", pingBody(fmt.Sprint("ok", i)), other, "", nil); got.status != http.StatusOK {
			t.Fatalf("tin đúng thứ %d = %d", i, got.status)
		}
	}
	time.Sleep(250 * time.Millisecond)
	if got := r.post(t, "selftest", pingBody("later"), bad, "", nil); got.status != http.StatusOK {
		t.Fatalf("hết thời gian chặn = %d, muốn 200", got.status)
	}
	rows := r.reg.Drain()
	if n := counter(rows, "hooks_bad_signature", "selftest"); n != 3 {
		t.Fatalf("hooks_bad_signature = %d", n)
	}
	if n := counter(rows, "hooks_blocked", ""); n != 1 {
		t.Fatalf("hooks_blocked = %d", n)
	}
}

func TestClientIP(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/hooks/x", nil) //nolint:noctx // chỉ dựng yêu cầu
	r.RemoteAddr = "203.0.113.9:5555"
	if got := webhooks.ClientIP(r); got != "203.0.113.9" {
		t.Fatalf("không có cf-connecting-ip: %q", got)
	}
	r.Header.Set("Cf-Connecting-Ip", " 2001:db8::1 ")
	if got := webhooks.ClientIP(r); got != "2001:db8::1" {
		t.Fatalf("có cf-connecting-ip: %q", got)
	}
}
