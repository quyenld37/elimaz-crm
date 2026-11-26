package channel_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
)

// setBucket đặt thùng của tài khoản id: tokens, capacity, refill, mốc updated_at (bằng chủ bảng).
func (e *chEnv) setBucket(id uuid.UUID, tokens float64, capacity int, refill float64, at time.Time) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), `INSERT INTO channel.send_buckets (tenant_id, channel_account_id, tokens, capacity,
			refill_per_sec, updated_at) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (tenant_id, channel_account_id) DO UPDATE SET tokens = EXCLUDED.tokens, capacity = EXCLUDED.capacity,
			refill_per_sec = EXCLUDED.refill_per_sec, updated_at = EXCLUDED.updated_at`, e.tenant, id, tokens, capacity, refill, at)
		return err //nolint:wrapcheck // test
	})
}

// newBucketEnv: shop có một Page fakechat với thùng tokens/capacity/refill đặt lúc now (mốc cố định).
func newBucketEnv(t *testing.T, tokens float64, capacity int, refill float64) (*chEnv, uuid.UUID, time.Time) {
	t.Helper()
	e := newChEnv(t)
	id := e.staff.ChannelAccount(t, e.tenant, "fakechat", "p-1", "P")
	now := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	e.setBucket(id, tokens, capacity, refill, now)
	return e, id, now
}

func (e *chEnv) acquire(id uuid.UUID, now time.Time) (bool, time.Duration) {
	e.t.Helper()
	ok, wait, err := channel.AcquireSendToken(e.t.Context(), e.staff.App, e.tenant, id, now)
	if err != nil {
		e.t.Fatal(err)
	}
	return ok, wait
}

// capacity 5, nạp 1/s, đồng hồ đứng yên: 5 lần đầu được, lần 6 không, chờ ≈ 1 s; tiến 2 s → 2 lần được rồi hết.
func TestBuckets_RefillClock(t *testing.T) {
	t.Parallel()
	e, id, now := newBucketEnv(t, 5, 5, 1)
	for i := range 5 {
		if ok, _ := e.acquire(id, now); !ok {
			t.Fatalf("lần %d phải được", i+1)
		}
	}
	ok, wait := e.acquire(id, now)
	if ok || wait < 900*time.Millisecond || wait > 1100*time.Millisecond {
		t.Fatalf("lần 6 = %v, chờ %v; muốn false, [0,9 s; 1,1 s]", ok, wait)
	}
	later := now.Add(2 * time.Second)
	for i := range 2 {
		if ok, _ := e.acquire(id, later); !ok {
			t.Fatalf("sau 2 s lần %d phải được", i+1)
		}
	}
	if ok, _ := e.acquire(id, later); ok {
		t.Fatal("sau 2 s chỉ được 2 lần")
	}
	// Đồng hồ lùi không nạp thêm.
	if ok, _ := e.acquire(id, now); ok {
		t.Fatal("đồng hồ lùi nạp thêm token")
	}
	// Để lâu không vượt capacity.
	far := later.Add(time.Hour)
	n := 0
	for range 10 {
		if ok, _ := e.acquire(id, far); ok {
			n++
		}
	}
	if n != 5 {
		t.Fatalf("sau 1 giờ lấy được %d, muốn đúng capacity 5", n)
	}
}

// Lần đầu (chưa có thùng): thùng đầy 50. Tài khoản không có → ErrAccountNotFound.
func TestBuckets_FirstUseAndMissing(t *testing.T) {
	t.Parallel()
	e := newChEnv(t)
	id := e.staff.ChannelAccount(t, e.tenant, "fakechat", "p-1", "P")
	now := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	n := 0
	for range 51 {
		if ok, _ := e.acquire(id, now); ok {
			n++
		}
	}
	if n != 50 {
		t.Fatalf("thùng mới cho %d lần, muốn 50", n)
	}
	if _, _, err := channel.AcquireSendToken(t.Context(), e.staff.App, e.tenant, uuid.New(), now); !errors.Is(err, channel.ErrAccountNotFound) {
		t.Fatalf("không có tài khoản: %v", err)
	}
}

// 20 goroutine cùng lúc trên thùng còn 5 token → đúng 5 lần được.
func TestBuckets_Concurrent(t *testing.T) {
	t.Parallel()
	e, id, now := newBucketEnv(t, 5, 50, 20)
	var got atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Go(func() {
			ok, _, err := channel.AcquireSendToken(context.Background(), e.staff.App, e.tenant, id, now)
			if ok {
				got.Add(1)
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got.Load() != 5 {
		t.Fatalf("được %d lần, muốn đúng 5", got.Load())
	}
}

// blocked_until ở tương lai → không được, chờ tới mốc đó; qua mốc thì được.
func TestBuckets_Blocked(t *testing.T) {
	t.Parallel()
	e := newChEnv(t)
	id := e.staff.ChannelAccount(t, e.tenant, "fakechat", "p-1", "P")
	now := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	if err := e.in(t.Context(), func(tx pgx.Tx) error { return channel.NoteBlockedUntil(t.Context(), tx, id, now.Add(time.Minute)) }); err != nil {
		t.Fatal(err)
	}
	if ok, wait := e.acquire(id, now); ok || wait != time.Minute {
		t.Fatalf("đang chặn: %v, chờ %v", ok, wait)
	}
	if ok, _ := e.acquire(id, now.Add(2*time.Minute)); !ok {
		t.Fatal("qua mốc chặn phải được")
	}
}
