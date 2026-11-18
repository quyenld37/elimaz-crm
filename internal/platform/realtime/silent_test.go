package realtime_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/testkit"
)

// blackhole là proxy TCP tới PostgreSQL; frozen = true thì nuốt mọi byte theo cả hai chiều mà không đóng kết nối
// (giống mạng mất mà không có RST).
type blackhole struct {
	ln     net.Listener
	target string
	frozen atomic.Bool
	wg     sync.WaitGroup
}

func newBlackhole(t *testing.T, target string) *blackhole {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	b := &blackhole{ln: ln, target: target}
	var mu sync.Mutex
	var conns []net.Conn
	b.wg.Go(func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			up, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", target)
			if err != nil {
				_ = c.Close()
				continue
			}
			mu.Lock()
			conns = append(conns, c, up)
			mu.Unlock()
			b.wg.Go(func() { b.pipe(up, c) })
			b.wg.Go(func() { b.pipe(c, up) })
		}
	})
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		for _, c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
		b.wg.Wait()
	})
	return b
}

// pipe chép src → dst; lúc frozen thì đọc rồi bỏ.
func (b *blackhole) pipe(dst io.Writer, src io.Reader) {
	buf := make([]byte, 32<<10)
	for {
		n, err := src.Read(buf)
		if n > 0 && !b.frozen.Load() {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func TestSilentDeadConnectionDetected(t *testing.T) {
	_, _, appURL := testkit.StartPostgres(t)
	u, err := url.Parse(appURL)
	if err != nil {
		t.Fatal(err)
	}
	bh := newBlackhole(t, u.Host)
	u.Host = bh.ln.Addr().String()
	hub := realtime.NewPGHub(realtime.PGHubOptions{
		URL: u.String(), CheckEvery: 200 * time.Millisecond, PingTimeout: 500 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { hub.Run(ctx) })
	t.Cleanup(func() {
		cancel()
		wg.Wait()
	})
	waitStatus := func(want bool, d time.Duration) error {
		deadline := time.Now().Add(d)
		for time.Now().Before(deadline) {
			if l, _ := hub.Status(); l == want {
				return nil
			}
			time.Sleep(10 * time.Millisecond)
		}
		return errors.New("hết hạn")
	}
	if err := waitStatus(true, 10*time.Second); err != nil {
		t.Fatal("hub không nghe được qua proxy")
	}
	bh.frozen.Store(true)
	if err := waitStatus(false, 3*time.Second); err != nil {
		t.Fatal("kết nối chết im lặng mà hub vẫn báo listening sau 3 s")
	}
}
