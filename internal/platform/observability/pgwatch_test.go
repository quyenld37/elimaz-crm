package observability_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/observability"
)

// Dựng thật: một transaction mở (đồng hồ giả 6 phút sau cho phần so sánh), một phiên chờ khóa, 15 kết nối crm_app
// với ConnBudget = 20 → đúng ba key của PostgreSQL; database yên → không key nào trong ba.
func TestPGWatchLive(t *testing.T) {
	pgKeys := []string{"long-tx", "lock-wait", "pg-connections"}
	e := newEnv(t)
	fresh(t, e)
	o := observability.RuleOptions{ConnBudget: 20}
	keys, _ := check(t, e, o)
	for _, k := range pgKeys {
		wantKey(t, keys, k, false)
	}

	admin := connect(t, e.adminURL)
	if _, err := admin.Exec(t.Context(), "CREATE TABLE public.khoa_thu (id int PRIMARY KEY); INSERT INTO public.khoa_thu VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	holder := connect(t, e.adminURL)
	tx, err := holder.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if _, err := tx.Exec(t.Context(), "UPDATE public.khoa_thu SET id = 1 WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	waiter := connect(t, e.adminURL)
	waitDone := make(chan error, 1)
	go func() {
		_, err := waiter.Exec(context.Background(), "UPDATE public.khoa_thu SET id = 1 WHERE id = 1")
		waitDone <- err
	}()
	t.Cleanup(func() {
		_ = tx.Rollback(context.Background())
		if err := <-waitDone; err != nil {
			t.Errorf("phiên chờ khóa: %v", err)
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for e.count(t, "SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("phiên chờ khóa không xuất hiện")
		}
		time.Sleep(20 * time.Millisecond)
	}
	conns := make([]*pgx.Conn, 0, 15)
	for range 15 {
		conns = append(conns, connect(t, e.appURL))
	}
	_ = conns

	o.Now = time.Now().Add(6 * time.Minute)
	keys, f := check(t, e, o)
	for _, k := range pgKeys {
		wantKey(t, keys, k, true)
		if s, _ := f.find(k); s.severity != "normal" {
			t.Fatalf("%s mức %q", k, s.severity)
		}
	}
}
