package e2e

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Database ngừng đúng lúc webhook đến (Review Focus 2): pool của ingest đi qua proxy có công tắc. Bật lỗi → 3 webhook
// đều 200, 3 tệp tạm, 0 dòng bản nhận. Tắt lỗi → trong 10 s: 0 tệp, 3 dòng, màn hình nhận đủ 3 selftest.pong, mỗi
// eventId đúng 1 lần.
func TestDatabaseOutageKeepsWebhooks(t *testing.T) {
	var proxy *switchProxy
	s := newStack(t, stackOptions{ingestURL: func(appURL string) string {
		p, u := startSwitchProxy(t, appURL)
		proxy = p
		return u
	}})
	c := s.openSSE()
	proxy.SetDown(true)
	ids := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	for i, id := range ids {
		if status := s.postPing(id); status != http.StatusOK {
			t.Fatalf("webhook %d khi database ngừng = %d, muốn 200", i+1, status)
		}
	}
	files, err := s.spool.List()
	if err != nil || len(files) != 3 {
		t.Fatalf("muốn 3 tệp tạm, nhận %d (%v)", len(files), err)
	}
	if n := s.count("SELECT count(*) FROM platform.webhook_deliveries"); n != 0 {
		t.Fatalf("database ngừng mà có %d bản nhận", n)
	}

	proxy.SetDown(false)
	start := time.Now()
	seen := map[string]int{}
	for len(seen) < len(ids) {
		id, err := nextPong(c, 10*time.Second-time.Since(start))
		if err != nil {
			t.Fatalf("sau 10 s mới nhận %v selftest.pong: %v", seen, err)
		}
		seen[id]++
	}
	// Chờ thêm để bắt báo hiệu lặp (nạp lại hai lần, xử lý hai lần).
	if id, err := nextPong(c, time.Second); err == nil {
		seen[id]++
	}
	for _, id := range ids {
		if seen[id] != 1 {
			t.Fatalf("eventId %s nhận %d lần, muốn 1 (tất cả: %v)", id, seen[id], seen)
		}
	}
	if len(seen) != len(ids) {
		t.Fatalf("nhận eventId lạ: %v", seen)
	}
	waitFor(t, 10*time.Second-time.Since(start), "0 tệp tạm", func() bool {
		f, err := s.spool.List()
		return err == nil && len(f) == 0
	})
	if n := s.count("SELECT count(*) FROM platform.webhook_deliveries"); n != 3 {
		t.Fatalf("muốn 3 bản nhận, nhận %d", n)
	}
	if n := s.count("SELECT count(*) FROM platform.jobs WHERE kind = 'inbound.selftest' AND status = 'done'"); n != 3 {
		t.Fatalf("muốn 3 việc inbound.selftest done, nhận %d", n)
	}
	t.Logf("database trở lại: đủ 3 báo hiệu sau %v", time.Since(start))
}
