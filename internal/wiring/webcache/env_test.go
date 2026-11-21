package webcache_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/testkit"
)

// hangGuard là chốt chống treo khi chờ một sự kiện; không phải tiêu chí đúng/sai về thời gian.
const hangGuard = 30 * time.Second

// env: database thật đã migrate (chủ bảng, crm_app) và hub LISTEN thật đã nghe.
type env struct {
	staff *testkit.Staff
	hub   *realtime.PGHub
}

func newEnv(t *testing.T) *env {
	t.Helper()
	_, ownerURL, appURL := testkit.StartPostgres(t)
	e := &env{staff: &testkit.Staff{App: openDB(t, appURL), Owner: openDB(t, ownerURL)}}
	e.hub = realtime.NewPGHub(realtime.PGHubOptions{URL: appURL, Logger: quiet()})
	// Chờ system.resync của lần nghe đầu bằng một đăng ký dò: người đăng ký sau đó không nhận lượt resync này.
	first := make(chan struct{})
	var once sync.Once
	stopProbe := e.hub.SubscribeSystem("", func(s realtime.Signal) {
		if s.Topic == realtime.TopicSystemResync {
			once.Do(func() { close(first) })
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { e.hub.Run(ctx) })
	t.Cleanup(func() {
		cancel()
		wg.Wait()
	})
	select {
	case <-first:
	case <-time.After(hangGuard):
		t.Fatal("hub không nghe được")
	}
	stopProbe()
	return e
}

// openDB mở pool tới url, đóng khi test xong.
func openDB(t *testing.T, url string) *db.DB {
	t.Helper()
	d, err := db.Open(t.Context(), url, db.PoolOptions{MaxConns: 8})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Pool.Close)
	return d
}

// sink nhận báo hiệu hệ thống của hub qua kênh có đệm.
func (e *env) sink(t *testing.T, prefix string) chan realtime.Signal {
	t.Helper()
	ch := make(chan realtime.Signal, 256)
	cancel := e.hub.SubscribeSystem(prefix, func(s realtime.Signal) {
		select {
		case ch <- s:
		default:
			t.Errorf("hàng nhận đầy")
		}
	})
	t.Cleanup(cancel)
	return ch
}

func next(t *testing.T, ch chan realtime.Signal) realtime.Signal {
	t.Helper()
	select {
	case s := <-ch:
		return s
	case <-time.After(hangGuard):
		t.Fatal("không nhận báo hiệu")
		return realtime.Signal{}
	}
}
