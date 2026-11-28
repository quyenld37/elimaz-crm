package webhooks_test

import (
	"errors"
	"sync"
	"testing"

	"elimaz.com.vn/crm/internal/platform/selftest"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

var errNotLast = errors.New("chưa tới lượt cuối")

// Task 265 (M7-20): Inbound mang số lần chạy của việc vào và cờ lượt cuối theo Policy.MaxAttempts của InboundKind
// (lần đầu: 1, false; lần thứ MaxAttempts: true). Handler trả nil ở lượt cuối thì việc done, không dead.
func TestInboundLastAttempt(t *testing.T) {
	t.Parallel()
	e := newSplitEnv(t, 0)
	maxAttempts := webhooks.InboundKind(selftest.Key).Policy.MaxAttempts
	var (
		mu   sync.Mutex
		seen []webhooks.Inbound
	)
	e.rec.fail = func(in webhooks.Inbound) error {
		mu.Lock()
		seen = append(seen, in)
		mu.Unlock()
		if !in.LastAttempt {
			return errNotLast
		}
		return nil
	}
	e.deliver(t, packet(t, ping("last", "ka", at(0))))
	e.drain(t)
	if st := e.inboundStatus(t); st["pending"] != 1 || len(st) != 1 {
		t.Fatalf("sau lần đầu: %v, muốn 1 pending", st)
	}
	if _, err := e.Owner.Pool.Exec(t.Context(), "UPDATE platform.jobs SET attempts = $1, run_at = now() WHERE kind = $2",
		maxAttempts-1, webhooks.InboundKind(selftest.Key).Name); err != nil {
		t.Fatal(err)
	}
	e.drain(t)
	jobs := e.jobs(t, webhooks.InboundKind(selftest.Key).Name)
	if len(jobs) != 1 || jobs[0].status != "done" || jobs[0].attempts != maxAttempts {
		t.Fatalf("việc vào: %+v, muốn done ở lần %d", jobs, maxAttempts)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0].Attempts != 1 || seen[0].LastAttempt || seen[1].Attempts != maxAttempts || !seen[1].LastAttempt {
		t.Fatalf("các lần gọi: %+v", seen)
	}
}
