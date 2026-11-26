package inbound_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Thứ tự khóa của đường vào: 200 sự kiện (tin, echo, đã nhận, đã xem) trên 20 hội thoại cùng một Page, 4 worker chạy
// song song → không lần nào RetryTx gặp deadlock (40P01; worker chạy handler trong RetryTx, tự chạy lại nên việc không
// dead: đếm bằng móc OnRetry/OnExhausted của db.DB), pg_stat_database.deadlocks không tăng (bộ đếm này có thể ghi trễ,
// chỉ là kiểm thêm); không việc nào dead; 20 khách, 20 hội thoại; đếm tin đúng.
func TestInboundConcurrentNoDeadlock(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	deadlocks := func() int {
		return e.staff.Count(t, `SELECT deadlocks::int FROM pg_stat_database WHERE datname = current_database()`)
	}
	before := deadlocks()
	var mu sync.Mutex
	var deadlockRetries []string
	note := func(code string) {
		if code == "40P01" {
			mu.Lock()
			deadlockRetries = append(deadlockRetries, code)
			mu.Unlock()
		}
	}
	e.staff.App.OnRetry = func(code string, _ int) { note(code) }
	e.staff.App.OnExhausted = note
	base := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Hour)
	const users, perUser = 20, 10
	for i := range perUser {
		for u := range users {
			user := fmt.Sprintf("u-%02d", u)
			when := base.Add(time.Duration(i) * time.Second)
			typ, payload := concurrentEvent(i, user, when)
			e.enqueue(facebook.Key, typ, fmt.Sprintf("%s:%d", user, i), pageExt1, user, when, payload)
		}
	}
	var wg sync.WaitGroup
	for w := range 4 {
		worker := e.newWorker(fmt.Sprintf("w-%d", w))
		wg.Go(func() { e.runUntilIdle(worker) })
	}
	wg.Wait()
	e.drain()
	mu.Lock()
	if len(deadlockRetries) != 0 {
		t.Errorf("RetryTx gặp deadlock %d lần", len(deadlockRetries))
	}
	mu.Unlock()
	e.noDeadJobs()
	if after := deadlocks(); after != before {
		t.Errorf("deadlock: %d → %d", before, after)
	}
	if n := e.staff.Count(t, `SELECT count(*) FROM platform.jobs WHERE kind = 'inbound.facebook' AND status <> 'done'`); n != 0 {
		t.Fatalf("%d việc vào chưa xong", n)
	}
	checks := []struct {
		q    string
		want int
	}{
		{`SELECT count(*) FROM customer.customers`, users},
		{`SELECT count(*) FROM conversation.conversations`, users},
		{`SELECT count(*) FROM conversation.messages WHERE direction = 'in'`, users * 6},
		{`SELECT count(*) FROM conversation.messages WHERE direction = 'out'`, users * 2},
		{`SELECT coalesce(sum(unread_count), 0)::int FROM conversation.conversations`, users * 6},
	}
	for _, c := range checks {
		if got := e.staff.CountIn(t, e.tenant, c.q); got != c.want {
			t.Errorf("%s = %d, mong %d", c.q, got, c.want)
		}
	}
}

// concurrentEvent là sự kiện thứ i của PSID user: 3/5 tin khách, 1/5 echo Business Suite, 1/5 đã xem.
func concurrentEvent(i int, user string, when time.Time) (string, any) {
	switch i % 5 {
	case 0, 1, 2:
		return channel.EventMessageReceived, channel.MessagePayload{
			PageID: pageExt1, UserID: user, MID: fmt.Sprintf("in-%s-%d", user, i), Text: "hỏi", SentAt: when,
		}
	case 3:
		return channel.EventMessageEcho, channel.MessagePayload{
			PageID: pageExt1, UserID: user, MID: fmt.Sprintf("out-%s-%d", user, i), Text: "đáp", IsEcho: true,
			EchoAppID: businessSuiteAppID, SentAt: when,
		}
	default:
		return channel.EventMessageRead, channel.WatermarkPayload{PageID: pageExt1, UserID: user, Watermark: when}
	}
}

// runUntilIdle chạy worker trong goroutine của test tới khi không còn việc vào pending/running, tối đa 200 lượt. Không
// gọi helper của testkit (t.Fatalf chỉ được gọi từ goroutine của test): lỗi báo bằng t.Errorf. Lượt nhận 0 mà còn việc
// pending thì đưa run_at của chúng lùi 1 giây (bù đồng hồ PostgreSQL lùi, như testkit.MakeDueSoon).
func (e *env) runUntilIdle(w *queue.Worker) {
	ctx := e.t.Context()
	for range 200 {
		n, err := w.RunOnce(ctx, 5)
		if err != nil {
			e.t.Errorf("RunOnce: %v", err)
			return
		}
		if n > 0 {
			continue
		}
		var left int
		if err := e.staff.Owner.Pool.QueryRow(ctx, `SELECT count(*) FROM platform.jobs WHERE kind = 'inbound.facebook'
			AND status IN ('pending', 'running')`).Scan(&left); err != nil {
			e.t.Errorf("đếm việc còn lại: %v", err)
			return
		}
		if left == 0 {
			return
		}
		if _, err := e.staff.Owner.Pool.Exec(ctx, `UPDATE platform.jobs SET run_at = now() - interval '1 second'
			WHERE kind = 'inbound.facebook' AND status = 'pending' AND run_at > now() - interval '1 second'`); err != nil {
			e.t.Errorf("đưa việc về hạn: %v", err)
			return
		}
	}
}
