package realtime_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/testkit"
)

// terminateListener cắt kết nối LISTEN của hub bằng pg_terminate_backend (người quản trị) và trả số backend đã cắt.
func (e *rtEnv) terminateListener() int {
	e.t.Helper()
	ctx := e.t.Context()
	c, err := pgx.Connect(ctx, e.adminURL)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = c.Close(context.WithoutCancel(ctx)) }()
	var n int
	if err := c.QueryRow(ctx, `SELECT count(*) FROM (
		SELECT pg_terminate_backend(pid) FROM pg_stat_activity
		WHERE datname = current_database() AND usename = 'crm_app' AND query ILIKE '%listen rt%') x`).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// blockReconnect chặn crm_app nối mới vào database thử (ALTER ROLE … CONNECTION LIMIT 0 không áp cho kết nối đang có);
// trả hàm mở lại.
func (e *rtEnv) blockReconnect() (unblock func()) {
	e.t.Helper()
	ctx := e.t.Context()
	exec := func(q string) {
		c, err := pgx.Connect(context.WithoutCancel(ctx), e.adminURL)
		if err != nil {
			e.t.Fatal(err)
		}
		defer func() { _ = c.Close(context.WithoutCancel(ctx)) }()
		if _, err := c.Exec(context.WithoutCancel(ctx), q); err != nil {
			e.t.Fatal(err)
		}
	}
	exec("REVOKE CONNECT ON DATABASE " + e.dbName() + " FROM crm_app, PUBLIC")
	return func() { exec("GRANT CONNECT ON DATABASE " + e.dbName() + " TO crm_app") }
}

func (e *rtEnv) dbName() string {
	e.t.Helper()
	cfg, err := pgx.ParseConfig(e.adminURL)
	if err != nil {
		e.t.Fatal(err)
	}
	return pgx.Identifier{cfg.Database}.Sanitize()
}

func TestReconnectResyncsClients(t *testing.T) {
	e := newRTEnv(t)
	a := e.staff.Tenant(t, "A")
	c := e.openSub(a, "lan@elimaz.test")

	// Giữ hub ở trạng thái rớt một lúc: chặn nối mới rồi cắt kết nối LISTEN.
	unblock := e.blockReconnect()
	if n := e.terminateListener(); n != 1 {
		t.Fatalf("phải cắt đúng 1 kết nối LISTEN, cắt %d", n)
	}
	e.waitListening(false, 5*time.Second)
	// Trong lúc listening = false: không có ": ping" (và không có gì khác) qua vài nhịp tim. Bỏ khung đã nằm sẵn trong
	// luồng trước lúc rớt (tối đa một nhịp).
	time.Sleep(testHeartbeat + testBatch)
	drain(c)
	if ev, data, err := c.Next(4 * testHeartbeat); err == nil {
		t.Fatalf("listening = false mà vẫn nhận %q %s", ev, data)
	}
	unblock()

	// Trong 10 s: hub nối lại, client nhận thêm một resync.
	deadline := time.Now().Add(10 * time.Second)
	for {
		ev, _, err := c.Next(time.Until(deadline))
		if err != nil {
			t.Fatalf("không nhận resync sau khi nối lại: %v", err)
		}
		if ev == "resync" {
			break
		}
	}
	if l, _ := e.hub.Status(); !l {
		t.Fatal("đã resync mà Status chưa listening")
	}
	e.publish(sig(a, "t.after", "after"), false)
	ev, data, err := nextNonPing(c, time.Second)
	if err != nil || ev != "changed" || !strings.Contains(string(data), "t.after") {
		t.Fatalf("Publish sau khi nối lại phải tới: %q %s %v", ev, data, err)
	}
}

// drain bỏ mọi khung đang chờ sẵn.
func drain(c *testkit.SSEClient) {
	for {
		if _, _, err := c.Next(10 * time.Millisecond); err != nil {
			return
		}
	}
}

func TestManyClients(t *testing.T) {
	const n = 500
	e := newRTEnv(t)
	a := e.staff.Tenant(t, "A")
	tok := e.user(a, "lan@elimaz.test", map[string][]string{"all": {"realtime.subscribe"}})

	clients := make([]*testkit.SSEClient, n)
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Go(func() {
			c := testkit.OpenSSE(t, e.srv.URL, tok)
			if ev, _, err := c.Next(10 * time.Second); err != nil || ev != "resync" {
				errs <- fmt.Errorf("client %d: khung đầu %q %w", i, ev, err)
			}
			clients[i] = c
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if _, got := e.hub.Status(); got != n {
		t.Fatalf("clients = %d, mong %d", got, n)
	}

	e.publish(sig(a, "t.fanout", "1"), false)
	recv := make(chan error, n)
	for i, c := range clients {
		wg.Go(func() {
			ev, data, err := nextNonPing(c, 5*time.Second)
			if err != nil || ev != "changed" || !strings.Contains(string(data), "t.fanout") {
				recv <- fmt.Errorf("client %d: %q %s %w", i, ev, data, err)
			}
		})
	}
	wg.Wait()
	close(recv)
	for err := range recv {
		t.Error(err)
	}

	for _, c := range clients {
		c.Close()
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, got := e.hub.Status()
		if got == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("đóng hết mà clients = %d", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
